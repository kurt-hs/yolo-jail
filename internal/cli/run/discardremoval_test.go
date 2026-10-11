package run

// discardremoval_test.go pins discardremoval.go: a gone jail's skeleton, pack tree and per-launch
// patched-extension copies are moved aside and deleted by a detached remover, never inline in the
// teardown the user's prompt waits on (shutdown.clear_tracking took 19.4 s that way, 2026-10-11).

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// goneJailFixture is a known-gone jail whose launch built a skeleton and handed its container a
// pack tree with a per-launch patched copy beside it. It returns the Options, the three
// directories, and a file inside the copy, standing for the thousands a real copy holds.
func goneJailFixture(t *testing.T, cname string) (o *Options, skeleton, tree, copies, inCopy string) {
	t.Helper()
	o, _, _ = trackingFixture(t, cname, ExecResult{Ran: true, RC: 0})
	skeleton = buildSkeletonForTest(t, cname, nil, nil, nil)
	tree = newTreeForTest(t, cname, "claude")
	if err := writeLivePackTree(cname, tree); err != nil {
		t.Fatal(err)
	}
	copies = patchedCopiesDir(tree)
	inCopy = filepath.Join(copies, PatchedCopySlug("matt/pi-subagents"), "node_modules", "x", "index.js")
	if err := os.MkdirAll(filepath.Dir(inCopy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inCopy, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	o.packTree, o.packTreeHeld = tree, true
	return o, skeleton, tree, copies, inCopy
}

func assertGone(t *testing.T, what string, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Errorf("%s: %s is still there (err %v)", what, p, err)
		}
	}
}

// THE BUG: the teardown deleted the trees inline. Driven through the real chain
// (teardownAfterExit), with a remover that is started and never runs: the directories must be
// gone from their paths at once, their content must still exist (moved aside, NOT deleted by the
// teardown), and exactly one detached `discard-rm` must have been started for it. Before the fix
// no remover was started and the content was unlinked inside shutdown.clear_tracking.
func TestATeardownHandsTheGoneJailsTreesToADetachedRemover(t *testing.T) {
	const cname = "yolo-discard-gone"
	o, skeleton, tree, copies, inCopy := goneJailFixture(t, cname)
	var started [][]string
	o.StartDetached = func(argv []string, inherit *os.File) error {
		started = append(started, argv)
		return nil
	}

	o.teardownAfterExit(nil, "", nil, "", cname, "podman", skeleton, 0)

	if len(started) != 1 {
		t.Fatalf("the teardown started %d detached removers, want one: %v", len(started), started)
	}
	if want := []string{"internal", DiscardRemoverVerb, "--", cname}; !slices.Equal(started[0][1:], want) {
		t.Errorf("remover argv = %v, want <yolo> %v", started[0], want)
	}
	assertGone(t, "the teardown left a directory at its path", skeleton, tree, copies,
		paths.LivePackTreeRecord(cname))
	var moved []string
	_ = filepath.WalkDir(paths.DiscardRoot(cname), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && filepath.Base(p) == filepath.Base(inCopy) {
			moved = append(moved, p)
		}
		return nil
	})
	if len(moved) != 1 {
		t.Fatalf("the patched copy's content is not waiting under %s (found %v): the teardown deleted "+
			"it inline, which is the wait shutdown.clear_tracking paid", paths.DiscardRoot(cname), moved)
	}

	// The remover itself, as the verb runs it: it empties the root and removes it.
	if rc := DiscardRemoverMain([]string{"--", cname}); rc != 0 {
		t.Fatalf("DiscardRemoverMain = %d, want 0", rc)
	}
	assertGone(t, "the remover left", paths.DiscardRoot(cname))
}

// A remover that does not start costs the old inline delete, never a leaked tree; nothing is left
// in the discard root for the reaper.
func TestADiscardRemoverThatDoesNotStartDeletesInline(t *testing.T) {
	const cname = "yolo-discard-nofork"
	o, skeleton, tree, copies, _ := goneJailFixture(t, cname)
	o.Timing = true
	o.initPerf(cname)
	o.StartDetached = func([]string, *os.File) error { return errors.New("no fork for you") }

	o.teardownAfterExit(nil, "", nil, "", cname, "podman", skeleton, 0)

	assertGone(t, "a failed spawn left", skeleton, tree, copies)
	entries, _ := os.ReadDir(paths.DiscardRoot(cname))
	if len(entries) != 0 {
		t.Errorf("a failed spawn left %d batch(es) in the discard root", len(entries))
	}
	if !strings.Contains(scratchPerfLog(t, o.Workspace), "mark   shutdown.clear_tracking.discard_rm_not_started") {
		t.Error("a failed spawn left no mark")
	}
}

// The sub-spans of shutdown.clear_tracking come from the real call site: delete any one from
// forgetGone and this fails. They are what attributes the next slow quit without reading code.
func TestClearTrackingRecordsItsSubSpans(t *testing.T) {
	const cname = "yolo-discard-spans"
	o, skeleton, _, _, _ := goneJailFixture(t, cname)
	o.Timing = true
	o.initPerf(cname)
	o.StartDetached = func([]string, *os.File) error { return nil }

	o.teardownAfterExit(nil, "", nil, "", cname, "podman", skeleton, 0)

	file := scratchPerfLog(t, o.Workspace)
	outer := strings.Index(file, "start  shutdown.clear_tracking\n")
	outerEnd := strings.Index(file, "end    shutdown.clear_tracking ")
	if outer < 0 || outerEnd < 0 {
		t.Fatalf("no shutdown.clear_tracking span; log:\n%s", file)
	}
	last := outer
	for _, name := range []string{
		"shutdown.clear_tracking.probe",
		"shutdown.clear_tracking.discard",
		"shutdown.clear_tracking.credentials",
	} {
		at := strings.Index(file, "end    "+name+" ")
		if at < 0 || at < last || at > outerEnd {
			t.Errorf("span %s missing, out of order, or outside shutdown.clear_tracking; log:\n%s", name, file)
			continue
		}
		last = at
	}
	if !strings.Contains(file, "mark   shutdown.clear_tracking.discard_rm_started") {
		t.Errorf("no discard_rm_started mark; log:\n%s", file)
	}
}

// The remover's argument is one path element: anything else would name a root that is not a
// direct child of the jail's AGENTS_DIR entry.
func TestTheDiscardRemoverRefusesAPathForAName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, bad := range [][]string{nil, {"--"}, {cname0()}, {"--", ""}, {"--", "."}, {"--", ".."},
		{"--", "../x"}, {"--", "a/b"}, {"--", "a", "b"}} {
		if rc := DiscardRemoverMain(bad); rc != 2 {
			t.Errorf("DiscardRemoverMain(%q) = %d, want the usage refusal 2", bad, rc)
		}
	}
	// A root that was never made is nothing to do, and the remover creates nothing.
	if rc := DiscardRemoverMain([]string{"--", "yolo-never"}); rc != 0 {
		t.Errorf("DiscardRemoverMain on a missing root = %d, want 0", rc)
	}
	assertGone(t, "the remover created", filepath.Join(paths.AgentsDir(), "yolo-never"))
}

func cname0() string { return "yolo-x" }
