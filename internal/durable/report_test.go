package durable

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMeasureFollowsNoLinkAndStopsAtItsBudget(t *testing.T) {
	ws := workspaceDir(t)
	dir, err := Ensure(ws)
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "big"), make([]byte, 5000), 0o644); err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(dir, "worktrees", "land", "src"), 0o755))
	must(os.WriteFile(filepath.Join(dir, "worktrees", "land", "src", "a"), make([]byte, 100), 0o644))
	must(os.WriteFile(filepath.Join(dir, "notes"), make([]byte, 20), 0o644))
	must(os.Symlink(outside, filepath.Join(dir, "linkdir")))
	must(os.Symlink(filepath.Join(outside, "big"), filepath.Join(dir, "linkfile")))

	sz, err := Measure(ws, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sz.Bytes != 120 || sz.Partial {
		t.Errorf("Measure = %+v, want 120 bytes, complete", sz)
	}

	// A clock that jumps past the deadline at once: the walk stops and says so.
	calls := 0
	base := time.Now()
	clock := func() time.Time {
		calls++
		if calls == 1 {
			return base
		}
		return base.Add(time.Hour)
	}
	sz, err = Measure(ws, time.Second, clock)
	if err != nil || !sz.Partial {
		t.Errorf("a walk past its budget = %+v, %v; want partial", sz, err)
	}

	if _, err := Measure(filepath.Join(ws, "absent"), 0, nil); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("an absent dir gave %v", err)
	}
}

