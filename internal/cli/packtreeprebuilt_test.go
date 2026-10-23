package cli

// packtreeprebuilt_test.go pins a pack-tree program's PREBUILT ARTIFACT
// (docs/design/pack-buildable-programs.md §7): the launch's slot fetches it, verifies it and admits
// it with no jail, a later launch is a store hit with no request, a digest mismatch stores nothing
// and names both digests, a present source never fetches it, and `yolo pack install` and
// `yolo pack build` take the same artifact.

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	goruntime "runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/hostfloor/floortest"
	"github.com/mschulkind-oss/yolo-jail/internal/packbin"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/richtext"
)

// prebuiltPlatform is the platform the fixtures declare an artifact for: a container jail's, which
// `yolo pack build` with no --at builds for.
var prebuiltPlatform = "linux/" + goruntime.GOARCH

// servePrebuilt serves body over TLS, counting requests, and points the pack-binary fetcher at it
// with a private cache. It returns the URL and the counter.
func servePrebuilt(t *testing.T, body []byte) (string, *atomic.Int32, string) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	cache := filepath.Join(floortest.ResolvedTemp(t), "bin-cache")
	prev := packBinaryFetcher
	packBinaryFetcher = func() packbin.Fetcher { return packbin.Fetcher{Dir: cache, Client: srv.Client()} }
	t.Cleanup(func() { packBinaryFetcher = prev })
	return srv.URL + "/x", &hits, cache
}

// prebuiltFixture is packTreeBuildFixture's pack fx with x's `prebuilt` for prebuiltPlatform at url
// pinned to sum; withSource keeps tools/x (a no-build copy), else it is removed.
func prebuiltFixture(t *testing.T, url, sum string, withSource bool) packload.Fork {
	t.Helper()
	f, _ := packTreeBuildFixture(t, "")
	if withSource {
		writeFile(t, filepath.Join(f.SourceDir(), "x"), "#!/bin/sh\necho from-source\n")
	} else if err := os.RemoveAll(f.SourceDir()); err != nil {
		t.Fatal(err)
	}
	p := &packload.Pack{Name: "fx", Root: f.Root, Decl: &packdecl.Manifest{Contributes: []packdecl.Contribution{{
		Kind: packdecl.KindProgram, Bin: "x", Via: packdecl.ViaPack, From: "tools/x", Run: "x",
		Prebuilt: map[string]packdecl.PrebuiltBuild{prebuiltPlatform: {URL: url, SHA256: sum}},
	}}}}
	return packload.PackTreePrograms([]*packload.Pack{p})[0]
}

// slotOnce runs one launch's slot for pin, as TestTheLaunchSlotBuildsAPackTreeProgramOnceThenHits
// does, and returns x's delivery and what the pool printed.
func slotOnce(t *testing.T, pin packload.ForkPin) (string, string, string) {
	t.Helper()
	var stream bytes.Buffer
	req := run.ForkBuildRequest{Pins: []packload.ForkPin{pin}, Platform: prebuiltPlatform, Runtime: "podman",
		Workspace: floortest.ResolvedTemp(t), Interrupt: &run.ActInterrupt{}}
	forks, _ := runBuildSlot(run.BuildSlotRequest{Forks: &req, MaxChecks: 1, MaxBuilds: 1}, &stream, false)
	return forks["x"].Key, forks["x"].Reason, stream.String()
}

