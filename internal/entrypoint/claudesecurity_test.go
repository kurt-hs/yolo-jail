package entrypoint

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/claudesecurity"
	"github.com/mschulkind-oss/yolo-jail/internal/claudeview"
)

func TestGenerateShimsDeliversTheAccountAdapterOnlyThroughClaudeLauncherPath(t *testing.T) {
	useProductionImageBins(t)
	home := t.TempDir()
	spyBin := filepath.Join(home, "fakebin")
	argvRecord := filepath.Join(home, "argv")
	spy := filepath.Join(spyBin, "yolo")
	if err := os.MkdirAll(spyBin, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$YOLO_TEST_ARGV\"\n"
	if err := os.WriteFile(spy, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(spy, 0o755); err != nil {
		t.Fatal(err)
	}
	packs := stageShippedPacksNamed(t, "claude")
	writeTestFile(t, filepath.Join(packs, "_official", "other", "pack.json"),
		`{"name":"other","contributes":[{"kind":"program","bin":"other","via":"installer","url":"https://example.invalid/install.sh"}]}`)
	e := NewEnv(map[string]string{
		"JAIL_HOME": home, "YOLO_PACK_ROOT": packs,
		claudesecurity.EnvVar: claudesecurity.AccountMode, claudeview.SwitchEnv: claudeview.ResolvedValue(true),
	})
	if err := GenerateShims(e); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(home, filepath.FromSlash(claudeSecurityAdapterDir), "claude", "security")
	st, err := os.Stat(shim)
	if err != nil || st.Mode()&0o111 == 0 {
		t.Fatalf("the generated security adapter is not executable: %v, %v", st, err)
	}
	if _, err := os.Lstat(filepath.Join(e.BlockDir(), "security")); !os.IsNotExist(err) {
		t.Fatalf("the adapter unexpectedly occupies the jail-wide blocker PATH: %v", err)
	}
	contents, err := os.ReadFile(shim)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(contents), "eval") || strings.Contains(string(contents), "opaque service") {
		t.Fatalf("generated adapter evaluates input or contains invocation data:\n%s", contents)
	}

	programDir := filepath.Join(home, ".local", "bin")
	claudePath := filepath.Join(programDir, "claude")
	otherPath := filepath.Join(programDir, "other")
	claudeSeen := filepath.Join(home, "claude-security-path")
	otherSeen := filepath.Join(home, "other-security-path")
	externalSecurity := filepath.Join(spyBin, "security")
	externalLog := filepath.Join(home, "external-security")
	writeTestFile(t, claudePath, "#!/bin/sh\ncommand -v security > \"$CLAUDE_SECURITY_PATH\"\nsecurity -i add-generic-password -s opaque\n")
	writeTestFile(t, otherPath, "#!/bin/sh\ncommand -v security > \"$OTHER_SECURITY_PATH\"\nsecurity unrelated\n")
	writeTestFile(t, externalSecurity, "#!/bin/sh\nprintf 'external\\n' >> \"$EXTERNAL_SECURITY_LOG\"\n")
	for _, p := range []string{claudePath, otherPath, externalSecurity} {
		if err := os.Chmod(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := GenerateAgentLaunchers(e); err != nil {
		t.Fatal(err)
	}
	pathEnv := strings.Join([]string{e.LaunchDir(), e.BlockDir(), programDir, spyBin, "/usr/bin", "/bin"}, string(os.PathListSeparator))
	if out, rc := runGeneratedLauncher(t, e, pathEnv, "claude", "YOLO_CLAUDE_SECURITY_COMPAT=account",
		"YOLO_TEST_ARGV="+argvRecord, "CLAUDE_SECURITY_PATH="+claudeSeen); rc != 0 {
		t.Fatalf("Claude launcher exited %d:\n%s", rc, out)
	}
	if got, err := os.ReadFile(claudeSeen); err != nil || strings.TrimSpace(string(got)) != shim {
		t.Fatalf("Claude resolved security to %q, want its scoped adapter %q (err=%v)", got, shim, err)
	}
	if got, err := os.ReadFile(argvRecord); err != nil || string(got) != "internal\nclaude-security\naccount\n--\n-i\nadd-generic-password\n-s\nopaque\n" {
		t.Fatalf("Claude adapter dispatch argv = %q (err=%v)", got, err)
	}

	if out, rc := runGeneratedLauncher(t, e, pathEnv, "other", "YOLO_CLAUDE_SECURITY_COMPAT=account",
		"OTHER_SECURITY_PATH="+otherSeen, "EXTERNAL_SECURITY_LOG="+externalLog); rc != 0 {
		t.Fatalf("other-program launcher exited %d:\n%s", rc, out)
	}
	if got, err := os.ReadFile(otherSeen); err != nil || strings.TrimSpace(string(got)) != externalSecurity {
		t.Fatalf("other program resolved security to %q, want unchanged PATH entry %q (err=%v)", got, externalSecurity, err)
	}
	if got, err := os.ReadFile(externalLog); err != nil || string(got) != "external\n" {
		t.Fatalf("other program did not use the ordinary security command: %q (err=%v)", got, err)
	}
	cmd := exec.Command("/bin/bash", "-c", "command -v security")
	cmd.Env = []string{"HOME=" + home, "PATH=" + pathEnv, "YOLO_CLAUDE_SECURITY_COMPAT=account"}
	plain, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(plain)) != externalSecurity {
		t.Fatalf("ordinary command PATH resolved security to %q, want %q (err=%v)", plain, externalSecurity, err)
	}
}

func TestGenerateShimsDoesNotInstallTheAccountAdapterWithoutItsResolvedCarrierOrHook(t *testing.T) {
	for _, mode := range []string{"", "self", "view", claudesecurity.AccountMode} {
		e := NewEnv(map[string]string{"JAIL_HOME": t.TempDir(), claudesecurity.EnvVar: mode})
		if err := GenerateShims(e); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(filepath.Join(e.Home, filepath.FromSlash(claudeSecurityAdapterDir), "claude", "security")); err == nil {
			t.Errorf("carrier %q without a selected Claude hook unexpectedly generated the adapter", mode)
		}
		if _, err := os.Lstat(filepath.Join(e.BlockDir(), "security")); err == nil {
			t.Errorf("carrier %q unexpectedly generated a jail-wide security shim", mode)
		}
	}
	packs := stageShippedPacksNamed(t, "claude")
	e := NewEnv(map[string]string{
		"JAIL_HOME": t.TempDir(), "YOLO_PACK_ROOT": packs, claudesecurity.EnvVar: claudesecurity.AccountMode,
	})
	if err := GenerateShims(e); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(e.Home, filepath.FromSlash(claudeSecurityAdapterDir), "claude", "security")); err == nil {
		t.Fatal("account carrier without the resolved Claude credential-view hook generated the adapter")
	}
}

func TestGenerateShimsKeepsAnExistingSecurityBlockAheadOfClaudeAdapter(t *testing.T) {
	packs := stageShippedPacksNamed(t, "claude")
	e := NewEnv(map[string]string{
		"JAIL_HOME": t.TempDir(), "YOLO_PACK_ROOT": packs, "YOLO_BLOCK_CONFIG": `[{"name":"security","replacement":"true"}]`,
		claudesecurity.EnvVar: claudesecurity.AccountMode, claudeview.SwitchEnv: claudeview.ResolvedValue(true),
	})
	if err := GenerateShims(e); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(e.BlockDir(), "security")); err != nil {
		t.Fatalf("existing blocker disappeared: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(e.Home, filepath.FromSlash(claudeSecurityAdapterDir), "claude", "security")); err == nil {
		t.Fatal("Claude adapter overrode the existing security blocker")
	}
}
