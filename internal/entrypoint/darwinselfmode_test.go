package entrypoint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// darwinselfmode_test.go covers what the native bootstrap lays for a run-as-you macos-user
// session (docs/design/macos-user-model-plan.md slice 6): HOME is the workspace sidecar itself,
// and ~/.cache is a link the layout laid into the machine root.

// selfModeEnv is a run-as-you bootstrap's Env: HOME is the sidecar under a workspace, and the
// machine root and shared store are named. extra adds variables.
func selfModeEnv(t *testing.T, extra map[string]string) (*Env, string, string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(base, "ws", ".yolo", "home")
	machine := filepath.Join(base, "state", "macos-user")
	vars := map[string]string{
		"HOME": home, "JAIL_HOME": home,
		DarwinHomeSidecarEnv: home,
		DarwinMachineHomeEnv: machine,
		DarwinSharedHomeEnv:  filepath.Join(base, "state", "home"),
	}
	for k, v := range extra {
		vars[k] = v
	}
	e := DarwinEnvFrom(vars, home)
	var said strings.Builder
	e.Stderr = &said
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("bootstrap said:\n%s", said.String())
		}
	})
	return e, home, machine
}

// IN THE RUN-AS-YOU LAYOUT ~/.cache IS THE LAYOUT'S OWN LINK into the machine root, so the
// relocations are laid in the directory it names, never refused as a link "yolo did not lay".
// Fails if InstallDarwinCacheRelocations goes back to laying under HOME/.cache.
func TestSelfModeCacheRelocationsAreLaidInTheMachineRoot(t *testing.T) {
	target, _ := filepath.EvalSymlinks(t.TempDir()) // the host CLI made it (macosuserrelocations.go)
	e, home, machine := selfModeEnv(t, map[string]string{
		DarwinCacheRelocationsEnv: DarwinCacheRelocationsWire(map[string]string{"huggingface": target}),
	})
	if err := InstallDarwinHomeLayout(e, nil); err != nil {
		t.Fatalf("InstallDarwinHomeLayout() = %v", err)
	}
	if got, _ := os.Readlink(filepath.Join(machine, ".cache", "huggingface")); got != target {
		t.Errorf("<machine>/.cache/huggingface -> %q, want %s", got, target)
	}
	// And the agent reaches it through its own ~/.cache.
	if got, err := filepath.EvalSymlinks(filepath.Join(home, ".cache", "huggingface")); err != nil || got != target {
		t.Errorf("~/.cache/huggingface resolves to %q (%v), want %s", got, err, target)
	}
}

// THE PACKAGE-CACHE ALIASES (MMP-D12): each is laid where it is named, in HOME or the machine
// root; one a later launch drops is swept; an EMPTY private copy is replaced and a warm one is
// left alone with a warning. Never fatal. Fails if InstallDarwinHomeLayout stops calling the
// alias step.
func TestSelfModeLaysAndSweepsCacheAliases(t *testing.T) {
	hostRoot, _ := filepath.EvalSymlinks(t.TempDir()) // resolved, as the launcher hands targets
	hostPip, hostGo := filepath.Join(hostRoot, "pip"), filepath.Join(hostRoot, "mod")
	for _, d := range []string{hostPip, hostGo} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	e, home, machine := selfModeEnv(t, nil)
	pipLink := filepath.Join(machine, "Library", "Caches", "pip")
	goLink := filepath.Join(home, "go", "pkg", "mod")
	// The sandbox's own Go module copy exists, empty: the link replaces it.
	if err := os.MkdirAll(goLink, 0o755); err != nil {
		t.Fatal(err)
	}
	e.Vars[DarwinCacheAliasesEnv] = DarwinCacheAliasesWire(map[string]string{pipLink: hostPip, goLink: hostGo})
	if err := InstallDarwinHomeLayout(e, nil); err != nil {
		t.Fatalf("InstallDarwinHomeLayout() = %v", err)
	}
	for link, target := range map[string]string{pipLink: hostPip, goLink: hostGo} {
		if got, _ := os.Readlink(link); got != target {
			t.Errorf("%s -> %q, want %s", link, got, target)
		}
	}
	if got, err := filepath.EvalSymlinks(filepath.Join(home, "Library", "Caches", "pip")); err != nil || got != hostPip {
		t.Errorf("~/Library/Caches/pip resolves to %q (%v), want the host's %s", got, err, hostPip)
	}

	// The next launch aliases pip only: the Go link goes, the host's cache stays.
	e.Vars[DarwinCacheAliasesEnv] = DarwinCacheAliasesWire(map[string]string{pipLink: hostPip})
	if err := InstallDarwinHomeLayout(e, nil); err != nil {
		t.Fatalf("second InstallDarwinHomeLayout() = %v", err)
	}
	if _, err := os.Lstat(goLink); err == nil {
		t.Error("a dropped alias's link was left in place")
	}
	if _, err := os.Stat(hostGo); err != nil {
		t.Errorf("sweeping the link touched the host cache: %v", err)
	}
}

// A WARM PRIVATE COPY IS NEVER REPLACED, and the boot still succeeds: the sandbox keeps its own
// copy and the bootstrap says why. An alias outside HOME and the machine root is not laid.
func TestSelfModeCacheAliasLeavesAWarmCopyAndRefusesAStrayLink(t *testing.T) {
	hostPip := filepath.Join(t.TempDir(), "pip")
	if err := os.MkdirAll(hostPip, 0o755); err != nil {
		t.Fatal(err)
	}
	stray := filepath.Join(t.TempDir(), "elsewhere")
	e, _, machine := selfModeEnv(t, nil)
	var said strings.Builder
	e.Stderr = &said
	pipLink := filepath.Join(machine, "Library", "Caches", "pip")
	if err := os.MkdirAll(pipLink, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pipLink, "wheel"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.Vars[DarwinCacheAliasesEnv] = DarwinCacheAliasesWire(map[string]string{pipLink: hostPip, stray: hostPip})
	if err := InstallDarwinHomeLayout(e, nil); err != nil {
		t.Fatalf("an alias failure refused the boot: %v", err)
	}
	if fi, err := os.Lstat(pipLink); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Errorf("the warm private copy was replaced (%v, %v)", fi, err)
	}
	if _, err := os.Lstat(stray); err == nil {
		t.Error("an alias outside HOME and the machine root was laid")
	}
	for _, want := range []string{pipLink, stray} {
		if !strings.Contains(said.String(), want) {
			t.Errorf("the bootstrap did not name %s:\n%s", want, said.String())
		}
	}
}

// An account-mode bootstrap (HOME is the account home, not the sidecar) lays no alias, even if
// the variable is somehow set: aliasing is the run-as-you mode's alone (OQ-BF11).
func TestAccountModeLaysNoCacheAlias(t *testing.T) {
	home := t.TempDir()
	link := filepath.Join(home, "Library", "Caches", "pip")
	e := DarwinEnvFrom(map[string]string{"HOME": home, "JAIL_HOME": home,
		DarwinCacheAliasesEnv: DarwinCacheAliasesWire(map[string]string{link: t.TempDir()})}, home)
	InstallDarwinCacheAliases(e)
	if _, err := os.Lstat(link); err == nil {
		t.Error("an account-mode bootstrap laid a cache alias")
	}
}
