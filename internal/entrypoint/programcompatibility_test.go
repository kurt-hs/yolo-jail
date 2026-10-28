package entrypoint

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// programcompatibility_test.go drives the shipped declaration through the real launcher
// generator and native launcher. The binaries are fakes: --version is the only probe and
// normal/update invocations only write a local log.
func TestShippedCopilotCompatibilityRefusesOnlyMacosUserSelf(t *testing.T) {
	orig := imageProbeBase
	imageProbeBase = t.TempDir()
	t.Cleanup(func() { imageProbeBase = orig })

	for _, version := range []string{"1.0.93", "1.0.103", "1.0.105"} {
		for _, tc := range []struct {
			name       string
			context    string
			native     bool
			updates    string
			hatch      string
			wantRefuse bool
		}{
			{name: "self", context: `{"mode":1,"workspace":"/w","home":"/h"}`, native: true, updates: "true", wantRefuse: true},
			{name: "self-updates-disabled-use-installed", context: `{"mode":1,"workspace":"/w","home":"/h"}`, native: true, updates: "false", hatch: "1", wantRefuse: true},
			{name: "account", context: `{"mode":0,"workspace":"/w","home":"/h"}`, native: true, updates: "false"},
			{name: "container", updates: "false"},
		} {
			t.Run(version+"/"+tc.name, func(t *testing.T) {
				p := newCopilotCompatibilityProbe(t, tc.context, tc.native, tc.updates, version)
				out, err := p.run(t, tc.hatch)
				log := p.log(t)
				refused := strings.Contains(out, "macos-user self's required SecurityServer deny")
				if refused != tc.wantRefuse {
					t.Errorf("refused=%v want=%v (err=%v):\n%s", refused, tc.wantRefuse, err, out)
				}
				if tc.wantRefuse {
					if err == nil || strings.Contains(log, "UPDATE") || strings.Contains(log, "EXEC") {
						t.Errorf("affected installed Copilot reached update or execution (err=%v, log=%q):\n%s", err, log, out)
					}
				} else if err != nil || !strings.Contains(log, "EXEC "+version) {
					t.Errorf("compatible control did not execute (err=%v, log=%q):\n%s", err, log, out)
				}
			})
		}
	}
}

