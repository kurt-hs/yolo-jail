package hostfloor

// recordplatform_test.go pins the floor's materialize call sites to the record-platform rule
// (docs/reference/install-capture.md#record-platform): an entry whose manifest names the platform
// the same bytes were admitted for first installs on this floor when a record beside it names the
// floor's CapturePlatform, through Floor.CaptureRecords. Each test fails when its call site stops
// passing that reader (packtree_test.go pins materializeBuild's).

import (
	"context"
	"errors"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
)

// otherFirst is a manifest platform no floor here runs: the platform the bytes were admitted for
// first.
const otherFirst = "plan9/mips"

// recordsFor is a CaptureRecords naming platform for whatever entry it is asked about.
func recordsFor(platform string) capture.Records {
	return func(string) ([]capture.Record, error) {
		return []capture.Record{{Bin: "any", Platform: otherFirst}, {Bin: "any", Platform: platform}}, nil
	}
}

// installFromCapture's call site: an installer capture.
func TestAnInstallerCaptureFirstAdmittedForAnotherPlatformInstallsByItsRecord(t *testing.T) {
	for _, withRecord := range []bool{true, false} {
		w := newLinuxWorld(t)
		cs := newCaptureStore(t)
		cs.addShaped("claude", "2.1.267", true, func(_ string, m *capture.Manifest) { m.Platform = otherFirst })
		w.floor.ResolveCapture = cs.resolve
		w.floor.Capture = func(string) error { return errors.New("no capture in this test") }
		if withRecord {
			w.floor.CaptureRecords = recordsFor(capture.Platform())
		}
		_, outcome, err := w.floor.Ensure(context.Background(), installerProgram("claude", "claude"))
		if withRecord && (err != nil || outcome != Installed) {
			t.Fatalf("Ensure with a record for %s = %s %v\n%s", capture.Platform(), outcome, err, w.out.String())
		}
		if !withRecord && err == nil {
			t.Fatalf("Ensure with no record for %s installed an entry its manifest names %s", capture.Platform(), otherFirst)
		}
	}
}

// installFromPatchedState's call site: a patched fork's good build.
func TestAPatchedGoodBuildFirstAdmittedForAnotherPlatformInstallsByItsRecord(t *testing.T) {
	for _, withRecord := range []bool{true, false} {
		w, pf := patchedWorld(t)
		p := patchedProgram()
		entry := pf.bs.addShapedFor(p, forkCommitOne, func(m *capture.Manifest) { m.Platform = otherFirst })
		pf.state = PatchedState{Recipe: patchedRecipeOne,
			Good: &PatchedBuild{Commit: forkCommitOne, Recipe: patchedRecipeOne, Label: "v1.0.0 + 2 patches", Entry: entry}}
		if withRecord {
			w.floor.CaptureRecords = recordsFor(capture.Platform())
		}
		_, outcome, err := w.floor.Ensure(context.Background(), p)
		if withRecord && (err != nil || outcome != Installed) {
			t.Fatalf("Ensure with a record for %s = %s %v\n%s", capture.Platform(), outcome, err, w.out.String())
		}
		if !withRecord && err == nil {
			t.Fatalf("Ensure with no record for %s installed a build its manifest names %s", capture.Platform(), otherFirst)
		}
	}
}
