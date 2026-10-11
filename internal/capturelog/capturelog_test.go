package capturelog

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var at = time.Date(2026, 10, 11, 12, 16, 40, 0, time.FixedZone("EDT", -4*3600))

// Every kind of change is one line, by dotted leaf, with old and new values.
func TestLinesNameEveryChangedLeaf(t *testing.T) {
	before := []byte(`{"env":{"A":"1","GONE":"x"},"model":"opus","same":true}`)
	after := []byte(`{"env":{"A":"2","NEW":"y"},"model":"opus","same":true,"del":null}`)
	got := Lines(at, "claude/settings", "boot", before, after, Reason{"env.GONE": "because"}, "default")
	want := []string{
		`2026-10-11T12:16:40-04:00 claude/settings boot: + del (captured deletion)`,
		`2026-10-11T12:16:40-04:00 claude/settings boot: ~ env.A "1" -> "2" (captured)`,
		`2026-10-11T12:16:40-04:00 claude/settings boot: - env.GONE (was "x"): because`,
		`2026-10-11T12:16:40-04:00 claude/settings boot: + env.NEW = "y" (captured)`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if len(Lines(at, "s", "w", after, after, nil, "")) != 0 {
		t.Error("an unchanged overlay must log nothing")
	}
	// A reset: everything goes, with the writer's reason.
	if got := Lines(at, "s", "reset", before, nil, nil, "removed by reset"); len(got) != 4 ||
		!strings.Contains(got[0], "removed by reset") {
		t.Errorf("reset lines: %v", got)
	}
}

// No credential reaches the log: by key name, or by shape. A model id is exempt from the shape.
func TestValueNeverPrintsACredential(t *testing.T) {
	const tok = "sk-ant-oat01-0123456789abcdefghijABCDEFGHIJ0123456789"
	for key, v := range map[string]any{
		"env.ANTHROPIC_AUTH_TOKEN": "short", "env.AWS_SECRET_ACCESS_KEY": "abc",
		"env.OPAQUE": tok, "apiKeyHelper.list": []any{tok}, "oauthAccount": map[string]any{"x": 1},
	} {
		if got := Value(key, v); !strings.HasPrefix(got, "<redacted") {
			t.Errorf("Value(%q) = %s, want redacted", key, got)
		}
	}
	for key, v := range map[string]any{
		"model": "global.anthropic.claude-opus-5-5", "env.CLAUDE_CODE_USE_BEDROCK": "1",
		"autoMemoryEnabled": true, "env.CLAUDE_CODE_PLUGIN_DIRS": "~/.claude/mods",
	} {
		if got := Value(key, v); strings.HasPrefix(got, "<redacted") {
			t.Errorf("Value(%q) = %s, want the value", key, got)
		}
	}
}

// The log is bounded: past MaxBytes the oldest lines go, and a marker says so.
func TestTrimBoundsTheLog(t *testing.T) {
	line := strings.Repeat("x", 99)
	var existing []byte
	for len(existing) < MaxBytes {
		existing = append(existing, []byte(line+"\n")...)
	}
	out := Trim(existing, []string{"newest"})
	if len(out) > MaxBytes {
		t.Errorf("trimmed log is %d bytes, over %d", len(out), MaxBytes)
	}
	if !bytes.HasSuffix(out, []byte("newest\n")) || !bytes.HasPrefix(out, []byte("# older lines trimmed")) {
		t.Errorf("trim must keep the newest line and mark the cut: %q…%q", out[:40], out[len(out)-20:])
	}
}

func TestAppendFileAppends(t *testing.T) {
	dir := t.TempDir()
	if err := AppendFile(dir, []string{"one"}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AppendFile(dir, []string{"two"}, 0o644); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, FileName))
	if string(data) != "one\ntwo\n" {
		t.Errorf("log = %q", data)
	}
	if !Held([]byte(`{"env":{"A":"1"}}`), "env.A") || Held([]byte(`{}`), "env.A") {
		t.Error("Held must report a held leaf")
	}
}
