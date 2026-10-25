package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
)

// THE HOUSEKEEPING SLOT ON A MAC (disk-footprint.md, the macos-user slot): a macos-user
// launch reaps the retired loophole-state generations past the newest three while its session
// runs, and says so in <workspace>/.yolo/housekeeping.log.
//
// What the Linux gate cannot reach and this does: the real backend's timing. The slot runs on its
// own goroutine and dies at the launch's exit, so a pass that only started after the agent exited
// would be cut, and one that waited would hold the prompt. A session of a few seconds is long
// enough for a pass over a fresh home, and the one launch is the whole test.
func TestMacosUserHousekeepingReapsRetiredLoopholeStateDuringTheSession(t *testing.T) {
	requireMacosUser(t)
	ws := macosUserWorkspace(t, `{}`)
	// The suite's isolated home the launch runs under (requireMacosUser): its machine store is the
	// one the slot reaps. It is shared with every earlier test, whose launches retire generations
	// of their own under today's timestamps, so the seeds are dated in the FUTURE: the keep is by
	// name order (prune.PruneRetiredLoopholeStateGuarded), which makes the newest three seeds the
	// newest three generations whatever else the suite left there.
	archive := filepath.Join(os.Getenv("HOME"), ".local", "share", "yolo-jail", "state", ".retired")
	base := time.Date(2099, 9, 1, 12, 0, 0, 0, time.UTC)
	var seeded []string
	for i := 0; i < 5; i++ {
		name := base.Add(time.Duration(i) * time.Hour).Format("20060102-150405")
		dir := filepath.Join(archive, name, "yolo-it-loophole")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		seeded = append(seeded, name)
	}
	// Future-dated seeds would outrank every real generation for the rest of the suite.
	t.Cleanup(func() {
		for _, name := range seeded {
			_ = os.RemoveAll(filepath.Join(archive, name))
		}
	})

	// THE CLASS'S DEBOUNCE STAMP IS THE MACHINE'S (§6.3 of
	// docs/design/macos-user-suite-on-a-developer-mac.md): the run store keeps this archive private
	// but links the machine's build dir, where the stamp lives, so a developer's own launch earlier
	// that day left this launch's slot not due, and it reaped nothing and noted nothing. The test
	// owns the stamp for its launch, under the cross-run lock so another run's launch cannot stamp
	// it meanwhile, and puts the machine's back.
	//
	// AND THE CACHE CLASS'S, made fresh: the slot runs the shared cache's measurement first, whose
	// walk is bounded at a minute, against a session of seconds. On a machine whose cache stamp is
	// stale that walk could use the session up before the loophole-state class ran.
	holdMachineLock(t, true, "the macos-user housekeeping test owns the machine's housekeeping debounce stamps")
	// The Copilot self-update class's too, which runs between the two: it is not this test's
	// subject, and a slot that reaped a developer's copilot/pkg on the way would be a side effect.
	before := ownHousekeepingStamp(t, run.LoopholeStateHousekeepingClass, true) +
		ownHousekeepingStamp(t, run.CacheHousekeepingClass, false) +
		ownHousekeepingStamp(t, run.CopilotPkgHousekeepingClass, false)

	r := runMacosUser(t, ws, "sleep 5\necho \"=== END ===\"", withAutoReapers())
	if r.rc != 0 || !strings.Contains(r.stdout, "=== END ===") {
		t.Fatalf("the macos-user launch did not run its session (rc %d), so nothing below is a "+
			"statement about the slot.\nstdout:\n%s\nstderr:\n%s", r.rc, r.stdout, r.stderr)
	}

	entries, _ := os.ReadDir(archive)
	var left []string
	for _, e := range entries {
		left = append(left, e.Name())
	}
	sort.Strings(left)
	// Exactly the newest three seeds kept: anything older, seed or not, is past the keep. A
	// generation this very launch retires after the pass may also remain, and sorts before them.
	if len(left) < 3 || strings.Join(left[len(left)-3:], ",") != strings.Join(seeded[2:], ",") ||
		strings.Contains(strings.Join(left, ","), seeded[0]) || strings.Contains(strings.Join(left, ","), seeded[1]) {
		t.Errorf("after the session the retired generations are %v, want the newest three %v kept "+
			"and %v reaped: the launch ran no housekeeping slot, its exit cut the pass, another "+
			"pass held the machine's pass lock, or a launch outside the suite stamped the class meanwhile%s", left, seeded[2:], seeded[:2], before)
	}
	note, err := os.ReadFile(filepath.Join(ws, ".yolo", "housekeeping.log"))
	if err != nil || !strings.Contains(string(note), "loophole state: reclaimed ") ||
		!strings.Contains(string(note), " retired generation(s), keeping the newest 3") {
		t.Errorf("housekeeping.log does not record the reap (err %v)%s:\n%s", err, before, note)
	}
}

// ownHousekeepingStamp takes the debounce stamp of the housekeeping class named class over for
// the calling test: it removes it when due, so the test's launch finds the class due, or writes a
// fresh one when not, so the launch skips the class; and it restores it when the test ends — the
// machine's bytes when there were any, no stamp when there was none — so the machine's own cadence
// is what it was. It returns a clause saying what it found, for a failure.
func ownHousekeepingStamp(t *testing.T, class string, due bool) string {
	t.Helper()
	path := run.HousekeepingStampPath(class)
	data, err := os.ReadFile(path)
	had := err == nil
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("reading the %s housekeeping stamp %s: %v", class, path, err)
	}
	if had && due {
		if err := os.Remove(path); err != nil {
			t.Fatalf("setting the machine's %s housekeeping stamp %s aside: %v", class, path, err)
		}
	}
	if !due {
		fresh := []byte(strconv.FormatInt(time.Now().Unix(), 10) + "\n")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, fresh, 0o644); err != nil {
			t.Fatalf("freshening the %s housekeeping stamp %s: %v", class, path, err)
		}
	}
	t.Cleanup(func() {
		if !had {
			_ = os.Remove(path)
			return
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Errorf("restoring the machine's %s housekeeping stamp %s (it held %q): %v",
				class, path, strings.TrimSpace(string(data)), err)
		}
	})
	held := "absent"
	if had {
		held = fmt.Sprintf("%q", strings.TrimSpace(string(data)))
	}
	made := "removed, so the class was due"
	if !due {
		made = "freshened, so the class was skipped"
	}
	return fmt.Sprintf(" (the machine's %s stamp %s, %s before the launch, was %s)", class, path, held, made)
}
