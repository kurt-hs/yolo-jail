package cli

// patchedscratchcases_test.go pins the scratch acts' cases the first tests left open
// (docs/design/patched-forks.md PF-D65, PF-D74): a series based past every version, which the check
// walks at its base; a plain fork beside a patched one, which the check leaves alone; a scratch copy refused
// inside yolo's state directory, and a TMPDIR that cannot hold one, each naming TMPDIR; and a
// macos-user sandbox, which reads every pack path as the host's own.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/shquote"
)

// seriesPastEveryVersion re-exports f's series onto an upstream commit newer than every version,
// v1.1.0 the newest, and returns that commit, the series' new base.
func seriesPastEveryVersion(t *testing.T, f *patchedFixture) string {
	t.Helper()
	f.commit(t, "v1.1.0", map[int]string{14: "fourteen"})
	newBase := f.commitMsg(t, "upstream later work", "", map[int]string{14: "fourteen", 25: "later"})
	upstreamGit(t, f.repo, "checkout", "-q", "-b", "fork2")
	f.commitMsg(t, "ten", "", map[int]string{14: "fourteen", 25: "later", 10: "ten"})
	patches := filepath.Join(f.forkDir, "patches")
	if err := os.RemoveAll(patches); err != nil {
		t.Fatal(err)
	}
	upstreamGit(t, f.repo, "format-patch", "-q", "--base="+newBase, "-o", patches, "main..fork2")
	upstreamGit(t, f.repo, "checkout", "-q", "main")
	return newBase
}

// A SERIES BASED PAST EVERY VERSION, as one exported from upstream main after its last tag: the
// check walks the series' base, which a first advance builds, and exits 0; the scratch rebase needs
// none, and clones nothing.
func TestTheScratchActsOnASeriesBasedPastEveryVersion(t *testing.T) {
	f := newPatchedFixture(t, "")
	newBase := seriesPastEveryVersion(t, f)
	tmp := inAJail(t, f.packs)
	rc, out, errw := seriesVerb(t, "check", f.forkDir)
	if rc != 0 || !strings.Contains(out, "the series' base "+shortSHA(newBase)+" (no version of the branch contains it) "+
		"takes the series (1 patch, series ") {
		t.Errorf("the check of a series past every version: rc=%d\n%s\n%s", rc, out, errw)
	}
	noScratchLeft(t, tmp)
}

// A MACOS-USER SANDBOX has no mount namespace and no /workspace (and sets no YOLO_WORKSPACE), so
// every pack path it reads is the host's own: the check's conflict prints the recipe exporting into
// the pack where it is, with no word of the host (PF-D74); a container jail, for the same pack
// outside its workspace, says the export belongs on the host.
func TestAMacosUserSandboxsCheckExportsIntoThePackWhereItIs(t *testing.T) {
	f := newPatchedFixture(t, "")
	f.commit(t, "v1.2.0", map[int]string{11: "eleven"})
	t.Setenv("YOLO_VERSION", "1.0.0")
	t.Setenv("YOLO_WORKSPACE", "")
	t.Setenv("TMPDIR", t.TempDir())
	prev := jailMountsItsOwnFiles
	jailMountsItsOwnFiles = func() bool { return false }
	t.Cleanup(func() { jailMountsItsOwnFiles = prev })
	export := "mkdir " + shquote.QuoteDisplay(filepath.Join(f.forkDir, "patches")+".new")
	_, out, errw := seriesVerb(t, "check", f.forkDir)
	if !strings.Contains(out, export) || strings.Contains(out, "on the host") {
		t.Errorf("the check in a macos-user sandbox does not export into the pack (%q):\n%s\n%s", export, out, errw)
	}
	jailMountsItsOwnFiles = func() bool { return true }
	_, out, errw = seriesVerb(t, "check", f.forkDir)
	if !strings.Contains(out, "outside this jail's workspace, so it is not the host's own copy") {
		t.Errorf("the check in a container jail does not say the export belongs on the host:\n%s\n%s", out, errw)
	}
}

// A PLAIN FORK BESIDE A PATCHED ONE is no series to check: the check reads the patched fork alone.
func TestTheSeriesCheckLeavesAPlainForkAlone(t *testing.T) {
	f := newPatchedFixture(t, "")
	f.commit(t, "v1.1.0", map[int]string{14: "fourteen"})
	writeFile(t, filepath.Join(f.packs, "basepack", "pack.json"), `{"name":"basepack","contributes":[`+
		`{"kind":"program","bin":"tool","via":"npm","package":"tool"},{"kind":"program","bin":"other","via":"npm","package":"other"}]}`)
	writeFile(t, f.manifest, `{"name":"forkpack","contributes":[{"kind":"program","bin":"other","via":"source",`+
		`"fork_of":"basepack","source":"git+file://`+f.repo+`?ref=main","build":"sh build.sh","produces":[".local/bin/other"]},`+
		`{"kind":"program","bin":"tool","via":"source","fork_of":"basepack","source":"git+file://`+f.repo+
		`?ref=main","patches":"patches","build":"sh build.sh","produces":[".local/bin/tool"]}]}`)
	rc, out, errw := seriesVerb(t, "check", f.forkDir)
	if rc != 0 || !strings.Contains(out, "fork forkpack/tool: upstream v1.1.0") || strings.Contains(out+errw, "forkpack/other") {
		t.Errorf("the check of a pack holding a plain fork: rc=%d\n%s\n%s", rc, out, errw)
	}
}

// A SCRATCH COPY IN YOLO'S STATE DIRECTORY, where a TMPDIR inside it would put it, is refused naming
// TMPDIR, and nothing is left there.
func TestTheSeriesCheckRefusesAScratchCopyInYolosState(t *testing.T) {
	f := newPatchedFixture(t, "")
	tmp := filepath.Join(paths.GlobalStorage(), "tmp")
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", tmp)
	rc, out, errw := seriesVerb(t, "check", f.forkDir)
	if rc != 1 || !strings.Contains(errw, "set TMPDIR to a directory outside it") {
		t.Errorf("rc=%d\n%s\n%s", rc, out, errw)
	}
	if ents, _ := os.ReadDir(tmp); len(ents) != 0 {
		t.Errorf("the refused scratch copy is left in yolo's state directory: %v", ents)
	}
}

// A TMPDIR THAT CANNOT HOLD A SCRATCH COPY names the next step.
func TestTheSeriesCheckWithAnUnusableTMPDIRNamesIt(t *testing.T) {
	f := newPatchedFixture(t, "")
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
	rc, out, errw := seriesVerb(t, "check", f.forkDir)
	if rc != 1 || !strings.Contains(errw, "making a scratch copy of the upstream") ||
		!strings.Contains(errw, "set TMPDIR to a writable directory (or unset it to use /tmp)") {
		t.Errorf("rc=%d\n%s\n%s", rc, out, errw)
	}
}
