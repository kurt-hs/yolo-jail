package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// `yolo init` writes a comment-only pointer file now (TestInitWritesPointerAndInfoExcludeOnly),
// so the claims about blocked tools and runtimes it once carried survive only in the user-scope
// scaffold below, which keeps its own copy of the runtime comment.

// initUserConfigFile runs `yolo init-user-config` against a throwaway HOME and returns the
// file it wrote — the user-scope sibling of the workspace template, carrying its own copy of
// the runtime comment. It goes through the command registry rather than calling
// InitUserConfig, so unwiring the subcommand fails this as surely as a stale word does.
func initUserConfigFile(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	cmd, ok := registry["init-user-config"]
	if !ok {
		t.Fatal("`yolo init-user-config` is not in the command registry")
	}
	if code := cmd(nil); code != 0 {
		t.Fatalf("`yolo init-user-config` exited %d", code)
	}
	b, err := os.ReadFile(paths.UserConfigPath())
	if err != nil {
		t.Fatalf("`yolo init-user-config` wrote no user config: %v", err)
	}
	return string(b)
}

// commentBlockBefore returns the run of `//` comment lines immediately above the first line
// containing marker — the prose a reader sees next to that key — as one lowercased line, with
// the comment markers stripped and the wrapping undone, so a phrase can be matched wherever
// the text happens to break.
func commentBlockBefore(t *testing.T, body, marker string) string {
	t.Helper()
	lines := strings.Split(body, "\n")
	at := -1
	for i, l := range lines {
		if strings.Contains(l, marker) {
			at = i
			break
		}
	}
	if at < 0 {
		t.Fatalf("the scaffolded config has no %q — this test has lost its subject", marker)
	}
	start := at
	for start > 0 && strings.HasPrefix(strings.TrimSpace(lines[start-1]), "//") {
		start--
	}
	var words []string
	for _, l := range lines[start:at] {
		words = append(words, strings.Fields(strings.TrimPrefix(strings.TrimSpace(l), "//"))...)
	}
	return strings.ToLower(strings.Join(words, " "))
}

// TestInitTemplateNamesEveryRuntime ties the runtime comment to the list validation accepts,
// in both configs yolo scaffolds: the workspace one `yolo init` writes and the user-scope one
// `yolo init-user-config` writes, each with its own copy of the comment. Both offered "podman"
// or "container" long after macos-user shipped, the second for longer, because fixing the
// first did not reach it; the workspace one once offered docker, which validation now refuses.
func TestInitTemplateNamesEveryRuntime(t *testing.T) {
	for _, c := range []struct {
		cmd   string
		write func(*testing.T) string
	}{
		{"yolo init-user-config", initUserConfigFile},
	} {
		t.Run(c.cmd, func(t *testing.T) {
			block := commentBlockBefore(t, c.write(t), `// "runtime":`)
			for _, rt := range paths.AllRuntimes {
				if !strings.Contains(block, `"`+rt+`"`) {
					t.Errorf("the runtime comment in the config `%s` writes does not offer %q, "+
						"a value config validation accepts:\n%s", c.cmd, rt, block)
				}
			}
			if strings.Contains(block, `"docker"`) {
				t.Errorf("the runtime comment in the config `%s` writes offers \"docker\", "+
					"which config validation refuses:\n%s", c.cmd, block)
			}
		})
	}
}
