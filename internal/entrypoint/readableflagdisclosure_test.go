package entrypoint

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/packoverlay"
	"github.com/mschulkind-oss/yolo-jail/internal/render"
)

func TestReadablePostureAppendStaysDeduplicatedAcrossActualRMWRenders(t *testing.T) {
	load := func(name, filename, body string) *packload.Pack {
		t.Helper()
		root := filepath.Join(t.TempDir(), name)
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, filename), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		pack, problems := packload.LoadDir(root, name)
		if len(problems) != 0 {
			t.Fatalf("LoadDir(%s): %v", name, problems)
		}
		return pack
	}
	owner := load("owner", packdecl.ManifestName, `{"contributes":[{"kind":"config","config":[{"agent":"claude","name":"settings","codec":"json","path":"~/.claude/settings.json","mode":"rmw","defaults":{"arr":["base"]},"managed":{}}]}]}`)
	foreign := load("foreign", packdecl.ManifestTOMLName, `[[autonomy]]
guarded = { config = [{ agent = "claude", name = "settings", codec = "json", path = "~/.claude/settings.json", managed = { arr = ["foreign", "base"] } }] }
`)
	packs := []*packload.Pack{owner, foreign}
	overlays := packoverlay.Collect(packs, false, nil)
	if len(overlays.Problems) != 0 || len(overlays.Orphans) != 0 {
		t.Fatalf("Collect problems=%v orphans=%+v", overlays.Problems, overlays.Orphans)
	}
	home := t.TempDir()
	path := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"arr":["base"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		results, err := RenderHostPack(owner, home, render.OwnershipOwn, false, overlays, nil, "claude/settings")
		if err != nil {
			t.Fatalf("RenderHostPack #%d: %v", i+1, err)
		}
		if len(results) != 1 || strings.HasPrefix(results[0].Action, "refused") {
			t.Fatalf("RenderHostPack #%d results = %+v", i+1, results)
		}
		var document map[string]any
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &document); err != nil {
			t.Fatalf("decode RMW output: %v", err)
		}
		if got, want := document["arr"], []any{"base", "foreign"}; !reflect.DeepEqual(got, want) {
			t.Errorf("RMW #%d array = %#v, want one append/dedup result %#v", i+1, got, want)
		}
	}
}

func TestReadableShellDisclosureAttributesActualContributingRows(t *testing.T) {
	root := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, packdecl.ManifestTOMLName), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("permission", `[[program]]
bin = "agy"
via = "npm"
package = "@acme/agy"

[[autonomy]]
autonomous = { launch = [{ bin = "agy", flags = ["--dangerously-skip-permissions"] }] }
`)
	write("model", `[[autonomy]]
autonomous = { launch = [{ bin = "agy", flags = ["--model", "gemini-3.8-flash-medium"] }] }
`)
	e := NewEnv(map[string]string{
		"JAIL_HOME":      t.TempDir(),
		"YOLO_PACK_ROOT": root,
	})
	var out strings.Builder
	e.Stderr = &out
	if err := GenerateBashrc(e); err != nil {
		t.Fatalf("GenerateBashrc: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"added by pack ",
		filepath.Join(root, "permission", packdecl.ManifestTOMLName),
		"autonomy[0].autonomous.launch[0]",
		"--dangerously-skip-permissions",
		filepath.Join(root, "model", packdecl.ManifestTOMLName),
		"--model gemini-3.8-flash-medium",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("shell disclosure missing %q; got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "(added by )") {
		t.Errorf("plural attribution collapsed into an empty singular pack: %s", got)
	}
	bashrc := readFileString(t, e.BashrcPath())
	for _, flag := range []string{"--dangerously-skip-permissions", "--model", "gemini-3.8-flash-medium"} {
		if !strings.Contains(bashrc, flag) {
			t.Errorf("alias is missing %q: %s", flag, bashrc)
		}
	}
}
