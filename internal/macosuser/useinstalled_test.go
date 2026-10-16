package macosuser

import (
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/patchrecovery"
)

// TestUseInstalledReachesTheSessionEnv: `YOLO_USE_INSTALLED=claude yolo -- claude` on a macos-user
// host must reach the launcher that reads it at run time (docs/design/program-delivery.md PD-NF2).
// Every sandboxed argv sources the session's env file after `env -i`, so that file is the channel:
// driven through buildPlan, so dropping the forwarding in MacosSandboxEnv fails this. Absent when
// unset, so an ordinary session's file does not grow it.
func TestUseInstalledReachesTheSessionEnv(t *testing.T) {
	for _, value := range []string{"claude", ""} {
		deps := mockDeps(nil)
		deps.Getenv = func(name string) string {
			if name == patchrecovery.UseInstalledEnv {
				return value
			}
			return ""
		}
		plan := buildPlan(deps, newOpts("/Users/Shared/yolo/proj"), mockDarwin())
		if value == "" {
			if strings.Contains(plan.EnvFileContent, patchrecovery.UseInstalledEnv) {
				t.Errorf("an unset hatch reached the session env file:\n%s", plan.EnvFileContent)
			}
			continue
		}
		if !SandboxEnvFileSets(plan.EnvFileContent, patchrecovery.UseInstalledEnv, value) {
			t.Errorf("the session env file does not export %s=%s:\n%s",
				patchrecovery.UseInstalledEnv, value, plan.EnvFileContent)
		}
	}
}
