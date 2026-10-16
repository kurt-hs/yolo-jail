package packload

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	officialpacks "github.com/mschulkind-oss/yolo-jail/packs"
)

func TestGuardrailsRequiredGitWritesRespectIndexLock(t *testing.T) {
	t.Run("default temp root", func(t *testing.T) {
		runGuardrailsRequiredGitWritesRespectIndexLock(t, t.TempDir())
	})
	t.Run("symlink temp root", func(t *testing.T) {
		target := t.TempDir()
		root := filepath.Join(t.TempDir(), "temp-root")
		if err := os.Symlink(target, root); err != nil {
			t.Fatal(err)
		}
		runGuardrailsRequiredGitWritesRespectIndexLock(t, root)
	})
}

func runGuardrailsRequiredGitWritesRespectIndexLock(t *testing.T, root string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("resolving Git fixture root: %v", err)
	}

	loaded, problems := MaterializeEmbedded(officialpacks.FS, t.TempDir())
	if len(problems) != 0 {
		t.Fatalf("materializing official packs: %v", problems)
	}
	var guardrails *Pack
	for _, p := range loaded {
		if p.Name == "guardrails" {
			guardrails = p
			break
		}
	}
	if guardrails == nil {
		t.Fatal("official guardrails pack not found")
	}
	scope, err := ScopeCredentials(ScopeInput{Packs: []*Pack{guardrails}, Providers: jsonx.NewOrderedMap()})
	if err != nil {
		t.Fatal(err)
	}
	optionalLocks, ok := scope.SharedEnv().Value("GIT_OPTIONAL_LOCKS")
	if !ok || optionalLocks != "0" {
		t.Fatalf("selected guardrails composed GIT_OPTIONAL_LOCKS=%q, present=%v; want 0", optionalLocks, ok)
	}

	home := filepath.Join(root, "home")
	hooks := filepath.Join(home, "hooks")
	if err := os.MkdirAll(hooks, 0o700); err != nil {
		t.Fatal(err)
	}
	globalConfig := filepath.Join(home, ".gitconfig")
	config := "[user]\n\tname = Guardrails fixture\n\temail = guardrails@example.invalid\n[commit]\n\tgpgsign = false\n[core]\n\thooksPath = " + hooks + "\n"
	if err := os.WriteFile(globalConfig, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	gitEnv := []string{
		"HOME=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"PATH=" + os.Getenv("PATH"),
		"LANG=C",
		"LC_ALL=C",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=" + globalConfig,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=" + optionalLocks,
	}
	repo := filepath.Join(root, "repo")
	if err := os.Mkdir(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	runGit := func(args ...string) ([]byte, error) {
		// maintenance.auto/gc.auto: no detached maintenance outliving git into t.TempDir's cleanup.
		cmd := exec.Command("git", append([]string{"-C", repo, "-c", "maintenance.auto=false", "-c", "gc.auto=0"}, args...)...)
		cmd.Env = gitEnv
		return cmd.CombinedOutput()
	}
	if out, err := runGit("init", "--quiet"); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	file := filepath.Join(repo, "tracked.txt")
	if err := os.WriteFile(file, []byte("before lock\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := runGit("add", "tracked.txt"); err != nil {
		t.Fatalf("ordinary git add with composed GIT_OPTIONAL_LOCKS=0: %v\n%s", err, out)
	}
	if out, err := runGit("commit", "--quiet", "-m", "initial fixture commit"); err != nil {
		t.Fatalf("ordinary git commit with composed GIT_OPTIONAL_LOCKS=0: %v\n%s", err, out)
	}

	if err := os.WriteFile(file, []byte("after lock\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(repo, ".git", "index.lock")
	lockBytes := []byte("owned guardrails index-lock fixture\n")
	lock, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatalf("create own exclusive index-lock fixture: %v", err)
	}
	if _, err := lock.Write(lockBytes); err != nil {
		_ = lock.Close()
		t.Fatal(err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := runGit("add", "tracked.txt")
	if err == nil {
		t.Fatalf("git add succeeded despite owned index.lock fixture; output: %s", out)
	}
	if !strings.Contains(string(out), lockPath) || !strings.Contains(string(out), "File exists") {
		t.Fatalf("git add refused for a reason other than the owned index.lock: err=%v output=%s", err, out)
	}
	gotLockBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read owned index-lock fixture after refusal: %v", err)
	}
	if string(gotLockBytes) != string(lockBytes) {
		t.Fatalf("git changed owned index-lock fixture: got %q, want %q", gotLockBytes, lockBytes)
	}
	if err := os.Remove(lockPath); err != nil {
		t.Fatalf("remove own index-lock fixture: %v", err)
	}
	if out, err := runGit("add", "tracked.txt"); err != nil {
		t.Fatalf("git add after removing own fixture lock: %v\n%s", err, out)
	}
	if out, err := runGit("commit", "--quiet", "-m", "commit after fixture lock"); err != nil {
		t.Fatalf("git commit after removing own fixture lock: %v\n%s", err, out)
	}
}

func TestGuardrailsComposesGitOptionalLocksOnlyWhenSelected(t *testing.T) {
	loaded, problems := MaterializeEmbedded(officialpacks.FS, t.TempDir())
	if len(problems) != 0 {
		t.Fatalf("materializing official packs: %v", problems)
	}
	var guardrails *Pack
	for _, p := range loaded {
		if p.Name == "guardrails" {
			guardrails = p
			break
		}
	}
	if guardrails == nil {
		t.Fatal("official guardrails pack not found")
	}

	for _, tc := range []struct {
		name  string
		packs []*Pack
		value string
		set   bool
	}{
		{name: "selected", packs: []*Pack{guardrails}, value: "0", set: true},
		{name: "unselected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scope, err := ScopeCredentials(ScopeInput{Packs: tc.packs, Providers: jsonx.NewOrderedMap()})
			if err != nil {
				t.Fatal(err)
			}
			got, ok := scope.SharedEnv().Value("GIT_OPTIONAL_LOCKS")
			if got != tc.value || ok != tc.set {
				t.Fatalf("shared GIT_OPTIONAL_LOCKS = %q, present=%v; want %q, present=%v", got, ok, tc.value, tc.set)
			}
		})
	}

	sources := jsonx.NewOrderedMap()
	sources.Set("GIT_OPTIONAL_LOCKS", "1")
	scope, err := ScopeCredentials(ScopeInput{
		Packs: []*Pack{guardrails}, Providers: jsonx.NewOrderedMap(), EnvSources: sources,
	})
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := scope.SharedEnv().Lookup("GIT_OPTIONAL_LOCKS")
	if !ok || entry.Value != "1" || entry.Origin != FromEnvSources {
		t.Errorf("env_sources winner = %+v, present=%v; want value 1 from env_sources", entry, ok)
	}
}
