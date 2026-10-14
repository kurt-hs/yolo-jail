package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
	"github.com/mschulkind-oss/yolo-jail/internal/packsrc"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func TestAdmittedProgramServingUsesExactGoodWithoutWrites(t *testing.T) {
	fx := newPatchedAdvanceFixture(t, "")
	if r, out, _ := fx.launch(t, "podman"); r.delivery.Key == "" {
		t.Fatalf("initial build: %+v %s", r, out)
	}
	f := fx.fork(t)
	store := &packsrc.Store{Dir: paths.PacksDir()}
	before, err := os.ReadFile(store.CheckRecordPath(f.Key()))
	if err != nil {
		t.Fatal(err)
	}
	builds := fx.child
	d, g := admittedProgramServing(f, "podman", patchedTestPlatform)
	if d.Key != fx.record(t).Good.Entry || g == nil || g.Entry != d.Key {
		t.Fatalf("not exact Good: %+v %+v", d, g)
	}
	after, err := os.ReadFile(store.CheckRecordPath(f.Key()))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) || fx.child != builds {
		t.Fatal("admitted read wrote record or built")
	}
	// The public consumer receives strict typed failure, not implicit historical recovery.
	fx.commit(t, "v1.2.0", map[int]string{11: "conflict", 14: "fourteen"})
	fx.later(2 * time.Hour)
	fx.launch(t, "podman")
	d, _ = admittedProgramServing(fx.fork(t), "podman", patchedTestPlatform)
	if d.PatchFailure == nil {
		t.Fatalf("strict failure lost: %+v", d)
	}
}

func TestAdmittedProgramServingRejectsChangedAndUnauthorizedInputs(t *testing.T) {
	fx := newPatchedAdvanceFixture(t, "")
	fx.launch(t, "podman")
	f := fx.fork(t)
	for _, tc := range []struct{ name, runtime, platform string }{
		{"platform", "podman", "linux-other"}, {"runtime", "macos-user", patchedTestPlatform}, {"unknown", "unknown", patchedTestPlatform},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, g := admittedProgramServing(f, tc.runtime, tc.platform)
			if d.Key != "" || g != nil || d.Reason == "" {
				t.Fatalf("qualified invalid inputs: %+v %+v", d, g)
			}
		})
	}
	changed := f
	changed.Build += "; true"
	if d, _ := admittedProgramServing(changed, "podman", patchedTestPlatform); d.Key != "" {
		t.Fatal("stale recipe qualified")
	}
	changed = f
	changed.Produces = []string{".local/bin/other"}
	if d, _ := admittedProgramServing(changed, "podman", patchedTestPlatform); d.Key != "" {
		t.Fatal("stale produces qualified")
	}
	writeFile(t, filepath.Join(fx.home, ".config", "yolo-jail", "config.jsonc"), `{"packs":[]}`)
	if d, _ := admittedProgramServing(f, "podman", patchedTestPlatform); d.Key != "" {
		t.Fatal("removed owner qualified")
	}
}

func TestAdmittedProgramServingRejectsCorruptCaptureAndMissingGood(t *testing.T) {
	fx := newPatchedAdvanceFixture(t, "")
	fx.launch(t, "podman")
	f := fx.fork(t)
	d, _ := admittedProgramServing(f, "podman", patchedTestPlatform)
	if d.Key == "" {
		t.Fatalf("initial read: %+v", d)
	}
	entry, err := (&capture.Store{Dir: paths.CapturesDir()}).Resolve(d.Key)
	if err != nil {
		t.Fatal(err)
	}
	// Removing the record must not silently recover another admission and qualify it.
	store := &packsrc.Store{Dir: paths.PacksDir()}
	record, err := os.ReadFile(store.CheckRecordPath(f.Key()))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(store.CheckRecordPath(f.Key())); err != nil {
		t.Fatal(err)
	}
	if d, _ = admittedProgramServing(f, "podman", patchedTestPlatform); d.Key != "" {
		t.Fatal("missing Good recovered as serving")
	}
	if err = os.WriteFile(store.CheckRecordPath(f.Key()), record, 0600); err != nil {
		t.Fatal(err)
	}
	tool := filepath.Join(entry.Tree, ".local", "bin", "tool")
	info, err := os.Stat(tool)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o222 != 0 {
		t.Fatalf("admitted tool is not immutable: mode %04o", info.Mode().Perm())
	}
	originalMode := info.Mode().Perm()
	if err = os.Chmod(tool, originalMode|0o200); err != nil {
		t.Fatalf("make only the fixture tool writable for deliberate corruption: %v", err)
	}
	defer func() {
		if err := os.Chmod(tool, originalMode); err != nil {
			t.Errorf("restore admitted tool mode %04o: %v", originalMode, err)
		}
	}()
	if err = os.WriteFile(tool, []byte("corrupt"), originalMode|0o200); err != nil {
		t.Fatalf("corrupt admitted tool after making only it writable: %v", err)
	}
	if d, _ = admittedProgramServing(f, "podman", patchedTestPlatform); d.Key != "" {
		t.Fatal("corrupt capture qualified")
	}
}

func TestAdmittedProgramServingRejectsStaleGoodAuthority(t *testing.T) {
	for _, name := range []string{"owner", "inputs", "legacy-inputs", "entry", "recipe", "series", "receipts"} {
		t.Run(name, func(t *testing.T) {
			fx := newPatchedAdvanceFixture(t, "")
			fx.launch(t, "podman")
			f := fx.fork(t)
			store := &packsrc.Store{Dir: paths.PacksDir()}
			record := fx.record(t)
			switch name {
			case "owner":
				record.Owner = "other/tool"
			case "inputs":
				record.Good.Read.Ref = "other-branch"
			case "legacy-inputs":
				record.Good.Read = nil
			case "entry":
				record.Good.Entry = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			case "recipe":
				record.Good.Recipe = "stale"
			case "series":
				record.Good.Series = "stale"
			case "receipts":
				entry, err := (&capture.Store{Dir: paths.CapturesDir()}).Resolve(record.Good.Entry)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.Remove(capture.ReceiptsPath(entry.Root)); err != nil {
					t.Fatal(err)
				}
			}
			bytes, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(store.CheckRecordPath(f.Key()), bytes, 0600); err != nil {
				t.Fatal(err)
			}
			d, g := admittedProgramServing(f, "podman", patchedTestPlatform)
			if d.Key != "" || g != nil || d.Reason == "" {
				t.Fatalf("stale %s qualified: %+v %+v", name, d, g)
			}
			after, err := os.ReadFile(store.CheckRecordPath(f.Key()))
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(bytes) {
				t.Fatal("qualification repaired record")
			}
		})
	}
}
