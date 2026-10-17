package capture

// installerstatus_test.go pins Options.StatusFile: the driver records the installer's own exit
// status the moment it exits, so the build's host tells a build line that failed, and its real
// status, from a driver that failed after a build line that succeeded. Before, both read as the
// driver's exit, 1, and as "the build line ran and failed".

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTheDriverRecordsTheInstallersOwnExitStatus(t *testing.T) {
	for _, tc := range []struct {
		name   string
		script func(tree string) string
		want   int
	}{
		{"a build line that exited 3", func(string) string { return "exit 3\n" }, 3},
		{"a build line a signal ended", func(string) string { return "kill -KILL $$\n" }, 137},
		// The line succeeded; the driver's move of its output then failed, the tree it moves into
		// being a file.
		{"a driver that failed after the build line", func(tree string) string {
			return "mkdir -p \"$HOME/.local/bin\" && echo x > \"$HOME/.local/bin/tool\" && rm -rf '" + tree + "' && printf x > '" + tree + "'\nexit 0\n"
		}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			fixtureHome(t, home)
			out := filepath.Join(t.TempDir(), "out")
			status := filepath.Join(t.TempDir(), InstallerStatusLeaf)
			_, err := Run(Options{Home: home, Out: out, Command: writeInstaller(t, tc.script(TreeDir(out))),
				StatusFile: status})
			if err == nil {
				t.Fatal("the capture succeeded")
			}
			got, ok := ReadInstallerStatus(status)
			if !ok || got != tc.want {
				t.Errorf("recorded status = %d (recorded %v), want %d; run: %v", got, ok, tc.want, err)
			}
		})
	}
}

// No record is written when the driver failed before the installer ran.
func TestTheDriverRecordsNoStatusUnlessTheInstallerRan(t *testing.T) {
	status := filepath.Join(t.TempDir(), InstallerStatusLeaf)
	if _, err := Run(Options{Home: t.TempDir(), Out: "relative/out", Command: []string{"/bin/true"},
		StatusFile: status}); err == nil {
		t.Fatal("a relative out was accepted")
	}
	if _, err := os.Stat(status); err == nil {
		t.Error("a driver that never ran its installer recorded a status")
	}
}
