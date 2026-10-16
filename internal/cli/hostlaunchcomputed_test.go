package cli

// hostlaunchcomputed_test.go pins the `launch_computed` pack kind at the host notch
// (docs/reference/pack-system.md#launch_computed): an explicit `yolo host -p <profile> -- <bin>`
// hands the program a COMPUTED key its derive composes for the -p's profile, for that process
// alone, as the argv words its pack declares, and never writes it into the program's file. yolo
// ships no such declaration (the maintainer's ruling of 2026-10-16, MM-D42 in
// docs/design/model-lists-and-pickers.md), so these cells declare one in a TEST PACK: a local copy
// of the shipped pi pack, selected under the name "pi" in its place, with a neutral flag and
// opt-in added. pi's derive composes the computed `subagents` block the flag carries. Every cell
// runs hostMain to the exec, so the declaration, the derive and the host step are asked through
// the front door.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// piSubagentsOptIn is the variable the test pack gates its flag behind.
const piSubagentsOptIn = "T_CHILD_POLICY_ON"

// piSubagentsFlag is the test pack's flag word.
const piSubagentsFlag = "--child-policy"

// shippedPiPackDir is the tree's pi pack, resolved before any cell moves the working directory.
var shippedPiPackDir, _ = filepath.Abs(filepath.Join("..", "..", "packs", "pi"))

