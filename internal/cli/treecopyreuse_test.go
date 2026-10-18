package cli

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// deliverReusing runs one macos-user launch's tree arm for the fixture's extension with root as
// the request's stable copy root (TreeBuildRequest.ReuseCopies).
func (fx *treeFixture) deliverReusing(t *testing.T, root string) (run.TreeDelivery, string) {
	t.Helper()
	var out, errw syncBuffer
	req := run.TreeBuildRequest{Trees: []packload.Fork{fx.tree(t)}, Platform: patchedTestPlatform, Runtime: "macos-user",
		Workspace: "/ws", Build: true, CopyRoot: root, ReuseCopies: true}
	got := deliverTreesForLaunch(req, &out, &errw, false)
	return got[treeKeyCLI], out.String() + errw.String()
}

// countTreeCopies counts the copies the tree arm makes from here on.
func countTreeCopies(t *testing.T) *int {
	t.Helper()
	n := 0
	prev := treeCopied
	treeCopied = func(key string) { n++; prev(key) }
	t.Cleanup(func() { treeCopied = prev })
	return &n
}

// AN UNCHANGED macos-user RELAUNCH COPIES NO TREE: the second launch is handed the copy the first
// made, the same directory and the same inodes, and it is still a copy — never the store's own
// inode. Red while every launch copied its tree out of the store again (preparation.copy_install).
func TestAMacosUserLaunchReusesItsTreeCopyWhileTheEntryIsUnchanged(t *testing.T) {
	fx := newTreeFixture(t, `"f.txt"`)
	copies := countTreeCopies(t)
	root := filepath.Join(t.TempDir(), "tree-copies")
	first, out := fx.deliverReusing(t, root)
	if first.Dir == "" {
		t.Fatalf("no copy was delivered: %+v\n%s", first, out)
	}
	second, out := fx.deliverReusing(t, root)
	if second.Dir != first.Dir || second.Entry != first.Entry {
		t.Fatalf("the relaunch was handed %+v, the first launch %+v\n%s", second, first, out)
	}
	if *copies != 1 {
		t.Errorf("two launches of one unchanged build made %d copies, want 1", *copies)
	}
	if ino(t, filepath.Join(first.Dir, "f.txt")) != ino(t, filepath.Join(second.Dir, "f.txt")) {
		t.Error("the relaunch's copy is not the first launch's")
	}
	entry, err := (&capture.Store{Dir: paths.CapturesDir()}).Resolve(first.Entry)
	if err != nil {
		t.Fatal(err)
	}
	stored := filepath.Join(entry.Tree, filepath.FromSlash(packdecl.TreeReservedDir("tool-ext")), "f.txt")
	if ino(t, stored) == ino(t, filepath.Join(second.Dir, "f.txt")) {
		t.Error("the reused copy is the store's own inode")
	}
	if got, err := os.ReadFile(filepath.Join(second.Dir, "f.txt")); err != nil ||
		string(got) != lines30(map[int]string{10: "ten", 12: "twelve"}) {
		t.Errorf("the reused copy's f.txt = %q (%v)", got, err)
	}
}

// A COPY NO LAUNCH HOLDS IS REAPED, and one a launch holds is not: another entry's copy of the same
// extension, and a directory a crashed launch left with no lease, go when a launch settles its own;
// a copy whose lease another process holds stays. Red with every old copy kept forever, or with a
// held one removed under the launch about to deliver it.
func TestAMacosUserLaunchReapsOnlyTheTreeCopiesNoLaunchHolds(t *testing.T) {
	fx := newTreeFixture(t, `"f.txt"`)
	root := filepath.Join(t.TempDir(), "tree-copies")
	first, out := fx.deliverReusing(t, root)
	if first.Dir == "" {
		t.Fatalf("no copy was delivered: %+v\n%s", first, out)
	}
	slug := filepath.Dir(filepath.Dir(first.Dir))
	sibling := func(name string, leased bool) string {
		t.Helper()
		dir := filepath.Join(slug, name)
		if err := os.MkdirAll(filepath.Join(dir, treeCopyTreeLeaf), 0o755); err != nil {
			t.Fatal(err)
		}
		if leased {
			if err := os.WriteFile(filepath.Join(dir, treeCopyLeaseLeaf), nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	stale := sibling("stale-entry", true)
	orphan := sibling(".tmp-crashed", false)
	held := sibling("held-entry", true)
	lease, err := os.Open(filepath.Join(held, treeCopyLeaseLeaf))
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	if err := syscall.Flock(int(lease.Fd()), syscall.LOCK_SH); err != nil {
		t.Fatal(err)
	}

	again, out := fx.deliverReusing(t, root)
	if again.Dir != first.Dir {
		t.Fatalf("the relaunch was handed %+v\n%s", again, out)
	}
	for _, gone := range []string{stale, orphan} {
		if _, err := os.Lstat(gone); !os.IsNotExist(err) {
			t.Errorf("%s survived the relaunch (%v)", gone, err)
		}
	}
	if _, err := os.Lstat(held); err != nil {
		t.Errorf("a copy another process holds was reaped: %v", err)
	}
	if _, err := os.Stat(filepath.Join(first.Dir, "f.txt")); err != nil {
		t.Errorf("the launch's own copy went: %v", err)
	}
}
