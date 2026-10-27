package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/hostfloor/floortest"
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/testsupport"
)

func readableInstallRemote(t *testing.T) (remote, work, initial string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	base := t.TempDir()
	remote = filepath.Join(base, "packs.git")
	work = filepath.Join(base, "work")
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(testsupport.HermeticGitEnv(packsrc.CleanGitEnv(os.Environ())),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.invalid",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.invalid")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	if err := os.MkdirAll(remote, 0o755); err != nil {
		t.Fatal(err)
	}
	git(base, "init", "-q", "--bare", "--initial-branch=trunk", remote)
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	git(work, "init", "-q", "--initial-branch=trunk")
	manifest := filepath.Join(work, "tools", "pack.json")
	if err := os.MkdirAll(filepath.Dir(manifest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, []byte(`{"name":"ignored"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	git(work, "add", "-A")
	git(work, "commit", "-qm", "initial")
	initial = git(work, "rev-parse", "HEAD")
	git(work, "remote", "add", "origin", "file://"+filepath.ToSlash(remote))
	git(work, "push", "-q", "origin", "trunk")
	git(remote, "symbolic-ref", "HEAD", "refs/heads/trunk")
	return remote, work, initial
}

func writeReadablePackInstallConfig(t *testing.T, home, remote string) {
	t.Helper()
	dir := filepath.Join(home, ".config", "yolo-jail")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "packs = ['acme/tools']\n[pack_sources.acme]\ngit = 'file://" + filepath.ToSlash(remote) + "'\n"
	if err := os.WriteFile(filepath.Join(dir, paths.UserConfigTOMLName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runPackInstallMode(t *testing.T, update bool) string {
	t.Helper()
	var out, errw bytes.Buffer
	repinForks := update
	if update {
		if rc := packInstallPins(&out, &errw, false, repinForks); rc != 0 {
			t.Fatalf("pack update rc=%d\n%s%s", rc, out.String(), errw.String())
		}
	} else if rc := packInstall(&out, &errw, false); rc != 0 {
		t.Fatalf("pack install rc=%d\n%s%s", rc, out.String(), errw.String())
	}
	return out.String() + errw.String()
}

func TestPackInstallMalformedReadableConfigPreservesExistingLock(t *testing.T) {
	for _, tc := range []struct {
		name, primary, fragment string
	}{
		{name: "base", primary: "format = 2\npacks = [\n"},
		{name: "fragment", primary: "format = 2\npacks = []\n", fragment: "packs = [\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, err := filepath.EvalSymlinks(floortest.ResolvedTemp(t))
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
			t.Setenv("YOLO_PACK_ROOT", "")
			dir := filepath.Join(home, ".config", "yolo-jail")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, paths.UserConfigTOMLName), []byte(tc.primary), 0o600); err != nil {
				t.Fatal(err)
			}
			if tc.fragment != "" {
				fragmentDir := filepath.Join(dir, "config.d")
				if err := os.MkdirAll(fragmentDir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(fragmentDir, "10-bad.toml"), []byte(tc.fragment), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			lockPath := packsrc.LockPath(paths.UserConfigPath())
			lock := &packsrc.Lock{}
			lock.Set(packsrc.LockEntry{Name: "keep/tools", Source: "git+https://example.invalid/keep/tools?ref=main",
				Ref: "main", Commit: strings.Repeat("a", 40)})
			if err := lock.Save(lockPath); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(lockPath)
			if err != nil {
				t.Fatal(err)
			}
			var out, errw bytes.Buffer
			if rc := packInstall(&out, &errw, false); rc == 0 {
				t.Fatalf("install accepted malformed %s config and may prune pins:\n%s%s", tc.name, out.String(), errw.String())
			}
			after, err := os.ReadFile(lockPath)
			if err != nil || !bytes.Equal(after, before) {
				t.Fatalf("malformed %s config changed the durable lock: %v\nbefore: %s\nafter:  %s", tc.name, err, before, after)
			}
		})
	}
}

func TestPackInstallUsesLegacyJSONCUserLayerWhenPrimaryAbsent(t *testing.T) {
	home, err := filepath.EvalSymlinks(floortest.ResolvedTemp(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("YOLO_PACK_ROOT", "")
	remote, _, initial := readableInstallRemote(t)
	layer := filepath.Join(home, "pack-layer.jsonc")
	body := `{"packs":[{"source":"git+file://` + filepath.ToSlash(remote) + `//tools?ref=trunk","name":"fixture"}]}`
	if err := os.WriteFile(layer, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YOLO_USER_LAYER", layer)
	configDir := filepath.Dir(paths.UserConfigPath())
	for _, path := range []string{
		filepath.Join(configDir, paths.UserConfigLegacyJSONCName),
		filepath.Join(configDir, paths.UserConfigLegacyJSONName),
		filepath.Join(configDir, paths.UserConfigTOMLName),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("test requires absent primary config %s, stat error %v", path, err)
		}
	}
	var out, errw bytes.Buffer
	if rc := packInstallPins(&out, &errw, false, false); rc != 0 {
		t.Fatalf("install from legacy user layer rc=%d:\n%s%s", rc, out.String(), errw.String())
	}
	lock, err := packsrc.LoadLock(packsrc.LockPath(paths.UserConfigPath()))
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := lock.Get("fixture")
	if !ok || entry.Source != "git+file://"+filepath.ToSlash(remote)+"//tools?ref=trunk" || entry.Commit != initial {
		t.Fatalf("legacy-layer pack was not installed with its legacy identity: %+v, present %v", entry, ok)
	}
}

func TestPackInstallMissingDefaultNamesExplicitRef(t *testing.T) {
	home, err := filepath.EvalSymlinks(floortest.ResolvedTemp(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("YOLO_PACK_ROOT", "")
	remote, _, _ := readableInstallRemote(t)
	cmd := exec.Command("git", "symbolic-ref", "HEAD", "refs/heads/missing")
	cmd.Dir = remote
	cmd.Env = testsupport.HermeticGitEnv(packsrc.CleanGitEnv(os.Environ()))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git symbolic-ref: %v\n%s", err, out)
	}
	writeReadablePackInstallConfig(t, home, remote)
	var out, errw bytes.Buffer
	if rc := packInstall(&out, &errw, false); rc == 0 {
		t.Fatalf("install accepted an unadvertised default branch:\n%s%s", out.String(), errw.String())
	}
	for _, want := range []string{"did not advertise a symbolic default branch", "write an explicit ?ref="} {
		if !strings.Contains(errw.String(), want) {
			t.Errorf("default branch refusal lacks %q:\n%s", want, errw.String())
		}
	}
	if lock, err := packsrc.LoadLock(packsrc.LockPath(paths.UserConfigPath())); err != nil {
		t.Fatal(err)
	} else if _, ok := lock.Get("acme/tools"); ok {
		t.Error("failed default discovery wrote a lock entry")
	}
}

func TestLegacyPackInstallStillRequiresExplicitRef(t *testing.T) {
	home, err := filepath.EvalSymlinks(floortest.ResolvedTemp(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("YOLO_PACK_ROOT", "")
	remote, _, _ := readableInstallRemote(t)
	path := filepath.Join(home, ".config", "yolo-jail", paths.UserConfigLegacyJSONCName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"packs":[{"source":"git+file://` + filepath.ToSlash(remote) + `//tools"}]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errw bytes.Buffer
	if rc := packInstall(&out, &errw, false); rc == 0 || !strings.Contains(errw.String(), "missing required ?ref=") {
		t.Fatalf("legacy omitted ref was accepted or misreported (rc %d):\n%s%s", rc, out.String(), errw.String())
	}
}

func TestPackInstallHoldsReadableDefaultUntilExplicitUpdate(t *testing.T) {
	home, err := filepath.EvalSymlinks(floortest.ResolvedTemp(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("YOLO_PACK_ROOT", "")
	remote, work, initial := readableInstallRemote(t)
	writeReadablePackInstallConfig(t, home, remote)
	lockPath := packsrc.LockPath(paths.UserConfigPath())

	runPackInstallMode(t, false)
	locked, err := packsrc.LoadLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	first, ok := locked.Get("acme/tools")
	if !ok || first.Ref != "trunk" || first.Commit != initial {
		t.Fatalf("first install lock = %+v, present %v; want trunk at %s", first, ok, initial)
	}

	if err := os.WriteFile(filepath.Join(work, "next.txt"), []byte("next\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(testsupport.HermeticGitEnv(packsrc.CleanGitEnv(os.Environ())),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.invalid",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.invalid")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git(work, "add", "-A")
	git(work, "commit", "-qm", "review")
	updated := git(work, "rev-parse", "HEAD")
	git(work, "branch", "alice/review", updated)
	git(work, "push", "-q", "origin", "alice/review")
	git(remote, "symbolic-ref", "HEAD", "refs/heads/alice/review")

	runPackInstallMode(t, false)
	locked, err = packsrc.LoadLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	afterInstall, ok := locked.Get("acme/tools")
	if !ok || afterInstall.Commit != initial || afterInstall.Ref != "trunk" {
		t.Fatalf("repeated install moved default pin: %+v, want trunk at %s", afterInstall, initial)
	}

	runPackInstallMode(t, true)
	locked, err = packsrc.LoadLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	afterUpdate, ok := locked.Get("acme/tools")
	if !ok || afterUpdate.Commit != updated || afterUpdate.Ref != "alice/review" {
		t.Fatalf("explicit update did not move default pin: %+v, want alice/review at %s", afterUpdate, updated)
	}
}
