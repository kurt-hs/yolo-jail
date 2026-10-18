package run

// providerenvfiles_test.go pins PP-D14 at the jail notch (docs/design/providers-and-profiles-
// redesign.md, the maintainer's 2026-10-11 ruling: "a Claude started from a Claude shell needs to
// follow the same auth path everywhere"): the files every entry writes make claude's provider
// variables exactly what its profile selection says, whatever the launching process inherited.
// Through writeAgentEnvFiles — the one writer every container backend and macos-user call — and
// then RUN by bash in the order the launcher sources them, so deleting the writer's call or the
// forced form fails here. No agent runs.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// sourceAgentFiles runs bash with the incoming environment parent, sources agent's env file
// and provider-keys file under home exactly as the launcher does, and prints each of names as
// `NAME=value` or `NAME=unset`, one per line. It returns the output and stderr.
func sourceAgentFiles(t *testing.T, home, agent string, parent map[string]string, names ...string) (string, string) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not found")
	}
	script := `f="$HOME/` + entrypoint.AgentEnvDirRel + `/` + agent + `.sh"; [ -r "$f" ] && . "$f"
f="$HOME/` + entrypoint.AgentEnvDirRel + `/` + entrypoint.ProviderKeysSubdir + `/` + agent + `.sh"; [ -r "$f" ] && . "$f"
`
	for _, n := range names {
		script += `printf '%s=%s\n' ` + n + ` "${` + n + `-unset}"` + "\n"
	}
	cmd := exec.Command("bash", "-c", script)
	cmd.Env = []string{"HOME=" + home, "PATH=/usr/bin:/bin"}
	for k, v := range parent {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("bash: %v\n%s", err, stderr.String())
	}
	return string(out), stderr.String()
}

func TestAClaudeStartedFromAnotherClaudesShellFollowsItsOwnSelection(t *testing.T) {
	home := retireHome(t)
	writeUserPacks(t, home, `[]`)
	writeAWSConfig(t, home, "[default]\nregion = eu-north-1\n")
	o := retireOptions(t, discardBuf())
	o.Getenv = shellWith(map[string]string{"HOME": home})
	packs := []*packload.Pack{officialPack(t, "claude"), officialPack(t, "bedrock")}

	// What a Bedrock claude's shell hands the claude started in it.
	bedrockParent := map[string]string{
		"CLAUDE_CODE_USE_BEDROCK":      "1",
		"AWS_REGION":                   "us-east-1",
		"ANTHROPIC_DEFAULT_OPUS_MODEL": "global.anthropic.claude-opus-5-5",
		"ANTHROPIC_MODEL":              "opus",
		"YOLO_TEST_UNRELATED":          "kept",
	}

	t.Run("claude not on bedrock", func(t *testing.T) {
		wsState := t.TempDir()
		channel := channelFor(t, o, newConfig(), packs, emptyEnv())
		writeAgentEnvFiles(wsState, entrypoint.AgentEnvDirRel, entrypoint.AgentEnvDirRel, jailHome, channel)
		if _, err := os.Stat(filepath.Join(wsState, entrypoint.AgentEnvDirRel,
			entrypoint.ProviderKeysSubdir, "claude.sh")); err != nil {
			t.Fatalf("an unprofiled claude must still get its provider-keys file: %v", err)
		}
		// The ruling's own probe: the inherited switch is gone.
		got, stderr := sourceAgentFiles(t, wsState, "claude", map[string]string{"CLAUDE_CODE_USE_BEDROCK": "1"},
			"CLAUDE_CODE_USE_BEDROCK")
		if got != "CLAUDE_CODE_USE_BEDROCK=unset\n" {
			t.Errorf("a claude whose profile is not bedrock inherited the switch: %q", got)
		}
		if !strings.Contains(stderr, "yolo: claude: replaced or cleared CLAUDE_CODE_USE_BEDROCK") ||
			!strings.Contains(stderr, "select a profile") {
			t.Errorf("the launcher must say what it cleared and how to set it: %q", stderr)
		}
		got, _ = sourceAgentFiles(t, wsState, "claude", bedrockParent, "CLAUDE_CODE_USE_BEDROCK",
			"AWS_REGION", "ANTHROPIC_DEFAULT_OPUS_MODEL", "ANTHROPIC_MODEL", "YOLO_TEST_UNRELATED")
		want := "CLAUDE_CODE_USE_BEDROCK=unset\nAWS_REGION=unset\nANTHROPIC_DEFAULT_OPUS_MODEL=unset\n" +
			"ANTHROPIC_MODEL=opus\nYOLO_TEST_UNRELATED=kept\n"
		if got != want {
			t.Errorf("claude off bedrock from a Bedrock claude's shell:\ngot  %q\nwant %q", got, want)
		}
		// And from a clean shell, nothing is said.
		if _, stderr := sourceAgentFiles(t, wsState, "claude", nil, "CLAUDE_CODE_USE_BEDROCK"); stderr != "" {
			t.Errorf("a clean shell has nothing to clear, so nothing may be said: %q", stderr)
		}
	})

	t.Run("claude on bedrock", func(t *testing.T) {
		wsState := t.TempDir()
		on := *o
		on.UseProfiles = map[string]string{"claude": "bedrock"}
		channel := channelFor(t, &on, newConfig(), packs, emptyEnv())
		writeAgentEnvFiles(wsState, entrypoint.AgentEnvDirRel, entrypoint.AgentEnvDirRel, jailHome, channel)
		got, _ := sourceAgentFiles(t, wsState, "claude", map[string]string{"CLAUDE_CODE_USE_BEDROCK": "0"},
			"CLAUDE_CODE_USE_BEDROCK")
		if got != "CLAUDE_CODE_USE_BEDROCK=1\n" {
			t.Errorf("a claude on bedrock must have the selection's switch even when the parent "+
				"exported 0: %q", got)
		}
	})
}
