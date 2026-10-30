package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
)

// `yolo pack init` writes a pack.toml the strict TOML reader accepts, names the pack by its
// directory, and the scaffold lints clean with it present. It drives packMain, the entry the CLI
// uses, so dropping the file from the scaffold fails this.
func TestPackInitWritesStrictTOML(t *testing.T) {
	dir := filepath.Join(t.TempDir(), `odd"name`)
	var out, errw bytes.Buffer
	if rc := packMain([]string{"init", dir}, &out, &errw, false); rc != 0 {
		t.Fatalf("init rc=%d: %s", rc, errw.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "pack.json")); err == nil {
		t.Error("init wrote a pack.json; the scaffold is TOML")
	}
	data, err := os.ReadFile(filepath.Join(dir, "pack.toml"))
	if err != nil {
		t.Fatalf("init wrote no pack.toml: %v", err)
	}
	m, problems := packdecl.DecodeTOML(data)
	if len(problems) > 0 || m == nil {
		t.Fatalf("strict DecodeTOML refused the scaffold: %v\n%s", problems, data)
	}
	if m.Name != `odd"name` || strings.TrimSpace(m.Description) == "" {
		t.Errorf("manifest name/description = %q / %q", m.Name, m.Description)
	}
	if !strings.Contains(out.String(), "create pack.toml") {
		t.Errorf("init did not report the file it created:\n%s", out.String())
	}
	if rc, report := lintReport(t, dir); rc != 0 {
		t.Errorf("the scaffold does not lint clean (rc=%d):\n%s", rc, report)
	}
}

// A pack with both manifests loads no pack; lint reports the refusal and exits 1 rather than
// dereferencing the pack it did not get (it panicked before the scaffold wrote a pack.toml).
func TestPackLintReportsTwoManifestsInsteadOfPanicking(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "both")
	var out, errw bytes.Buffer
	if rc := packMain([]string{"init", dir}, &out, &errw, false); rc != 0 {
		t.Fatalf("init rc=%d: %s", rc, errw.String())
	}
	writeFile(t, filepath.Join(dir, "pack.json"), `{"name":"both"}`)
	rc, report := lintReport(t, dir)
	if rc == 0 || !strings.Contains(report, "both pack.toml and pack.json are present") {
		t.Fatalf("lint rc=%d, report:\n%s", rc, report)
	}
}

// packInitLegacyManifest runs `pack init` and then removes the pack.toml it wrote, for the many
// tests that go on to write a pack.json of their own: a pack may carry one manifest, and both
// present is refused. Those tests exercise the legacy reader, which stays for the window.
func packInitLegacyManifest(dir string, out, errw *bytes.Buffer) int {
	rc := packMain([]string{"init", dir}, out, errw, false)
	_ = os.Remove(filepath.Join(dir, "pack.toml"))
	return rc
}
