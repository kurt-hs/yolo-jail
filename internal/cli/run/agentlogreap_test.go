package run

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// agentlogreap_test.go pins the agent-log class on the launch path (agentlogreap.go): the
// launching workspace's allowlisted log dirs are swept by the housekeeping slot itself, with no
// registry and no runtime, and a session transcript is never touched.

func TestTheSlotAgesOutTheLaunchingWorkspacesAgentLogs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	ws := t.TempDir()
	o := goldenOptions(ws, home)
	now := time.Now()
	o.Now = func() time.Time { return now }
	// The runtime answers nothing: the class must not need it (a registry-less workspace).
	o.Exec = func([]string, string, []string, time.Duration) ExecResult { return ExecResult{Ran: false} }

	old := now.Add(-(agentLogAgeDays + 5) * 24 * time.Hour)
	overlay := filepath.Join(ws, ".yolo", "home")
	write := func(rel string, mtime time.Time) string {
		t.Helper()
		p := filepath.Join(overlay, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("log"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatal(err)
		}
		return p
	}
	oldPi := write("pi/agent/logs/perf-2026-01-01T00-00-00-000Z-1.jsonl", old)
	oldCodex := write("codex/log/codex-tui.log", old)
	freshPi := write("pi/agent/logs/perf-now.jsonl", now)
	piSession := write("pi/agent/sessions/--workspace--/s.jsonl", old)
	codexSession := write("codex/sessions/2026/01/01/rollout.jsonl", old)
	claudeTranscript := write("claude/projects/-workspace/t.jsonl", old)

	o.runHousekeeping("podman", reclaimConsent{}, testCname)

	for _, gone := range []string{oldPi, oldCodex} {
		if _, err := os.Lstat(gone); !os.IsNotExist(err) {
			t.Errorf("the slot kept %s, an agent log past %d days; delete its call and this fails",
				strings.TrimPrefix(gone, overlay), agentLogAgeDays)
		}
	}
	for _, kept := range []string{freshPi, piSession, codexSession, claudeTranscript} {
		if _, err := os.Lstat(kept); err != nil {
			t.Errorf("the slot removed %s: a fresh log or a session transcript (user data)",
				strings.TrimPrefix(kept, overlay))
		}
	}
}

// In a jail the overlay is the enclosing jail's home, its host's to sweep.
func TestTheAgentLogClassIsHostOnly(t *testing.T) {
	ws := t.TempDir()
	o := goldenOptions(ws, t.TempDir())
	o.Getenv = func(k string) string {
		if k == "YOLO_VERSION" {
			return "1"
		}
		return ""
	}
	p := filepath.Join(ws, ".yolo", "home", "pi", "agent", "logs", "old.jsonl")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-365 * 24 * time.Hour)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	o.reapWorkspaceAgentLogs(nil)
	if _, err := os.Lstat(p); err != nil {
		t.Fatalf("the in-jail slot swept the overlay: %v", err)
	}
}
