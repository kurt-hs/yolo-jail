package cli

// hostpisubagents_test.go pins MM-D42 at the host notch (docs/design/model-lists-and-pickers.md):
// an explicit `yolo host -p <profile> -- pi` hands pi-subagents the CHILD MODEL POLICY the -p's
// profile composes (the settings derive's `subagents` block: defaultProvider, defaultModel,
// agentOverrides and modelScope), for that process alone, as `--subagents-model-settings <json>`,
// and never writes it into ~/.pi/agent/settings.json. Every cell runs hostMain to the exec over the
// SHIPPED pi pack, so the pack's declaration, the derive and the host step are asked through the
// front door. The flag ships behind YOLO_PI_SUBAGENTS_MODEL_SETTINGS=1 until pi-subagents reads
// it, since pi 1.0.1 exits over an extension flag no extension registers.

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

var updatePiSubagentsGolden = flag.Bool("update-pi-subagents-golden", false,
	"rewrite internal/cli/testdata/pi-subagents-model-settings.golden.json from the launch")

// piSubagentsOptIn is the variable the shipped pi pack gates the flag behind.
const piSubagentsOptIn = "YOLO_PI_SUBAGENTS_MODEL_SETTINGS"

// piSubagentsFlag is the flag's word.
const piSubagentsFlag = "--subagents-model-settings"

// piSubagentsCodexCfg configures openrouter as pi's profile, with openai-auth's codex profile
// selectable by -p.
const piSubagentsCodexCfg = `{"packs": ["claude", "pi", "openrouter", "openai-auth"], ` +
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
	home := hostGateHome(t, piSubagentsCodexCfg, map[string]string{piSubagentsOptIn: "1"})
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
	home := hostGateHome(t, piSubagentsCodexCfg, nil)
	unchanged := piSettingsFixture(t, home, `{"defaultProvider":"openrouter"}`)
	run := hostSelectionRun(t, home, []string{"-p", "pi=codex"}, "pi")
	if _, ok := subagentsWord(run.argv); ok {
		t.Errorf("pi got %s with %s unset: %q", piSubagentsFlag, piSubagentsOptIn, run.argv)
	}
	if len(run.argv) < 3 || run.argv[1] != "--provider" || run.argv[2] != "openai-codex" {
		t.Errorf("the parent must still move onto codex: %q\n%s", run.argv, run.errs)
	}
	for _, want := range []string{"composes its own subagents for pi", piSubagentsOptIn + "=1",
		"keeps the subagents its own config holds (~/.pi/agent/settings.json)", "MM-D42"} {
		if !strings.Contains(run.errs, want) {
			t.Errorf("the launch must say %q:\n%s", want, run.errs)
		}
	}
	unchanged()
}

// TWO PROFILES ON ONE PROVIDER, DIFFERENT ROLES: the parent selection agrees, so only the child
// policy is handed, and a model the user picked in pi since `yolo host apply` stands; each profile
// hands its own; an agreeing -p hands nothing. The acme-b bytes are the golden the Pi side decodes.
func TestHostPiSameProviderProfilesHandTheirOwnRoles(t *testing.T) {
	// The package directory, before hostGateHome moves the working directory into a temp dir.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join(wd, "testdata", "pi-subagents-model-settings.golden.json")
	home := hostGateHome(t, piSubagentsAcmeCfg, map[string]string{piSubagentsOptIn: "1"})
	unchanged := piSettingsFixture(t, home, `{"theme":"native"}`)

	b := hostSelectionRun(t, home, []string{"-p", "pi=acme-b"}, "pi")
	word, ok := subagentsWord(b.argv)
	if !ok || len(b.argv) != 3 {
		t.Fatalf("-p acme-b over configured acme-a handed %q, want only %s and its policy\n%s",
			b.argv, piSubagentsFlag, b.errs)
	}
	if *updatePiSubagentsGolden {
		writeFile(t, golden, word+"\n")
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v (run with -update-pi-subagents-golden to write it)", err)
	}
	if word != strings.TrimSuffix(string(want), "\n") {
		t.Errorf("%s bytes changed; the Pi side decodes the golden:\n got %s\nwant %s", piSubagentsFlag, word, want)
	}

	// The configured profile's own roles, from the other side.
	hostGateHome(t, strings.Replace(piSubagentsAcmeCfg, `"profile": {"pi": "acme-a"}`, `"profile": {"pi": "acme-b"}`, 1),
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
	home := hostGateHome(t, piSubagentsCodexCfg, map[string]string{piSubagentsOptIn: "1"})
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