// THE CALL SITE: a launch's slot (runBuildSlot → addForkKeys → fetchPrebuiltForLaunch) fetches an
// absent source's prebuilt once, verifies it and admits it with no jail, laid out as the program's
// file and link; the next launch is a hit that sends no request. Red if addForkKeys stops routing a
// prebuilt pin, or the act builds or fetches on a hit.
func TestTheLaunchSlotFetchesAPrebuiltOnceThenHits(t *testing.T) {
	body := []byte("#!/bin/sh\necho prebuilt-x\n")
	sum := sha256Hex(body)
	url, hits, _ := servePrebuilt(t, body)
	f := prebuiltFixture(t, url, sum, false)
	withFakeCaptureJail(t, func(run.Options) int {
		t.Error("a prebuilt started a build jail")
		return 1
	})
	pin := packload.PackTreeResolve(f, prebuiltPlatform, false)
	if !pin.Fork.IsPrebuilt() {
		t.Fatalf("the fixture did not resolve to its prebuilt: %+v", pin)
	}
	key, reason, out := slotOnce(t, pin)
	if key == "" || hits.Load() != 1 {
		t.Fatalf("the first launch: key %q (reason %q), %d requests, want one fetch\n%s", key, reason, hits.Load(), out)
	}
	if strings.Contains(out, "program builds") || strings.Contains(out, "built program") {
		t.Errorf("a prebuilt was announced as a build:\n%s", out)
	}
	store := &capture.Store{Dir: paths.CapturesDir()}
	b := forkBuild{Fork: pin.Fork, Commit: pin.Commit, Platform: prebuiltPlatform}
	entry, _, err := resolveForkBuild(store, "x", prebuiltPlatform, b.selectSource(), sum, b.recipe())
	if err != nil || entry.Key != key {
		t.Fatalf("the store holds no entry for the artifact's key: %v", err)
	}
	tree := capture.TreeDir(entry.Root)
	got, err := os.ReadFile(filepath.Join(tree, ".local", "lib", "yolo-programs", "x", "x"))
	if err != nil || !bytes.Equal(got, body) {
		t.Errorf("the admitted file is %q (%v), want the served bytes", got, err)
	}
	if fi, err := os.Stat(filepath.Join(tree, ".local", "lib", "yolo-programs", "x", "x")); err != nil || fi.Mode()&0o111 == 0 {
		t.Errorf("the admitted file is not executable: %v %v", fi, err)
	}
	if target, err := os.Readlink(filepath.Join(tree, ".local", "bin", "x")); err != nil || target != "../lib/yolo-programs/x/x" {
		t.Errorf("the link is %q (%v)", target, err)
	}
	receipts, _ := os.ReadFile(capture.ReceiptsPath(entry.Root))
	for _, want := range []string{`"declared":"pack:fx/tools/x"`, sum, url} {
		if !strings.Contains(string(receipts), want) {
			t.Errorf("the receipt lacks %s:\n%s", want, receipts)
		}
	}
	key2, _, out2 := slotOnce(t, pin)
	if key2 != key || hits.Load() != 1 {
		t.Errorf("the second launch: key %s (first %s), %d requests, want the hit and no fetch\n%s", key2, key, hits.Load(), out2)
	}
}

// A DIGEST MISMATCH STORES NOTHING: no entry, no cached download, and the reason names the URL, both
// digests and the pack's fix as the next step.
func TestAPrebuiltDigestMismatchStoresNothingAndNamesBothDigests(t *testing.T) {
	served := []byte("#!/bin/sh\necho tampered\n")
	pinned := sha256Hex([]byte("#!/bin/sh\necho the real one\n"))
	url, _, cache := servePrebuilt(t, served)
	f := prebuiltFixture(t, url, pinned, false)
	pin := packload.PackTreeResolve(f, prebuiltPlatform, false)
	key, reason, out := slotOnce(t, pin)
	if key != "" {
		t.Fatalf("a mismatched artifact was delivered: %s\n%s", key, out)
	}
	for _, want := range []string{url, pinned, sha256Hex(served), "yolo pack update", "nothing was run or stored"} {
		if !strings.Contains(reason, want) {
			t.Errorf("the reason does not name %q:\n%s", want, reason)
		}
	}
	if !strings.Contains(out, "Warning: ") {
		t.Errorf("the launch did not warn:\n%s", out)
	}
	b := forkBuild{Fork: pin.Fork, Commit: pin.Commit, Platform: prebuiltPlatform}
	if _, _, err := resolveForkBuild(&capture.Store{Dir: paths.CapturesDir()}, "x", prebuiltPlatform, b.selectSource(), pinned, b.recipe()); err == nil {
		t.Errorf("the store holds an entry for a mismatched artifact")
	}
	if packbin.Present(packbin.Path(cache, pinned, "x")) {
		t.Errorf("the download cache holds the mismatched bytes under the pinned digest")
	}
}

