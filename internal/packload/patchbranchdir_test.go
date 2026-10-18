package packload

import (
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
)

// A RELATIVE PATCH BRANCH's directory is the one the fork pack was read FROM (Pack.SourceRoot) when its Root is a
// staged copy, and it rides the rewrite into the base's program, where the host floor reads it.
func TestThePatchBranchDirIsTheSourceTheForkPackWasReadFrom(t *testing.T) {
	base := claimPack(t, "pi",
		packdecl.Contribution{Kind: packdecl.KindProgram, Bin: "pi", Via: "npm", Package: "pi-coding-agent"})
	c := forkContribution("pi", "pi")
	c.Source, c.PatchBranch = "git+https://example.invalid/upstream/pi?ref=main", "../..?ref=mine"
	fork := claimPack(t, "pi-fork", c)
	if f := Forks([]*Pack{base, fork}); len(f) != 1 || f[0].PatchBranchDir != fork.Root {
		t.Fatalf("a pack read in place: Forks = %+v, want its patch branch resolved against its root", f)
	}
	fork.SourceRoot = "/src/dotfiles/yolo-packs/pi-fork"
	f := Forks([]*Pack{base, fork})
	if len(f) != 1 || f[0].PatchBranchDir != fork.SourceRoot {
		t.Fatalf("a pack read through a copy: Forks = %+v, want its patch branch resolved against its source", f)
	}
	out, err := ApplyForks([]*Pack{base, fork})
	if err != nil {
		t.Fatal(err)
	}
	if in := out[0].Decl.InstallContributions()[0]; in.ForkPatchBranchDir != fork.SourceRoot || in.PatchBranch != c.PatchBranch {
		t.Errorf("the rewritten base program = %+v, want the patch branch and its directory", in)
	}
}
