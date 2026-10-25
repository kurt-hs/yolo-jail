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
		procs: func() ([]prune.ProcStart, bool) { return nil, true },
		now:   time.Now,
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
