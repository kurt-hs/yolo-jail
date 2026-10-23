package selfupdate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
)

// The command RunGit runs is in its own session, with a process-group Cancel
// and a bounded wait: removing packsrc.DetachGit from gitCommand fails this.
func TestGitCommandIsDetachedAndBounded(t *testing.T) {
	cmd := gitCommand(t.Context(), "/src/yolo-jail", "status")
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setsid {
		t.Error("git does not run in a new session, so ssh can prompt on the terminal")
	}
	if cmd.Cancel == nil {
		t.Error("no process-group Cancel: a timeout kills git but not its helpers")
	}
	if cmd.WaitDelay <= 0 {
		t.Error("no WaitDelay: a helper holding the output pipe blocks Wait forever")
	}
}

func hasControl(s string) bool {
	return strings.IndexFunc(s, unicode.IsControl) >= 0
}

// Text from outside yolo is printed to a terminal, so an escape sequence in a
// tag is dropped before it is saved or shown.
func TestCheckStripsControlCharacters(t *testing.T) {
	srv := releaseServer(t, 200, `{"tag_name":"v0.11.0\u001b[2J"}`)
	rel := Channel{Kind: KindHomebrew, Exe: "/opt/homebrew/bin/yolo", Version: "0.10.0"}
	st := Check(t.Context(), rel, State{}, CheckDeps{HTTP: srv.Client(), ReleaseURL: srv.URL, Now: fixedNow})
	if hasControl(st.Latest) {
		t.Errorf("a tag's control characters survived: %q", st.Latest)
	}
	if n := Notice(st); hasControl(n) {
		t.Errorf("the notice carries control characters: %q", n)
	}
}

// A state file is stripped on the way in and out as well, so a cache written
// before this rule, or by hand, prints nothing a terminal would obey.
func TestStateFileIsStrippedOnSaveAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	raw := `{"kind":"homebrew","latest":"abc\u0007","error":"x\u001b]0;t\u0007","available":true}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	got := LoadState(path)
	if hasControl(got.Latest) || hasControl(got.Error) {
		t.Errorf("LoadState kept control characters: %+v", got)
	}

	if err := SaveState(path, State{Latest: "a\x00b", Error: "e\x9b"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `\u001b`) || strings.Contains(string(data), `\u0000`) || strings.Contains(string(data), `\u009b`) {
		t.Errorf("SaveState wrote control characters: %s", data)
	}
}
