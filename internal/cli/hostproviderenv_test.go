package cli

// hostproviderenv_test.go pins PP-D14 at `yolo host --` (docs/design/providers-and-profiles-
// redesign.md, the maintainer's 2026-10-11 ruling: "a Claude started from a Claude shell needs
// to follow the same auth path everywhere"): claude's provider variables are what its profile
// selection says, whatever the invoking shell exported. Through hostMain to the exec, so deleting
// the clearing in hostComposition.environ fails here.

import (
	"strings"
	"testing"
)

func TestHostLaunchMakesClaudesProviderVariablesTheSelections(t *testing.T) {
	// The shell a Bedrock claude leaves behind: its switch, a gateway, a Bedrock model id, and
	// a subscription model id plus a variable no pack decides, which are the user's.
	shell := map[string]string{
		"CLAUDE_CODE_USE_BEDROCK":    "1",
		"ANTHROPIC_BASE_URL":         "http://127.0.0.1:9/gateway",
		"ANTHROPIC_SMALL_FAST_MODEL": "global.anthropic.claude-haiku-4-5",
		"ANTHROPIC_MODEL":            "opus",
		"YOLO_TEST_UNRELATED":        "kept",
	}

	t.Run("subscription", func(t *testing.T) {
		rc, env, errs := hostGateRunIn(t, claudeAlone, shell, nil, "claude", nil)
		if rc != 0 || env == nil {
			t.Fatalf("launch did not exec: rc=%d\n%s", rc, errs)
		}
		for _, k := range []string{"CLAUDE_CODE_USE_BEDROCK", "ANTHROPIC_BASE_URL",
			"ANTHROPIC_SMALL_FAST_MODEL", "AWS_REGION"} {
			if v, ok := env[k]; ok {
				t.Errorf("%s reached a claude on its subscription as %q", k, v)
			}
		}
		if env["ANTHROPIC_MODEL"] != "opus" || env["YOLO_TEST_UNRELATED"] != "kept" {
			t.Errorf("a value the selection does not decide must stay the user's: %v", env)
		}
		if !strings.Contains(errs, "yolo host: cleared CLAUDE_CODE_USE_BEDROCK, ANTHROPIC_BASE_URL") ||
			!strings.Contains(errs, "select a profile that sets them") {
			t.Errorf("the launch must say what it cleared and how to set it:\n%s", errs)
		}
	})

	t.Run("bedrock", func(t *testing.T) {
		on := map[string]string{}
		for k, v := range shell {
			on[k] = v
		}
		on["CLAUDE_CODE_USE_BEDROCK"] = "0"
		rc, env, errs := hostGateRunIn(t, claudeAlone, on, []string{"-p", "bedrock"}, "claude", nil)
		if rc != 0 || env == nil {
			t.Fatalf("launch did not exec: rc=%d\n%s", rc, errs)
		}
		if env["CLAUDE_CODE_USE_BEDROCK"] != "1" {
			t.Errorf("the selection's switch must win over the shell's 0: %q", env["CLAUDE_CODE_USE_BEDROCK"])
		}
		if env["AWS_REGION"] != hostGateRegion {
			t.Errorf("a region Bedrock reads from the shell is the selection's input, kept: %q", env["AWS_REGION"])
		}
		if env["ANTHROPIC_SMALL_FAST_MODEL"] == "" {
			t.Errorf("a Bedrock model id is Bedrock's own on Bedrock: %v", env)
		}
		if _, ok := env["ANTHROPIC_BASE_URL"]; ok {
			t.Errorf("a gateway the Bedrock selection does not set must not reach it: %v", env["ANTHROPIC_BASE_URL"])
		}
	})
}
