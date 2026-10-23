package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/provision"
)

// programoverride_test.go is the container-level cell for a DEV OVERRIDE
// (docs/design/pack-buildable-programs.md §5): `programs.overrides.<bin>` in the user config makes
// the jail run that program from a checkout in the workspace, the launch names the override, and an
// edit in the checkout shows at the next invocation with no fresh launch — the design's done step 4.
//
// THE PACK'S BUILD IS `exit 3`, and the readiness act is ON (withReadiness), so the test also proves
// the override wins over the build: had the launch built the pack's tree, the build would fail and
// the readiness act would refuse the launch.
//
// HERMETIC: the pack is a `file://` directory, the checkout a shell script in the workspace.

const (
	overrideFixturePack = "overridefx"
	overrideFixtureBin  = "overridefx-x"
)

// overrideFixture writes the pack and the user config overriding its program with dir/dev/x, and
// returns the checkout's script path.
func overrideFixture(t *testing.T, ws string) string {
	t.Helper()
	pack := t.TempDir()
	if err := os.MkdirAll(filepath.Join(pack, "tools", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pack, "tools", "x", "VERSION"), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pack, "pack.json"), []byte(`{"name":"`+overrideFixturePack+
		`","contributes":[{"kind":"program","bin":"`+overrideFixtureBin+`","via":"pack","from":"tools/x",`+
		`"build":"echo the pack build ran; exit 3","run":"x"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	checkout := filepath.Join(ws, "dev", "x")
	if err := os.MkdirAll(checkout, 0o755); err != nil {
		t.Fatal(err)
	}
	packHome(t, `{"packs": [{"source": "file://`+pack+`", "name": "`+overrideFixturePack+`"}], `+
		`"programs": {"overrides": {"`+overrideFixtureBin+`": {"path": "`+checkout+`"}}}}`)
	withPrivateFixtureYoloStore(t)
	t.Setenv(paths.AllowMissingProgramsEnv, "")
	return filepath.Join(checkout, "x")
}

func writeScript(t *testing.T, path, say string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho "+say+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestPackTreeProgramOverrideRunsTheCheckout(t *testing.T) {
	requireJail(t)
	ws := writeProject(t, `{}`)
	script := overrideFixture(t, ws)
	writeScript(t, script, "dev-1")
	overrideLine := overrideFixtureBin + ": dev override — runs " + script

	// 1. A FRESH LAUNCH runs the checkout, names the override, and builds nothing.
	const releaseName = "release-override"
	first := startYoloBackground(t, "first", ws,
		overrideFixtureBin+`; echo FIRST-SESSION-UP; `+
			`for _ in $(seq 1 600); do [ -f /workspace/`+releaseName+` ] && break; sleep 0.5; done`,
		paths.NoProgramReadinessEnv+"=", "YOLO_VERSION=")
	release := func() { _ = os.WriteFile(filepath.Join(ws, releaseName), []byte("go\n"), 0o644) }
	t.Cleanup(release)
	deadline := time.Now().Add(jailTimeout())
	for !strings.Contains(first.combined(), "FIRST-SESSION-UP\n") {
		select {
		case err := <-first.done:
			t.Fatalf("the launch exited (%v) before its session ran:\n%s", err, first.combined())
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("the session never started within %s:\n%s", jailTimeout(), first.combined())
		}
		time.Sleep(50 * time.Millisecond)
	}
	out := first.combined()
	t.Logf("the fresh launch:\n%s", out)
	if !strings.Contains(out, "dev-1") {
		t.Errorf("the fresh launch did not run the checkout (want dev-1)")
	}
	if !strings.Contains(out, overrideLine) {
		t.Errorf("the fresh launch did not name the override (want %q)", overrideLine)
	}
	if strings.Contains(out, "the pack build ran") || strings.Contains(out, "built program "+overrideFixturePack) {
		t.Errorf("the pack's build ran although an override replaces it")
	}
	awaitLaunchLockReleased(t, ws, first)

	// 2. AN EDIT IN THE CHECKOUT shows at the next invocation, on the same jail, with no launch.
	writeScript(t, script, "dev-2")
	r := runCommand(t, ws, append(jailRunArgs(), "--", overrideFixtureBin), withHostSemantics())
	all := r.combined()
	t.Logf("the attach:\n%s", all)
	if r.rc != 0 || !strings.Contains(r.stdout, "Attaching to existing jail") {
		t.Fatalf("rc %d: the second invocation did not attach, so it tested a fresh launch", r.rc)
	}
	if !strings.Contains(all, "dev-2") {
		t.Errorf("the edit did not show at the next invocation (want dev-2)")
	}
	if !strings.Contains(all, overrideLine) {
		t.Errorf("the attach did not name the override (want %q)", overrideLine)
	}

	release()
	if rc := first.wait(t, jailTimeout()); rc != 0 {
		t.Errorf("the first launch rc = %d, want 0:\n%s", rc, first.combined())
	}
}

// TestPackTreeProgramOverrideWithNoFileRefusesTheLaunch: an override whose file is missing refuses
// the launch through the readiness act (OQ-BG1's leaning), naming the file, the key and the pack's
// build line, and never runs the pack's own copy.
func TestPackTreeProgramOverrideWithNoFileRefusesTheLaunch(t *testing.T) {
	requireJail(t)
	ws := writeProject(t, `{}`)
	script := overrideFixture(t, ws)
	r := runCommand(t, ws, append(jailRunArgs(), "--", overrideFixtureBin), withHostSemantics(), withReadiness())
	all := r.combined()
	t.Logf("the launch with no file in the checkout:\n%s", all)
	if r.rc != provision.StageRefusedStatus {
		t.Fatalf("rc = %d, want the readiness refusal's %d", r.rc, provision.StageRefusedStatus)
	}
	for what, want := range map[string]string{
		"the program":    "program " + overrideFixtureBin + " (pack " + overrideFixturePack + ")",
		"the file":       script + " does not exist",
		"the key":        "programs.overrides." + overrideFixtureBin,
		"the build line": "echo the pack build ran; exit 3",
		"the hatch":      paths.AllowMissingProgramsEnv + "=1",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("the refusal does not name %s (%q)", what, want)
		}
	}
}
