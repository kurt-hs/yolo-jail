package macosuser

// cagodebug_test.go pins OQ-PS16 as ruled 2026-10-23 (PS-D17): in the separate-account mode a Go
// program verifies against the session's CA bundle (GODEBUG x509sslcertoverrideplatform=1, read by
// Go 1.27 and later), so it trusts the login keychain's CAs as curl and git do; a caller's GODEBUG
// keeps every setting it has; a caller who pinned the setting off keeps it and is told the next
// step; run-as-you mode and a caller's own bundle set nothing.

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

const goCAFileOn = goCAFileSetting + "=1"

// launchEnvFile runs an account-mode launch with the login fixture and returns the env file it
// wrote and what it printed. env is the launch's own env layers (nil for none).
func launchEnvFile(t *testing.T, env *jsonx.OrderedMap, self bool) (envFile, printed string) {
	t.Helper()
	sys := newKeychainFixture(t)
	login := newLoginFixture(t, sys)
	var rec []string
	contents := map[string]string{}
	d := loginCADeps(t, &rec, sys, login, contents)
	var out bytes.Buffer
	d.Out = &out
	o := newOpts("/Users/Shared/yolo/proj")
	if self {
		d.SandboxUserExists = func() bool { return false }
		d.RunAsGrant = func() bool { return false }
		d.PathIsDir = func(p string) bool { return p != SandboxHome() }
		o = selfModeOpts("/Users/you/code/alpha")
	}
	if env != nil {
		o.SandboxEnv = env
	}
	if rc := RunMacosUser(d, o); rc != 42 {
		t.Fatalf("rc = %d\n%s", rc, out.String())
	}
	for _, c := range contents {
		if strings.Contains(c, "export SSL_CERT_FILE='") {
			envFile = c
		}
	}
	if envFile == "" {
		t.Fatalf("the launch wrote no env file naming SSL_CERT_FILE:\n%s", strings.Join(rec, "\n"))
	}
	return envFile, out.String()
}

// THE LAUNCH'S CALL SITE: an account-mode launch's env file sets the GODEBUG setting beside the
// bundle, and the launch names it. Fails if applyCATrust stops calling withGoCAFileTrust.
func TestAnAccountLaunchHasGoProgramsVerifyAgainstTheBundle(t *testing.T) {
	envFile, out := launchEnvFile(t, nil, false)
	if v, _ := sandboxEnvFileValue(envFile, godebugVar); v != goCAFileOn {
		t.Errorf("GODEBUG = %q, want %q:\n%s", v, goCAFileOn, envFile)
	}
	if !strings.Contains(out, "The agent runs as a separate account, so GODEBUG sets "+goCAFileOn) {
		t.Errorf("the launch does not name the GODEBUG setting:\n%s", out)
	}
	if !strings.Contains(out, "`go version <path to the program>`") {
		t.Errorf("the launch does not name the check for an older Go:\n%s", out)
	}
	if strings.Contains(out, "do not trust it.") {
		t.Errorf("the launch still prints PS-D14's limit line:\n%s", out)
	}
}

// RUN-AS-YOU MODE asks macOS as you, so it needs and sets nothing.
func TestASelfModeLaunchSetsNoGodebug(t *testing.T) {
	envFile, out := launchEnvFile(t, nil, true)
	if _, set := sandboxEnvFileValue(envFile, godebugVar); set {
		t.Errorf("the run-as-you launch set GODEBUG:\n%s", envFile)
	}
	if strings.Contains(out, goCAFileSetting) {
		t.Errorf("the run-as-you launch names the setting:\n%s", out)
	}
}

// A CALLER'S GODEBUG keeps every setting it has, and gains ours after them.
func TestACallersGodebugIsKeptAndExtended(t *testing.T) {
	envFile, _ := launchEnvFile(t, userCAEnv(godebugVar, "http2debug=1,"), false)
	if v, _ := sandboxEnvFileValue(envFile, godebugVar); v != "http2debug=1,"+goCAFileOn {
		t.Errorf("GODEBUG = %q, want the caller's settings then ours", v)
	}
}

// A CALLER WHO PINNED THE SETTING OFF keeps it, and the launch says what that costs and the next
// step; the launch never refuses.
func TestACallersPinnedOffSettingIsKeptAndNamed(t *testing.T) {
	envFile, out := launchEnvFile(t, userCAEnv(godebugVar, goCAFileSetting+"=0"), false)
	if v, _ := sandboxEnvFileValue(envFile, godebugVar); v != goCAFileSetting+"=0" {
		t.Errorf("GODEBUG = %q, want the caller's own", v)
	}
	for _, want := range []string{"your configured environment sets GODEBUG " + goCAFileSetting,
		"from your login keychain (Staging CA)", "Remove that setting from env_sources and launch again"} {
		if !strings.Contains(out, want) {
			t.Errorf("the launch does not say %q:\n%s", want, out)
		}
	}
}

// A CALLER'S OWN BUNDLE: the keychains' CAs are in no bundle of ours, so GODEBUG is left alone and
// PS-D14's line stays.
func TestACallersBundleSetsNoGodebug(t *testing.T) {
	envFile, out := launchEnvFile(t, userCAEnv("SSL_CERT_FILE", corpBundle), false)
	if _, set := sandboxEnvFileValue(envFile, godebugVar); set {
		t.Errorf("GODEBUG set under the caller's bundle:\n%s", envFile)
	}
	if !strings.Contains(out, "programs that ask macOS instead, such as gh, do not trust it.") {
		t.Errorf("the launch does not keep PS-D14's line:\n%s", out)
	}
}

// THE CAPTURE'S CALL SITE: an account-mode capture's env file carries the setting too.
func TestAnAccountCaptureHasGoProgramsVerifyAgainstTheBundle(t *testing.T) {
	f := newKeychainFixture(t)
	opts := testCaptureOptions()
	opts.CATrust = ComposeCATrust(caDeps(t, nil, f), &Darwin{ProfilePath: testProfile})
	opts.CATrust.SeparateAccount = true
	plan := BuildCapturePlan(opts)
	if !SandboxEnvFileSets(plan.EnvFileContent, godebugVar, goCAFileOn) {
		t.Errorf("the capture's env file does not set GODEBUG:\n%s", plan.EnvFileContent)
	}
}

// THE SETTING'S PARSER reads Go's order: the last mention wins.
func TestGodebugValueReadsTheLastMention(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		named    bool
	}{
		{"", "", false},
		{"http2debug=1", "", false},
		{goCAFileSetting + "=0", "0", true},
		{goCAFileSetting + "=0, " + goCAFileSetting + "=1", "1", true},
	} {
		v, named := godebugValue(tc.in, goCAFileSetting)
		if v != tc.want || named != tc.named {
			t.Errorf("godebugValue(%q) = %q, %v; want %q, %v", tc.in, v, named, tc.want, tc.named)
		}
	}
}

// NOTHING KEPT, NOTHING SET: with no bundle of ours the platform verifier already trusts what the
// bundle would hold.
func TestNoBundleSetsNoGodebug(t *testing.T) {
	env := userCAEnv("SSL_CERT_FILE", "/profile/ca-bundle.crt")
	got := withGoCAFileTrust(env, CATrust{SeparateAccount: true}, "")
	if _, set := got.Get(godebugVar); set {
		t.Error("GODEBUG set with no session bundle")
	}
}