// A PRESENT SOURCE NEVER FETCHES (P5): with tools/x shipped, the launch copies it, and neither the
// slot nor `yolo pack install`'s prefetch sends a request for the declared artifact.
func TestAPresentSourceNeverFetchesThePrebuilt(t *testing.T) {
	body := []byte("#!/bin/sh\necho prebuilt-x\n")
	url, hits, _ := servePrebuilt(t, body)
	f := prebuiltFixture(t, url, sha256Hex(body), true)
	pin := packload.PackTreeResolve(f, prebuiltPlatform, false)
	if pin.Fork.IsPrebuilt() {
		t.Fatalf("a present source resolved to the prebuilt: %+v", pin)
	}
	if key, reason, out := slotOnce(t, pin); key == "" {
		t.Fatalf("the copy was not delivered: %s\n%s", reason, out)
	}
	pack := &packload.Pack{Name: "fx", Root: f.Root, Decl: &packdecl.Manifest{Contributes: []packdecl.Contribution{{
		Kind: packdecl.KindProgram, Bin: "x", Via: packdecl.ViaPack, From: "tools/x", Run: "x",
		Prebuilt: f.Prebuilts,
	}}}}
	if rc := fetchPackTreePrebuilts([]*packload.Pack{pack}, packBinaryFetcher(), "linux", goruntime.GOARCH,
		richtext.Printer{W: io.Discard}, io.Discard); rc != 0 {
		t.Errorf("the prefetch failed: %d", rc)
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("%d requests for the prebuilt while the source is present, want none", n)
	}
}

// `yolo pack install` prefetches an absent source's artifact into the cache a launch reads.
func TestPackInstallPrefetchesAnAbsentSourcesPrebuilt(t *testing.T) {
	body := []byte("#!/bin/sh\necho prebuilt-x\n")
	sum := sha256Hex(body)
	url, hits, cache := servePrebuilt(t, body)
	f := prebuiltFixture(t, url, sum, false)
	pack := &packload.Pack{Name: "fx", Root: f.Root, Decl: &packdecl.Manifest{Contributes: []packdecl.Contribution{{
		Kind: packdecl.KindProgram, Bin: "x", Via: packdecl.ViaPack, From: "tools/x", Run: "x", Prebuilt: f.Prebuilts,
	}}}}
	var out bytes.Buffer
	if rc := fetchPackBinaries([]*packload.Pack{pack}, packBinaryFetcher(), "linux", goruntime.GOARCH,
		richtext.Printer{W: &out}, io.Discard); rc != 0 {
		t.Fatalf("rc %d\n%s", rc, out.String())
	}
	if hits.Load() != 1 || !packbin.Present(packbin.Path(cache, sum, "x")) {
		t.Errorf("%d requests, cached %v, want the artifact fetched once", hits.Load(), packbin.Present(packbin.Path(cache, sum, "x")))
	}
	if !strings.Contains(out.String(), "fx: program x: prebuilt artifact for "+prebuiltPlatform+": fetched and verified") {
		t.Errorf("no fetched line:\n%s", out.String())
	}
}

// `yolo pack build` takes the same artifact: fetched once, then up to date, each line naming the
// sha256 it verified.
func TestPackBuildFetchesAPrebuiltThenReportsItUpToDate(t *testing.T) {
	body := []byte("#!/bin/sh\necho prebuilt-x\n")
	sum := sha256Hex(body)
	url, hits, _ := servePrebuilt(t, body)
	f := prebuiltFixture(t, url, sum, false)
	var lines []string
	for range 2 {
		var out bytes.Buffer
		if !packBuildOne(richtext.Printer{W: &out}, f, "", packBuildOptions{at: "jail"}, io.Discard, false) {
			t.Fatalf("packBuildOne failed:\n%s", out.String())
		}
		lines = append(lines, out.String())
	}
	if !strings.HasPrefix(lines[0], "fetched") || !strings.HasPrefix(lines[1], "up to date") || hits.Load() != 1 {
		t.Errorf("lines %q with %d requests, want fetched then up to date and one request", lines, hits.Load())
	}
	for _, l := range lines {
		if !strings.Contains(l, "sha256 "+shortSHA(sum)+" verified") || !strings.Contains(l, url) {
			t.Errorf("the line does not name the verified sha256 and URL: %q", l)
		}
	}
	if slices.ContainsFunc(lines, func(l string) bool { return strings.Contains(l, "tree digest") }) {
		t.Errorf("a prebuilt's line names a tree digest: %q", lines)
	}
}
