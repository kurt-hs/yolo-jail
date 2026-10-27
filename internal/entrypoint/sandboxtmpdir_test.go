package entrypoint

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/shquote"
)

func TestSourceLauncherAdaptsDeclaredTempAtProgramExec(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not found")
	}
	home := t.TempDir()
	workspace := filepath.Join(home, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	programDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(programDir, 0o755); err != nil {
		t.Fatal(err)
	}
	report := filepath.Join(home, "source-env")
	program := "#!/bin/bash\nprintf 'TMPDIR=%s\\nTOOL_TMPDIR=%s\\n' \"${TMPDIR-}\" \"${TOOL_TMPDIR-}\" > " +
		shquote.Quote(report) + "\n"
	if err := os.WriteFile(filepath.Join(programDir, "tool"), []byte(program), 0o755); err != nil {
		t.Fatal(err)
	}
	stampDir, keyDir, captures := filepath.Join(home, "stamps"), filepath.Join(home, "keys"), filepath.Join(home, "captures")
	for _, dir := range []string{stampDir, keyDir, captures} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(keyDir, "tool"), []byte("key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fakeBin := filepath.Join(home, "fake-bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fakeBin, "yolo"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	context := `{"mode":1,"workspace":"` + workspace + `","home":"` + home + `"}`
	inst := &packdecl.Install{Kind: packdecl.InstallKindSource, Bin: "tool", ForkedBy: "forkpack",
		Produces: []string{".local/bin/tool"}, SandboxTmpdirEnv: []string{"TOOL_TMPDIR"}}
	body := strings.Join(sourceAgentLauncherSegmentsWithTmpdir(inst, ForkDelivery{Key: "key"}, stampDir,
		keyDir, filepath.Join(home, "receipts.jsonl"), captures, false, launcherServers{}, nil, context, true), "")
	launcher := filepath.Join(home, "source-launcher")
	if err := os.WriteFile(launcher, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(workspace, "tmp with spaces")
	cmd := exec.Command(launcher, "--version")
	cmd.Dir = workspace
	cmd.Env = []string{"HOME=" + home, "PATH=" + fakeBin + string(os.PathListSeparator) + os.Getenv("PATH"), "TMPDIR=" + tmp}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("source program launch failed: %v\n%s", err, out)
	}
	got, err := os.ReadFile(report)
	if err != nil {
		t.Fatalf("source program did not execute: %v", err)
	}
	if want := "TMPDIR=" + tmp + "\nTOOL_TMPDIR=" + tmp + "\n"; string(got) != want {
		t.Errorf("source program env = %q, want %q", got, want)
	}
}

func TestAccountSourceLauncherPreservesAccountTMPDIR(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not found")
	}
	home := t.TempDir()
	workspace := filepath.Join(home, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	programDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(programDir, 0o755); err != nil {
		t.Fatal(err)
	}
	report := filepath.Join(home, "account-env")
	program := "#!/bin/bash\nprintf 'TMPDIR=%s\\n' \"${TMPDIR-}\" > " + shquote.Quote(report) + "\n"
	if err := os.WriteFile(filepath.Join(programDir, "tool"), []byte(program), 0o755); err != nil {
		t.Fatal(err)
	}
	stampDir, keyDir, captures := filepath.Join(home, "stamps"), filepath.Join(home, "keys"), filepath.Join(home, "captures")
	for _, dir := range []string{stampDir, keyDir, captures} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(keyDir, "tool"), []byte("key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fakeBin := filepath.Join(home, "fake-bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fakeBin, "yolo"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	context := `{"mode":0,"workspace":"` + workspace + `","home":"` + home + `"}`
	inst := &packdecl.Install{Kind: packdecl.InstallKindSource, Bin: "tool", ForkedBy: "forkpack",
		Produces: []string{".local/bin/tool"}, SandboxTmpdirEnv: []string{"TOOL_TMPDIR"}}
	body := strings.Join(sourceAgentLauncherSegmentsWithTmpdir(inst, ForkDelivery{Key: "key"}, stampDir,
		keyDir, filepath.Join(home, "receipts.jsonl"), captures, false, launcherServers{}, nil, context, true), "")
	launcher := filepath.Join(home, "source-launcher")
	if err := os.WriteFile(launcher, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	const accountTmp = "/account/sandbox/tmp"
	cmd := exec.Command(launcher, "--version")
	cmd.Dir = workspace
	cmd.Env = []string{"HOME=" + home, "PATH=" + fakeBin + string(os.PathListSeparator) + os.Getenv("PATH"), "TMPDIR=" + accountTmp}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("account-mode source launcher rejected inherited TMPDIR: %v\n%s", err, out)
	}
	got, err := os.ReadFile(report)
	if err != nil {
		t.Fatalf("account-mode program did not execute: %v", err)
	}
	if want := "TMPDIR=" + accountTmp + "\n"; string(got) != want {
		t.Errorf("account-mode program TMPDIR = %q, want %q", got, want)
	}
}