func TestShippedCopilotInvalidGenerationMetadataRefuses(t *testing.T) {
	orig := imageProbeBase
	imageProbeBase = t.TempDir()
	t.Cleanup(func() { imageProbeBase = orig })

	for _, tc := range []struct {
		name, context string
	}{
		{name: "missing", context: ""},
		{name: "malformed", context: `{`},
		{name: "missing-mode", context: `{"workspace":"/w","home":"/h"}`},
		{name: "null-mode", context: `{"mode":null,"workspace":"/w","home":"/h"}`},
		{name: "unknown-mode", context: `{"mode":2,"workspace":"/w","home":"/h"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newCopilotCompatibilityProbe(t, tc.context, true, "true", "1.0.92")
			out, err := p.run(t, "")
			if err == nil || !strings.Contains(out, "invalid macos-user launch context") || strings.Contains(p.log(t), "UPDATE") || strings.Contains(p.log(t), "EXEC") {
				t.Fatalf("invalid native metadata did not refuse before update/execution (err=%v, log=%q):\n%s", err, p.log(t), out)
			}
		})
	}
}

func TestShippedCopilotCompatibilityRefusalNamesIssueAndRemedy(t *testing.T) {
	orig := imageProbeBase
	imageProbeBase = t.TempDir()
	t.Cleanup(func() { imageProbeBase = orig })
	p := newCopilotCompatibilityProbe(t, `{"mode":1,"workspace":"/w","home":"/h"}`, true, "false", "1.0.103")
	out, err := p.run(t, "1")
	if err == nil || !strings.Contains(out, "https://github.com/github/copilot-cli/issues/5085") ||
		!strings.Contains(out, "Use a container backend (podman or container)") ||
		!strings.Contains(out, "only after the upstream TLS implementation no longer reproduces the incompatibility") {
		t.Fatalf("compatibility refusal lacks the tracked issue and workable remedy (err=%v):\n%s", err, out)
	}
}

func TestShippedCopilotMalformedVersionTokensFailClosed(t *testing.T) {
	orig := imageProbeBase
	imageProbeBase = t.TempDir()
	t.Cleanup(func() { imageProbeBase = orig })

	for _, version := range []string{"1.0.92.103", "1.0.", "1234567890.0.1"} {
		t.Run(version, func(t *testing.T) {
			p := newCopilotCompatibilityProbe(t, `{"mode":1,"workspace":"/w","home":"/h"}`, true, "false", version)
			out, err := p.run(t, "")
			if err == nil || strings.Contains(p.log(t), "EXEC") ||
				(!strings.Contains(out, "cannot parse copilot's declared version probe") && !strings.Contains(out, "outside the supported numeric comparison bound")) {
				t.Fatalf("malformed version token did not fail closed (err=%v, log=%q):\n%s", err, p.log(t), out)
			}
		})
	}
}

func TestShippedCopilotVersionProbeAcceptsVendorLabelAndNumericComparison(t *testing.T) {
	orig := imageProbeBase
	imageProbeBase = t.TempDir()
	t.Cleanup(func() { imageProbeBase = orig })
	for _, tc := range []struct {
		name, version string
		wantRefuse    bool
	}{
		{name: "affected", version: "Copilot CLI v1.0.103 (stable)", wantRefuse: true},
		{name: "compatible", version: "Copilot CLI v1.0.92 (stable)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newCopilotCompatibilityProbe(t, `{"mode":1,"workspace":"/w","home":"/h"}`, true, "false", tc.version)
			out, err := p.run(t, "")
			refused := strings.Contains(out, "macos-user self's required SecurityServer deny")
			if refused != tc.wantRefuse || strings.Contains(out, "cannot parse copilot's declared version probe") ||
				(err == nil) == tc.wantRefuse || (tc.wantRefuse && strings.Contains(p.log(t), "EXEC")) ||
				(!tc.wantRefuse && !strings.Contains(p.log(t), "EXEC")) {
				t.Fatalf("vendor-labeled version was not compared numerically (refused=%v, err=%v, log=%q):\n%s", refused, err, p.log(t), out)
			}
		})
	}
}

func TestShippedCopilotVersionCompatibilityTriesProbeAlternatives(t *testing.T) {
	orig := imageProbeBase
	imageProbeBase = t.TempDir()
	t.Cleanup(func() { imageProbeBase = orig })
	p := newCopilotCompatibilityProbe(t, `{"mode":1,"workspace":"/w","home":"/h"}`, true, "false", "1.0.92", "--version", "-v")
	binary := filepath.Join(p.home, ".local", "bin", "copilot")
	body := `#!/bin/sh
case "$*" in
  --version) printf '%s\n' '--version' >> "$HOME/probes.log"; exit 2 ;;
  -v) printf '%s\n' '-v' >> "$HOME/probes.log"; echo 1.0.92; exit 0 ;;
  *) printf 'EXEC %s\n' "$*" >> "$HOME/calls.log"; exit 0 ;;
