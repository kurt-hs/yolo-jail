package hostfloor

import (
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
)

// The copy of a floor program keeps the directory a relative patch branch resolves against, which is never
// serialized and so is lost by the JSON round trip unless copied by hand, as ForkRoot is.
func TestACopiedProgramKeepsItsPatchBranchDir(t *testing.T) {
	p := Program{Pack: "pi", Install: packdecl.Install{Bin: "pi", PatchBranch: "../..?ref=mine",
		ForkRoot: "/staged/pi-fork", ForkPatchBranchDir: "/src/pi-fork"}}
	got, err := cloneProgram(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Install.ForkPatchBranchDir != "/src/pi-fork" || got.Install.ForkRoot != "/staged/pi-fork" {
		t.Errorf("cloneProgram = %+v, want the fork's root and patch branch directory kept", got.Install)
	}
}
