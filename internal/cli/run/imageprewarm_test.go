package run

// imageprewarm_test.go pins the prewarm's end (imageprewarm.go): whatever way a launch returns, the
// nix its prewarm started is stopped before Run returns, rather than left running with no parent.

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/image"
	"github.com/mschulkind-oss/yolo-jail/internal/nixchildren"
	"github.com/mschulkind-oss/yolo-jail/internal/testsupport"
)

// A REFUSED LAUNCH LEAVES NO PREWARM NIX RUNNING: a fresh launch whose slot hands no build is
// refused (missingbuilds.go) while its prewarm's `nix eval .#imageIdentity` still runs, and Run
// returns only once that nix has ended. Red with Run's deferred stopImagePrewarm deleted: the
// stand-in nix is still running, and in a real launch it went on with parent pid 1 for up to its
// two-minute limit, writing the user's nix cache.
func TestARefusedLaunchLeavesNoPrewarmNixRunning(t *testing.T) {
	slotLaunchHome(t)
	set := nixchildren.Isolate(t)
	started := filepath.Join(t.TempDir(), "started")
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "nix"),
		[]byte("#!/bin/sh\n"+testsupport.UntilInterrupted(":", started)+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	argv, printed := fakePodmanLaunch(t, func(o *Options) {
		t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
		o.prewarmImage = image.Prewarm
		o.BuildSlot = func(BuildSlotRequest) (map[string]entrypoint.ForkDelivery, map[string]TreeDelivery) {
			// The refusal follows the slot, so the prewarm's nix is running when the launch returns.
			for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(5 * time.Millisecond) {
				if _, err := os.Stat(started); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Error("the prewarm's nix never started")
					break
				}
			}
			return map[string]entrypoint.ForkDelivery{"tool": {Reason: "test"}},
				map[string]TreeDelivery{treeKey: {Reason: "test"}}
		}
	})
	if argv != nil {
		t.Fatalf("the launch was not refused\n%s", printed)
	}
	if n := set.Running(); n != 0 {
		t.Errorf("the refused launch returned with %d nix children of its prewarm still running\n%s", n, printed)
	}
}
