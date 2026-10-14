package integration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/macosuser"
	"github.com/mschulkind-oss/yolo-jail/internal/shquote"
)

const macosUserReadMarker = "yolo-harmless-read-fixture"

// Runbook item 2's twin uses a host-created, world-readable marker, not credentials.
// A bare read AS THE SANDBOX ACCOUNT rules out DAC before the real launch tests Seatbelt.
func TestMacosUserLaunchDeniesReadOfHost0644File(t *testing.T) {
	requireMacosUser(t)
	ws := macosUserWorkspace(t, `{}`)
	// /Users is read-denied outside the workspace/home. A /tmp fixture would be
	// readable by policy, and a host-home fixture could fail on parent permissions.
	dir, err := os.MkdirTemp("/Users/Shared", "yolo-read0644-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("remove marker fixture %s: %v", dir, err)
		}
	})
	path := macosUserRead0644Fixture(t, dir)
	probe := macosUserRead0644Probe(path)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	bare, err := exec.CommandContext(ctx, "sudo", "-n", "--user="+macosuser.SandboxUser,
		"/bin/bash", "-c", probe).CombinedOutput()
	if err != nil {
		t.Fatalf("unsandboxed account control failed: %v\n%s", err, bare)
	}
	if got := macosUserHomeProbeFields(t, string(bare), "READ0644"); got["read"] != "ALLOWED" || got["content"] != macosUserReadMarker {
		t.Fatalf("fixture is not readable without Seatbelt: %v", got)
	}
	r := runMacosUser(t, ws, probe)
	if r.rc != 0 {
		t.Fatalf("launch failed before the read probe answered (rc %d)\nstdout:\n%s\nstderr:\n%s", r.rc, r.stdout, r.stderr)
	}
	got := macosUserHomeProbeFields(t, r.stdout, "READ0644")
	if got["read"] != "DENIED" || strings.Contains(r.stdout, macosUserReadMarker) {
		t.Errorf("real launch did not deny the host marker read: %v\n%s", got, r.stdout)
	}
}

func macosUserRead0644Fixture(t *testing.T, dir string) string {
	t.Helper()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "host marker's file")
	if err := os.WriteFile(path, []byte(macosUserReadMarker), 0o644); err != nil {
		t.Fatal(err)
	}
	// Explicit chmod defeats the host's umask: this test must not pass on DAC alone.
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("fixture must be mode 0644: stat=%v, err=%v", info, err)
	}
	return path
}

func macosUserRead0644Probe(path string) string {
	return strings.Join([]string{
		`echo '=== READ0644 ==='`,
		`if content=$(/bin/cat ` + shquote.Quote(path) + ` 2>/dev/null); then`,
		`  echo 'read|ALLOWED'; printf 'content|%s\n' "$content"`,
		`else echo 'read|DENIED'; fi`,
		`echo '=== END READ0644 ==='`,
	}, "\n")
}

// Linux preflight: proves only fixture permissions, quoting and probe parsing, NOT Seatbelt.
func TestMacosUserRead0644ProbeReadsUnsandboxedFixture(t *testing.T) {
	path := macosUserRead0644Fixture(t, resolvedTempDir(t))
	out, err := exec.Command("bash", "-c", macosUserRead0644Probe(path)).CombinedOutput()
	if err != nil {
		t.Fatalf("probe failed: %v\n%s", err, out)
	}
	got := macosUserHomeProbeFields(t, string(out), "READ0644")
	if got["read"] != "ALLOWED" || got["content"] != macosUserReadMarker {
		t.Fatalf("unsandboxed read misreported: %v\n%s", got, out)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	out, err = exec.Command("bash", "-c", macosUserRead0644Probe(path)).CombinedOutput()
	if err != nil {
		t.Fatalf("negative probe failed: %v\n%s", err, out)
	}
	if got := macosUserHomeProbeFields(t, string(out), "READ0644"); got["read"] != "DENIED" || strings.Contains(string(out), macosUserReadMarker) {
		t.Fatalf("failed read misreported: %v\n%s", got, out)
	}
}
