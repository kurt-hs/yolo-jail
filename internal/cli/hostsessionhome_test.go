package cli

// hostsessionhome_test.go pins `yolo host`'s per-launch SESSION HOME (docs/design/per-session-
// profiles.md §4.7, ruled by OQ-SP4; "session home" is a term coined there: a per-session copy of
// the one directory a pack's program reads its config from, its rendered files the session's own
// and everything else linked to the shared copy). Every cell runs hostMain to the exec, so the
// declaration, the render and the export are asked through the front door: deleting the call in
// hostLaunch fails every cell here.
//
// The variable's name is read from the shipped pi pack's own declaration, never spelled here.

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/prune"
	"github.com/mschulkind-oss/yolo-jail/internal/render"
)

// piSessionHomeEnv is the variable the shipped pi pack's session-home declaration names.
func piSessionHomeEnv(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(shippedPiPackDir, "pack.json"))
	if err != nil {
		t.Fatal(err)
	}
	m, problems := packdecl.Decode(data)
	if len(problems) > 0 {
		t.Fatalf("the shipped pi pack does not decode: %v", problems)
	}
	homes := m.SessionHomes()
	if len(homes) != 1 || homes[0].Env == "" {
		t.Fatalf("the shipped pi pack declares %d session homes, want one with a variable", len(homes))
	}
	return homes[0].Env
}

func ownedSessionHomeConfig(cfg string) string {
	return strings.Replace(cfg, `{"packs"`, `{"host_management":"own","host_apply_on_launch":false,"packs"`, 1)
}

// treeSnapshot is every regular file and link under root, with its bytes or target.
func treeSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		if d.Type()&fs.ModeSymlink != 0 {
			target, _ := os.Readlink(p)
			out[rel] = "link:" + target
			return nil
		}
		data, _ := os.ReadFile(p)
		out[rel] = string(data)
		return nil
	})
	return out
}

// hostCaptureStores are the real home's host capture, provenance and archive stores: a session
// render keeps its bookkeeping in its own throwaway home, so none of them may gain a file.
func hostCaptureStores(home string) []string {
	gs := paths.GlobalStorageUnder(home)
	return []string{filepath.Join(gs, "host-capture"), filepath.Join(gs, "host-provenance"),
		filepath.Join(gs, "archive")}
}

// THE RULING, END TO END: `yolo host -p pi=codex -- pi` over a configured openrouter profile hands
// pi a session home in yolo's state dir, its settings.json the -p's, auth.json a link to the real
// one, and leaves every byte of the real ~/.pi/agent and the real capture stores as it was.
func TestHostPiLaunchGetsItsOwnSessionHome(t *testing.T) {
	env := piSessionHomeEnv(t)
	// The user opted into host ownership, which is required for every command launch, not just
	// for an explicit profile selection.
	home := hostGateHome(t, ownedSessionHomeConfig(piSubagentsCodexCfg), nil)
	unchanged := piSettingsFixture(t, home, `{"defaultProvider":"openrouter","theme":"dark"}`)
	shared := filepath.Join(home, ".pi", "agent")
	before := treeSnapshot(t, shared)
	run := hostSelectionRun(t, home, []string{"-p", "pi=codex"}, "pi")
	unchanged()
	after := treeSnapshot(t, shared)
	for rel, data := range before {
		if after[rel] != data {
			t.Errorf("the real %s changed:\n%s", rel, after[rel])
		}
	}
	for rel := range after {
		if _, ok := before[rel]; !ok {
			t.Errorf("the launch wrote %s into the real %s", rel, shared)
		}
	}
	for _, store := range hostCaptureStores(home) {
		if got := treeSnapshot(t, store); len(got) > 0 {
			t.Errorf("the session render wrote into the real store %s: %v", store, got)
		}
	}
	dir := run.env[env]
	root := filepath.Join(paths.GlobalStorageUnder(home), prune.HostSessionHomesLeaf)
	if !strings.HasPrefix(dir, root+string(filepath.Separator)) {
		t.Fatalf("pi got %s=%q, want a directory under %s\n%s", env, dir, root, run.errs)
	}
	var settings struct {
		DefaultProvider string `json:"defaultProvider"`
		Theme           string `json:"theme"`
	}
	data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatalf("the session home has no settings.json: %v\n%s", err, run.errs)
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("the session's settings.json is not JSON: %v\n%s", err, data)
	}
	if settings.DefaultProvider != "openai-codex" {
		t.Errorf("the session's defaultProvider is %q, want the -p's openai-codex:\n%s", settings.DefaultProvider, data)
	}
	if settings.Theme != "dark" {
		t.Errorf("the session's settings.json lost the user's own key (theme %q):\n%s", settings.Theme, data)
	}
	if fi, err := os.Lstat(filepath.Join(dir, "settings.json")); err != nil || fi.Mode()&fs.ModeSymlink != 0 {
		t.Errorf("the session's settings.json must be its own file, not a link")
	}
	if got, err := os.Readlink(filepath.Join(dir, "auth.json")); err != nil || got != filepath.Join(shared, "auth.json") {
		t.Errorf("auth.json must link to the real one: %q, %v", got, err)
	}
	if got, err := os.Readlink(filepath.Join(dir, "sessions")); err != nil || got != filepath.Join(shared, "sessions") {
		t.Errorf("sessions must link to the real directory: %q, %v", got, err)
	}
	// The lease that keeps the home from being reaped while pi runs (the exec stub returned, so
	// this process holds it, as pi would after a real exec).
	state, unlock, _ := packload.ProbeLease(filepath.Dir(filepath.Dir(filepath.Dir(dir))))
	unlock()
	if state != packload.LeaseHeld {
		t.Errorf("the launch's session home lease is %s, want held across the exec", state)
	}
}

