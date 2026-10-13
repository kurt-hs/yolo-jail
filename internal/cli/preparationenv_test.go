package cli

import (
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

func preparationEnvMap(env []string) map[string]string {
	out := map[string]string{}
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		if ok {
			out[k] = v
		}
	}
	return out
}

func TestPreparationEnvironmentBoundsInheritedInputs(t *testing.T) {
	providers := jsonx.NewOrderedMap()
	p := jsonx.NewOrderedMap()
	// A claimed name can also be an otherwise allowed transport input.
	p.Set("api_key_env_name", []any{"HTTPS_PROXY", "SYNTHETIC_SECRET"})
	providers.Set("synthetic", p)
	in := []string{"HOME=/host", "PATH=/tools", "XDG_CONFIG_HOME=/config", "XDG_DATA_HOME=/data",
		"SSL_CERT_FILE=/roots", "NIX_REMOTE=daemon", "YOLO_REPO_ROOT=/source", "SSH_AUTH_SOCK=/ssh",
		"GIT_SSH_COMMAND=ssh -F /authorized", "GIT_CONFIG_GLOBAL=/gitconfig", "CONTAINER_HOST=unix:///podman",
		"HTTPS_PROXY=secret", "SYNTHETIC_SECRET=secret", "UNKNOWN_SECRET=secret", "GIT_DIR=/workspace/.git",
		"GIT_INDEX_FILE=/workspace/index", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=credential.helper",
		"GIT_CONFIG_VALUE_0=unapproved", "YOLO_ALLOW_PATCH_FAILURES=1", "YOLO_ALLOW_SOURCE_SKEW=1",
		"YOLO_USER_LAYER=/workspace/layer", "YOLO_JAIL=1", "YOLO_SERVICE_FAKE_TOKEN=secret",
		"GIT_TERMINAL_PROMPT=1", "GCM_INTERACTIVE=always", "SSH_ASKPASS=/interactive", "DISPLAY=:1"}
	original := append([]string(nil), in...)
	got := preparationEnvMap(preparationEnvironment(in, providers, nil))
	for _, k := range []string{"HOME", "PATH", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "SSL_CERT_FILE", "NIX_REMOTE",
		"YOLO_REPO_ROOT", "SSH_AUTH_SOCK", "GIT_SSH_COMMAND", "GIT_CONFIG_GLOBAL", "CONTAINER_HOST"} {
		if _, ok := got[k]; !ok {
			t.Errorf("required transport/runtime input %s removed", k)
		}
	}
	for _, k := range []string{"HTTPS_PROXY", "SYNTHETIC_SECRET", "UNKNOWN_SECRET", "GIT_DIR", "GIT_INDEX_FILE",
		"GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0", "YOLO_ALLOW_PATCH_FAILURES", "YOLO_ALLOW_SOURCE_SKEW",
		"YOLO_USER_LAYER", "YOLO_JAIL", "YOLO_SERVICE_FAKE_TOKEN", "SSH_ASKPASS", "DISPLAY"} {
		if _, ok := got[k]; ok {
			t.Errorf("unbounded input %s inherited", k)
		}
	}
	for k, v := range map[string]string{"GIT_TERMINAL_PROMPT": "0", "GCM_INTERACTIVE": "Never", "SSH_ASKPASS_REQUIRE": "never"} {
		if got[k] != v {
			t.Errorf("%s=%q, want %q", k, got[k], v)
		}
	}
	if !slices.Equal(in, original) {
		t.Fatal("input mutated")
	}
}

func TestPreparationEnvironmentCredentialCaseVariants(t *testing.T) {
	for _, declared := range []string{"https_proxy", "HTTPS_PROXY", "HtTpS_PrOxY"} {
		for _, authority := range []string{"user provider", "pack provider", "program provider"} {
			t.Run(authority+"/"+declared, func(t *testing.T) {
				var providers *jsonx.OrderedMap
				var packs []*packload.Pack
				if authority == "user provider" {
					providers = jsonx.NewOrderedMap()
					entry := jsonx.NewOrderedMap()
					entry.Set("api_key_env_name", declared)
					providers.Set("synthetic", entry)
				} else {
					manifest := `{"contributes":[{"kind":"provider","name":"synthetic","api_key_env_name":"` + declared + `","endpoints":{"openai":{"base_url":"https://example.invalid"}}}]}`
					if authority == "program provider" {
						manifest = `{"provider_keys":{"acme":[{"surface":"acme/settings","pointer":"/env/` + declared + `"}]},"provider_env":{"acme":{"surface":"acme/settings","pointer":"/env"}},"contributes":[{"kind":"program","bin":"acme","via":"npm","package":"acme"}]}`
					}
					m, problems := packdecl.Decode([]byte(manifest))
					if len(problems) > 0 {
						t.Fatal(problems)
					}
					packs = []*packload.Pack{{Name: "synthetic", Decl: m}}
				}
				got := preparationEnvMap(preparationEnvironment([]string{
					"HTTPS_PROXY=synthetic-upper", "https_proxy=synthetic-lower", "HtTpS_PrOxY=synthetic-mixed",
					"HTTP_PROXY=http://upper.invalid", "http_proxy=http://lower.invalid",
				}, providers, packs))
				for _, name := range []string{"HTTPS_PROXY", "https_proxy", "HtTpS_PrOxY"} {
					if _, ok := got[name]; ok {
						t.Errorf("claimed credential alias %s survived", name)
					}
				}
				for name, value := range map[string]string{"HTTP_PROXY": "http://upper.invalid", "http_proxy": "http://lower.invalid"} {
					if got[name] != value {
						t.Errorf("allowed transport %s changed", name)
					}
				}
			})
		}
	}
}

