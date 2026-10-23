package packload

// packtreeprebuilt_test.go pins the resolver's build-or-prebuilt half for a pack-tree program
// (docs/design/pack-buildable-programs.md §4, §7, OQ-BG2's leaning): the build while the source is
// present and a build can run, the platform's prebuilt where the source is absent or no build can
// ever run, and a reason naming what is declared otherwise.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
)

func prebuiltProgram(t *testing.T, withSource bool) Fork {
	t.Helper()
	c := packTreeContribution()
	c.Prebuilt = map[string]packdecl.PrebuiltBuild{
		"linux/amd64":  {URL: "https://example.invalid/board-linux-amd64", SHA256: strings.Repeat("a", 64)},
		"darwin/arm64": {URL: "https://example.invalid/board-darwin-arm64", SHA256: strings.Repeat("b", 64)},
	}
	p := claimPack(t, "fx", c)
	if withSource {
		writeTree(t, filepath.Join(p.Root, "tools", "board-cli"), map[string]string{"main.c": "int main(){}\n"})
	}
	return PackTreePrograms([]*Pack{p})[0]
}

func TestAPresentSourceBuildsAndNeverTakesThePrebuilt(t *testing.T) {
	f := prebuiltProgram(t, true)
	digest, _ := f.Digest()
	pin := PackTreeResolve(f, "linux/amd64", true)
	if pin.Commit != digest || pin.Fork.IsPrebuilt() || pin.Reason != "" {
		t.Fatalf("a present, buildable source resolved to %+v, want its build at %s", pin, digest)
	}
	if pin.Fork.PackTreeRecipe() != packdecl.PackTreeRecipe("tools/board-cli", "make", "dist/board.js") {
		t.Errorf("a build's recipe is not the build recipe")
	}
}

func TestAnAbsentSourceTakesThePlatformsPrebuilt(t *testing.T) {
	f := prebuiltProgram(t, false)
	pin := PackTreeResolve(f, "linux/amd64", true)
	if !pin.Fork.IsPrebuilt() || pin.Commit != strings.Repeat("a", 64) || pin.Reason != "" {
		t.Fatalf("an absent source resolved to %+v, want linux/amd64's prebuilt", pin)
	}
	if pin.Fork.Prebuilt.URL != "https://example.invalid/board-linux-amd64" || pin.Fork.ProgramRun() != "board" {
		t.Errorf("prebuilt pin = %+v (run %q)", pin.Fork.Prebuilt, pin.Fork.ProgramRun())
	}
	if !strings.Contains(pin.Fork.PrebuiltWhy, "no tools/board-cli in the pack") {
		t.Errorf("why = %q", pin.Fork.PrebuiltWhy)
	}
	// A prebuilt's recipe is its own: a hit on it never serves a build, nor the reverse.
	if r := pin.Fork.PackTreeRecipe(); r != packdecl.PackTreePrebuiltRecipe("board") || r == f.PackTreeRecipe() {
		t.Errorf("prebuilt recipe %q, build recipe %q", r, f.PackTreeRecipe())
	}
	if f.IsPrebuilt() {
		t.Errorf("resolving changed the Fork it was handed")
	}
}

func TestAnAbsentSourceWithNoPrebuiltForThePlatformNamesWhatIsDeclared(t *testing.T) {
	f := prebuiltProgram(t, false)
	pin := PackTreeResolve(f, "linux/arm64", true)
	if pin.Commit != "" || pin.Fork.IsPrebuilt() {
		t.Fatalf("resolved to %+v, want a reason", pin)
	}
	for _, want := range []string{"pack fx", "tools/board-cli", "linux/arm64", "darwin/arm64, linux/amd64"} {
		if !strings.Contains(pin.Reason, want) {
			t.Errorf("the reason does not name %q: %s", want, pin.Reason)
		}
	}
	f.Prebuilts = nil
	if pin := PackTreeResolve(f, "linux/amd64", true); !strings.Contains(pin.Reason, "(it declares: none)") {
		t.Errorf("no prebuilt at all: %q", pin.Reason)
	}
}

// OQ-BG2's leaning: a builder that can never run here lets the prebuilt stand in; without one for
// the platform the build pin comes back, for the caller's own refusal of that notch.
func TestAnUnbuildableNotchTakesThePrebuiltWhenOneIsDeclared(t *testing.T) {
	f := prebuiltProgram(t, true)
	pin := PackTreeResolve(f, "darwin/arm64", false)
	if !pin.Fork.IsPrebuilt() || pin.Commit != strings.Repeat("b", 64) || !strings.Contains(pin.Fork.PrebuiltWhy, "no build") {
		t.Fatalf("an unbuildable notch resolved to %+v, want darwin/arm64's prebuilt", pin)
	}
	digest, _ := f.Digest()
	if pin := PackTreeResolve(f, "darwin/amd64", false); pin.Fork.IsPrebuilt() || pin.Commit != digest {
		t.Errorf("an unbuildable notch with no prebuilt resolved to %+v, want the build pin", pin)
	}
}
