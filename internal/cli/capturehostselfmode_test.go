package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/macosuser"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// capturehostselfmode_test.go pins slice 9 of docs/design/macos-user-model-plan.md at its CLI call
// sites: with `"macos_user_mode": "self"` in the user config, `yolo capture`'s macos-user arm hands
// the act the run-as-you mode and the invoking user's identity, and the act's plan crosses into no
// account; and the host floor's Mac predicate asks no sandbox account.

// withSelfModeUserConfig adds `"macos_user_mode": "self"` to the user config captureFixtureHome wrote.
func withSelfModeUserConfig(t *testing.T) {
	t.Helper()
	// Out of a jail, as on a Mac: in-jail, the user config is the generated snapshot.
	t.Setenv("YOLO_VERSION", "")
	p := paths.UserConfigPath()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	s := strings.Replace(string(b), "{", `{"macos_user_mode": "self", `, 1)
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

// THE CAPTURE ARM PASSES THE MODE: it invokes the real closure on its dry-run path, through the
// real act, and reads both the options the act was handed and the plan it printed. Fails if the
// closure stops reading macos_user_mode, or drops Mode, HostHome or HostUser on the way to the act.
func TestCaptureHandsTheMacosUserActTheRunAsYouMode(t *testing.T) {
	captureFixtureHome(t, captureFixtureInstaller)
	withSelfModeUserConfig(t)
	t.Setenv("USER", "you")
	var seen run.Options
	withFakeCaptureJail(t, fakeCaptureJail(t, &seen, []capture.ManifestEntry{
		{Path: ".local", Kind: capture.KindDir, Mode: "0755"},
	}))
	var got []macosuser.CaptureOptions
	orig := macCaptureAct
	macCaptureAct = func(d macosuser.Deps, o macosuser.CaptureOptions, dest string, dryRun bool) int {
		got = append(got, o)
		return orig(d, o, dest, dryRun)
	}
	t.Cleanup(func() { macCaptureAct = orig })

	var out, errw bytes.Buffer
	if rc := captureHost([]string{"probetool"}, &out, &errw, false); rc != 0 {
		t.Fatalf("captureHost rc = %d, stderr = %s", rc, errw.String())
	}
	if seen.MacosUserRun == nil {
		t.Fatal("MacosUserRun is nil: the macos-user arm is unwired")
	}
	before := out.Len()
	_ = seen.MacosUserRun(jsonx.NewOrderedMap(), "", nil, nil, "", "", macosuser.HomeOverlay{},
		macosuser.HostContext{}, true, jsonx.NewOrderedMap(), []packload.BlockedTool{}, macosuser.JailDaemons{})
	plan := out.String()[before:]

	if len(got) != 1 {
		t.Fatalf("the macos-user capture act ran %d times, want 1", len(got))
	}
	home, _ := filepath.EvalSymlinks(os.Getenv("HOME"))
	if o := got[0]; o.Mode != macosuser.ModeSelf || o.HostHome != home || o.HostUser != "you" {
		t.Errorf("the act was handed mode %v, home %q, user %q; want self, %q, you", o.Mode, o.HostHome,
			o.HostUser, home)
	}
	if strings.Contains(plan, "/usr/bin/sudo") || strings.Contains(plan, macosuser.SandboxUser) {
		t.Errorf("the run-as-you capture plan crosses into the account:\n%s", plan)
	}
	if !strings.Contains(plan, "as you, inside the capture profile") ||
		!strings.Contains(plan, "all capture plan invariants hold") {
		t.Errorf("the run-as-you capture plan is not the run-as-you one, or is not viable:\n%s", plan)
	}
}

// With no key the arm hands the act the account mode, and no identity, so the act's own HostUser
// (Deps.HostUser) places the account's staging tree as before.
func TestCaptureHandsTheMacosUserActTheAccountModeByDefault(t *testing.T) {
	captureFixtureHome(t, captureFixtureInstaller)
	var seen run.Options
	withFakeCaptureJail(t, fakeCaptureJail(t, &seen, []capture.ManifestEntry{
		{Path: ".local", Kind: capture.KindDir, Mode: "0755"},
	}))
	var got []macosuser.CaptureOptions
	orig := macCaptureAct
	macCaptureAct = func(_ macosuser.Deps, o macosuser.CaptureOptions, _ string, _ bool) int {
		got = append(got, o)
		return 0
	}
	t.Cleanup(func() { macCaptureAct = orig })
	var out, errw bytes.Buffer
	if rc := captureHost([]string{"probetool"}, &out, &errw, false); rc != 0 {
		t.Fatalf("captureHost rc = %d, stderr = %s", rc, errw.String())
	}
	_ = seen.MacosUserRun(jsonx.NewOrderedMap(), "", nil, nil, "", "", macosuser.HomeOverlay{},
		macosuser.HostContext{}, true, jsonx.NewOrderedMap(), nil, macosuser.JailDaemons{})
	if len(got) != 1 || got[0].Mode != macosuser.ModeAccount || got[0].HostHome != "" || got[0].HostUser != "" {
		t.Errorf("the default capture was handed %+v; want the account mode and no identity", got)
	}
}

// THE FLOOR'S MAC PREDICATE IN THE RUN-AS-YOU MODE asks no sandbox account, and its privilege facts
// are the staging root's, in words that say the act runs as you. Fails if hostFloorMacProbes stops
// reading the mode, or macActBlocked asks the account in self mode.
func TestAMacFloorInSelfModeAsksNoSandboxAccount(t *testing.T) {
	captureFixtureHome(t, captureFixtureInstaller)
	withSelfModeUserConfig(t)
	// The production probes (this package's init disarms hostFloorMacProbes itself); building them
	// asks the machine nothing.
	if got := realHostFloorMacProbes().Mode; got != macosuser.ModeSelf {
		t.Fatalf("the floor's Mac probes read mode %v, want self", got)
	}
	p := macSetup{}.probes()
	p.Mode = macosuser.ModeSelf
	p.Privilege = func() (macosuser.Precondition, bool) { return macosuser.Precondition{}, false }
	if got := macCaptureBlocked(p, "captures it"); got != "" {
		t.Errorf("a run-as-you Mac with no sandbox account cannot capture: %s", got)
	}
	var staging macosuser.Precondition
	for _, c := range macosuser.LaunchPreconditions() {
		if c.ID == macosuser.PreconditionStagingRootOwner {
			staging = c
		}
	}
	p.Privilege = func() (macosuser.Precondition, bool) { return staging, true }
	if got := macBuildBlocked(p, "builds it"); !strings.Contains(got, "runs as you") ||
		strings.Contains(got, "sudo rule") {
		t.Errorf("a run-as-you Mac with a foreign staging root says %q", got)
	}
}
