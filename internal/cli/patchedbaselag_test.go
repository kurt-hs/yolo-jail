package cli

// patchedbaselag_test.go pins a series' BASE LAG (packsrc/baselag.go) where it is said: `yolo pack
// status` and the launch's fork line name the versions newer than the series' base, from the check
// record alone, while the series still applies — and nothing when the base is the newest version.

import (
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
)

// A SERIES ANCHORED ON v1.0.0 that v1.1.0 and v1.2.0 still take: once a launch advanced to v1.2.0,
// pack status names both releases, that v1.2.0 takes the series, and the git recipe that
// re-anchors it; the fork line a launch prints names the lag too.
func TestPackStatusAndTheForkLineNameTheBaseLag(t *testing.T) {
	fx := newPatchedAdvanceFixture(t, "")
	fx.commit(t, "v1.1.0", map[int]string{14: "fourteen"})
	v12 := fx.commit(t, "v1.2.0", map[int]string{14: "fourteen", 25: "x"})
	if r, out, _ := fx.launch(t, "podman"); r.delivery.Key == "" {
		t.Fatalf("the first advance built nothing:\n%s", out)
	}
	rec := fx.record(t)
	if rec.Good == nil || rec.Good.Commit != v12 {
		t.Fatalf("the good build is %+v, want v1.2.0", rec.Good)
	}
	lag := "its base v1.0.0 (" + shortSHA(fx.base) + ") is 2 releases behind: v1.1.0, v1.2.0"
	_, status, errw := packVerb(t, "status")
	for _, w := range []string{
		"  base lag: " + lag + "; v1.2.0 takes the series as it stands\n",
		"    to re-anchor the series on v1.2.0, rebase it with git (it applies there as it stands):\n",
		"      git rebase " + v12 + "\n",
		" --base=" + v12 + " ",
	} {
		if !strings.Contains(status, w) {
			t.Errorf("pack status lacks %q:\n%s\n%s", w, status, errw)
		}
	}

	line, warn := run.PatchedForkLine(fx.fork(t), run.FloorCopy{Commit: rec.Good.Commit, Recipe: rec.Good.Recipe},
		"a fresh launch")
	if warn || !strings.HasSuffix(line, ", at v1.2.0 ("+shortSHA(v12)+"); "+lag+
		"; v1.2.0 takes the series as it stands") {
		t.Errorf("the fork line (warn=%v) does not end with the base lag:\n%s", warn, line)
	}
}

// NO LAG: a series whose base is the newest version says nothing of one.
func TestPackStatusSaysNoBaseLagAtTheNewestVersion(t *testing.T) {
	fx := newPatchedAdvanceFixture(t, "")
	if r, out, _ := fx.launch(t, "podman"); r.delivery.Key == "" {
		t.Fatalf("the first advance built nothing:\n%s", out)
	}
	if _, status, _ := packVerb(t, "status"); strings.Contains(status, "base lag") {
		t.Errorf("pack status names a base lag at the newest version:\n%s", status)
	}
}
