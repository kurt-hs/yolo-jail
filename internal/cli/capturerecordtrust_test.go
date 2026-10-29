package cli

// capturerecordtrust_test.go pins the production record reader's side of the record-platform rule
// (docs/reference/install-capture.md#record-platform): which records it lets speak for an entry —
// only lines keyed to that entry, and on the host floor a host capture's +host record too — and that
// the installer-capture launcher hands Materialize that reader at all.

import (
	"bytes"
	goruntime "runtime"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/hostfloor/floortest"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func appendRecordLine(t *testing.T, entry *capture.Entry, line string) {
	t.Helper()
	if err := entrypoint.AppendReceiptLine(capture.ReceiptsPath(entry.Root), line); err != nil {
		t.Fatal(err)
	}
}

// A FORGED LINE — a record copied from, or naming, another entry — speaks for nothing: selection
// does not pick the entry for the platform it claims, and the fork-build launcher refuses it.
func TestARecordKeyedToAnotherEntryIsIgnoredBySelectionAndMaterialize(t *testing.T) {
	darwin, linux := "darwin/"+goruntime.GOARCH, "linux/"+goruntime.GOARCH
	store, key := admitSameBytesCopy(t, linux)
	entry, err := store.Resolve(key)
	if err != nil {
		t.Fatal(err)
	}
	appendRecordLine(t, entry, entrypoint.BuildReceipt{
		Bin: "x", Source: "pack:fx/tools/x", Key: "ffffffffffffffff", Digest: "sha256:forged", Path: entry.Root,
		Platform: darwin, Revision: "r", Recipe: "q", Act: entrypoint.ReceiptActRecord, Time: time.Now().Add(time.Hour),
	}.Line())
	selected, err := capture.Select(store, captureRecords)
	if err != nil {
		t.Fatal(err)
	}
	for p := range selected {
		if p.Platform == darwin {
			t.Errorf("a record keyed to another entry selected %s for %v", key, p)
		}
	}
	var errw bytes.Buffer
	a := materializeArgs{store: store.Dir, bin: "x", home: floortest.ResolvedTemp(t), key: key, declared: "pack:fx/tools/x"}
	if rc := materializeForkBuild(store, a, darwin, &errw); rc == 0 {
		t.Errorf("a forged record licensed %s:\n%s", darwin, errw.String())
	}
}

// The host floor selects a host capture's record (darwin/X+host) as well as a jail's (darwin/X), so
// its materialize must honor either: a script captured first in a Linux jail and then by `yolo
// capture` on the Mac host installs on the Mac's floor. Without it the floor selects the entry, the
// materialize refuses, and a recapture only adds another +host record.
func TestTheProductionFloorMaterializesByAHostCapturesRecord(t *testing.T) {
	darwin, linux := "darwin/"+goruntime.GOARCH, "linux/"+goruntime.GOARCH
	store, key := admitSameBytesCopy(t, linux)
	entry, err := store.Resolve(key)
	if err != nil {
		t.Fatal(err)
	}
	appendRecordLine(t, entry, entrypoint.CaptureReceipt{
		Bin: "x", Declared: "https://example.invalid/install.sh", Key: key, Digest: "sha256:x", Path: entry.Root,
		Platform: hostCapturePlatform(darwin), Act: entrypoint.ReceiptActRecord, Time: time.Now(),
	}.Line())
	orig := hostFloorCapturePlatform
	hostFloorCapturePlatform = func() string { return darwin }
	t.Cleanup(func() { hostFloorCapturePlatform = orig })
	f := productionHostFloor(nil, nil)
	if _, err := capture.Materialize(capture.MaterializeOptions{Entry: entry, Home: floortest.ResolvedTemp(t),
		Confined: true, Platform: f.CapturePlatform, PlatformRecords: f.CaptureRecords}); err != nil {
		t.Fatalf("the floor's materialize of an entry its +host record selects = %v", err)
	}
	// A jail's materialize never takes a host capture's record: no jail selects one.
	if _, err := capture.Materialize(capture.MaterializeOptions{Entry: entry, Home: floortest.ResolvedTemp(t),
		Platform: darwin, PlatformRecords: captureRecords}); err == nil {
		t.Error("a jail's materialize took a host capture's +host record")
	}
}

// THE INSTALLER-CAPTURE LAUNCHER's call site (materializeCapture with no --key): an installer
// capture whose manifest names another platform, recorded for this one, materializes. Drop
// PlatformRecords from that call and this fails.
func TestTheInstallerCaptureLauncherMaterializesByTheRecordedPlatform(t *testing.T) {
	t.Setenv("HOME", floortest.ResolvedTemp(t))
	store := &capture.Store{Dir: paths.CapturesDir()}
	staged, err := store.Stage("installer")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, capture.TreeDir(staged)+"/.local/bin/tool", "#!/bin/sh\necho tool\n")
	m, err := capture.ManifestForTree(capture.TreeDir(staged), "/home/agent", "plan9/"+goruntime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	if err := capture.WriteManifest(staged, m); err != nil {
		t.Fatal(err)
	}
	entry, err := store.AdmitEntry(staged)
	if err != nil {
		t.Fatal(err)
	}
	appendRecordLine(t, entry, entrypoint.CaptureReceipt{
		Bin: "tool", Declared: "https://example.invalid/install.sh", Key: entry.Key, Digest: "sha256:x",
		Path: entry.Root, Platform: capture.Platform(), Act: entrypoint.ReceiptActRecord, Time: time.Now(),
	}.Line())
	var errw bytes.Buffer
	a := materializeArgs{store: store.Dir, bin: "tool", home: floortest.ResolvedTemp(t), declared: "https://example.invalid/install.sh"}
	if rc := materializeCapture(a, &errw); rc != 0 {
		t.Fatalf("the installer-capture launcher refused an entry recorded for %s = %d:\n%s", capture.Platform(), rc, errw.String())
	}
}
