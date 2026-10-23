package hostfloor

// override_test.go is the floor's arm for a DEV OVERRIDE (override.go;
// docs/design/pack-buildable-programs.md §5, the plan's Task 9): the floor entry of an overridden
// program is a launcher that execs `<dir>/<run>` from the host path itself, nothing is installed, an
// edit in the checkout serves at the next invocation, and a run file that is not there refuses the
// program rather than falling back to its pack's copy.

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// overrideWorld is a Linux world whose floor serves an npm program `board` from a checkout: the
// override names dev/board.sh, and the program's own npm install would fail the test if it ran.
func overrideWorld(t *testing.T) (*world, Program, string) {
	t.Helper()
	w := newLinuxWorld(t)
	dev := resolvedTemp(t)
	p := npmProgram("fx", "board", "board-pkg")
	w.floor.Overrides = map[string]Override{"board": {Host: dev, Dir: dev, Run: "bin/board.sh", Build: "make"}}
	return w, p, dev
}

func writeScript(t *testing.T, path, body string) {
	t.Helper()
	must(t, os.MkdirAll(filepath.Dir(path), 0o755))
	must(t, os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755))
}

// THE OVERRIDE ARM: bin/<bin> execs the checkout's file, live — an edit shows with no Ensure — and no
// npm install, Node fetch or capture runs for it.
func TestAnOverriddenProgramsFloorEntryRunsTheCheckoutLive(t *testing.T) {
	w, p, dev := overrideWorld(t)
	file := filepath.Join(dev, "bin", "board.sh")
	writeScript(t, file, `echo dev-1 "$@"`)

	if st := w.floor.Status(p); st.Disposition != Missing || !strings.Contains(st.Reason, "dev override") {
		t.Fatalf("before Ensure: %s (%s), want missing naming the override", st.Disposition, st.Reason)
	}
	st, outcome, err := w.floor.Ensure(context.Background(), p)
	if err != nil || outcome != Installed || st.Disposition != Provisioned {
		t.Fatalf("Ensure: %s %s %v\n%s", outcome, st.Disposition, err, w.out.String())
	}
	if st.Record.Via != ViaOverride || st.Record.Entry != file || st.Record.Version != "dev override" {
		t.Errorf("record = %+v", st.Record)
	}
	if _, err := os.Stat(filepath.Join(w.floor.Dir, "node")); err == nil {
		t.Error("the floor fetched Node for an overridden program")
	}
	run := func() string {
		cmd := exec.Command(st.Launcher, "a b")
		cmd.Env = []string{}
		got, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("the floor's board: %q %v", got, err)
		}
		return string(got)
	}
	if got := run(); got != "dev-1 a b\n" {
		t.Fatalf("the floor's board ran %q, want the checkout's file with its arguments", got)
	}
	writeScript(t, file, `echo dev-2`)
	if got := run(); got != "dev-2\n" {
		t.Errorf("after an edit in the checkout the floor runs %q, want the edit, with no install", got)
	}
	if _, outcome, err := w.floor.Ensure(context.Background(), p); err != nil || outcome != Current {
		t.Errorf("the next launch: %s %v, want current", outcome, err)
	}
}

// A RUN FILE THAT IS NOT THERE refuses the program, naming the file, the pack's build line and the key
// to remove — and nothing serves in its place, not even an install of the pack's copy left from before.
func TestAnOverrideWhoseFileIsMissingIsNoFloorEntryAndNeverFallsBack(t *testing.T) {
	w, p, dev := overrideWorld(t)
	_, _, err := w.floor.Ensure(context.Background(), p)
	for _, want := range []string{filepath.Join(dev, "bin", "board.sh"), "make", "programs.overrides.board"} {
		if !errors.Is(err, ErrNoEntry) || !strings.Contains(err.Error(), want) {
			t.Errorf("Ensure = %v, want no floor entry naming %q", err, want)
		}
	}
	if _, err := os.Stat(w.floor.Launcher("board")); err == nil {
		t.Error("a launcher was written for an override that cannot run")
	}
	writeScript(t, filepath.Join(dev, "bin", "board.sh"), "echo x")
	must(t, os.Chmod(filepath.Join(dev, "bin", "board.sh"), 0o644))
	if st := w.floor.Status(p); st.Disposition != NoEntry || !strings.Contains(st.Reason, "not executable") {
		t.Errorf("a file that is not executable: %s (%s)", st.Disposition, st.Reason)
	}
}

// A REFUSED OVERRIDE (the caller's checks: no directory, the credential boundary, no run file) is no
// floor entry, and it is never outside the floor: `host_floor` leaving the pack out does not send the
// launch to a copy on the PATH, since the override is the user's choice of copy.
func TestARefusedOverrideIsNoFloorEntryEvenWhenTheFloorLeavesThePackOut(t *testing.T) {
	w, p, _ := overrideWorld(t)
	w.floor.Overrides["board"] = Override{Host: "~", Refused: "the override path IS your home directory (programs.overrides.board in ~/.config/yolo-jail/config.jsonc)"}
	w.floor.Include = func(string) bool { return false }
	if w.floor.OutsideTheFloor(p) {
		t.Error("an overridden program is outside the floor")
	}
	st := w.floor.Status(p)
	if st.Disposition != NoEntry || !strings.Contains(st.Reason, "dev override is refused") ||
		!strings.Contains(st.Reason, "programs.overrides.board") {
		t.Errorf("Status = %s (%s)", st.Disposition, st.Reason)
	}
}

// REMOVING THE OVERRIDE puts the pack's copy back: the override's entry is pending (its Via is no
// manifest word), and a failed reinstall never keeps the checkout serving.
func TestRemovingAnOverrideReinstallsThePacksCopyAndNeverKeepsTheCheckout(t *testing.T) {
	w, p, dev := overrideWorld(t)
	writeScript(t, filepath.Join(dev, "bin", "board.sh"), "echo dev")
	if _, _, err := w.floor.Ensure(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	f := *w.floor
	f.Overrides = nil
	st := f.Status(p)
	if st.Disposition != Provisioned || !strings.Contains(st.Pending, "npm, not override") {
		t.Fatalf("after the override is removed: %s pending %q", st.Disposition, st.Pending)
	}
	if !f.servesANearMiss(p, st.Record) {
		t.Error("the checkout would keep serving after a failed reinstall of the pack's copy")
	}
}