// writeRegistration fabricates git's admin files for one worktree, as `git worktree add`
// leaves them: gitdir names the worktree's .git file, HEAD the checkout.
func writeRegistration(t *testing.T, ws, id, gitdir, head string, mtime time.Time, locked bool) {
	t.Helper()
	admin := filepath.Join(ws, ".git", "worktrees", id)
	if err := os.MkdirAll(admin, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"gitdir": gitdir + "\n", "HEAD": head + "\n", "index": ""} {
		p := filepath.Join(admin, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	if locked {
		if err := os.WriteFile(filepath.Join(admin, "locked"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// The scan reads the admin files, in either frame's spelling: a worktree made in the jail
// records /workspace/…, which the host maps to its own path through an alias.
func TestScanDirCountsDurableWorktreesFromTheAdminFiles(t *testing.T) {
	ws := workspaceDir(t)
	dir, err := Ensure(ws)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	for _, wt := range []string{"land", "fix-footer"} {
		if err := os.MkdirAll(filepath.Join(dir, "worktrees", wt), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "worktrees", wt, ".git"), []byte("gitdir: x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Made in the jail: the jail's spelling.
	writeRegistration(t, ws, "land", "/workspace/.yolo/durable/worktrees/land/.git",
		"1a2b3c4d5e6f", now.Add(-2*24*time.Hour), true)
	// Made on the host: the host's spelling.
	writeRegistration(t, ws, "fix-footer", filepath.Join(dir, "worktrees", "fix-footer", ".git"),
		"ref: refs/heads/fix-footer", now.Add(-41*24*time.Hour), false)
	// Outside the durable dir, and gone: the /tmp incident class.
	writeRegistration(t, ws, "tmpland", "/tmp/definitely-gone-"+filepath.Base(ws)+"/land/.git",
		"ref: refs/heads/x", now, false)

	sc := ScanDir(ScanOptions{Workspace: ws, Aliases: map[string]string{"/workspace": ws}})
	if sc.Err != nil || sc.NotGit {
		t.Fatalf("scan: %+v", sc)
	}
	if len(sc.Worktrees) != 2 {
		t.Fatalf("worktrees = %+v, want land and fix-footer", sc.Worktrees)
	}
	oldest, newest := sc.Worktrees[0], sc.Worktrees[1]
	if oldest.Name() != "fix-footer" || oldest.Describe() != "fix-footer" || oldest.Locked {
		t.Errorf("oldest = %+v", oldest)
	}
	if newest.Name() != "land" || newest.Describe() != "detached at 1a2b3c4d" || !newest.Locked {
		t.Errorf("newest = %+v", newest)
	}
	if len(sc.Others) != 1 || sc.Others[0] != "notes" {
		t.Errorf("others = %v", sc.Others)
	}
	if len(sc.Prunable) != 0 {
		t.Errorf("the host frame checked registrations outside the durable dir: %v", sc.Prunable)
	}

	sc = ScanDir(ScanOptions{Workspace: ws, Aliases: map[string]string{"/workspace": ws}, GoneRoots: []string{ws, "/tmp"}})
	if len(sc.Prunable) != 1 || !strings.HasSuffix(sc.Prunable[0], "/land") || !strings.HasPrefix(sc.Prunable[0], "/tmp/") {
		t.Errorf("prunable = %v, want the one /tmp registration", sc.Prunable)
	}

	lines := LaunchLines(sc, Size{Bytes: 1_200_000_000}, nil, WalkBudget, now)
	want := []string{
		"Durable dir: 2 worktrees and 1 other entry, 1.2 GB, oldest idle 41 days (fix-footer) — `yolo check` lists them; yolo deletes nothing here.",
		"Durable dir: 1 worktree registration points at paths that no longer exist (" + sc.Prunable[0] + ") — `git worktree prune -n -v` lists what a prune would drop.",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("launch lines:\n%s\nwant:\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	partial := LaunchLines(sc, Size{Bytes: 2_000_000_000, Partial: true}, nil, WalkBudget, now)
	if !strings.Contains(partial[0], ", ≥ 2.0 GB counted in 2s, not the total,") {
		t.Errorf("a partial walk with no complete count: %q", partial[0])
	}
	// A walk that stopped leads with the last complete count, never the floor alone: a 410 GB
	// dir once printed "≥ 12.9 GB".
	partial = LaunchLines(sc, Size{Bytes: 12_900_000_000, Partial: true, Counting: true,
		Last: SizeRecord{Bytes: 410_000_000_000, MeasuredAt: now.Add(-3 * time.Hour)}}, nil, WalkBudget, now)
	if want := ", 410.0 GB at the last full count, 3 hours ago (≥ 12.9 GB counted in 2s); " +
		"a full count is running for the next launch,"; !strings.Contains(partial[0], want) {
		t.Errorf("a partial walk with a complete count:\n%q\nwant it to contain\n%q", partial[0], want)
	}
}

// The record round-trips, a partial count is never recorded, and a record the jail mangled
// reads as none.
func TestTheSizeRecordKeepsOnlyCompleteCounts(t *testing.T) {
	ws := workspaceDir(t)
	if _, err := Ensure(ws); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	if _, ok := LastSize(ws); ok {
		t.Fatal("a record before any count")
	}
	if err := RecordSize(ws, Size{Bytes: 7, Partial: true}, at); err != nil {
		t.Fatal(err)
	}
	if _, ok := LastSize(ws); ok {
		t.Fatal("a partial count was recorded as complete")
	}
	if err := RecordSize(ws, Size{Bytes: 410_000_000_000}, at); err != nil {
		t.Fatal(err)
	}
	if rec, ok := LastSize(ws); !ok || rec.Bytes != 410_000_000_000 || !rec.MeasuredAt.Equal(at) {
		t.Fatalf("LastSize = %+v, %v", rec, ok)
	}
	if err := os.WriteFile(filepath.Join(ws, ".yolo", SizeRecordName), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := LastSize(ws); ok {
		t.Fatal("a mangled record was read")
	}
}

// The background count records the true total, with no budget, and makes no `.yolo`.
func TestTheBackgroundCountRecordsTheWholeTotal(t *testing.T) {
	ws := workspaceDir(t)
	dir, err := Ensure(ws)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		sub := filepath.Join(dir, "worktrees", "w"+string(rune('a'+i)))
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sub, "f"), make([]byte, 1000), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if rc := CountMain([]string{"--workspace", ws}); rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	if rec, ok := LastSize(ws); !ok || rec.Bytes != 3000 {
		t.Fatalf("LastSize = %+v, %v; want 3000 bytes", rec, ok)
	}
	bare := workspaceDir(t)
	if rc := CountMain([]string{"--workspace", bare}); rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	if _, err := os.Lstat(filepath.Join(bare, ".yolo")); err == nil {
		t.Fatal("the count made a .yolo in a workspace that had none")
	}
}

// Silence when there is nothing to say, and the directory count when there is no repository.
func TestLaunchLinesDegenerateCases(t *testing.T) {
	ws := workspaceDir(t)
	dir, err := Ensure(ws)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if lines := LaunchLines(ScanDir(ScanOptions{Workspace: ws}), Size{}, nil, WalkBudget, now); len(lines) != 0 {
		t.Errorf("an empty durable dir printed %q", lines)
	}
	if err := os.MkdirAll(filepath.Join(dir, "worktrees", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	sc := ScanDir(ScanOptions{Workspace: ws})
	if !sc.NotGit || sc.WorktreeDirs != 1 {
		t.Fatalf("no repository: %+v", sc)
	}
	lines := LaunchLines(sc, Size{Bytes: 5}, nil, WalkBudget, now)
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "Durable dir: 1 directory under worktrees/, 5 B — ") {
		t.Errorf("no repository: %q", lines)
	}
}

// Against real git: a worktree made with `git worktree add --lock` into the durable dir is a
// durable worktree, and the branch and lock are read back from git's own admin files.
func TestScanDirReadsWhatRealGitWrites(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	ws := workspaceDir(t)
	dir, err := Ensure(ws)
	if err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		// maintenance.auto/gc.auto: no detached maintenance outliving git into t.TempDir's cleanup.
		cmd := exec.Command("git", append([]string{"-C", ws, "-c", "user.name=t", "-c", "user.email=t@t",
			"-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main",
			"-c", "maintenance.auto=false", "-c", "gc.auto=0"}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	git("commit", "-q", "--allow-empty", "-m", "c")
	git("worktree", "add", "-q", "--lock", "-b", "topic", filepath.Join(dir, "worktrees", "land"))
	sc := ScanDir(ScanOptions{Workspace: ws, GoneRoots: []string{ws}})
	if len(sc.Worktrees) != 1 {
		t.Fatalf("scan: %+v", sc)
	}
	w := sc.Worktrees[0]
	if w.Name() != "land" || w.Branch != "topic" || !w.Locked || w.LastActive.IsZero() {
		t.Errorf("worktree = %+v", w)
	}
	if len(sc.Prunable) != 0 {
		t.Errorf("a live worktree was called prunable: %v", sc.Prunable)
	}
}

// The per-worktree figure walks to its directory one component at a time beneath the
// durable dir's root, so a link the jail put on the way is refused rather than followed.
func TestMeasureRelRefusesALinkOnTheWay(t *testing.T) {
	ws := workspaceDir(t)
	dir, err := Ensure(ws)
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "big"), make([]byte, 5000), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "worktrees", "land"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "worktrees", "land", "f"), make([]byte, 7), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "worktrees", "evil")); err != nil {
		t.Fatal(err)
	}
	if sz, err := MeasureRel(ws, "worktrees/land", 0, nil); err != nil || sz.Bytes != 7 {
		t.Errorf("MeasureRel(land) = %+v, %v", sz, err)
	}
	if sz, err := MeasureRel(ws, "worktrees/evil", 0, nil); err == nil {
		t.Errorf("MeasureRel followed a link out of the durable dir: %+v", sz)
	}
	if _, err := MeasureRel(ws, "../..", 0, nil); err == nil {
		t.Error("MeasureRel accepted a path out of the durable dir")
	}
}
