package packmigrate

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
)

// piShapedLegacy is a program whose legacy install_hints name one manager: every other manager
// has NO recipe, which check-deps and the provisioner answer as such.
const piShapedLegacy = `{
  "name": "fixture-pi",
  "contributes": [
    {"kind": "program", "bin": "pi", "install_hints": {"brew": "pi-coding-agent"}}
  ]
}`

// TestProofReportsAnInventedInstallHint is the proof's own regression: a grouped manifest that
// leaves the unnamed managers to the reader's default reads back with `apt install pi` and four
// more recipes the legacy manifest never had. The equality proof must call that a difference,
// not normalize it away.
func TestProofReportsAnInventedInstallHint(t *testing.T) {
	old, problems := packdecl.Decode([]byte(piShapedLegacy))
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	inflated, problems := packdecl.DecodeTOML([]byte("name = \"fixture-pi\"\n\n[[program]]\nbin = \"pi\"\ninstall = { brew = \"pi-coding-agent\" }\n"))
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	if got := inflated.Contributes[0].InstallHints["apt"]; got != "pi" {
		t.Fatalf("fixture premise: the reader's default should give apt the bin, got %q", got)
	}
	diffs := ManifestDifferences(old, inflated)
	if len(diffs) == 0 || !strings.Contains(strings.Join(diffs, "\n"), "install_hints") {
		t.Fatalf("the proof called an invented apt/dnf/pacman/nix hint identical: %v", diffs)
	}
}

// TestMigrateWritesNoHintForUnnamedManagers pins the migrator's half: the written pack.toml
// says `default = false` for the managers the legacy hints do not name, reads back with the
// one legacy hint and no other, and a program with no hints at all becomes `install = false`.
func TestMigrateWritesNoHintForUnnamedManagers(t *testing.T) {
	for _, tc := range []struct {
		name, manifest, want string
		hints                map[string]string
	}{
		{"some managers", piShapedLegacy, "install = { default = false, brew = \"pi-coding-agent\" }", map[string]string{"brew": "pi-coding-agent"}},
		{"no manager", `{"name": "fixture-none", "contributes": [{"kind": "program", "bin": "tool"}]}`, "install = false", nil},
	} {
		for _, keep := range []bool{true, false} {
			t.Run(tc.name, func(t *testing.T) {
				dir := writePack(t, tc.manifest, nil)
				m, err := Plan(dir, Options{KeepInstallHints: keep})
				if err != nil {
					t.Fatalf("Plan: %v", err)
				}
				if !strings.Contains(string(m.Manifest), tc.want+"\n") {
					t.Fatalf("keep=%t: want %q in:\n%s", keep, tc.want, m.Manifest)
				}
				got, problems := packdecl.DecodeTOML(m.Manifest)
				if len(problems) != 0 {
					t.Fatal(problems)
				}
				hints := got.Contributes[0].InstallHints
				if len(hints) != len(tc.hints) {
					t.Fatalf("keep=%t: hints read back %v, want %v", keep, hints, tc.hints)
				}
				for manager, pkg := range tc.hints {
					if hints[manager] != pkg {
						t.Fatalf("keep=%t: hints read back %v, want %v", keep, hints, tc.hints)
					}
				}
			})
		}
	}
}

// optionNullLegacy declares two provider options with no default (JSON null) beside one with
// an empty-string default.
const optionNullLegacy = `{
  "name": "fixture-provider",
  "contributes": [
    {"kind": "provider", "name": "fixture-provider", "api_key_env_name": "FIXTURE_KEY",
     "endpoints": {"openai": {"base_url": "https://example.invalid/v1"}},
     "options": {"model": null, "pin_model": null, "thinking": ""}}
  ]
}`

// TestMigrateSpellsAnOptionWithoutDefaultAsFalse pins the option null: migrate writes `false`,
// and the reader turns it back into a declared option with no default — not an undeclared one
// and not an empty-string default.
func TestMigrateSpellsAnOptionWithoutDefaultAsFalse(t *testing.T) {
	dir := writePack(t, optionNullLegacy, nil)
	m, err := Plan(dir, Options{KeepInstallHints: true})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if !strings.Contains(string(m.Manifest), "model = false") {
		t.Fatalf("want model = false in:\n%s", m.Manifest)
	}
	got, problems := packdecl.DecodeTOML(m.Manifest)
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	options := got.Contributes[0].Options
	for _, key := range []string{"model", "pin_model"} {
		option, declared := options[key]
		if !declared || option.Defaulted {
			t.Errorf("options.%s read back as %+v (declared %t), want declared with no default", key, option, declared)
		}
	}
	if thinking := options["thinking"]; !thinking.Defaulted || thinking.Value != "" {
		t.Errorf("options.thinking read back as %+v, want the empty-string default", thinking)
	}
	if _, problems := packdecl.DecodeTOML([]byte("name = \"x\"\n\n[providers.x]\napi_key_env_name = \"X\"\nendpoints.openai.base_url = \"https://example.invalid/v1\"\noptions.model = true\n")); len(problems) == 0 {
		t.Error("options.model = true was accepted; only a string or false is an option value")
	}
}

// managedNullLegacy deletes a key from the agent's own config through a null in the managed
// merge patch, beside a key it sets.
const managedNullLegacy = `{
  "name": "fixture-surface",
  "contributes": [
    {"kind": "config", "config": [
      {"agent": "fixture", "name": "config", "path": "~/.fixture/config.toml",
       "managed": {"projects": {"a/b": {"trust": "yes"}}, "tui": {"nux": null}, "a/b~c": null}}
    ]}
  ]
}`

// TestMigrateMovesAManagedDeletionIntoUnset pins the managed null: migrate writes the deleted
// keys as `unset` JSON Pointers and drops the table left empty, and the reader puts the nulls
// back exactly where the legacy patch had them.
func TestMigrateMovesAManagedDeletionIntoUnset(t *testing.T) {
	dir := writePack(t, managedNullLegacy, nil)
	m, err := Plan(dir, Options{KeepInstallHints: true})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	text := string(m.Manifest)
	if !strings.Contains(text, `unset = ["/tui/nux", "/a~1b~0c"]`) || strings.Contains(text, "tui = {}") {
		t.Fatalf("want the two deletions as pointers and no empty tui table in:\n%s", text)
	}
	got, problems := packdecl.DecodeTOML(m.Manifest)
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	var rows []struct {
		Managed map[string]any `json:"managed"`
	}
	if err := json.Unmarshal(got.Contributes[0].Raw, &rows); err != nil || len(rows) != 1 {
		t.Fatalf("surface rows %s: %v", got.Contributes[0].Raw, err)
	}
	managed := rows[0].Managed
	tui, _ := managed["tui"].(map[string]any)
	if value, present := tui["nux"]; !present || value != nil {
		t.Errorf("managed.tui = %v, want nux: null", managed["tui"])
	}
	if value, present := managed["a/b~c"]; !present || value != nil {
		t.Errorf("managed = %v, want \"a/b~c\": null", managed)
	}
	for _, bad := range []string{`unset = "/tui/nux"`, `unset = ["tui.nux"]`, `unset = ["/projects/a~1b/trust"]`} {
		toml := strings.Replace(text, `unset = ["/tui/nux", "/a~1b~0c"]`, bad, 1)
		if _, problems := packdecl.DecodeTOML([]byte(toml)); len(problems) == 0 {
			t.Errorf("%s was accepted", bad)
		}
	}
}