esac
`
	if err := os.WriteFile(binary, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := p.run(t, "")
	probes, readErr := os.ReadFile(filepath.Join(p.home, "probes.log"))
	if err != nil || readErr != nil || !strings.HasPrefix(string(probes), "--version\n-v\n") || !strings.Contains(p.log(t), "EXEC --yolo chat") {
		t.Fatalf("version check did not try declared alternatives separately (err=%v, readErr=%v, probes=%q, log=%q):\n%s", err, readErr, probes, p.log(t), out)
	}
}

func TestShippedCopilotUnaffectedVersionRunsInMacosUserSelf(t *testing.T) {
	orig := imageProbeBase
	imageProbeBase = t.TempDir()
	t.Cleanup(func() { imageProbeBase = orig })
	p := newCopilotCompatibilityProbe(t, `{"mode":1,"workspace":"/w","home":"/h"}`, true, "false", "1.0.92")
	out, err := p.run(t, "1")
	if err != nil || !strings.Contains(p.log(t), "EXEC 1.0.92") || strings.Contains(out, "macos-user self's required SecurityServer deny") {
		t.Fatalf("unaffected Copilot 1.0.92 did not run in self mode (err=%v, log=%q):\n%s", err, p.log(t), out)
	}
}

func TestShippedCopilotVersionProbeRemainsUsableWhenAffected(t *testing.T) {
	orig := imageProbeBase
	imageProbeBase = t.TempDir()
	t.Cleanup(func() { imageProbeBase = orig })
	p := newCopilotCompatibilityProbe(t, `{"mode":1,"workspace":"/w","home":"/h"}`, true, "true", "1.0.103")
	out, err := p.runArgs(t, "--version")
	if err != nil || strings.TrimSpace(out) != "1.0.103" || strings.Contains(p.log(t), "UPDATE") || strings.Contains(p.log(t), "EXEC") {
		t.Fatalf("version probe should answer without update or ordinary execution (err=%v, log=%q):\n%s", err, p.log(t), out)
	}
}

func TestShippedCopilotInstallOnlyRefusesAnAffectedInstall(t *testing.T) {
	orig := imageProbeBase
	imageProbeBase = t.TempDir()
	t.Cleanup(func() { imageProbeBase = orig })
	p := newCopilotCompatibilityProbe(t, `{"mode":1,"workspace":"/w","home":"/h"}`, true, "true", "")
	if err := os.Remove(filepath.Join(p.home, ".local", "bin", "copilot")); err != nil {
		t.Fatal(err)
	}
	curlDir := t.TempDir()
	curl := `#!/bin/sh
out=
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-o" ]; then out=$2; shift 2; else shift; fi
done
cat > "$out" <<'INSTALLER'
#!/bin/sh
mkdir -p "$HOME/.local/bin"
cat > "$HOME/.local/bin/copilot" <<'COPILOT'
#!/bin/sh
if [ "${1:-}" = "--version" ]; then printf 'PROBE\n' >> "$HOME/calls.log"; echo 1.0.103; exit 0; fi
printf 'EXEC\n' >> "$HOME/calls.log"
COPILOT
chmod +x "$HOME/.local/bin/copilot"
INSTALLER
`
	if err := os.WriteFile(filepath.Join(curlDir, "curl"), []byte(curl), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(p.launcher)
	cmd.Env = append(os.Environ(), "HOME="+p.home, "PATH="+curlDir+":"+p.path, "YOLO_INSTALL_ONLY=1")
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "macos-user self's required SecurityServer deny") || strings.Contains(p.log(t), "EXEC") {
		t.Fatalf("install-only accepted an affected binary (err=%v, log=%q):\n%s", err, p.log(t), out)
	}
	if _, statErr := os.Stat(filepath.Join(p.home, ".local", "bin", "copilot")); statErr != nil {
		t.Fatalf("install-only refusal should follow a completed first install: %v", statErr)
	}
}

func TestShippedCopilotUnparseableVersionProbeFailsClosed(t *testing.T) {
	orig := imageProbeBase
	imageProbeBase = t.TempDir()
	t.Cleanup(func() { imageProbeBase = orig })
	p := newCopilotCompatibilityProbe(t, `{"mode":1,"workspace":"/w","home":"/h"}`, true, "false", "not-a-version")
	out, err := p.run(t, "1")
	if err == nil || !strings.Contains(out, "cannot parse copilot's declared version probe") || strings.Contains(p.log(t), "EXEC") {
		t.Fatalf("unparseable version did not fail closed (err=%v, log=%q):\n%s", err, p.log(t), out)
	}
}

func TestShippedCopilotPostUpdateCompatibilityCheck(t *testing.T) {
	orig := imageProbeBase
	imageProbeBase = t.TempDir()
	t.Cleanup(func() { imageProbeBase = orig })
	p := newCopilotCompatibilityProbe(t, `{"mode":1,"workspace":"/w","home":"/h"}`, true, "true", "1.0.92")
	if err := os.MkdirAll(filepath.Dir(p.stamp), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.stamp, []byte("stamp"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p.stamp, time.Now().Add(-2*time.Hour), time.Now().Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	out, err := p.runWithUpdate(t, "1.0.103", "1")
	log := p.log(t)
	if err == nil || !strings.Contains(out, "macos-user self's required SecurityServer deny") || strings.Contains(log, "EXEC") || !strings.Contains(log, "UPDATE 1.0.103") {
		t.Fatalf("post-update version was not refused before execution (err=%v, log=%q):\n%s", err, log, out)
	}
}

type copilotCompatibilityProbe struct {
	launcher string
	home     string
	logPath  string
	stamp    string
	path     string
}

func newCopilotCompatibilityProbe(t *testing.T, context string, native bool, updates, version string, probeArgs ...string) copilotCompatibilityProbe {
	t.Helper()
	home := t.TempDir()
	packRoot := stageShippedPacks(t)
	if len(probeArgs) > 0 {
		manifestPath := filepath.Join(packRoot, "_official", "copilot", "pack.json")
		manifest, err := os.ReadFile(manifestPath)
		if err != nil {
			t.Fatal(err)
		}
		oldProbe := "\"probe_args\": [\n        \"--version\"\n      ]"
		newProbe := `"probe_args": [`
		for _, arg := range probeArgs {
			newProbe += `"` + arg + `",`
		}
		newProbe = strings.TrimSuffix(newProbe, ",") + `]`
		if !strings.Contains(string(manifest), oldProbe) {
			t.Fatal("staged Copilot manifest did not contain its expected probe_args declaration")
		}
		manifest = []byte(strings.Replace(string(manifest), oldProbe, newProbe, 1))
		if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	vars := map[string]string{"JAIL_HOME": home, "YOLO_PACK_ROOT": packRoot, AgentUpdatesEnv: updates}
	if native {
		vars["YOLO_DARWIN_WORKSPACE"] = filepath.Join(home, "ws")
		vars["YOLO_MACOS_USER_TMPDIR_CONTEXT"] = context
	}
	e := NewEnv(vars)
	e.Stderr = &strings.Builder{}
	if err := GenerateAgentLaunchers(e); err != nil {
		t.Fatalf("GenerateAgentLaunchers: %v", err)
	}
	launcher := filepath.Join(e.LaunchDir(), "copilot")
	logPath := filepath.Join(home, "calls.log")
	binary := filepath.Join(e.LocalBin(), "copilot")
	if err := os.MkdirAll(filepath.Dir(binary), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `#!/bin/sh
