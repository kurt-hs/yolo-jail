package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/prune"
)

func fakeDevBundleDeps(storage string, podmanUp bool) devBundleReapDeps {
	return devBundleReapDeps{
		storage: func() string { return storage },
		rt:      func() string { return "podman" },
		run: func([]string, time.Duration) prune.ProbeResult {
			return prune.ProbeResult{Ran: podmanUp, RC: 0}
		},
		procs:       func() ([]prune.ProcStart, bool) { return nil, true },
		now:         time.Now,
		getenv:      func(string) string { return "" },
		osCanAnswer: func() bool { return true },
	}
}

func TestDevBundleReapVerbHonorsTheAutoReapOptOut(t *testing.T) {
	storage := t.TempDir()
	bundles := mkOldDevBundles(t, prune.DevBundlesDirUnder(storage), 5)
	d := fakeDevBundleDeps(storage, true)
	d.getenv = func(k string) string {
		if k == "YOLO_NO_AUTO_IMAGE_REAP" {
			return "1"
		}
		return ""
	}
	var stderr bytes.Buffer
	if rc := runDevBundleReap(nil, &stderr, d); rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	for _, b := range bundles {
		if _, err := os.Stat(b); err != nil {
			t.Fatal("YOLO_NO_AUTO_IMAGE_REAP=1 did not stop the post-build sweep")
		}
	}
}

// On an OS that can never answer, the verb asks nothing and names the by-hand removal rather
// than a `yolo prune --apply` that would decline the same way.
func TestDevBundleReapVerbOnAnOSThatCannotAnswerNamesTheByHandRemoval(t *testing.T) {
	storage := t.TempDir()
	bundles := mkOldDevBundles(t, prune.DevBundlesDirUnder(storage), 5)
	d := fakeDevBundleDeps(storage, true)
	d.osCanAnswer = func() bool { return false }
	d.run = func([]string, time.Duration) prune.ProbeResult {
		t.Fatal("asked the runtime on an OS whose sweep can never remove a bundle")
		return prune.ProbeResult{}
	}
	d.procs = func() ([]prune.ProcStart, bool) {
		t.Fatal("read the process table on an OS whose sweep can never remove a bundle")
		return nil, false
	}
	var stderr bytes.Buffer
	if rc := runDevBundleReap(nil, &stderr, d); rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	for _, b := range bundles {
		if _, err := os.Stat(b); err != nil {
			t.Fatal("removed a bundle on an OS that cannot answer")
		}
	}
	out := stderr.String()
	if !strings.Contains(out, prune.DevBundleByHand) || strings.Contains(out, "retries") {
		t.Errorf("stderr = %q, want the by-hand removal and no retry promise", out)
	}
}

func mkOldDevBundles(t *testing.T, dir string, n int) []string {
	t.Helper()
	var out []string
	for i := 0; i < n; i++ {
		p := filepath.Join(dir, fmt.Sprintf("%016x-%032x", i, i))
		if err := os.MkdirAll(filepath.Join(p, "bin"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "bin", "yolo"), []byte("x"), 0o555); err != nil {
			t.Fatal(err)
		}
		_ = os.Chmod(filepath.Join(p, "bin"), 0o555)
		at := time.Now().Add(-time.Duration(i+2) * time.Hour)
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	t.Cleanup(func() { _ = prune.RemoveReadOnlyTree(dir) })
	return out
}

func TestDevBundleReapVerbRemovesTheOldAndDeclinesWithoutTheRuntime(t *testing.T) {
	storage := t.TempDir()
	dir := prune.DevBundlesDirUnder(storage)
	bundles := mkOldDevBundles(t, dir, 5)

	var stderr bytes.Buffer
	if rc := runDevBundleReap(nil, &stderr, fakeDevBundleDeps(storage, false)); rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	for _, b := range bundles {
		if _, err := os.Stat(b); err != nil {
			t.Fatal("removed a bundle with the runtime unreachable")
		}
	}
	if !strings.Contains(stderr.String(), "Kept every old development bundle") ||
		!strings.Contains(stderr.String(), "yolo prune --apply") {
		t.Errorf("the decline does not say so or name the next step: %q", stderr.String())
	}

	stderr.Reset()
	if rc := runDevBundleReap([]string{"--dir", dir}, &stderr, fakeDevBundleDeps("/nonexistent", true)); rc != 0 {
		t.Fatalf("rc=%d: %s", rc, stderr.String())
	}
	for i, b := range bundles {
		_, err := os.Stat(b)
		if kept := err == nil; kept != (i < prune.DevBundleKeep) {
			t.Errorf("bundle %d kept=%v", i, kept)
		}
	}
	if !strings.Contains(stderr.String(), "Removed 2 unused development bundle(s)") {
		t.Errorf("stderr = %q", stderr.String())
	}
	if rc := runDevBundleReap([]string{"--bogus"}, &stderr, fakeDevBundleDeps(storage, true)); rc != 2 {
		t.Errorf("a bad argument returned %d, want 2", rc)
	}
}

// TestDevBundleReapIsWiredFromInternalAndTheBuildScript pins both ends of the opportunistic
// sweep: `yolo internal` dispatches the verb, and matched-bundle.py runs it by that spelling
// after each publish. Either end deleted, and dev bundles accrue again with every unit test green.
func TestDevBundleReapIsWiredFromInternalAndTheBuildScript(t *testing.T) {
	src, err := os.ReadFile("internal.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "case devBundleReapVerb:") ||
		!strings.Contains(string(src), "runDevBundleReap(args[1:], os.Stderr, defaultDevBundleReapDeps())") {
		t.Error("`yolo internal` no longer dispatches " + devBundleReapVerb)
	}
	script, err := os.ReadFile(filepath.Join("..", "..", "scripts", "matched-bundle.py"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"internal", "` + devBundleReapVerb + `", "--dir"`, `reaper(published / "bin/yolo", root)`} {
		if !strings.Contains(string(script), want) {
			t.Errorf("scripts/matched-bundle.py no longer has %s — a published bundle no longer "+
				"collects the ones nothing uses", want)
		}
	}
}
