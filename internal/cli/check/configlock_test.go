package check

// configlock_test.go pins that an in-jail `yolo check` says the workspace config is read-only
// before the agent tries to write it: the briefing and the configuring-the-jail skill send the agent
// to edit that file, and a `workspace_readonly` launch binds it read-only, so the first an agent
// learned of the lock was an EROFS from its editor. Driven through Check, the line's call site.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const configLockLine = "yolo-jail.jsonc is read-only in this jail"

func configLockCheck(t *testing.T, inJail, writable bool) string {
	t.Helper()
	var out bytes.Buffer
	o := baseOptions(t, &out)
	if inJail {
		o = inJailOptions(t, &out)
	}
	o.PathExists = func(p string) bool { _, err := os.Stat(p); return err == nil }
	cfg := filepath.Join(o.Workspace, "yolo-jail.jsonc")
	if err := os.WriteFile(cfg, []byte(`{"workspace_readonly": [".git/hooks"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	o.Writable = func(p string) bool {
		if p != cfg {
			t.Errorf("the lock probe asked about %q, not the workspace config %q", p, cfg)
		}
		return writable
	}
	Check(o)
	return stripANSI(out.String())
}

func TestInJailCheckSaysTheWorkspaceConfigIsReadOnly(t *testing.T) {
	got := configLockCheck(t, true, false)
	if !strings.Contains(got, configLockLine) {
		t.Fatalf("an in-jail check over a read-only workspace config does not say so (want %q):\n%s",
			configLockLine, got)
	}
	if want := "ask the human to apply it on the host"; !strings.Contains(got, want) {
		t.Errorf("the read-only line names no next step (want %q):\n%s", want, got)
	}
}

// The line is a measurement: a config this process can write gets none, and neither does the host,
// where the lock does not exist.
func TestCheckIsSilentAboutAWritableWorkspaceConfig(t *testing.T) {
	if got := configLockCheck(t, true, true); strings.Contains(got, configLockLine) {
		t.Errorf("an in-jail check over a writable config reported it read-only:\n%s", got)
	}
	if got := configLockCheck(t, false, false); strings.Contains(got, configLockLine) {
		t.Errorf("a host check reported the in-jail lock:\n%s", got)
	}
}

// A TOML workspace's config is yolo-jail.toml, the file the launch reads and locks
// (config.ResolveReadableWorkspaceConfigPath): the lock probe measures it and the line names it.
func TestInJailCheckSaysATOMLWorkspaceConfigIsReadOnly(t *testing.T) {
	var out bytes.Buffer
	o := inJailOptions(t, &out)
	o.PathExists = func(p string) bool { _, err := os.Stat(p); return err == nil }
	cfg := filepath.Join(o.Workspace, "yolo-jail.toml")
	if err := os.WriteFile(cfg, []byte("workspace_readonly = [\".git/hooks\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	o.Writable = func(p string) bool {
		if p != cfg {
			t.Errorf("the lock probe asked about %q, not the workspace config %q", p, cfg)
		}
		return false
	}
	Check(o)
	if got := stripANSI(out.String()); !strings.Contains(got, "yolo-jail.toml is read-only in this jail") {
		t.Fatalf("an in-jail check over a read-only yolo-jail.toml does not say so:\n%s", got)
	}
}
