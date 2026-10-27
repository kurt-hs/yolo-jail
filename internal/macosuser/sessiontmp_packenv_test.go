package macosuser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

func stagedShippedClaudePack(t *testing.T) string {
	t.Helper()
	manifest, err := os.ReadFile(filepath.Join("..", "..", "packs", "claude", "pack.json"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	pack := filepath.Join(root, "claude")
	if err := os.MkdirAll(pack, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pack, "pack.json"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "_pack-tree.json"),
		[]byte(`{"packs":[{"name":"claude","dir":"claude"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// The production run-plan caller must adapt only the selected program's declared temp
// followers, after final TMPDIR composition. This pins both final self/account env and the
// existing self-mode temp confinement; testing the declaration projection alone is insufficient.
func TestBuildRunPlanMapsSelectedProgramTempEnvToSandboxTMPDIR(t *testing.T) {
	root := stagedShippedClaudePack(t)
	plan := func(mode Mode, command string, env *jsonx.OrderedMap) RunPlan {
		session := PlanSession{ID: testSessionID, Mode: mode}
		if mode == ModeSelf {
			session.HostHome, session.HostUser, session.DarwinUserTempDir = "/Users/you", "you", testUserTmp
		}
		return BuildRunPlanWithDaemons("/Users/you/code/alpha", jsonx.NewOrderedMap(), nil, []string{command},
			"/usr/local/bin/yolo", root, HomeOverlay{}, HostContext{}, env, nil, nil,
			JailDaemons{}, FloorStage{}, session)
	}
	envValue := func(p RunPlan, key string) string {
		t.Helper()
		got, ok := sandboxEnvFileValue(p.EnvFileContent, key)
		if !ok {
			t.Fatalf("final sandbox env has no %s: %s", key, p.EnvFileContent)
		}
		return got
	}

	self := plan(ModeSelf, "claude", nil)
	if got := envValue(self, "CLAUDE_CODE_TMPDIR"); got != testSessionTmp {
		t.Errorf("self-mode CLAUDE_CODE_TMPDIR = %q, want session TMPDIR %q", got, testSessionTmp)
	}
	if got := envValue(self, "TMPDIR"); got != testSessionTmp {
		t.Errorf("self-mode TMPDIR = %q, want %q", got, testSessionTmp)
	}
	if problems := PlanInvariants(self); len(problems) != 0 {
		t.Errorf("self-mode plan invariants: %s", strings.Join(problems, "\n"))
	}

	accountEnv := jsonx.NewOrderedMap()
	accountEnv.Set("TMPDIR", "/account/sandbox/tmp")
	account := plan(ModeAccount, "claude", accountEnv)
	if got := envValue(account, "CLAUDE_CODE_TMPDIR"); got != "/account/sandbox/tmp" {
		t.Errorf("account-mode follower = %q, want its existing TMPDIR", got)
	}
	if account.SessionTmpDir != "" {
		t.Errorf("account mode acquired a session temp dir: %q", account.SessionTmpDir)
	}
	if len(account.SessionTmpRemoveCommands) != 0 || strings.Contains(account.Seatbelt, testSessionTmp) {
		t.Error("Claude's account-mode env mapping changed session-temp teardown or Seatbelt confinement")
	}

	safeEnv := jsonx.NewOrderedMap()
	safeEnv.Set("CLAUDE_CODE_TMPDIR", "/Users/you/code/alpha/.claude-tmp")
	safe := plan(ModeSelf, "claude", safeEnv)
	if got := envValue(safe, "CLAUDE_CODE_TMPDIR"); got != "/Users/you/code/alpha/.claude-tmp" {
		t.Errorf("explicit safe follower = %q, want preserved override", got)
	}
	if safe.TmpdirProblem != "" {
		t.Errorf("safe follower override was refused: %s", safe.TmpdirProblem)
	}

	unsafeEnv := jsonx.NewOrderedMap()
	unsafeEnv.Set("CLAUDE_CODE_TMPDIR", "/Users/you/tmp")
	unsafe := plan(ModeSelf, "claude", unsafeEnv)
	if unsafe.TmpdirProblem == "" || !strings.Contains(unsafe.TmpdirProblem, "CLAUDE_CODE_TMPDIR") {
		t.Errorf("unsafe follower override was not refused by temp validation: %q", unsafe.TmpdirProblem)
	}

	otherEnv := jsonx.NewOrderedMap()
	otherEnv.Set("TMPDIR", testSessionTmp)
	other := plan(ModeSelf, "codex", otherEnv)
	if _, ok := sandboxEnvFileValue(other.EnvFileContent, "CLAUDE_CODE_TMPDIR"); ok {
		t.Error("Claude's temp env leaked into a different active program")
	}
}

func TestBuildRunPlanMapsTempEnvOnlyFromSelectedProgramPack(t *testing.T) {
	root := t.TempDir()
	pack := filepath.Join(root, "other")
	if err := os.MkdirAll(pack, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"contributes":[{"kind":"program","bin":"other","via":"installer","url":"https://example.invalid/install.sh","sandbox_tmpdir_env":["OTHER_TMPDIR"]}]}`
	if err := os.WriteFile(filepath.Join(pack, "pack.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "_pack-tree.json"),
		[]byte(`{"packs":[{"name":"other","dir":"other"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	env := jsonx.NewOrderedMap()
	env.Set("TMPDIR", testSessionTmp)
	plan := BuildRunPlanWithDaemons("/Users/you/code/alpha", jsonx.NewOrderedMap(), nil, []string{"other"},
		"/usr/local/bin/yolo", root, HomeOverlay{}, HostContext{}, env, nil, nil,
		JailDaemons{}, FloorStage{}, PlanSession{})
	if _, ok := sandboxEnvFileValue(plan.EnvFileContent, "CLAUDE_CODE_TMPDIR"); ok {
		t.Error("Claude's literal temp variable leaked from an unselected pack")
	}
	if _, ok := sandboxEnvFileValue(plan.EnvFileContent, "OTHER_TMPDIR"); !ok {
		t.Error("selected program's generic temp follower was not applied")
	}
}
