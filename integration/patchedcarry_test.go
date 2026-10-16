package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
)

// patchedcarry_test.go is the container-level cell for a CARRY (docs/design/patched-forks.md §3.5,
// OQ-PFK6): the patched-fork fixture with its series replaced by a branch of a clone of the
// upstream that patches line ten, delivered by fresh launches.
//
//  1. The first launch checks the upstream and fetches the carry, merges the carry into the newest
//     version, builds that in a sealed capture jail and runs it, naming the carry it merged.
//  2. A version the carry does not merge into refuses the next launch before its jail starts,
//     naming the conflicting file and the merge that repairs the carry's branch.
//
// HERMETIC, like patchedfork_test.go: both repositories are local (git+file://), and the test removes
// the capture entries it added and its fork's check record.
func TestCarryForkMergesItsBranchAndRefusesAConflict(t *testing.T) {
	requireJail(t)
	up := patchFixtureUpstream{t: t, dir: t.TempDir()}
	up.git("init", "-q", "-b", "main")
	base := up.release("1.0.0", "ten", "v1.0.0")

	// THE CARRY: a clone of the upstream whose branch `carry`, started at v1.0.0, patches line ten.
	carry := patchFixtureUpstream{t: t, dir: filepath.Join(t.TempDir(), "carry")}
	up.git("clone", "-q", up.dir, carry.dir)
	carry.git("checkout", "-q", "-b", "carry", base)
	body, err := os.ReadFile(filepath.Join(carry.dir, "f.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(carry.dir, "f.txt"), []byte(strings.Replace(string(body), "ten\n", "patched\n", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	carry.git("commit", "-qam", "patch line ten")
	tip := carry.git("rev-parse", "HEAD")
	v11 := up.release("1.1.0", "ten", "v1.1.0")

	basePack, fork := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(basePack, "install.sh"), []byte("#!/bin/bash\n"+
		"mkdir -p \"$HOME/.local/bin\"\nprintf '#!/bin/bash\\necho "+patchFixtureBaseRan+"\\n' > "+
		"\"$HOME/.local/bin/"+patchFixtureBin+"\"\nchmod +x \"$HOME/.local/bin/"+patchFixtureBin+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeManifest := func(dir, body string) {
		if err := os.WriteFile(filepath.Join(dir, "pack.json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeManifest(basePack, `{"name":"`+patchFixtureBasePack+`","contributes":[{"kind":"program","bin":"`+
		patchFixtureBin+`","via":"installer","url":"file:///ctx/packs/`+patchFixtureBasePack+`/install.sh"}]}`)
	build := `mkdir -p "$HOME/.local/bin" && printf '#!/bin/sh\necho ` + patchFixtureMarker + `_%s_%s\n' "$(cat ver.txt)" ` +
		`"$(head -n 10 f.txt | tail -n 1)" > "$HOME/.local/bin/` + patchFixtureBin + `" && chmod +x "$HOME/.local/bin/` +
		patchFixtureBin + `"`
	buildJSON := strings.ReplaceAll(strings.ReplaceAll(build, `\`, `\\`), `"`, `\"`)
	writeManifest(fork, `{"name":"`+patchFixtureForkPack+`","contributes":[{"kind":"program","bin":"`+patchFixtureBin+
		`","via":"source","fork_of":"`+patchFixtureBasePack+`","source":"git+file://`+up.dir+`?ref=main",`+
		`"carry":"git+file://`+carry.dir+`?ref=carry","build":"`+buildJSON+`","produces":[".local/bin/`+patchFixtureBin+`"]}]}`)
	packHome(t, `{"packs": [{"source": "file://`+basePack+`", "name": "`+patchFixtureBasePack+`"}, `+
		`{"source": "file://`+fork+`", "name": "`+patchFixtureForkPack+`"}]}`)
	withPrivateFixtureYoloStore(t)

	state := filepath.Join(os.Getenv("HOME"), ".local", "share", "yolo-jail")
	store := filepath.Join(state, "captures")
	before := captureEntryNames(t, store)
	owner := patchFixtureForkPack + "/" + patchFixtureBin
	record := (&packsrc.Store{Dir: filepath.Join(state, "packs")}).CheckRecordPath(owner)
	_ = os.Remove(record)
	t.Cleanup(func() {
		removeNewCaptureEntries(t, store, before, patchFixtureBin)
		_ = os.Remove(record)
	})

	// 1. THE FIRST LAUNCH merges the carry into v1.1.0, builds it and runs it.
	r := runCommand(t, t.TempDir(), append(jailRunArgs(), "--", patchFixtureBin), withHostSemantics())
	out := r.combined()
	if r.rc != 0 || !strings.Contains(out, patchFixtureMarker+"_1.1.0_patched") || strings.Contains(out, patchFixtureBaseRan) {
		t.Fatalf("the first launch does not run v1.1.0 with the carry merged: rc %d\n%s", r.rc, out)
	}
	if !strings.Contains(out, "built fork "+owner+": v1.1.0 ("+v11[:8]+") + carry carry@"+tip[:8]+"; this jail runs it") {
		t.Errorf("the first launch does not say what it merged:\n%s", out)
	}

	// 2. A VERSION THE CARRY DOES NOT MERGE INTO refuses the next launch, naming the file and the merge.
	v12 := up.release("1.2.0", "upstream-ten", "v1.2.0")
	if r := runCommand(t, t.TempDir(), []string{"pack", "update"}, withHostSemantics()); r.rc == 0 {
		t.Fatalf("yolo pack update did not fail on the carry's conflict at v1.2.0:\n%s", r.combined())
	}
	refused := runCommand(t, t.TempDir(), append(jailRunArgs(), "--", patchFixtureBin), withHostSemantics())
	for _, w := range []string{"ERROR: fork " + owner + ": the carry does not merge into upstream v1.2.0 (" + v12 + ")",
		"Conflict: f.txt", "git merge " + v12, "Refusing to launch: the carry of fork " + owner + " does not merge"} {
		if !strings.Contains(refused.combined(), w) {
			t.Errorf("the refused launch lacks %q (rc %d):\n%s", w, refused.rc, refused.combined())
		}
	}
	if refused.rc == 0 || strings.Contains(refused.combined(), patchFixtureMarker) {
		t.Errorf("the launch after v1.2.0 started its jail: rc %d\n%s", refused.rc, refused.combined())
	}
}
