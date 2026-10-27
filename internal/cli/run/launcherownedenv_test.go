package run

// launcherownedenv_test.go pins that no config env entry can set a launcher-owned contract name
// (macosuser.LauncherOwnedEnvNames) in a jail: an env_sources entry naming YOLO_FORK_BUILDS would
// otherwise decide which file a program's launcher runs. On macos-user
// the composed launch env is relayed to the darwin bootstrap for those names, so the composition
// must not carry them; on podman the env_sources values reach only the user env file as defaults,
// which the boot never takes a YOLO_ name from (entrypoint's launcherContractKey).

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/macosuser"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

func TestAConfigEnvEntryCannotSetALauncherOwnedName(t *testing.T) {
	home := packHome(t)
	t.Setenv("YOLO_VERSION", "")
	t.Setenv("YOLO_PACK_ROOT", "")
	dir := filepath.Join(home, ".config", "yolo-jail")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	evilForks := `{"x":{"key":"evil"}}`
	const spoofedSecurityCarrier = "YOLO_CLAUDE_SECURITY_COMPAT"
	cfg := `{"packs":[],"env_sources":[{"` + entrypoint.ForkBuildsEnv + `":` + jsonQuote(evilForks) +
		`,"` + spoofedSecurityCarrier + `":"account","KEEP_ME":"1"}]}`
	if err := os.WriteFile(filepath.Join(dir, "config.jsonc"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	// macos-user: the session's launch env, the one the run plan relays to the bootstrap.
	var stdout, stderr bytes.Buffer
	o := dispatchOptions(t, t.TempDir(), "macos-user", &stdout, &stderr, nil)
	o.MacosUserLaunchProbes = setUpMacProbes
	var env *jsonx.OrderedMap
	o.MacosUserRun = func(_ *jsonx.OrderedMap, _ string, _, _ []string, _, _ string, _ macosuser.HomeOverlay,
		_ macosuser.HostContext, _ bool, e *jsonx.OrderedMap, _ []packload.BlockedTool, _ macosuser.JailDaemons) int {
		env = e
		return 0
	}
	if rc := Run(*o); rc != 0 || env == nil {
		t.Fatalf("Run() = %d, env %v\n%s%s", rc, env, stdout.String(), stderr.String())
	}
	if _, ok := env.Get("KEEP_ME"); !ok {
		t.Fatalf("the fixture is not the case: env_sources did not reach the launch env: %v", env.Keys())
	}
	for _, k := range []string{entrypoint.ForkBuildsEnv, spoofedSecurityCarrier} {
		if v, ok := env.Get(k); ok {
			t.Errorf("macos-user: env_sources set the launcher-owned %s = %v in the launch env", k, v)
		}
	}

	// podman: neither name reaches the container's argv from config.
	argv, _ := fakePodmanLaunch(t, func(*Options) {})
	for _, a := range argv {
		if strings.Contains(a, "evil") {
			t.Errorf("podman: a config value reached the argv: %s", a)
		}
	}
}

func jsonQuote(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}
