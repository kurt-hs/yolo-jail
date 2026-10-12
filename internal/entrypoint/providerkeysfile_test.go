package entrypoint

// providerkeysfile_test.go pins the launcher half of PP-D14 (docs/design/providers-and-profiles-
// redesign.md, ruled 2026-10-11): every carrier sources the agent's PROVIDER-KEYS FILE after its
// env file, so a provider variable the file forces beats both the env file's default form and
// the value the launching process inherited; and an agent the image provides that has only a
// provider-keys file this entry (an unprofiled claude) still gets a carrier, or nothing would
// clear the switch it inherited. Each cell RUNS the generated script. No real agent runs.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// writeProviderKeysFile puts one agent's provider-keys file where the host launcher writes it.
func writeProviderKeysFile(t *testing.T, home, agent, body string) {
	t.Helper()
	p := ProviderKeysFile(home, agent)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestTheLaunchersSourceTheProviderKeysFileLast(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not found")
	}
	for _, kind := range []string{"npm", "native"} {
		t.Run(kind, func(t *testing.T) {
			home := t.TempDir()
			logPath := filepath.Join(home, "seen.log")
			launcher := probeLauncher(t, home, kind, logPath, launcherServers{})
			writeAgentEnvFile(t, home, "probe", "export SCOPED=${SCOPED:-'default'}\n")
			writeProviderKeysFile(t, home, "probe", "export SCOPED='selected'\nunset OTHER\n")
			out, rc := runScript(t, launcher, nil, []string{"HOME=" + home, "PATH=/usr/bin:/bin",
				"SCOPED=inherited", "OTHER=inherited"})
			if rc != 0 {
				t.Fatalf("the %s launcher exited %d:\n%s", kind, rc, out)
			}
			if got := strings.Join(logLines(t, logPath), " "); got != "SCOPED=selected OTHER=" {
				t.Errorf("the %s launcher handed its program %q, want the provider-keys file's "+
					"values over the inherited ones\n%s", kind, got, out)
			}
		})
	}
}

func TestAnImageProvidedAgentWithOnlyAProviderKeysFileGetsACarrier(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not found")
	}
	image := t.TempDir()
	orig := imageProbeBase
	imageProbeBase = image
	t.Cleanup(func() { imageProbeBase = orig })
	home := t.TempDir()
	e := NewEnv(map[string]string{"JAIL_HOME": home, "YOLO_PACK_ROOT": stageShippedPacks(t)})
	envReporter(t, filepath.Join(image, "opencode"), filepath.Join(home, "opencode.log"))
	writeProviderKeysFile(t, home, "opencode", "unset SCOPED\n")
	runLaunchDirPasses(t, e)
	carrier := filepath.Join(e.LaunchDir(), "opencode")
	if _, err := os.Stat(carrier); err != nil {
		t.Fatalf("no carrier for an image-provided agent with a provider-keys file: %v", err)
	}
	path := strings.Join([]string{e.LaunchDir(), image, "/usr/bin", "/bin"}, ":")
	out, rc := runScript(t, carrier, nil, []string{"HOME=" + home, "PATH=" + path, "SCOPED=inherited"})
	if rc != 0 {
		t.Fatalf("the carrier exited %d:\n%s", rc, out)
	}
	if got := strings.Join(logLines(t, filepath.Join(home, "opencode.log")), " "); got != "SCOPED= OTHER=" {
		t.Errorf("opencode ran with %q, want the inherited value cleared\n%s", got, out)
	}
}

// The boot's model of an agent's environment (agentEnvLookup, which decides the per-agent MCP
// tables) applies the provider-keys file after the env file, as the launcher does.
func TestAgentEnvLookupAppliesTheProviderKeysFile(t *testing.T) {
	home := t.TempDir()
	writeAgentEnvFile(t, home, "probe", "export SCOPED=${SCOPED:-'default'}\nexport KEPT='k'\n")
	writeProviderKeysFile(t, home, "probe", "export SCOPED='selected'\nunset GONE\n")
	e := NewEnv(map[string]string{"JAIL_HOME": home, "GONE": "boot"})
	look := agentEnvLookup(e, "probe")
	if v, _ := look("SCOPED"); v != "selected" {
		t.Errorf("SCOPED = %q, want the provider-keys file's", v)
	}
	if _, ok := look("GONE"); ok {
		t.Error("a variable the provider-keys file unsets must read as unset")
	}
	if v, _ := look("KEPT"); v != "k" {
		t.Errorf("KEPT = %q", v)
	}
}
