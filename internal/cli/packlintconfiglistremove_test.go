package cli

// packlintconfiglistremove_test.go pins `yolo pack lint` on a config-list's `remove` verb and
// its `profile` gate. Before `remove` existed the strict decoder refused the manifest with
// `json: unknown field "remove"`, and `profile` on a config-list was refused by name.

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// A remove-only list gated on a profile, and an add-and-remove list, lint clean, and the
// footprint line says what each deletes, what it appends and the gate.
func TestPackLintAcceptsConfigListRemove(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "local")
	writeFile(t, filepath.Join(dir, "pack.json"), `{"name":"local","contributes":[`+
		`{"kind":"config-list","surface":"pi/settings","path":"/packages",`+
		`"remove":["B"],"profile":"lean"},`+
		`{"kind":"config-list","surface":"pi/settings","path":"/extensions",`+
		`"add":["D"],"remove":["C"]}]}`)

	var out, errw bytes.Buffer
	if rc := packMain([]string{"lint", dir}, &out, &errw, false); rc != 0 {
		t.Fatalf("lint refused a config-list remove (rc=%d):\n%s%s", rc, out.String(), errw.String())
	}
	got := out.String()
	for _, want := range []string{
		`removes 1 entry: "B"`,
		`when profile "lean" is active`,
		`removes 1 entry: "C" wherever the layers below hold them, then appends 1 entry: "D"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("lint output does not say %q:\n%s", want, got)
		}
	}
}

// A config-list with neither verb is refused, naming both.
func TestPackLintRefusesConfigListWithNeitherVerb(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "local")
	writeFile(t, filepath.Join(dir, "pack.json"), `{"name":"local","contributes":[`+
		`{"kind":"config-list","surface":"pi/settings","path":"/packages"}]}`)

	var out, errw bytes.Buffer
	rc := packMain([]string{"lint", dir}, &out, &errw, false)
	if all := out.String() + errw.String(); rc == 0 || !strings.Contains(all, `needs "add" or "remove"`) {
		t.Fatalf("lint did not refuse a config-list with neither verb (rc=%d):\n%s", rc, all)
	}
}
