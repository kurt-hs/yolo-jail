package cli

// checkbrokernote_test.go pins `yolo check`'s grants-nothing note for a selected broker, end to
// end through the command a user runs, for a TOML user: the note is there, and it names the
// readable next step (`yolo allow git fetch`), never the legacy `brokered.git.grants`.
//
// It also pins that the note does not depend on test order. The broker set comes from
// loopholes.PackModules, whose lazy resolver memoizes its first answer for the life of the
// process: an earlier test that asked under its own HOME (often with no packs selected) fixed
// the answer every later test got, so this assertion passed alone and failed in the full package
// run. newNamedVerbFixture now clears that memo on the way in and out; the test forces the
// leaking order itself rather than relying on which test happens to run first.

import (
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/loopholes"
)

func TestCheckNamesYoloAllowInTheGrantsNothingNoteForATOMLUser(t *testing.T) {
	// The leaking order, forced: an earlier "test" asks for the pack modules under a HOME that
	// selects no packs, and leaves the memoized answer behind.
	t.Cleanup(loopholes.ResetPackModules)
	t.Setenv("HOME", t.TempDir())
	_ = loopholes.PackModules()

	newNamedVerbFixture(t, "", "")
	_, out, errs := captureStd(t, func() int { return registry["check"]([]string{"check", "--no-build"}) })
	all := out + errs
	for _, want := range []string{"git-broker is selected and grants nothing", "`yolo allow git fetch`"} {
		if !strings.Contains(all, want) {
			t.Fatalf("yolo check lacks %q:\n%s", want, all)
		}
	}
	if strings.Contains(all, "brokered.git.grants") {
		t.Fatalf("yolo check names the legacy grant to a TOML user:\n%s", all)
	}
}
