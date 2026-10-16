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
	"github.com/mschulkind-oss/yolo-jail/internal/shquote"
	"github.com/mschulkind-oss/yolo-jail/internal/testsupport"
)

// A REFUSED LAUNCH LEAVES NO PREWARM NIX RUNNING: a fresh launch whose slot hands no build is
// refused (missingbuilds.go) while its prewarm's `nix eval .#imageIdentity` still runs, and Run
// returns promptly, once it has INTERRUPTED that nix and the nix has ended. Red with Run's deferred
// stopImagePrewarm deleted: the stand-in nix is still running, and in a real launch it went on with
// parent pid 1 for up to its two-minute limit, writing the user's nix cache. Red too with the eval
// not ending with the prewarm's context (identityMemo's ctx): Run then waits out the stand-in's own
// limit, and no interrupt reaches it.
func TestARefusedLaunchLeavesNoPrewarmNixRunning(t *testing.T) {
	slotLaunchHome(t)
	set := nixchildren.Isolate(t)
	marks := t.TempDir()
	started, interrupted := filepath.Join(marks, "started"), filepath.Join(marks, "interrupted")
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "nix"),
		[]byte("#!/bin/sh\n"+testsupport.UntilInterrupted(": >"+shquote.Quote(interrupted), started)+"\n"),
		0o755); err != nil {
		t.Fatal(err)
	}
	var returned time.Time
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
			returned = time.Now()
			return map[string]entrypoint.ForkDelivery{"tool": {Reason: "test"}},
				map[string]TreeDelivery{treeKey: {Reason: "test"}}
		}
	})
	// From the slot's end, the refusal and the stop take well under the stand-in's 30-second limit:
	// the interrupt ends it at once.
	if took := time.Since(returned); took > 10*time.Second {
		t.Errorf("the refused launch took %s after its slot to return; its prewarm's nix was not interrupted", took)
	}
	if _, err := os.Stat(interrupted); err != nil {
		t.Errorf("the refused launch did not interrupt its prewarm's nix: %v", err)
	}
	if argv != nil {
		t.Fatalf("the launch was not refused\n%s", printed)
	}
	if n := set.Running(); n != 0 {
		t.Errorf("the refused launch returned with %d nix children of its prewarm still running\n%s", n, printed)
	}
}