func TestPreparationEnvironmentExcludesDeclaredProgramProviderEnv(t *testing.T) {
	m, problems := packdecl.Decode([]byte(`{"provider_keys":{"acme":[{"surface":"acme/settings","pointer":"/env/SSL_CERT_FILE"}]},
 "provider_env":{"acme":{"surface":"acme/settings","pointer":"/env"}},
 "contributes":[{"kind":"program","bin":"acme","via":"npm","package":"acme"}]}`))
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	env := preparationEnvMap(preparationEnvironment([]string{"SSL_CERT_FILE=agent-specific", "HOME=/host"}, nil,
		[]*packload.Pack{{Name: "acme", Decl: m}}))
	if _, ok := env["SSL_CERT_FILE"]; ok {
		t.Fatal("declared provider environment survived allowlist")
	}
	if env["HOME"] != "/host" {
		t.Fatal("home lost")
	}
}

func TestPreparationEnvironmentRealSpawnUsesAuthorizedDeclarations(t *testing.T) {
	for _, names := range [][2]string{{"https_proxy", "all_proxy"}, {"HTTPS_PROXY", "ALL_PROXY"}, {"HtTpS_PrOxY", "AlL_PrOxY"}} {
		t.Run(names[0], func(t *testing.T) {
			testPreparationEnvironmentRealSpawn(t, names[0], names[1])
		})
	}
}

func testPreparationEnvironmentRealSpawn(t *testing.T, packCredential, userCredential string) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("YOLO_USER_LAYER", "")
	dir := filepath.Join(home, "pack")
	writeFile(t, filepath.Join(dir, "pack.json"), `{"contributes":[{"kind":"provider","name":"synthetic",
 "api_key_env_name":"`+packCredential+`","endpoints":{"openai":{"base_url":"https://example.invalid"}}}]}`)
	writeFile(t, filepath.Join(home, ".config", "yolo-jail", "config.jsonc"), `{"packs":[{"name":"synthetic","source":"file://`+dir+`"}],
 "providers":{"custom":{"api_key_env_name":"`+userCredential+`","endpoints":{"openai":{"base_url":"https://example.invalid"}}}}}`)
	t.Setenv("HTTPS_PROXY", "pack-secret")
	t.Setenv("https_proxy", "pack-secret-lower")
	t.Setenv("ALL_PROXY", "user-secret")
	t.Setenv("all_proxy", "user-secret-lower")
	t.Setenv("UNKNOWN_SECRET", "secret")
	t.Setenv("YOLO_ALLOW_PATCH_FAILURES", "1")
	old := startPreparedBackgroundCommand
	t.Cleanup(func() { startPreparedBackgroundCommand = old })
	calls := 0
	startPreparedBackgroundCommand = func(c *exec.Cmd) error {
		calls++
		if c.Env == nil {
			t.Fatal("spawn implicitly inherits environment")
		}
		env := preparationEnvMap(c.Env)
		for _, k := range []string{"HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy", "UNKNOWN_SECRET", "YOLO_ALLOW_PATCH_FAILURES"} {
			if _, ok := env[k]; ok {
				t.Errorf("real spawn retained %s", k)
			}
		}
		if env["HOME"] != home || env["GIT_TERMINAL_PROMPT"] != "0" {
			t.Error("bounded environment lost home or noninteractive control")
		}
		if !slices.Equal(c.Args[1:], []string{"internal", backgroundAdvanceVerb, "--runtime=podman"}) {
			t.Errorf("argv changed: %v", c.Args)
		}
		if c.Stdout == nil || c.Stderr == nil {
			t.Fatal("detached log streams absent")
		}
		return nil
	}
	if err := startBackgroundAdvance([]string{"internal", backgroundAdvanceVerb, "--runtime=podman"}, filepath.Join(home, "detach.log")); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("%d starts", calls)
	}
	// Config errors must not turn into an unfiltered process launch.
	writeFile(t, filepath.Join(home, ".config", "yolo-jail", "config.jsonc"), `{"providers":`)
	if err := startBackgroundAdvance(nil, filepath.Join(home, "detach.log")); err == nil || !strings.Contains(err.Error(), "yolo check --no-build") {
		t.Fatalf("config error lacks repair: %v", err)
	}
	if calls != 1 {
		t.Fatal("unreadable authority still spawned")
	}
}
