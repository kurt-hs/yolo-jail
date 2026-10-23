package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/macosuser"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// macosModeLaunch runs `yolo -- true` with userCfg as the user config, captures the run.Options
// the front door hands the pipeline, then invokes the macos-user handler it found with the
// backend stubbed, and returns both Options and the handler's exit code. The handler is the
// REAL closure the run command assembled, so deleting the mode's wiring from it fails a test.
func macosModeLaunch(t *testing.T, userCfg string) (run.Options, macosuser.Options, bool, int) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("YOLO_VERSION", "")
	t.Setenv("USER", "you")
	if userCfg != "" {
		p := paths.UserConfigPath()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(userCfg), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var seen run.Options
	prevPipeline := launchRunPipeline
	launchRunPipeline = func(o run.Options) int { seen = o; return 0 }
	t.Cleanup(func() { launchRunPipeline = prevPipeline })
	var backend macosuser.Options
	called := false
	prevBackend := runMacosUserBackend
	runMacosUserBackend = func(_ macosuser.Deps, o macosuser.Options) int { backend, called = o, true; return 7 }
	t.Cleanup(func() { runMacosUserBackend = prevBackend })

	captureBoth(t, func() { _ = Main([]string{"yolo", "--", "true"}) })
	if seen.MacosUserRun == nil {
		t.Fatal("the front door handed the pipeline no macos-user handler")
	}
	var rc int
	captureBoth(t, func() {
		rc = seen.MacosUserRun(jsonx.NewOrderedMap(), t.TempDir(), nil, []string{"true"}, "", "",
			macosuser.HomeOverlay{}, macosuser.HostContext{}, false, jsonx.NewOrderedMap(), nil,
			macosuser.JailDaemons{})
	})
	return seen, backend, called, rc
}

// TestMacosLaunchPassesSelfModeToTheBackend: `"macos_user_mode": "self"` in the user config now
// LAUNCHES (slice 6), and both readers get it from one read: the pipeline (run.Options, which its
// package-cache alias and siting read) and the backend (macosuser.Options), with the invoking
// user's home and name. Fails if either half of the wiring is deleted.
func TestMacosLaunchPassesSelfModeToTheBackend(t *testing.T) {
	seen, backend, called, rc := macosModeLaunch(t, `{"macos_user_mode": "self"}`)
	if !called || rc != 7 {
		t.Fatalf("the handler did not reach the backend (called %v, rc %d)", called, rc)
	}
	if backend.Mode != macosuser.ModeSelf || seen.MacosUserMode != macosuser.ModeSelf {
		t.Errorf("mode: backend %v, pipeline %v; want self for both", backend.Mode, seen.MacosUserMode)
	}
	home, _ := filepath.EvalSymlinks(os.Getenv("HOME"))
	if backend.HostHome != home || seen.MacosUserHostHome != home || backend.HostUser != "you" {
		t.Errorf("identity: backend %q/%q, pipeline %q; want %q/you", backend.HostHome, backend.HostUser,
			seen.MacosUserHostHome, home)
	}
}

// TestMacosLaunchStaysInAccountModeByDefault: no key, or `account`, is the second account, the
// default until slice 11.
func TestMacosLaunchStaysInAccountModeByDefault(t *testing.T) {
	for _, cfg := range []string{"", `{"macos_user_mode": "account"}`} {
		seen, backend, called, _ := macosModeLaunch(t, cfg)
		if !called || backend.Mode != macosuser.ModeAccount || seen.MacosUserMode != macosuser.ModeAccount {
			t.Errorf("config %q: called %v, backend %v, pipeline %v; want account", cfg, called,
				backend.Mode, seen.MacosUserMode)
		}
	}
}

// TestMacosLaunchRefusesAnUnknownMode: a value that slipped past validation never silently picks
// a mode; the line names the key and the next step.
func TestMacosLaunchRefusesAnUnknownMode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("YOLO_VERSION", "")
	p := paths.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`{"macos_user_mode": "jail"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	mode, refusal := macosUserLaunchMode()
	if mode != macosuser.ModeAccount || !strings.Contains(refusal, "macos_user_mode") ||
		!strings.Contains(refusal, `"self" or "account"`) {
		t.Errorf("macosUserLaunchMode() = %v, %q", mode, refusal)
	}
}
