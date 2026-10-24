package cli

// localclonesrefusal_test.go pins the `local_clones` refusal at every verb that resolves patch branches
// (docs/reference/patched-forks.md PF-D100; the maintainer, 2026-10-24: "It's in your user config. You don't
// want to silently not have this applied."): each refuses with the entry and its next step, and each test fails
// when its verb's call site is deleted.

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/richtext"
)

// brokenLocalClones is a user config whose one `local_clones` entry does not exist.
func brokenLocalClones(t *testing.T) (cfg, missing string) {
	t.Helper()
	missing = filepath.Join(t.TempDir(), "gone")
	return `{"packs": ["claude"], "local_clones": ["` + missing + `"]}`, missing
}

func wantLocalClonesRefusal(t *testing.T, verb, got, missing string) {
	t.Helper()
	for _, want := range []string{"config.local_clones: the entry '" + missing + "' does not exist",
		"fix the path, clone the repository there, or remove the entry from"} {
		if !strings.Contains(got, want) {
			t.Errorf("%s's refusal must say %q:\n%s", verb, want, got)
		}
	}
}

func TestHostLaunchRefusesABrokenLocalClonesEntry(t *testing.T) {
	cfg, missing := brokenLocalClones(t)
	rc, env, errs := hostGateRun(t, cfg, nil, nil, "claude")
	if rc == 0 || env != nil {
		t.Fatalf("yolo host -- claude over a broken local_clones must refuse before the exec: rc=%d\n%s", rc, errs)
	}
	wantLocalClonesRefusal(t, "yolo host --", errs, missing)
}

func TestHostApplyRefusesABrokenLocalClonesEntry(t *testing.T) {
	cfg, missing := brokenLocalClones(t)
	hostGateHome(t, cfg, nil)
	var out, errw bytes.Buffer
	if rc := applyHostSurveyed(&out, &errw, false, true, nil, &hostApplySurvey{}); rc == 0 {
		t.Fatalf("yolo host apply over a broken local_clones must refuse:\n%s%s", out.String(), errw.String())
	}
	wantLocalClonesRefusal(t, "yolo host apply", errw.String(), missing)
}

func TestPackVerbsRefuseABrokenLocalClonesEntry(t *testing.T) {
	cfg, missing := brokenLocalClones(t)
	hostGateHome(t, cfg, nil)
	for _, verb := range []string{"install", "status"} {
		var out, errw bytes.Buffer
		if rc := packMain([]string{verb}, &out, &errw, false); rc == 0 {
			t.Errorf("yolo pack %s over a broken local_clones must refuse:\n%s", verb, out.String())
		}
		wantLocalClonesRefusal(t, "yolo pack "+verb, errw.String(), missing)
	}
}

func TestPackUpdateRefusesABrokenLocalClonesEntry(t *testing.T) {
	cfg, missing := brokenLocalClones(t)
	hostGateHome(t, cfg, nil)
	orig := programRefresh
	programRefresh = func(richtext.Printer, io.Writer) int { return 0 }
	t.Cleanup(func() { programRefresh = orig })
	var out, errw bytes.Buffer
	if rc := packMain([]string{"update"}, &out, &errw, false); rc == 0 {
		t.Fatalf("yolo pack update over a broken local_clones must refuse:\n%s", out.String())
	}
	wantLocalClonesRefusal(t, "yolo pack update", errw.String(), missing)
	if !strings.Contains(errw.String(), "yolo pack update:") {
		t.Errorf("the refusal must name the verb:\n%s", errw.String())
	}
}
