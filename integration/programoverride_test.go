package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/provision"
)

// programoverride_test.go is the container-level cell for a DEV OVERRIDE
// (docs/design/pack-buildable-programs.md §5, OQ-BG3's ruling): `programs.overrides.<bin>` in the
// user config makes each fresh launch BUILD that program from the user's checkout, in place of its
// pack's tree, with the pack's own build line, keyed on the checkout's tree digest — the design's done
// step 4: set the override; relaunch, and it builds from the checkout and the launch names it;
// relaunch unchanged, and nothing is rebuilt; edit the checkout and relaunch, and it rebuilds.
//
// HERMETIC: the pack is a `file://` directory and the checkout a directory beside it, each holding a
// VERSION file; the pack's build line writes a script printing that VERSION. The capture store is a
// private fixture one (withPrivateFixtureYoloStore).

const (
	overrideFixturePack = "overridefx"
	overrideFixtureBin  = "overridefx-x"
	overrideBuiltLine   = "built program " + overrideFixturePack + "/" + overrideFixtureBin + " (built from your checkout "
)

// overrideFixture writes the pack, whose tools/x says "pack", and a checkout outside any workspace,
// and the user config overriding the program with that checkout. It returns the checkout and the
// capture store. The build fails where the tree has no VERSION, so a checkout without one breaks it.
func overrideFixture(t *testing.T) (checkout, store string) {
	t.Helper()
	pack := t.TempDir()
	writeFile := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(filepath.Join(pack, "tools", "x", "VERSION"), "pack\n")
	build := `test -f VERSION && printf '#!/bin/sh\necho x-%s\n' "$(cat VERSION)" > x`
	buildJSON := strings.ReplaceAll(strings.ReplaceAll(build, `\`, `\\`), `"`, `\"`)
	writeFile(filepath.Join(pack, "pack.json"), `{"name":"`+overrideFixturePack+`","contributes":[{"kind":"program",`+
		`"bin":"`+overrideFixtureBin+`","via":"pack","from":"tools/x","build":"`+buildJSON+`","run":"x"}]}`)
	checkout, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	packHome(t, `{"packs": [{"source": "file://`+pack+`", "name": "`+overrideFixturePack+`"}], `+
		`"programs": {"overrides": {"`+overrideFixtureBin+`": {"path": "`+checkout+`"}}}}`)
	withPrivateFixtureYoloStore(t)
	t.Setenv(paths.AllowMissingProgramsEnv, "")
	store = filepath.Join(os.Getenv("HOME"), ".local", "share", "yolo-jail", "captures")
	before := captureEntryNames(t, store)
	t.Cleanup(func() { removeNewCaptureEntries(t, store, before, overrideFixtureBin) })
	return checkout, store
}

func setCheckoutVersion(t *testing.T, checkout, v string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(checkout, "VERSION"), []byte(v+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPackTreeProgramOverrideBuildsTheCheckout(t *testing.T) {
	requireJail(t)
	checkout, _ := overrideFixture(t)
	setCheckoutVersion(t, checkout, "dev1")
	key := "(programs.overrides." + overrideFixtureBin + " in ~/.config/yolo-jail/config.jsonc)"
	named := overrideFixtureBin + ": built from your checkout " + checkout + " " + key

	launch := func(what string) string {
		t.Helper()
		r := runCommand(t, t.TempDir(), append(jailRunArgs(), "--", overrideFixtureBin), withHostSemantics(), withReadiness())
		out := r.combined()
		t.Logf("%s:\n%s", what, out)
		if r.rc != 0 {
			t.Fatalf("%s: rc %d", what, r.rc)
		}
		if strings.Contains(out, "x-pack") {
			t.Fatalf("%s: the pack's own tree ran although the override names a checkout", what)
		}
		return out
	}

	// 1. SET THE OVERRIDE, LAUNCH: it builds from the checkout once, runs it, and the launch names it.
	out := launch("the first launch")
	if !strings.Contains(out, "x-dev1") || strings.Count(out, overrideBuiltLine) != 1 ||
		!strings.Contains(out, named+" — rebuilt at tree digest ") {
		t.Errorf("the first launch did not build the checkout once, run x-dev1 and name it (%q)", named)
	}

	// 2. RELAUNCH UNCHANGED: no rebuild, and the line says the build was reused.
	out = launch("the launch with no change")
	if !strings.Contains(out, "x-dev1") || strings.Contains(out, overrideBuiltLine) ||
		!strings.Contains(out, named+" — reused its build at tree digest ") {
		t.Errorf("the launch with no change rebuilt, did not run x-dev1, or did not say it reused the build")
	}

	// 3. EDIT THE CHECKOUT (uncommitted, no git at all), RELAUNCH: it rebuilds and runs the edit.
	setCheckoutVersion(t, checkout, "dev2")
	out = launch("the launch after the edit")
	if !strings.Contains(out, "x-dev2") || strings.Count(out, overrideBuiltLine) != 1 ||
		!strings.Contains(out, named+" — rebuilt at tree digest ") {
		t.Errorf("the launch after the edit did not rebuild once and run x-dev2")
	}
}

// TestPackTreeProgramOverrideBuildFailureRefusesTheLaunch: a checkout the pack's build fails on
// refuses the launch through the readiness act (OQ-BG1), naming the program, its retry and the
// hatch, and never runs the pack's own tree in its place.
func TestPackTreeProgramOverrideBuildFailureRefusesTheLaunch(t *testing.T) {
	requireJail(t)
	checkout, _ := overrideFixture(t) // no VERSION in the checkout: the build fails
	r := runCommand(t, t.TempDir(), append(jailRunArgs(), "--", overrideFixtureBin), withHostSemantics(), withReadiness())
	all := r.combined()
	t.Logf("the launch whose checkout does not build:\n%s", all)
	if r.rc != provision.StageRefusedStatus {
		t.Fatalf("rc = %d, want the readiness refusal's %d", r.rc, provision.StageRefusedStatus)
	}
	for what, want := range map[string]string{
		"the program":  overrideFixtureBin,
		"the checkout": "your checkout " + checkout,
		"the retry":    "yolo pack build " + overrideFixturePack,
		"the hatch":    paths.AllowMissingProgramsEnv + "=1",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("the refusal does not name %s (%q)", what, want)
		}
	}
	if strings.Contains(all, "x-pack") {
		t.Errorf("the pack's own tree ran in the failed checkout's place")
	}
}
