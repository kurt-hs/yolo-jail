package cli

// applyhostprograms_test.go pins `yolo host apply`'s refusal of one program two selected packs
// carry a recipe for (docs/design/provisioner-sets.md PS-D18). AgentNameCollisions skips a
// clash made only of `program` claims, so this block is the only thing that stops host apply
// writing such a set into a real home: delete it and the run proceeds.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyHostRefusesTwoProvidersOfOneProgram(t *testing.T) {
	home := t.TempDir()
	recipe := `{"kind":"program","bin":"xtool","via":"npm","package":"xtool-pkg"}`
	var entries []string
	for _, name := range []string{"provb", "provc"} {
		dir := filepath.Join(t.TempDir(), name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		manifest := `{"name":"` + name + `","contributes":[` + recipe + `]}`
		if err := os.WriteFile(filepath.Join(dir, "pack.json"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, `{"source":"file://`+dir+`","name":"`+name+`"}`)
	}
	cfgDir := filepath.Join(home, ".config", "yolo-jail")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := `{"packs":[` + strings.Join(entries, ",") + `]}`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.jsonc"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	var out, errw bytes.Buffer
	rc := applyHost(&out, &errw, false, false, nil)
	report := out.String() + errw.String()
	if rc != 1 {
		t.Fatalf("host apply rc = %d, want 1: two providers of one program must refuse\n%s", rc, report)
	}
	for _, want := range []string{"xtool provided by provb, provc", "more than one provider",
		"drop all but one of provb, provc from `packs`", "Nothing was written"} {
		if !strings.Contains(report, want) {
			t.Errorf("missing %q:\n%s", want, report)
		}
	}
}
