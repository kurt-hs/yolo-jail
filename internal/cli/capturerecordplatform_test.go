package cli

// capturerecordplatform_test.go pins the ruling that a capture's PLATFORM is a property of each
// `record` beside its entry, not of the entry (docs/reference/install-capture.md). The store is
// keyed by content alone, so a pack-tree program whose bytes are the same on every platform — a
// script — admitted for one platform and then another lands under ONE key whose manifest names the
// first. The second platform's record is what selects it, so it is what a materialize honors.

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
	"github.com/mschulkind-oss/yolo-jail/internal/hostfloor/floortest"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/richtext"
)

// admitSameBytesCopy admits fixture program x, a script, as a no-build pack-tree copy once per
// platform, in order, and returns the store and the key every admission landed under.
func admitSameBytesCopy(t *testing.T, platforms ...string) (*capture.Store, string) {
	t.Helper()
	f, _ := packTreeBuildFixture(t, "")
	writeFile(t, filepath.Join(f.SourceDir(), "x"), "#!/bin/sh\necho plain\n")
	digest, err := f.Digest()
	if err != nil {
		t.Fatal(err)
	}
	store := &capture.Store{Dir: paths.CapturesDir()}
	key := ""
	for _, platform := range platforms {
		entry, err := admitPackTreeCopyUnderLock(forkBuild{Fork: f, Commit: digest, Platform: platform},
			buildMode{}, store, richtext.Printer{W: io.Discard})
		if err != nil {
			t.Fatalf("admitting for %s: %v", platform, err)
		}
		if key != "" && entry.Key != key {
			t.Fatalf("identical bytes filed under two keys %s and %s — the premise of this test is gone", key, entry.Key)
		}
		key = entry.Key
	}
	return store, key
}

func TestASameBytesPackTreeCopyMaterializesForEveryPlatformItWasAdmittedFor(t *testing.T) {
	darwin, linux := "darwin/"+goruntime.GOARCH, "linux/"+goruntime.GOARCH
	for _, order := range [][2]string{{darwin, linux}, {linux, darwin}} {
		t.Run(strings.ReplaceAll(order[0]+" then "+order[1], "/", "_"), func(t *testing.T) {
			store, key := admitSameBytesCopy(t, order[0], order[1])
			for _, platform := range order {
				var errw bytes.Buffer
				a := materializeArgs{store: store.Dir, bin: "x", home: floortest.ResolvedTemp(t), key: key,
					declared: "pack:fx/tools/x"}
				if rc := materializeForkBuild(store, a, platform, &errw); rc != 0 {
					t.Errorf("materializing the entry first admitted for %s, for %s = %d:\n%s",
						order[0], platform, rc, errw.String())
				}
			}
		})
	}
}

// A PREBUILT whose artifact is the same bytes for two platforms (a script) lands under one key as the
// no-build copy does, and each platform's record serves it.
func TestASameBytesPrebuiltMaterializesForEveryPlatformItWasAdmittedFor(t *testing.T) {
	darwin := "darwin/" + goruntime.GOARCH
	body := []byte("#!/bin/sh\necho prebuilt-x\n")
	sum := sha256Hex(body)
	url, _, _ := servePrebuilt(t, body)
	f := prebuiltFixture(t, url, sum, false)
	f.Prebuilts = map[string]packdecl.PrebuiltBuild{prebuiltPlatform: {URL: url, SHA256: sum}, darwin: {URL: url, SHA256: sum}}
	store := &capture.Store{Dir: paths.CapturesDir()}
	key := ""
	for _, platform := range []string{darwin, prebuiltPlatform} {
		pin := packload.PackTreeResolve(f, platform, false)
		if !pin.Fork.IsPrebuilt() {
			t.Fatalf("the fixture did not resolve to its %s prebuilt: %+v", platform, pin)
		}
		entry, err := admitPackTreePrebuiltUnderLock(forkBuild{Fork: pin.Fork, Commit: pin.Commit, Platform: platform},
			buildMode{}, store)
		if err != nil {
			t.Fatalf("admitting the %s prebuilt: %v", platform, err)
		}
		if key != "" && entry.Key != key {
			t.Fatalf("the same artifact filed under two keys %s and %s — the premise of this test is gone", key, entry.Key)
		}
		key = entry.Key
	}
	for _, platform := range []string{darwin, prebuiltPlatform} {
		var errw bytes.Buffer
		a := materializeArgs{store: store.Dir, bin: "x", home: floortest.ResolvedTemp(t), key: key, declared: "pack:fx/tools/x"}
		if rc := materializeForkBuild(store, a, platform, &errw); rc != 0 {
			t.Errorf("materializing the prebuilt for %s = %d:\n%s", platform, rc, errw.String())
		}
	}
}