// launchComputedPackDir is a copy of the shipped pi pack with a launch_computed block for pi
// handing the derive's `subagents` key as piSubagentsFlag behind piSubagentsOptIn: what a user's
// own pack declares, since yolo's declares none.
func launchComputedPackDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "pi-with-computed")
	if err := os.CopyFS(dir, os.DirFS(shippedPiPackDir)); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(dir, "pack.json")
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "launch_computed") {
		t.Fatal("the shipped pi pack declares launch_computed again; the fixture would double it")
	}
	if !strings.HasPrefix(string(data), "{\n") {
		t.Fatal("the shipped pi pack.json no longer opens with a line of its own")
	}
	block := "{\n  \"launch_computed\": {\"pi\": [{\"key\": \"subagents\", \"argv\": [\"" + piSubagentsFlag +
		"\", \"{value}\"], \"opt_in\": \"" + piSubagentsOptIn + "\"}]},\n"
	if err := os.WriteFile(manifest, []byte(block+string(data)[2:]), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// withComputedPack rewrites cfg's bare "pi" pack, its last, into the test pack under the name pi.
func withComputedPack(t *testing.T, cfg string) string {
	t.Helper()
	src, _ := json.Marshal(launchComputedPackDir(t))
	out := strings.Replace(cfg, `"pi"]`, `{"source": `+string(src)+`, "name": "pi"}]`, 1)
	if out == cfg {
		t.Fatalf("no bare pi pack to replace in %s", cfg)
	}
	return out
}

// piSubagentsCodexCfg configures openrouter as pi's profile, with openai-auth's codex profile
// selectable by -p.
const piSubagentsCodexCfg = `{"packs": ["claude", "openrouter", "openai-auth", "pi"], ` +
	`"profile": {"pi": "openrouter"}, "env_sources": [{"OPENROUTER_API_KEY": "tok-router"}]}`

// piSubagentsAcmeCfg is one user provider with two profiles over it whose role pins differ, the
// case a static map keyed on the parent's provider could not express; acme-a is configured.
const piSubagentsAcmeCfg = `{"packs": ["claude", "pi"], ` +
	`"providers": {"acme": {"endpoints": {"openai": {"base_url": "https://acme.example/v1"}}, ` +
	`"api_key_env_name": "ACME_KEY", "models": {"default": "acme-large", "fast": "acme-mini"}}}, ` +
	`"profiles": {` +
	`"acme-a": {"provider": "acme", "subagents": {"roles": {"worker": "fast"}}}, ` +
	`"acme-b": {"provider": "acme", "subagents": {"default": "fast", "roles": {"worker": "default", "scout": "fast"}}}}, ` +
	`"profile": {"pi": "acme-a"}, "env_sources": [{"ACME_KEY": "tok-acme"}]}`

// THE SHIPPED PI PACK HANDS NO COMPUTED FLAG (the maintainer's ruling of 2026-10-16: "yolo
// shouldn't depend on my custom pi fork in any way, or even know it exists"). The same -p that
// hands the test pack's flag hands the shipped pack's pi its selection alone, says nothing of a
// held flag.
func TestHostShippedPiHandsNoComputedFlag(t *testing.T) {
	home := hostGateHome(t, piSubagentsCodexCfg, nil)
	unchanged := piSettingsFixture(t, home, `{"defaultProvider":"openrouter"}`)
	run := hostSelectionRun(t, home, []string{"-p", "pi=codex"}, "pi")
	if len(run.argv) < 3 || run.argv[1] != "--provider" || run.argv[2] != "openai-codex" {
		t.Fatalf("the parent must still move onto codex: %q\n%s", run.argv, run.errs)
	}
	for _, w := range run.argv {
		if strings.HasPrefix(w, "{") {
			t.Errorf("the shipped pi pack handed a computed JSON word: %q", run.argv)
		}
	}
	for _, not := range []string{"subagents as this launch composes it", "composes its own subagents"} {
		if strings.Contains(run.errs, not) {
			t.Errorf("the launch speaks of a computed flag the shipped pack does not declare (%q):\n%s", not, run.errs)
		}
	}
	unchanged()
}

// piSettingsFixture writes a settings.json holding the CONFIGURED profile's child policy, and an
// auth.json, as `yolo host apply` and a login would leave them, and returns a check that both are
// byte-unchanged.
func piSettingsFixture(t *testing.T, home, settings string) func() {
	t.Helper()
	settingsPath := filepath.Join(home, ".pi", "agent", "settings.json")
	authPath := filepath.Join(home, ".pi", "agent", "auth.json")
	const auth = `{"openai-codex":{"type":"oauth","access":"fixture"}}`
	writeFile(t, settingsPath, settings)
	writeFile(t, authPath, auth)
	return func() {
		t.Helper()
		if got, _ := os.ReadFile(settingsPath); string(got) != settings {
			t.Errorf("pi's settings.json changed:\n%s", got)
		}
		if got, _ := os.ReadFile(authPath); string(got) != auth {
			t.Errorf("pi's auth.json changed:\n%s", got)
		}
	}
}

// subagentsWord is the JSON word after the flag in argv, and whether the flag is there at all.
func subagentsWord(argv []string) (string, bool) {
	i := slices.Index(argv, piSubagentsFlag)
	if i < 0 || i+1 >= len(argv) {
		return "", false
	}
	return argv[i+1], true
}

// THE DEFECT, REPRODUCED AND REPAIRED: a configured OpenRouter profile, an explicit `-p pi=codex`.
// The parent moves onto openai-codex by --provider/--model/--models (MM-D30), and before MM-D42 the
// child policy stayed the configured OpenRouter one in settings.json, because the `subagents`
// block is a computed key no launch selection carried. Now it is handed as the codex profile's
// policy, all of it on openai-codex and none of it on openrouter.
func TestHostPiCodexOverConfiguredOpenRouterHandsItsChildPolicy(t *testing.T) {
	home := hostGateHome(t, withComputedPack(t, piSubagentsCodexCfg), map[string]string{piSubagentsOptIn: "1"})
	unchanged := piSettingsFixture(t, home, `{"defaultProvider":"openrouter","subagents":{"defaultProvider":"openrouter",`+
		`"modelScope":{"enforce":true,"strict":true,"allow":["openrouter/*"]}}}`)
	run := hostSelectionRun(t, home, []string{"-p", "pi=codex"}, "pi", "--continue")
	if len(run.argv) < 3 || run.argv[1] != "--provider" || run.argv[2] != "openai-codex" ||
		run.argv[len(run.argv)-1] != "--continue" {
		t.Fatalf("pi got %q, want the codex selection ahead of its own argv\n%s", run.argv, run.errs)
	}
	word, ok := subagentsWord(run.argv)
	if !ok {
		t.Fatalf("pi got no %s: its children would run on the configured openrouter policy\nargv=%q\n%s",
			piSubagentsFlag, run.argv, run.errs)
	}
	var policy struct {
		DefaultProvider string `json:"defaultProvider"`
		DefaultModel    string `json:"defaultModel"`
		ModelScope      struct {
			Enforce bool     `json:"enforce"`
			Strict  bool     `json:"strict"`
			Allow   []string `json:"allow"`
		} `json:"modelScope"`
	}
	if err := json.Unmarshal([]byte(word), &policy); err != nil {
		t.Fatalf("%s %q is not JSON: %v", piSubagentsFlag, word, err)
	}
	if policy.DefaultProvider != "openai-codex" || !strings.HasPrefix(policy.DefaultModel, "openai-codex/") {
		t.Errorf("the child default is %s / %s, want openai-codex's", policy.DefaultProvider, policy.DefaultModel)
	}
	if !policy.ModelScope.Enforce || !policy.ModelScope.Strict || len(policy.ModelScope.Allow) == 0 {
		t.Errorf("the child scope must stay enforced and strict over codex's models: %+v", policy.ModelScope)
	}
	for _, a := range policy.ModelScope.Allow {
		if !strings.HasPrefix(a, "openai-codex/") {
			t.Errorf("the child scope allows %q, off the -p's provider: %s", a, word)
		}
	}
	if strings.Contains(word, "openrouter") {
		t.Errorf("the configured openrouter policy leaked into the -p's: %s", word)
	}
	if !strings.Contains(run.errs, "subagents as this launch composes it") {
		t.Errorf("the handoff must be disclosed:\n%s", run.errs)
	}
	unchanged()
}

// OFF BY DEFAULT: with the opt-in unset, the same launch moves the parent exactly as before and
// hands pi no flag it would exit over, and says why its children keep the file's policy, naming the
// variable that turns the handoff on.
func TestHostPiChildPolicyWaitsOnItsOptIn(t *testing.T) {
	home := hostGateHome(t, withComputedPack(t, piSubagentsCodexCfg), nil)
	unchanged := piSettingsFixture(t, home, `{"defaultProvider":"openrouter"}`)
	run := hostSelectionRun(t, home, []string{"-p", "pi=codex"}, "pi")
	if _, ok := subagentsWord(run.argv); ok {
		t.Errorf("pi got %s with %s unset: %q", piSubagentsFlag, piSubagentsOptIn, run.argv)
	}
	if len(run.argv) < 3 || run.argv[1] != "--provider" || run.argv[2] != "openai-codex" {
		t.Errorf("the parent must still move onto codex: %q\n%s", run.argv, run.errs)
	}
	for _, want := range []string{"composes its own subagents for pi", piSubagentsOptIn + "=1",
		"keeps the subagents its own config holds (~/.pi/agent/settings.json)", "pack-system.md#launch_computed"} {
		if !strings.Contains(run.errs, want) {
			t.Errorf("the launch must say %q:\n%s", want, run.errs)
		}
	}
	unchanged()
}

// TWO PROFILES ON ONE PROVIDER, DIFFERENT ROLES: the parent selection agrees, so only the child
// policy is handed, and a model the user picked in pi since `yolo host apply` stands; each profile
// hands its own; an agreeing -p hands nothing.
func TestHostPiSameProviderProfilesHandTheirOwnRoles(t *testing.T) {
	home := hostGateHome(t, withComputedPack(t, piSubagentsAcmeCfg), map[string]string{piSubagentsOptIn: "1"})
	unchanged := piSettingsFixture(t, home, `{"theme":"native"}`)

	b := hostSelectionRun(t, home, []string{"-p", "pi=acme-b"}, "pi")
	word, ok := subagentsWord(b.argv)
	if !ok || len(b.argv) != 3 {
		t.Fatalf("-p acme-b over configured acme-a handed %q, want only %s and its policy\n%s",
			b.argv, piSubagentsFlag, b.errs)
	}
	// The configured profile's own roles, from the other side.
	hostGateHome(t, withComputedPack(t, strings.Replace(piSubagentsAcmeCfg, `"profile": {"pi": "acme-a"}`, `"profile": {"pi": "acme-b"}`, 1)),
		map[string]string{piSubagentsOptIn: "1"})
	a := hostSelectionRun(t, "", []string{"-p", "pi=acme-a"}, "pi")
	aWord, ok := subagentsWord(a.argv)
	if !ok {
		t.Fatalf("-p acme-a over configured acme-b handed %q\n%s", a.argv, a.errs)
	}
	var pa, pb map[string]any
	_ = json.Unmarshal([]byte(aWord), &pa)
	_ = json.Unmarshal([]byte(word), &pb)
	if reflect.DeepEqual(pa, pb) {
		t.Fatalf("two profiles with different roles handed one policy: %s", aWord)
	}
	if got := pa["agentOverrides"]; !reflect.DeepEqual(got, map[string]any{"worker": map[string]any{"model": "acme/acme-mini"}}) {
		t.Errorf("acme-a's roles = %v, want worker on acme/acme-mini alone", got)
	}

	// An agreeing -p hands nothing: the file is the configured profile's.
	if same := hostSelectionRun(t, "", []string{"-p", "pi=acme-b"}, "pi"); !reflect.DeepEqual(same.argv, []string{"pi"}) {
		t.Errorf("an agreeing -p handed %q", same.argv)
	}
	unchanged()
}

// WHAT DOES NOT CHANGE: no -p keeps today's argv exactly, opt-in or not; a declared subcommand
// runs as typed; and YOLO_NO_LAUNCH_FLAGS=1 skips the child policy with the rest of the selection.
func TestHostPiChildPolicyLeavesTheUnmovedLaunchAlone(t *testing.T) {
	home := hostGateHome(t, withComputedPack(t, piSubagentsCodexCfg), map[string]string{piSubagentsOptIn: "1"})
	unchanged := piSettingsFixture(t, home, `{"defaultProvider":"openrouter"}`)
	if bare := hostSelectionRun(t, home, nil, "pi", "--continue"); !reflect.DeepEqual(bare.argv, []string{"pi", "--continue"}) {
		t.Errorf("with no -p pi got %q, want its own argv\n%s", bare.argv, bare.errs)
	}
	if sub := hostSelectionRun(t, home, []string{"-p", "pi=codex"}, "pi", "update"); !reflect.DeepEqual(sub.argv, []string{"pi", "update"}) {
		t.Errorf("`pi update` under -p got %q, want it as typed\n%s", sub.argv, sub.errs)
	}
	t.Setenv("YOLO_NO_LAUNCH_FLAGS", "1")
	off := hostSelectionRun(t, home, []string{"-p", "pi=codex"}, "pi")
	if !reflect.DeepEqual(off.argv, []string{"pi"}) {
		t.Errorf("YOLO_NO_LAUNCH_FLAGS=1 still handed %q", off.argv)
	}
	unchanged()
}
