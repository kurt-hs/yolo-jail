package prune

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/outfmt"
)

var devNow = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

// mkDevBundle publishes a fake bundle shaped like matched-bundle.py's — bin/yolo plus the
// helper dir a launch mounts — made read-only the way _make_readonly does, with its mtime age
// before devNow. Returns its path, resolved (macOS's t.TempDir is under a symlink).
func mkDevBundle(t *testing.T, dir string, n int, age time.Duration) string {
	t.Helper()
	name := fmt.Sprintf("%016x-%032x", n, n)
	p := filepath.Join(dir, name)
	helpers := filepath.Join(p, "share", "yolo-jail", "bin", "linux-amd64")
	for _, d := range []string{filepath.Join(p, "bin"), helpers} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{filepath.Join(p, "bin", "yolo"), filepath.Join(helpers, "yolo-entrypoint")} {
		if err := os.WriteFile(f, []byte("binary"), 0o555); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []string{helpers, filepath.Dir(helpers), filepath.Join(p, "share", "yolo-jail"),
		filepath.Join(p, "share"), filepath.Join(p, "bin"), p} {
		if err := os.Chmod(d, 0o555); err != nil {
			t.Fatal(err)
		}
	}
	setAge(t, p, age)
	return p
}

func setAge(t *testing.T, p string, age time.Duration) {
	t.Helper()
	at := devNow.Add(-age)
	if err := os.Chtimes(p, at, at); err != nil {
		t.Fatal(err)
	}
}

func devDir(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(d, devBundlesLeaf)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Restore write permission so t.TempDir's cleanup can remove what a test kept.
	t.Cleanup(func() { _ = RemoveReadOnlyTree(dir) })
	return dir
}

func knownNothingRunning() DevBundleLiveness {
	return DevBundleLiveness{MountSources: map[string]bool{}, MountsKnown: true, ProcsKnown: true}
}

func removedNames(r DevBundleReap) map[string]bool {
	out := map[string]bool{}
	for _, e := range r.Removed {
		out[e.Name] = true
	}
	return out
}

func TestDevBundlesKeepTheNewestAndRemoveTheRestReadOnlyOrNot(t *testing.T) {
	dir := devDir(t)
	var bundles []string
	for i := 0; i < 6; i++ {
		bundles = append(bundles, mkDevBundle(t, dir, i, time.Duration(i+2)*time.Hour))
	}
	// Dry run lists, touches nothing.
	dry := PruneDevBundles(dir, DevBundleKeep, knownNothingRunning(), false, devNow, nil)
	if dry.Unknown != "" || len(dry.Removed) != 3 {
		t.Fatalf("dry run: unknown=%q removed=%d, want the 3 oldest", dry.Unknown, len(dry.Removed))
	}
	for _, b := range bundles {
		if !exists(b) {
			t.Fatalf("dry run removed %s", b)
		}
	}
	r := PruneDevBundles(dir, DevBundleKeep, knownNothingRunning(), true, devNow, nil)
	for i, b := range bundles {
		if want := i < DevBundleKeep; exists(b) != want {
			t.Errorf("bundle %d (age %dh): exists=%v, want %v", i, i+2, exists(b), want)
		}
	}
	for _, e := range r.Removed {
		if e.Err != nil {
			t.Errorf("%s: %v — a read-only bundle must be made writable before removal", e.Name, e.Err)
		}
	}
	if r.RemovedCount() != 3 || r.Bytes <= 0 {
		t.Errorf("removed %d (%d bytes), want 3 with bytes", r.RemovedCount(), r.Bytes)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != DevBundleKeep {
		t.Errorf("left %d entries, want only the kept bundles (no .reap- leftovers)", len(entries))
	}
}

func TestDevBundlesInUseByARunningJailAreKept(t *testing.T) {
	dir := devDir(t)
	for i := 0; i < DevBundleKeep; i++ {
		mkDevBundle(t, dir, 100+i, time.Hour+time.Minute)
	}
	old := mkDevBundle(t, dir, 1, 48*time.Hour)
	ev := knownNothingRunning()
	ev.MountSources[filepath.Join(old, "share", "yolo-jail", "bin", "linux-amd64")] = true
	r := PruneDevBundles(dir, DevBundleKeep, ev, true, devNow, nil)
	if !exists(old) || len(r.Removed) != 0 {
		t.Fatalf("the bundle a running jail mounts at /opt/yolo-jail/bin was removed (removed=%v)", removedNames(r))
	}
}

func TestDevBundlesInUseByAProcessAreKept(t *testing.T) {
	dir := devDir(t)
	for i := 0; i < DevBundleKeep; i++ {
		mkDevBundle(t, dir, 100+i, 2*time.Hour)
	}
	daemon := mkDevBundle(t, dir, 1, 48*time.Hour)
	// A prefix lookalike: <…>/0001 must not hold <…>/00010…; a different bundle with no holder.
	other := mkDevBundle(t, dir, 2, 48*time.Hour)
	ev := knownNothingRunning()
	ev.Procs = []ProcStart{
		{PID: 4242, Name: "yolo", Exe: filepath.Join(daemon, "bin", "yolo")},
		{PID: 7, Name: "bash", Exe: "/usr/bin/bash"},
	}
	r := PruneDevBundles(dir, DevBundleKeep, ev, true, devNow, nil)
	if !exists(daemon) {
		t.Fatal("the bundle a live host daemon executes out of was removed")
	}
	if exists(other) {
		t.Errorf("an unused old bundle survived: removed=%v", removedNames(r))
	}
}

func TestDevBundlesDeclineWhenLivenessIsUnknown(t *testing.T) {
	for _, tc := range []struct {
		name string
		ev   DevBundleLiveness
		want string
	}{
		{"runtime", DevBundleLiveness{ProcsKnown: true, MountsWhy: "could not enumerate running jails from podman"}, "podman"},
		{"proc table", DevBundleLiveness{MountsKnown: true, ProcsWhy: "could not read the process table"}, "process table"},
		{"hidden yolo exe", DevBundleLiveness{MountsKnown: true, ProcsKnown: true,
			Procs: []ProcStart{{PID: 9, Name: "yolo-jaild"}}}, "process 9"},
		{"truncated comm", DevBundleLiveness{MountsKnown: true, ProcsKnown: true,
			Procs: []ProcStart{{PID: 10, Name: "abcdefghijklmno", NameTruncated: true}}}, "process 10"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := devDir(t)
			old := mkDevBundle(t, dir, 1, 30*24*time.Hour)
			for i := 0; i < DevBundleKeep; i++ {
				mkDevBundle(t, dir, 100+i, 2*time.Hour)
			}
			r := PruneDevBundles(dir, DevBundleKeep, tc.ev, true, devNow, nil)
			if !exists(old) || len(r.Removed) != 0 {
				t.Fatal("removed a bundle without being able to ask what uses it — " +
					"\"I could not ask\" is not \"unreferenced\"")
			}
			if !strings.Contains(r.Unknown, tc.want) {
				t.Errorf("Unknown = %q, want it to name %q", r.Unknown, tc.want)
			}
		})
	}
	// A non-yolo process hiding its exe (a non-dumpable ssh-agent) is no evidence either way.
	dir := devDir(t)
	old := mkDevBundle(t, dir, 1, 30*24*time.Hour)
	ev := knownNothingRunning()
	ev.Procs = []ProcStart{{PID: 11, Name: "ssh-agent"}}
	if r := PruneDevBundles(dir, 0, ev, true, devNow, nil); r.Unknown != "" || exists(old) {
		t.Errorf("a non-yolo process with no readable exe blocked the sweep: %q", r.Unknown)
	}
}

func TestDevBundlesRecentlyUsedAreKeptBeyondTheNewest(t *testing.T) {
	dir := devDir(t)
	recent := mkDevBundle(t, dir, 1, 10*time.Minute)
	r := PruneDevBundles(dir, 0, knownNothingRunning(), true, devNow, nil)
	if !exists(recent) || len(r.Removed) != 0 {
		t.Fatal("a bundle used within DevBundleGrace was removed — its launch may not have started yet")
	}
}

func TestDevBundleStagingAndLeftoversAreAgedAndOnlyOurNamesTouched(t *testing.T) {
	dir := devDir(t)
	mk := func(name string, age time.Duration) string {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Join(p, "bin"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(filepath.Join(p, "bin"), 0o555); err != nil {
			t.Fatal(err)
		}
		setAge(t, p, age)
		return p
	}
	fresh := mk(".building-abc123_x", 5*time.Minute)
	stale := mk(".building-zzzz9999", 3*time.Hour)
	leftover := mk(fmt.Sprintf(".reap-%016x-%032x-1-2", 5, 5), time.Minute)
	stranger := mk("keep-this", 365*24*time.Hour)
	PruneDevBundles(dir, DevBundleKeep, knownNothingRunning(), true, devNow, nil)
	if !exists(fresh) {
		t.Error("a publish in flight was removed")
	}
	if exists(stale) {
		t.Error("an abandoned publish (older than the staging floor) survived")
	}
	if exists(leftover) {
		t.Error("an interrupted removal's leftover survived")
	}
	if !exists(stranger) {
		t.Error("a directory whose name matched-bundle.py never writes was removed")
	}
}

func TestDevBundleGuardRecheckKeepsABundleReusedSinceTheListing(t *testing.T) {
	dir := devDir(t)
	b := mkDevBundle(t, dir, 1, 48*time.Hour)
	guard := Guard(func(recheck func() bool, del func()) bool {
		setAge(t, b, 0) // build_dev_bundle reused it meanwhile
		if !recheck() {
			return false
		}
		del()
		return true
	})
	r := PruneDevBundles(dir, 0, knownNothingRunning(), true, devNow, guard)
	if !exists(b) || r.RemovedCount() != 0 {
		t.Fatal("the guarded recheck did not notice a reuse between listing and deletion")
	}
}

// TestPruneRunReportsAndReclaimsDevBundles pins the `yolo prune` call site: delete the section
// from Run and this fails, whatever the unit tests above say.
func TestPruneRunReportsAndReclaimsDevBundles(t *testing.T) {
	o, gs := baseOpts(t)
	dir := DevBundlesDirUnder(gs)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = RemoveReadOnlyTree(dir) })
	o.DevBundleProcs = func() ([]ProcStart, bool) { return nil, true }
	o.Now = func() time.Time { return devNow }
	var old []string
	for i := 0; i < 5; i++ {
		b := mkDevBundle(t, dir, i, time.Duration(i+2)*time.Hour)
		if i >= DevBundleKeep {
			old = append(old, b)
		}
	}
	var buf bytes.Buffer
	o.Out = &buf
	Run(o)
	out := buf.String()
	if !strings.Contains(out, "Development bundles") || !strings.Contains(out, "would remove") ||
		!strings.Contains(out, filepath.Base(old[0])) {
		t.Fatalf("dry-run prune does not report the development bundles:\n%s", out)
	}
	for _, b := range old {
		if !exists(b) {
			t.Fatal("dry-run prune removed a development bundle")
		}
	}

	buf.Reset()
	o.Apply = true
	o.Format = outfmt.JSON
	Run(o)
	for _, b := range old {
		if exists(b) {
			t.Errorf("prune --apply left %s", b)
		}
	}
	var rep Report
	if err := json.Unmarshal(buf.Bytes(), &rep); err != nil {
		t.Fatalf("json: %v\n%s", err, buf.String())
	}
	found := false
	for _, c := range rep.Categories {
		if c.Name == "dev_bundles" {
			found = true
			if c.Count != 2 || c.Bytes <= 0 {
				t.Errorf("dev_bundles = %+v, want 2 bundles with bytes", c)
			}
		}
	}
	if !found {
		t.Error("the JSON report has no dev_bundles category")
	}
}

// TestPruneRunDeclinesDevBundlesWhenTheRuntimeCannotBeAsked: the same section, with podman
// unreachable, removes nothing and says why and what to do.
func TestPruneRunDeclinesDevBundlesWhenTheRuntimeCannotBeAsked(t *testing.T) {
	o, gs := baseOpts(t)
	dir := DevBundlesDirUnder(gs)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = RemoveReadOnlyTree(dir) })
	o.DevBundleProcs = func() ([]ProcStart, bool) { return nil, true }
	o.Now = func() time.Time { return devNow }
	o.Exec = func([]string, time.Duration) ProbeResult { return ProbeResult{Ran: false} }
	old := mkDevBundle(t, dir, 1, 30*24*time.Hour)
	var buf bytes.Buffer
	o.Out = &buf
	o.Apply = true
	Run(o)
	if !exists(old) {
		t.Fatal("prune --apply removed a development bundle with the runtime unreachable")
	}
	if !strings.Contains(buf.String(), "declining to remove development bundles") ||
		!strings.Contains(buf.String(), "rm -rf") {
		t.Errorf("the decline does not say so or name the next step:\n%s", buf.String())
	}
}
