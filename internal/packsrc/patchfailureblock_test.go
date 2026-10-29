package packsrc

import (
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/patchrecovery"
)

// TestPatchFailureBlockGolden pins the error block's shape (ruled 2026-10-16): one ERROR line, the
// facts, a blank line and the repair, a blank line and the hatches as copyable commands — no prose.
func TestPatchFailureBlockGolden(t *testing.T) {
	for _, env := range append(append([]string{}, patchrecovery.Scoped...), patchrecovery.UseInstalledEnv) {
		t.Setenv(env, "")
	}
	f := &PatchFailure{Target: ListEntry{Commit: "abcdef0123456789", Tag: "v1.4.0", Version: "1.4.0"}, Kind: "conflict",
		Member: "0001-x.patch", Paths: []string{"src/a.ts", "src/b.ts"}}
	recipe := RebaseRecipe{Repo: "https://example.com/pi.git", Base: "1111111", Patches: "/packs/pi-fork/patches", Dir: "pi-rebase"}
	bypass := PatchBypass{Command: "yolo", Key: "pi-fork/pi", Hatches: []patchrecovery.Hatch{
		{Env: patchrecovery.AllowPatchFailuresEnv, Value: "pi-fork/pi", Command: "yolo", Label: "run v1.1.0 + 2 patches"},
		{Env: patchrecovery.SkipPatchesEnv, Value: "pi-fork/pi", Command: "yolo", Label: "run upstream without the patches"},
	}}
	var want strings.Builder
	want.WriteString("ERROR: fork pi-fork/pi: patch series does not apply to upstream v1.4.0 (abcdef01)\n")
	want.WriteString("  Patch: 0001-x.patch\n")
	want.WriteString("  Conflict: src/a.ts, src/b.ts\n")
	want.WriteString("\nRepair:\n")
	for _, l := range recipe.Lines(f.Target) {
		want.WriteString("  " + l + "\n")
	}
	want.WriteString("\nStart anyway:\n")
	want.WriteString("  YOLO_ALLOW_PATCH_FAILURES=pi-fork/pi yolo    run v1.1.0 + 2 patches\n")
	want.WriteString("  YOLO_SKIP_PATCHES=pi-fork/pi yolo            run upstream without the patches\n")
	if got := f.Block("fork pi-fork/pi", recipe, bypass); got != want.String() {
		t.Fatalf("Block =\n%s\nwant\n%s", got, want.String())
	}
}

// TestPatchBypassLegacyFieldsDeriveHatches keeps a caller still setting Runs, CachedGood or Missing
// listing the same hatches the new shape names, scoped to the subject's key.
func TestPatchBypassLegacyFieldsDeriveHatches(t *testing.T) {
	t.Setenv(patchrecovery.AllowPatchFailuresEnv, "")
	got := PatchBypass{Command: "yolo host -- pi", Runs: "v1.1.0 + 2 patches"}.List("pi-fork/pi")
	if len(got) != 1 || got[0].Text() != "YOLO_ALLOW_PATCH_FAILURES=pi-fork/pi yolo host -- pi" {
		t.Fatalf("Runs = %+v", got)
	}
	got = PatchBypass{CachedGood: "pi-fork/pi", CachedGoodLabel: "v1.0.0", Missing: true}.List("pi-fork/pi")
	if len(got) != 2 || got[0].Env != patchrecovery.UseCachedGoodEnv || got[1].Text() != "YOLO_ALLOW_MISSING_PROGRAMS=1 yolo" {
		t.Fatalf("CachedGood+Missing = %+v", got)
	}
	f := &PatchFailure{Target: ListEntry{Commit: "abc"}, Kind: "conflict"}
	if b := f.Block("extension a/x", RebaseRecipe{}, PatchBypass{Unneeded: true}); !strings.HasSuffix(b, "\nNo selected agent pack loads it here; this launch goes on.\n") {
		t.Fatalf("Unneeded block =\n%s", b)
	}
}