// UNDER `host_management: own` the session's render folds in the real capture store's captured
// edits, as the next `yolo host apply` would, and reads that store without writing a byte of it.
func TestHostPiSessionHomeFoldsTheCapturedEditsUnderOwn(t *testing.T) {
	env := piSessionHomeEnv(t)
	// The apply-on-launch gate is off: it renders the real home by design, and this cell asks
	// what the session render writes.
	cfg := strings.Replace(piSubagentsCodexCfg, `{"packs"`, `{"host_management": "own", "host_apply_on_launch": false, "packs"`, 1)
	home := hostGateHome(t, cfg, nil)
	const settings = `{"defaultProvider":"openrouter"}`
	unchanged := piSettingsFixture(t, home, settings)
	real := render.Host(home, nil, render.OwnershipOwn)
	writeFile(t, real.OverlayPath("pi", "settings"), `{"theme":"captured"}`)
	writeFile(t, real.LastRenderPath("pi", "settings"), settings+"\n")
	stores := map[string]map[string]string{}
	for _, s := range hostCaptureStores(home) {
		stores[s] = treeSnapshot(t, s)
	}
	run := hostSelectionRun(t, home, []string{"-p", "pi=codex"}, "pi")
	unchanged()
	for s, before := range stores {
		after := treeSnapshot(t, s)
		if len(after) != len(before) {
			t.Errorf("the real store %s changed: %v -> %v", s, before, after)
		}
		for rel, data := range before {
			if after[rel] != data {
				t.Errorf("the real store's %s changed:\n%s", rel, after[rel])
			}
		}
	}
	data, err := os.ReadFile(filepath.Join(run.env[env], "settings.json"))
	if err != nil {
		t.Fatalf("no session settings.json: %v\n%s", err, run.errs)
	}
	var got struct {
		DefaultProvider, Theme string
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.DefaultProvider != "openai-codex" || got.Theme != "captured" {
		t.Errorf("the session's settings.json must hold the -p's provider and the captured theme:\n%s", data)
	}
}

func TestHostPiSessionHomeCarriesTheProfilesChildPolicy(t *testing.T) {
	env := piSessionHomeEnv(t)
	home := hostGateHome(t, ownedSessionHomeConfig(piSubagentsAcmeCfg), nil)
	unchanged := piSettingsFixture(t, home, `{"defaultProvider":"acme"}`)
	runB := hostSelectionRun(t, home, []string{"-p", "pi=acme-b"}, "pi")
	// Typed, since the launch under host_management=own creates a fresh, profile-specific home.
	runA := hostSelectionRun(t, home, []string{"-p", "pi=acme-a"}, "pi")
	unchanged()
	type policy struct {
		Subagents map[string]any `json:"subagents"`
	}
	read := func(dir string) policy {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
		if err != nil {
			t.Fatalf("no settings.json in %s: %v", dir, err)
		}
		var p policy
		if err := json.Unmarshal(data, &p); err != nil {
			t.Fatal(err)
		}
		if p.Subagents == nil {
			t.Fatalf("the session's settings.json in %s has no subagents block:\n%s", dir, data)
		}
		return p
	}
	if runA.env[env] == "" || runB.env[env] == "" || runA.env[env] == runB.env[env] {
		t.Fatalf("each launch needs its own session home: %q and %q\n%s", runA.env[env], runB.env[env], runB.errs)
	}
	a, b := read(runA.env[env]), read(runB.env[env])
	aj, _ := json.Marshal(a.Subagents)
	bj, _ := json.Marshal(b.Subagents)
	if string(aj) == string(bj) {
		t.Errorf("-p acme-b's child policy is the configured acme-a's: %s", bj)
	}
	if !strings.Contains(string(bj), "acme-mini") {
		t.Errorf("-p acme-b's child policy must default to its fast model: %s", bj)
	}
}

func TestHostPiSessionHomeYieldsToThePresetVariable(t *testing.T) {
	env := piSessionHomeEnv(t)
	own := t.TempDir()
	home := hostGateHome(t, ownedSessionHomeConfig(piSubagentsCodexCfg), map[string]string{env: own})
	piSettingsFixture(t, home, `{"defaultProvider":"openrouter"}`)
	run := hostSelectionRun(t, home, []string{"-p", "pi=codex"}, "pi")
	if run.env[env] != own {
		t.Errorf("pi got %s=%q, want the user's own %q", env, run.env[env], own)
	}
	if n := strings.Count(run.errs, env+" is already set"); n != 1 {
		t.Errorf("want one line naming the preset %s, got %d:\n%s", env, n, run.errs)
	}
	root := filepath.Join(paths.GlobalStorageUnder(home), prune.HostSessionHomesLeaf)
	if entries, _ := os.ReadDir(root); len(entries) > 0 {
		t.Errorf("a preset variable must make no session home, found %d in %s", len(entries), root)
	}
}

// SP-R1: NONE OR UNSET REFUSES EVERY HOST COMMAND LAUNCH before trace creation, profile
// composition, readiness, credentials/services, session-home creation, or the exec. The
// configuration-management verbs use a different hostMain route and remain outside this gate.
func TestHostCommandLaunchRequiresOwnBeforeAssistance(t *testing.T) {
	for _, mgmt := range []string{"none", ""} {
		name := mgmt
		if name == "" {
			name = "unset"
		}
		for _, flags := range [][]string{nil, {"-p", "pi=codex"}, {"--with-credentials", "all"}} {
			t.Run(name+"/"+strings.Join(flags, "-"), func(t *testing.T) {
				cfg := piSubagentsCodexCfg
				if mgmt != "" {
					cfg = strings.Replace(cfg, `{"packs"`, `{"host_management":"none","packs"`, 1)
				}
				home := hostGateHomeRaw(t, cfg, nil)
				bin := filepath.Join(t.TempDir(), "bin")
				writeFile(t, filepath.Join(bin, "pi"), "#!/bin/sh\nexit 0\n")
				if err := os.Chmod(filepath.Join(bin, "pi"), 0o755); err != nil {
					t.Fatal(err)
				}
				t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
				execs := 0
				orig := hostSyscallExec
				hostSyscallExec = func(string, []string, []string) error { execs++; return nil }
				t.Cleanup(func() { hostSyscallExec = orig })
				var out, errw bytes.Buffer
				argv := append(append([]string{}, flags...), "--", "pi")
				rc := hostMain(argv, &out, &errw, false, nil)
				if rc == 0 || execs != 0 || !strings.Contains(errw.String(), "host_management") ||
					!strings.Contains(errw.String(), "own") {
					t.Fatalf("host launch under %s must refuse before exec (rc=%d execs=%d):\n%s", name, rc, execs, errw.String())
				}
				if _, err := os.Stat(paths.GlobalStorageUnder(home)); !os.IsNotExist(err) {
					t.Errorf("refusal created yolo host state before launch assistance was allowed: %v", err)
				}
			})
		}
	}
}

// Under own, the configured profile applies to a bare launch and a typed profile overrides it.
func TestHostPiSessionHomeUsesConfiguredOrExplicitProfile(t *testing.T) {
	env := piSessionHomeEnv(t)
	cfg := strings.Replace(piSubagentsCodexCfg, `{"packs"`, `{"host_management":"own","host_apply_on_launch":false,"packs"`, 1)
	home := hostGateHome(t, cfg, nil)
	piSettingsFixture(t, home, `{"defaultProvider":"openrouter"}`)
	for _, tc := range []struct {
		flags []string
		want  string
	}{{nil, "openrouter"}, {[]string{"-p", "pi=codex"}, "openai-codex"}} {
		run := hostSelectionRun(t, home, tc.flags, "pi")
		var settings map[string]any
		data, err := os.ReadFile(filepath.Join(run.env[env], "settings.json"))
		if err != nil {
			t.Fatalf("read session settings: %v", err)
		}
		if err := json.Unmarshal(data, &settings); err != nil {
			t.Fatal(err)
		}
		if got := settings["defaultProvider"]; got != tc.want {
			t.Errorf("profile settings defaultProvider=%v, want %q", got, tc.want)
		}
	}
}

// An owned resident session captures only user edits through the host overlay; computed profile
// values regenerate on the next selection, and the native settings file remains untouched.
func TestHostPiResidentSessionCapturesUserEditNormally(t *testing.T) {
	env := piSessionHomeEnv(t)
	upstream, _ := fakeUpstream(t)
	fakeHostBroker(t)
	cfg := strings.TrimSuffix(piSubagentsCodexCfg, "}") +
		`,"host_management":"own","host_apply_on_launch":false,"providers":{"openai-codex":{"endpoints":{"openai-responses":{"base_url":"` +
		upstream.URL + `/codex"}}}}}`
	home := hostGateHome(t, cfg, nil)
	const original = `{"defaultProvider":"openrouter","theme":"system"}`
	unchanged := piSettingsFixture(t, home, original)
	bin := t.TempDir()
	agent := filepath.Join(bin, "claude")
	if err := os.WriteFile(agent, []byte("#!/bin/sh\nprintf '%s\\n' '{\"customUserSetting\":\"saved-by-user\"}' > \"$PI_CODING_AGENT_DIR/settings.json\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	origExec := hostSyscallExec
	hostSyscallExec = func(string, []string, []string) error { t.Error("the resident launch exec'd"); return nil }
	t.Cleanup(func() { hostSyscallExec = origExec })
	var out, errw bytes.Buffer
	rc := hostMain([]string{"-p", "codex", "--", "claude"}, &out, &errw, false, nil)
	if rc != 0 {
		t.Fatalf("resident host launch rc=%d:\\n%s", rc, errw.String())
	}
	unchanged()
	overlayPath := render.Host(home, nil, render.OwnershipOwn).OverlayPath("pi", "settings")
	overlay, err := os.ReadFile(overlayPath)
	if err != nil {
		t.Fatalf("resident session did not persist the normal host capture overlay: %v\\n%s", err, errw.String())
	}
	var captured map[string]any
	if err := json.Unmarshal(overlay, &captured); err != nil {
		t.Fatalf("capture overlay is invalid: %v (%s)", err, overlay)
	}
	if captured["customUserSetting"] != "saved-by-user" || captured["defaultProvider"] != nil || captured["defaultModel"] != nil {
		t.Errorf("capture should contain only the user edit, not the profile render: %s", overlay)
	}
	next := hostSelectionRun(t, home, []string{"-p", "pi=openrouter"}, "pi")
	var settings map[string]any
	data, err := os.ReadFile(filepath.Join(next.env[env], "settings.json"))
	if err != nil || json.Unmarshal(data, &settings) != nil {
		t.Fatalf("read next session settings: %v (%s)", err, data)
	}
	if settings["defaultProvider"] != "openrouter" {
		t.Errorf("next profile did not regenerate its provider: %s", data)
	}
}

// An ended exec's retained session-home render is captured through the same host sidecars before
// the reaper removes it. The explicit profile is part of the stored baseline, so capture excludes
// it while preserving only the session user's theme edit.
func TestHostPiEndedSessionHomeIsCapturedBeforeReap(t *testing.T) {
	cfg := ownedSessionHomeConfig(piSubagentsCodexCfg)
	home := hostGateHome(t, cfg, nil)
	piSettingsFixture(t, home, `{"defaultProvider":"openrouter","theme":"system"}`)
	launch := composeHostLaunch("pi", "codex", nil, nil)
	homes, _, err := prepareHostSessionHomes(launch, os.Environ(), io.Discard)
	if err != nil || homes == nil || len(homes.dirs) != 1 {
		t.Fatalf("prepare session home: homes=%+v err=%v", homes, err)
	}
	dir := homes.dirs[0]
	sessionSettings := filepath.Join(homes.vars[0][strings.Index(homes.vars[0], "=")+1:], "settings.json")
	data, err := os.ReadFile(sessionSettings)
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	settings["customUserSetting"] = "saved-by-user"
	data, err = json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sessionSettings, data, 0o600); err != nil {
		t.Fatal(err)
	}
	// The exec has ended: release its lease and make the home old enough for the normal reaper.
	homes.preserveForRetry()
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(dir, old, old); err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(dir)
	r := prune.ReapHostSessionHomes(root, true, time.Now(), func(path string) error {
		return captureHostSessionHomePath(path, io.Discard)
	})
	if len(r.Removed) != 1 || r.Removed[0].Err != nil || existsHostPath(dir) {
		t.Fatalf("ended home was not captured and reaped: %+v exists=%v", r.Removed, existsHostPath(dir))
	}
	overlay, err := os.ReadFile(render.Host(home, nil, render.OwnershipOwn).OverlayPath("pi", "settings"))
	if err != nil {
		t.Fatalf("reaper did not persist the normal host capture overlay: %v", err)
	}
	var captured map[string]any
	if err := json.Unmarshal(overlay, &captured); err != nil {
		t.Fatal(err)
	}
	if captured["customUserSetting"] != "saved-by-user" || captured["defaultProvider"] != nil {
		t.Errorf("capture included generated profile values or lost the user edit: %s", overlay)
	}
}

func TestHostPiResidentCaptureRefusalKeepsHomeForRetry(t *testing.T) {
	home := hostGateHome(t, ownedSessionHomeConfig(piSubagentsCodexCfg), nil)
	piSettingsFixture(t, home, `{"defaultProvider":"openrouter"}`)
	launch := composeHostLaunch("pi", "codex", nil, nil)
	homes, _, err := prepareHostSessionHomes(launch, os.Environ(), io.Discard)
	if err != nil || homes == nil || len(homes.dirs) != 1 {
		t.Fatalf("prepare session home: homes=%+v err=%v", homes, err)
	}
	dir := homes.dirs[0]
	_, sessionHome, ok := strings.Cut(homes.vars[0], "=")
	if !ok {
		t.Fatalf("malformed session-home variable %q", homes.vars[0])
	}
	settingsPath := filepath.Join(sessionHome, "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{"customUserSetting":"saved-by-user"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	writer := tryHostApplyLock(paths.Home())
	if writer == nil {
		t.Fatal("could not take host-apply lock for the concurrency fixture")
	}
	if err := finishHostSessionHomes(homes, io.Discard); err == nil {
		t.Fatal("capture must refuse while another yolo writer holds the host lock")
	}
	if !homes.preserved {
		t.Fatal("a refused resident capture did not preserve the source for retry")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("the resident session source was removed after capture refused: %v", err)
	}
	writer.Close()
	if err := finishHostSessionHomes(homes, io.Discard); err != nil {
		t.Fatalf("retry after the writer finished: %v", err)
	}
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Errorf("successful retry did not remove the ended home: %v", err)
	}
	overlay, err := os.ReadFile(render.Host(home, nil, render.OwnershipOwn).OverlayPath("pi", "settings"))
	if err != nil {
		t.Fatalf("retry did not write the host capture overlay: %v", err)
	}
	var captured map[string]any
	if err := json.Unmarshal(overlay, &captured); err != nil {
		t.Fatal(err)
	}
	if captured["customUserSetting"] != "saved-by-user" {
		t.Errorf("retry did not capture the session edit: %s", overlay)
	}
}

func existsHostPath(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// The host env command is an inspection verb, not a command launch, so it remains available under
// `none` and must not enter the SP-R1 launch gate.
func TestHostEnvRemainsAvailableWhenCommandLaunchesAreRefused(t *testing.T) {
	hostGateHomeRaw(t, `{"host_management":"none","packs":["pi"]}`, nil)
	var out, errw bytes.Buffer
	rc := hostMain([]string{"env", "--agent", "pi"}, &out, &errw, false, nil)
	if rc != 0 || strings.Contains(errw.String(), "refusing to launch") {
		t.Fatalf("host env is an inspection command under none (rc=%d):\n%s", rc, errw.String())
	}
}

// A DEAD LAUNCH'S HOME IS REAPED by the next launch: one whose lease nobody holds goes, one whose
// lease is held (a pi still running) stays.
func TestHostPiSessionHomeReapsADeadLaunch(t *testing.T) {
	env := piSessionHomeEnv(t)
	home := hostGateHome(t, ownedSessionHomeConfig(piSubagentsCodexCfg), nil)
	piSettingsFixture(t, home, `{"defaultProvider":"openrouter"}`)
	root := filepath.Join(paths.GlobalStorageUnder(home), prune.HostSessionHomesLeaf)
	old := time.Now().Add(-2 * time.Hour)
	mk := func(name string) string {
		t.Helper()
		dir := filepath.Join(root, name)
		writeFile(t, filepath.Join(dir, packload.EmbeddedLeaseName), "")
		writeFile(t, filepath.Join(dir, "home", ".pi", "agent", "settings.json"), "{}")
		if err := os.Chtimes(dir, old, old); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	dead, live := mk("pi-dead"), mk("pi-live")
	f, err := os.Open(filepath.Join(live, packload.EmbeddedLeaseName))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_SH); err != nil {
		t.Fatal(err)
	}
	run := hostSelectionRun(t, home, []string{"-p", "pi=codex"}, "pi")
	if run.env[env] == "" {
		t.Fatalf("the launch got no session home\n%s", run.errs)
	}
	if _, err := os.Lstat(dead); !os.IsNotExist(err) {
		t.Errorf("the dead launch's session home %s was not reaped (%v)", dead, err)
	}
	if _, err := os.Lstat(live); err != nil {
		t.Errorf("a running launch's session home %s was reaped: %v", live, err)
	}
}

// SP-D6 AT THE HOST: a home that cannot be made refuses the launch, naming the path and the
// command to copy, and pi never starts.
func TestHostPiSessionHomeRefusesWhenItCannotBeMade(t *testing.T) {
	var root string
	rc, env, errs := hostGateRunIn(t, ownedSessionHomeConfig(piSubagentsCodexCfg), nil, []string{"-p", "pi=codex"}, "pi", func(home string) {
		piSettingsFixture(t, home, `{"defaultProvider":"openrouter"}`)
		root = filepath.Join(paths.GlobalStorageUnder(home), prune.HostSessionHomesLeaf)
		writeFile(t, root, "not a directory")
	})
	if rc == 0 || env != nil {
		t.Fatalf("a session home that cannot be made must refuse the launch (rc=%d)\n%s", rc, errs)
	}
	for _, want := range []string{"cannot make", root, "ls -la"} {
		if !strings.Contains(errs, want) {
			t.Errorf("the refusal must say %q:\n%s", want, errs)
		}
	}
}

// IN A JAIL `yolo host` makes no session home: the jail's own session pass made one for the
// session already (SP-D25). The guard is the step's first act, so it reads nothing of the launch.
func TestHostSessionHomesDoNothingInAJail(t *testing.T) {
	t.Setenv("YOLO_VERSION", "test")
	env := []string{"A=1"}
	homes, got, err := prepareHostSessionHomes(nil, env, os.Stderr)
	if homes != nil || err != nil || len(got) != 1 || got[0] != "A=1" {
		t.Errorf("in a jail the step must hand the environment back untouched: %v %q %v", homes, got, err)
	}
}

// THE RESIDENT PATH: a launch that stays resident removes its directory when the program exits
// (Close), and one whose exec handed the leases over leaves it for the reaper.
func TestHostSessionHomesCloseRemovesUnlessHandedOver(t *testing.T) {
	root := t.TempDir()
	mk := func() *hostSessionHomes {
		t.Helper()
		dir, lease, err := makeHostSessionDir(root, "pack")
		if err != nil || lease == nil {
			t.Fatalf("makeHostSessionDir: %v (lease %v)", err, lease)
		}
		return &hostSessionHomes{dirs: []string{dir}, leases: []*os.File{lease}}
	}
	resident := mk()
	dir := resident.dirs[0]
	if state, unlock, _ := packload.ProbeLease(dir); state != packload.LeaseHeld {
		unlock()
		t.Fatalf("a fresh launch directory's lease is %s, want held", state)
	}
	resident.Close()
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Errorf("Close left %s (%v)", dir, err)
	}
	execd := mk()
	execd.KeepAcrossExec(os.Stderr)
	execd.Close()
	if _, err := os.Lstat(execd.dirs[0]); err != nil {
		t.Errorf("Close after KeepAcrossExec removed the program's directory: %v", err)
	}
	execd.remove()
}
