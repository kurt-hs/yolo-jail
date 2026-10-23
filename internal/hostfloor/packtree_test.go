package hostfloor

// packtree_test.go pins the floor's half of the pack-tree hazard (docs/design/pack-buildable-programs.md,
// `via: "pack"`): a pack-tree program projects to install kind "source", and before it had its own
// arm the floor read it as a fork with an empty source and said "fork has no usable pin". The floor
// holds none of them yet, and no path asks a fork pin for one.

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
)

func packTreeProgram() Program {
	m := &packdecl.Manifest{Contributes: []packdecl.Contribution{{
		Kind: packdecl.KindProgram, Bin: "board", Via: packdecl.ViaPack,
		From: "tools/board-cli", Build: "make", Run: "dist/board.js",
	}}}
	return Program{Pack: "fx", Install: m.InstallContributions()[0]}
}

// TestAPackTreeProgramNeverReachesTheForkPath fails if any floor path asks a fork pin, pins a fork,
// or resolves a fork build for a pack-tree program: every fork hook here fails the test when called.
func TestAPackTreeProgramNeverReachesTheForkPath(t *testing.T) {
	w := newWorld(t)
	f := *w.floor
	f.GOOS = "linux"
	f.ForkPin = func(p Program) (string, string) {
		t.Errorf("ForkPin asked for pack-tree program %s", p.Bin())
		return "", ""
	}
	f.ForkPinnable = func(p Program) bool {
		t.Errorf("ForkPinnable asked for pack-tree program %s", p.Bin())
		return true
	}
	f.PinFork = func(p Program, _ func(string)) (string, string) {
		t.Errorf("PinFork asked for pack-tree program %s", p.Bin())
		return "", ""
	}
	p := packTreeProgram()
	if !p.Install.IsPackTree() {
		t.Fatalf("fixture is not a pack-tree program: %+v", p.Install)
	}
	st := f.Status(p)
	if st.Disposition != NoEntry || !strings.Contains(st.Reason, "pack fx's own tree (tools/board-cli)") ||
		strings.Contains(st.Reason, "pin") || strings.Contains(st.Reason, "fork") {
		t.Fatalf("Status = %s (%s), want no floor entry naming the pack's tree and no fork pin", st.Disposition, st.Reason)
	}
	if !strings.Contains(st.Reason, "`host_floor`") {
		t.Errorf("the refusal names no next step: %s", st.Reason)
	}
	if _, _, err := f.Ensure(context.Background(), p); !errors.Is(err, ErrNoEntry) {
		t.Errorf("Ensure = %v, want ErrNoEntry", err)
	}
	if f.awaitsPin(p) {
		t.Error("awaitsPin says a pack-tree program awaits a fork pin")
	}
	if commit, why := f.forkPin(p); commit != "" || !strings.Contains(why, "own tree") {
		t.Errorf("forkPin = %q, %q; want no commit and the pack-tree reason", commit, why)
	}
	if via(p.Install) != packdecl.ViaPack || declared(p.Install) != "" {
		t.Errorf("via/declared = %q/%q, want pack and no fork address", via(p.Install), declared(p.Install))
	}
	if !f.servesANearMiss(p, &Record{Via: packdecl.ViaSource}) {
		t.Error("an installed copy would keep serving a pack-tree program")
	}
	if _, err := os.Stat(f.Dir); err == nil {
		t.Errorf("asking about a pack-tree program created %s", f.Dir)
	}
}