if [ "${1:-}" = "--version" ]; then
  printf '%s\n' "$(cat "$HOME/version")" >> "$HOME/calls.log"
  cat "$HOME/version"
  exit 0
fi
if [ "${1:-}" = "update" ]; then
  printf 'UPDATE %s\n' "${FAKE_UPDATE_VERSION:-}" >> "$HOME/calls.log"
  [ -z "${FAKE_UPDATE_VERSION:-}" ] || printf '%s\\n' "$FAKE_UPDATE_VERSION" > "$HOME/version"
  exit 0
fi
printf 'EXEC %s\n' "$(cat "$HOME/version")" >> "$HOME/calls.log"
exit 0
`
	if err := os.WriteFile(binary, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "version"), []byte(version+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return copilotCompatibilityProbe{launcher: launcher, home: home, logPath: logPath,
		stamp: filepath.Join(home, ".cache", "yolo-agent-stamps", "copilot.stamp"),
		path:  e.LocalBin() + ":" + os.Getenv("PATH")}
}

func (p copilotCompatibilityProbe) run(t *testing.T, hatch string) (string, error) {
	return p.runWithUpdate(t, "", hatch)
}

func (p copilotCompatibilityProbe) runArgs(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(p.launcher, args...)
	cmd.Env = append(os.Environ(), "HOME="+p.home, "PATH="+p.path, NoLaunchFlagsEnv+"=1")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (p copilotCompatibilityProbe) runWithUpdate(t *testing.T, updateVersion, hatch string) (string, error) {
	t.Helper()
	cmd := exec.Command(p.launcher, "chat")
	cmd.Env = append(os.Environ(), "HOME="+p.home, "PATH="+p.path, "FAKE_UPDATE_VERSION="+updateVersion)
	if hatch != "" {
		cmd.Env = append(cmd.Env, "YOLO_USE_INSTALLED="+hatch)
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (p copilotCompatibilityProbe) log(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(p.logPath)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
