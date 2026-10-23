package entrypoint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// On macos-user the sandbox home is shared by every workspace, and each workspace's context dir
// is its own child of one staged parent. The second workspace's launch must move yolo's own link
// to its own tree, and must leave a link that points anywhere else alone.
func TestLinkContextDirRetargetsItsOwnLinkForASecondWorkspace(t *testing.T) {
	parent := t.TempDir()
	ws1 := filepath.Join(parent, "cname-one")
	ws2 := filepath.Join(parent, "cname-two")
	for _, d := range []string{ws1, ws2} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	e, errw := ctxLinkEnv(t, ws1)
	e.Vars[ContextLinkOwnerEnv] = parent
	if err := LinkContextDir(e); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(e.Home, paths.ContextLinkName)
	if got, _ := os.Readlink(link); got != ws1 {
		t.Fatalf("first workspace: ~/ctx -> %q, want %q", got, ws1)
	}

	e2 := &Env{Home: e.Home, Workspace: e.Workspace, Stderr: e.Stderr,
		Vars: map[string]string{paths.ContextDirEnv: ws2, ContextLinkOwnerEnv: parent}}
	if err := LinkContextDir(e2); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.Readlink(link); got != ws2 {
		t.Errorf("second workspace: ~/ctx -> %q, want yolo's link retargeted to %q", got, ws2)
	}
	if strings.Contains(errw.String(), "not yolo's link") {
		t.Errorf("the second workspace called yolo's own link foreign: %q", errw.String())
	}
}

// A link that points somewhere outside yolo's staged parent is not yolo's, even with the parent
// set: it is left alone, and the one line says so.
func TestLinkContextDirLeavesALinkElsewhereAloneWithTheParentSet(t *testing.T) {
	parent := t.TempDir()
	ws := filepath.Join(parent, "cname-one")
	if err := os.Mkdir(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	elsewhere := t.TempDir()
	e, errw := ctxLinkEnv(t, ws)
	e.Vars[ContextLinkOwnerEnv] = parent
	link := filepath.Join(e.Home, paths.ContextLinkName)
	if err := os.Symlink(elsewhere, link); err != nil {
		t.Fatal(err)
	}
	if err := LinkContextDir(e); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.Readlink(link); got != elsewhere {
		t.Errorf("a link elsewhere was retargeted: ~/ctx -> %q, want %q", got, elsewhere)
	}
	want := "yolo: ~/ctx already exists and is not yolo's link to $YOLO_CONTEXT_DIR (" + ws + "); leaving it.\n"
	if errw.String() != want {
		t.Errorf("stderr = %q, want %q", errw.String(), want)
	}
}

// A sibling of the staged parent that only shares its name as a prefix is not inside it.
func TestOwnsContextLinkIsADirectoryTestNotAPrefixTest(t *testing.T) {
	e := &Env{Vars: map[string]string{ContextLinkOwnerEnv: "/Users/Shared/yolo/ctx"}}
	if !e.ownsContextLink("/Users/Shared/yolo/ctx/cname") {
		t.Error("a link into the staged parent is not counted as yolo's")
	}
	if e.ownsContextLink("/Users/Shared/yolo/ctx-other/cname") {
		t.Error("a sibling that shares the parent's name as a prefix is counted as yolo's")
	}
	if (&Env{Vars: map[string]string{}}).ownsContextLink("/Users/Shared/yolo/ctx/cname") {
		t.Error("with no staged parent set, nothing is yolo's")
	}
}
