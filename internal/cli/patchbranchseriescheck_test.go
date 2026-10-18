package cli

// patchbranchseriescheck_test.go pins `yolo pack series check` on a PATCH BRANCH: the same question it answers
// for a series — does the newest upstream candidate take it — answered by merging the branch's commit into
// that candidate in a scratch mirror, as a launch would, so it answers the same in a jail as on the host
// and writes nothing on this machine. It used to decline a patch branch as having nothing to check.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func TestSeriesCheckSaysWhetherTheNewestUpstreamMergesThePatchBranch(t *testing.T) {
	fx := newPatchBranchAdvanceFixture(t)
	fx.commit(t, "v1.1.0", map[int]string{14: "fourteen"})
	tip := upstreamGit(t, fx.patchBranch, "rev-parse", "mine")
	tmp := inAJail(t, filepath.Dir(fx.forkDir))
	rc, out, errw := seriesVerb(t, "check", fx.forkDir)
	want := "fork forkpack/tool: upstream v1.1.0"
	if rc != 0 || !strings.Contains(out, want) || !strings.Contains(out, "merges the patch branch mine@"+shortSHA(tip)+" cleanly") {
		t.Fatalf("series check of a clean patch branch: rc=%d, want %q merging cleanly\n%s%s", rc, want, out, errw)
	}
	if strings.Contains(out, "nothing here to check") {
		t.Errorf("series check still declines the patch branch:\n%s", out)
	}
	noScratchLeft(t, tmp)
	if _, err := os.Stat(filepath.Join(paths.PacksDir(), "checks")); err == nil {
		t.Errorf("series check wrote this machine's check records")
	}
}

func TestSeriesCheckNamesAPatchBranchConflictAndTheMergeThatRepairsIt(t *testing.T) {
	fx := newPatchBranchAdvanceFixture(t)
	fx.commit(t, "v1.1.0", map[int]string{11: "eleven"})
	rc, out, errw := seriesVerb(t, "check", fx.forkDir)
	for _, w := range []string{"upstream v1.1.0", "does not merge the patch branch", "conflicts in f.txt",
		"git checkout mine", "git push origin mine"} {
		if rc == 0 || !strings.Contains(out, w) {
			t.Errorf("series check of a conflicting patch branch: rc=%d, want %q\n%s%s", rc, w, out, errw)
		}
	}
}

// A PATCH BRANCH THAT CANNOT BE FETCHED names the fetch, and the check to run again.
func TestSeriesCheckOfAPatchBranchItCannotFetchNamesTheNextStep(t *testing.T) {
	fx := newPatchBranchAdvanceFixture(t)
	fx.writePatchBranchManifest(t, "git+file://"+filepath.Join(t.TempDir(), "gone")+"?ref=mine")
	rc, out, errw := seriesVerb(t, "check", fx.forkDir)
	if rc == 0 || !strings.Contains(errw, "patch branch") || !strings.Contains(errw, "yolo pack series check") {
		t.Errorf("series check of an unfetchable patch branch: rc=%d\n%s%s", rc, out, errw)
	}
}
