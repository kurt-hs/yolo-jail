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
	"encoding/json"
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
	home := hostGateHome(t, piSubagentsCodexCfg, nil)
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

// A PROFILE THAT DECLARES A CHILD POLICY reaches the session's own settings.json, so pi's
// subagents follow the -p rather than the configured profile.
func TestHostPiSessionHomeCarriesTheProfilesChildPolicy(t *testing.T) {
	env := piSessionHomeEnv(t)
	home := hostGateHome(t, piSubagentsAcmeCfg, nil)
	unchanged := piSettingsFixture(t, home, `{"defaultProvider":"acme"}`)
	runB := hostSelectionRun(t, home, []string{"-p", "pi=acme-b"}, "pi")
	runA := hostSelectionRun(t, home, nil, "pi")
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

// SP-D5 AT THE HOST: a variable the user already set wins, no home is made, and one line says so.
func TestHostPiSessionHomeYieldsToThePresetVariable(t *testing.T) {
	env := piSessionHomeEnv(t)
	own := t.TempDir()
	home := hostGateHome(t, piSubagentsCodexCfg, map[string]string{env: own})
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

// A DEAD LAUNCH'S HOME IS REAPED by the next launch: one whose lease nobody holds goes, one whose
// lease is held (a pi still running) stays.
func TestHostPiSessionHomeReapsADeadLaunch(t *testing.T) {
	env := piSessionHomeEnv(t)
	home := hostGateHome(t, piSubagentsCodexCfg, nil)
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
	run := hostSelectionRun(t, home, nil, "pi")
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
	rc, env, errs := hostGateRunIn(t, piSubagentsCodexCfg, nil, []string{"-p", "pi=codex"}, "pi", func(home string) {
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
