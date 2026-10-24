package gitbroker

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFakeTool writes an executable shell script named name into dir.
func writeFakeTool(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// THE DOCTOR REPORTS THE HOST SSH AND GIT IT WOULD RUN (git-broker-plan.md task 7's
// `doctor_cmd`): each one's path and version, found on the PATH the broker inherits.
func TestSelfCheckReportsHostSSHAndGit(t *testing.T) {
	bin := t.TempDir()
	ssh := writeFakeTool(t, bin, "ssh", "[ \"$1\" = -V ] && echo 'OpenSSH_9.9p1 fake' >&2\nexit 0\n")
	git := writeFakeTool(t, bin, "git", "[ \"$1\" = --version ] && echo 'git version 2.55.0'\nexit 0\n")
	t.Setenv("PATH", bin)
	t.Setenv("HOME", t.TempDir())
	var out bytes.Buffer
	if rc := SelfCheck(&out); rc != 0 {
		t.Fatalf("SelfCheck = %d, want 0\n%s", rc, out.String())
	}
	for _, want := range []string{ssh, "OpenSSH_9.9p1 fake", git, "git version 2.55.0"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the self-check does not name %q:\n%s", want, out.String())
		}
	}
}

// With no ssh the broker reaches no remote, so the doctor fails naming the fix. A host with no
// git still serves SSH remotes, so its absence is reported without failing.
func TestSelfCheckWithoutSSHFailsNamingTheFix(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	var out bytes.Buffer
	if rc := SelfCheck(&out); rc != 1 {
		t.Fatalf("SelfCheck = %d with no ssh, want 1\n%s", rc, out.String())
	}
	if !strings.Contains(out.String(), "install OpenSSH") {
		t.Errorf("the self-check does not name the fix:\n%s", out.String())
	}

	bin := t.TempDir()
	writeFakeTool(t, bin, "ssh", "echo 'OpenSSH_9.9p1 fake' >&2\n")
	t.Setenv("PATH", bin)
	out.Reset()
	if rc := SelfCheck(&out); rc != 0 {
		t.Fatalf("SelfCheck = %d with ssh and no git, want 0\n%s", rc, out.String())
	}
	if !strings.Contains(out.String(), "no git on the host") {
		t.Errorf("the self-check does not report the missing git:\n%s", out.String())
	}
}

// Main routes --self-check to SelfCheck, so the manifest's doctor_cmd runs it.
func TestMainSelfCheckFlag(t *testing.T) {
	bin := t.TempDir()
	writeFakeTool(t, bin, "ssh", "echo 'OpenSSH_9.9p1 fake' >&2\n")
	t.Setenv("PATH", bin)
	t.Setenv("HOME", t.TempDir())
	if rc := Main([]string{"--self-check"}); rc != 0 {
		t.Fatalf("Main(--self-check) = %d, want 0", rc)
	}
}
