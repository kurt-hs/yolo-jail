package packload_test

// testbinarytree_test.go pins that a go test binary run OUTSIDE cmd/go's $WORK — a per-test
// runner's `go test -c` binary (scripts/completion-census.py), one run by hand — leaves no
// per-process pack tree in TMPDIR when it exits. Each such process used to take the
// per-process fallback tree, which nothing releases at a test binary's exit, so one ~0.9 MB
// yolo-embedded-lease-* tree stayed per test process (7,658 of them, 6.7 GB, in about 30
// minutes, 2026-10-24). They share one leased cache tree under TestSharedCachePrefix now.

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

const outsideWorkHelperEnv = "YOLO_TEST_OUTSIDE_WORK_HELPER"

// TestATestBinaryOutsideWorkLeavesNoPerProcessTree copies this test binary out of $WORK and
// runs it, three times, as a process that reads the embedded packs and exits normally. TMPDIR
// must then hold no fallback tree, and every run must have read the one shared tree.
// MUTATION: send an unrecognised test binary back to the fallback in embeddedCacheBaseLocked
// and this fails.
func TestATestBinaryOutsideWorkLeavesNoPerProcessTree(t *testing.T) {
	if os.Getenv(outsideWorkHelperEnv) != "" {
		t.Skip("the helper's own run")
	}
	scratch, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(scratch, "bin", "packload.test")
	copyExecutable(t, exe, bin)
	tmp := filepath.Join(scratch, "tmp")
	if err := os.Mkdir(tmp, 0o755); err != nil {
		t.Fatal(err)
	}
	shared := filepath.Join(tmp, packload.TestSharedCachePrefix+strconv.Itoa(os.Geteuid()))
	for i := 0; i < 3; i++ {
		cmd := exec.Command(bin, "-test.run=^TestOutsideWorkHelperProcess$", "-test.count=1")
		cmd.Env = append(os.Environ(), "TMPDIR="+tmp, outsideWorkHelperEnv+"=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("helper run %d: %v\n%s", i, err, out)
		}
		if !strings.Contains(string(out), "ROOT="+shared+string(filepath.Separator)) {
			t.Fatalf("helper run %d did not read the shared tree under %s:\n%s", i, shared, out)
		}
	}
	es, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range es {
		if e.Name() != filepath.Base(shared) {
			t.Errorf("a test binary that exited normally left %s in TMPDIR", e.Name())
		}
	}
	trees, err := os.ReadDir(shared)
	if err != nil {
		t.Fatal(err)
	}
	if len(trees) != 1 {
		t.Errorf("the shared base holds %d entries after three runs of one build, want 1", len(trees))
	}
}

// TestOutsideWorkHelperProcess is TestATestBinaryOutsideWorkLeavesNoPerProcessTree's child:
// it reads the embedded packs with no override, as any test in an unrecognised binary does.
func TestOutsideWorkHelperProcess(t *testing.T) {
	if os.Getenv(outsideWorkHelperEnv) == "" {
		t.Skip("only runs as a helper process")
	}
	mustEmbedded(t)
	root, fallback := packload.EmbeddedLocation()
	if fallback {
		t.Fatalf("the helper took the per-process fallback: %v", packload.EmbeddedProblems())
	}
	os.Stdout.WriteString("ROOT=" + root + "\n")
}

// The shared test base has no `yolo prune` to reap other builds' trees, so loading from it
// does: an abandoned old build's tree goes, a fresh one (inside the age floor) stays.
// MUTATION: drop the final-tree sweep in loadFromCacheLocked and this fails.
func TestTheSharedTestBaseReapsAbandonedOtherBuildTrees(t *testing.T) {
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(parent, packload.TestSharedCachePrefix+"x")
	if err := os.Mkdir(base, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(base, strings.Repeat("0", 32))
	young := filepath.Join(base, strings.Repeat("1", 32))
	for _, d := range []string{stale, young} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, packload.EmbeddedLeaseName), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", t.TempDir())
	t.Cleanup(packload.OverrideEmbeddedCacheDir(base))
	mustEmbedded(t)
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("an abandoned other build's tree survived a load from the shared test base (stat %v)", err)
	}
	if _, err := os.Stat(young); err != nil {
		t.Errorf("a tree inside the age floor was reaped: %v", err)
	}
}

// leasedDir makes dir with a free lease, modified age ago.
func leasedDir(t *testing.T, dir string, age time.Duration) {
	t.Helper()
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, packload.EmbeddedLeaseName), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(dir, when, when); err != nil {
		t.Fatal(err)
	}
}

// A load from the shared test base also reaps the per-process fallback trees test binaries
// left in TMPDIR before they shared it: an abandoned one goes, one inside the age floor stays.
// MUTATION: drop the sweepDeadFallbacks call in loadFromCacheLocked and this fails.
func TestTheSharedTestBaseReapsDeadFallbackTreesInTMPDIR(t *testing.T) {
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(parent, packload.TestSharedCachePrefix+"x")
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(tmp, packload.EmbeddedFallbackPrefix+"1")
	young := filepath.Join(tmp, packload.EmbeddedFallbackPrefix+"2")
	leasedDir(t, stale, 2*time.Hour)
	leasedDir(t, young, 0)
	t.Setenv("TMPDIR", tmp)
	t.Cleanup(packload.OverrideEmbeddedCacheDir(base))
	mustEmbedded(t)
	if root, fallback := packload.EmbeddedLocation(); fallback || !strings.HasPrefix(root, base) {
		t.Fatalf("setup: loaded %s (fallback=%v), want the shared test base %s", root, fallback, base)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("an abandoned fallback tree survived a load from the shared test base (stat %v)", err)
	}
	if _, err := os.Stat(young); err != nil {
		t.Errorf("a fallback tree inside the age floor was reaped: %v", err)
	}
}

// A link at the shared test base's name — another user's, in a shared TMPDIR — is never
// followed: the process takes its own fallback tree and writes nothing through the link.
// MUTATION: judge the test base on the resolved path and this fails.
func TestASymlinkAtTheSharedTestBaseIsRefused(t *testing.T) {
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(parent, "elsewhere")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(parent, packload.TestSharedCachePrefix+"x")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", t.TempDir())
	t.Cleanup(packload.OverrideEmbeddedCacheDir(link))
	mustEmbedded(t)
	if _, fallback := packload.EmbeddedLocation(); !fallback {
		t.Error("the loader adopted a test base that is a symlink")
	}
	if es, _ := os.ReadDir(target); len(es) != 0 {
		t.Errorf("the loader wrote %d entries through the link", len(es))
	}
}

func copyExecutable(t *testing.T, src, dst string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	in, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}
