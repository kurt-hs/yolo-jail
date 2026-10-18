package integration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
)

// TestOwnHousekeepingStampHandsTheMachinesStampBack pins ownHousekeepingStamp, which
// TestMacosUserHousekeepingReapsRetiredLoopholeStateDuringTheSession uses to make its launch's
// slot due (docs/design/macos-user-suite-on-a-developer-mac.md §6.3). The stamp is the machine's,
// so the test must leave it as it found it: the same bytes when there was one, none when there
// was none. Runs under -short against a HOME of its own.
func TestOwnHousekeepingStampHandsTheMachinesStampBack(t *testing.T) {
	t.Setenv("HOME", resolvedTempDir(t))
	path := run.HousekeepingStampPath(run.LoopholeStateHousekeepingClass)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	const machine = "1760000000\n"
	if err := os.WriteFile(path, []byte(machine), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("present", func(t *testing.T) {
		ownHousekeepingStamp(t, run.LoopholeStateHousekeepingClass)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("the stamp is still at %s for the test's launch (err %v), so its slot is not due", path, err)
		}
		// What the test's own slot writes when its pass completes.
		if err := os.WriteFile(path, []byte("1760099999\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	})
	if got, err := os.ReadFile(path); err != nil || string(got) != machine {
		t.Fatalf("after the test the stamp holds %q (err %v), want the machine's %q back", got, err, machine)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	t.Run("absent", func(t *testing.T) {
		ownHousekeepingStamp(t, run.LoopholeStateHousekeepingClass)
		if err := os.WriteFile(path, []byte("1760099999\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("a machine with no stamp has one after the test (err %v): its next real pass is delayed a day", err)
	}
}