// The record is what licenses another platform than the manifest's: an entry recorded for one
// platform only is still refused for another, and the refusal names the command that records a
// build for this platform.
func TestAnEntryRecordedForOnePlatformIsRefusedForAnotherAndNamesTheNextStep(t *testing.T) {
	darwin, linux := "darwin/"+goruntime.GOARCH, "linux/"+goruntime.GOARCH
	store, key := admitSameBytesCopy(t, linux)
	var errw bytes.Buffer
	a := materializeArgs{store: store.Dir, bin: "x", home: floortest.ResolvedTemp(t), key: key, declared: "pack:fx/tools/x"}
	if rc := materializeForkBuild(store, a, darwin, &errw); rc == 0 {
		t.Fatalf("a linux-only entry materialized for %s:\n%s", darwin, errw.String())
	}
	assertNamesPackBuildForce(t, errw.String())

	// The library gate, beneath the launcher's record check: with a record reader and no record for
	// the platform asked for, Materialize still refuses, with the sentinel a caller keys its next
	// step on.
	entry, err := store.Resolve(key)
	if err != nil {
		t.Fatal(err)
	}
	_, err = capture.Materialize(capture.MaterializeOptions{Entry: entry, Home: floortest.ResolvedTemp(t),
		Platform: darwin, PlatformRecords: captureRecords})
	if !errors.Is(err, capture.ErrWrongPlatform) {
		t.Fatalf("Materialize for an unrecorded platform = %v, want ErrWrongPlatform", err)
	}
}

// The cached-good path judges the platform by the Good entry's record, as the materialize does: a
// manifest naming the platform the same bytes were admitted for first (here rewritten to stand in
// for that first admission) does not refuse a Good build recorded for this jail's platform, and a
// Good entry with no record for it still is.
func TestCachedGoodJudgesThePlatformByTheRecordNotTheManifest(t *testing.T) {
	fx, key := admittedCachedGood(t)
	entry, err := (&capture.Store{Dir: paths.CapturesDir()}).Resolve(key)
	if err != nil {
		t.Fatal(err)
	}
	other := "plan9/" + goruntime.GOARCH
	rewrite := func(path, from, to string) {
		t.Helper()
		data := mustRead(t, path)
		changed := strings.ReplaceAll(data, `"platform":"`+from+`"`, `"platform":"`+to+`"`)
		changed = strings.ReplaceAll(changed, `"platform": "`+from+`"`, `"platform": "`+to+`"`)
		if changed == data {
			t.Fatalf("%s names no platform %s:\n%s", path, from, data)
		}
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
		writeFile(t, path, changed)
	}
	rewrite(capture.ManifestPath(entry.Root), patchedTestPlatform, other)
	if d, out := askForCachedGood(t, fx, "forkpack/tool"); d.Key != key {
		t.Fatalf("a Good entry recorded for %s, whose manifest names %s, was refused: %+v\n%s",
			patchedTestPlatform, other, d, out)
	}

	rewrite(capture.ReceiptsPath(entry.Root), patchedTestPlatform, other)
	if d, out := askForCachedGood(t, fx, "forkpack/tool"); d.Key != "" || !strings.Contains(d.Reason, "not this jail's "+patchedTestPlatform) {
		t.Errorf("a Good entry recorded only for %s was served for %s: %+v\n%s", other, patchedTestPlatform, d, out)
	}
}

// The production host floor hands its materializes the record reader, so a Mac's floor takes a
// script first admitted by a Linux jail launch. Delete the newHostFloor wiring and this fails.
func TestTheProductionFloorMaterializesByTheRecordedPlatform(t *testing.T) {
	darwin, linux := "darwin/"+goruntime.GOARCH, "linux/"+goruntime.GOARCH
	store, key := admitSameBytesCopy(t, linux, darwin)
	entry, err := store.Resolve(key)
	if err != nil {
		t.Fatal(err)
	}
	orig := hostFloorCapturePlatform
	hostFloorCapturePlatform = func() string { return darwin }
	t.Cleanup(func() { hostFloorCapturePlatform = orig })
	f := productionHostFloor(io.Discard, nil)
	if _, err := capture.Materialize(capture.MaterializeOptions{Entry: entry, Home: floortest.ResolvedTemp(t),
		Confined: true, Platform: f.CapturePlatform, PlatformRecords: f.CaptureRecords}); err != nil {
		t.Fatalf("the production floor's materialize of an entry recorded for %s = %v", darwin, err)
	}
}

// assertNamesPackBuildForce checks msg names `yolo pack build fx --force`, and that the command it
// names is real: `pack` dispatches, its build verb takes --force, and it parses this argv.
func assertNamesPackBuildForce(t *testing.T, msg string) {
	t.Helper()
	if !strings.Contains(msg, "`yolo pack build fx --force` on the host, in this workspace") {
		t.Errorf("the refusal names no next step (want `yolo pack build fx --force`):\n%s", msg)
	}
	if _, ok := registry["pack"]; !ok {
		t.Errorf("the message names `yolo pack`, which does not dispatch")
	}
	if !strings.Contains(packBuildUsage, "--force") {
		t.Errorf("the message names `yolo pack build --force`, and its usage has no --force: %s", packBuildUsage)
	}
}

// A FORK's refusal names the fork's explicit rebuild, `yolo capture <bin>` — `yolo pack build` builds
// only pack-tree programs (packbuild.go).
func TestAForkBuildsPlatformRefusalNamesYoloCapture(t *testing.T) {
	got := forkBuildPlatformStep("tool", "git+https://example.invalid/tool", "darwin/arm64")
	if !strings.Contains(got, "`yolo capture tool` on the host, in this workspace") {
		t.Errorf("a fork's next step = %q, want `yolo capture tool`", got)
	}
	if _, ok := registry["capture"]; !ok {
		t.Errorf("the message names `yolo capture`, which does not dispatch")
	}
}
