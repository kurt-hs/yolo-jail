package cli

import (
	"path/filepath"
	goruntime "runtime"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// TestHostOrdinarySelectionJudgesTheJailsPlatform pins that the host's ordinary preparation
// judges a vendor's published platforms against the JAIL's platform, never the host's
// GOOS/GOARCH. A macOS host preparing a Linux jail must still prepare a Linux-only vendor's
// program; a darwin judge (what the host's own GOOS used to give there) must not.
func TestHostOrdinarySelectionJudgesTheJailsPlatform(t *testing.T) {
	// The arch this machine is NOT, so the host's own GOARCH cannot stand in for the jail's.
	jailArch := "arm64"
	if goruntime.GOARCH == "arm64" {
		jailArch = "amd64"
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "pack.json"),
		`{"name":"linuxonly","contributes":[{"kind":"program","bin":"linux-only-tool","via":"npm","package":"linux-only-pkg","platforms":["linux/`+jailArch+`"]}]}`)
	p, problems := packload.LoadDir(dir, "linuxonly")
	if len(problems) != 0 || p == nil {
		t.Fatalf("fixture pack: %v", problems)
	}
	env := entrypoint.NewEnv(map[string]string{})
	for _, tc := range []struct {
		platform string
		want     int
	}{
		{"linux/" + jailArch, 1},
		{"darwin/arm64", 0},
		{"linux/" + goruntime.GOARCH, 0},
	} {
		vector, err := selectedOrdinaryInputs(env, []*packload.Pack{p}, entrypoint.ProgramInstallOnly, tc.platform)
		if err != nil {
			t.Fatal(err)
		}
		if len(vector) != tc.want {
			t.Errorf("jail platform %s selected %d programs, want %d: %+v", tc.platform, len(vector), tc.want, vector)
		}
	}
	if _, err := selectedOrdinaryInputs(env, []*packload.Pack{p}, entrypoint.ProgramInstallOnly, ""); err == nil {
		t.Error("an empty jail platform must be refused, not judged as the host's")
	}
}
