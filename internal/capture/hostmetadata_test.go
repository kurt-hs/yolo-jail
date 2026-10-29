package capture

// hostmetadata_test.go pins two guards on what a record can claim. The receipts beside an entry and
// its completion marker are the HOST's to write, so a staged proto-entry — the out directory a
// capture or build jail fills — that already holds one is refused before admission, on every
// admission entry point. And a record counts only for the entry its Key names, so a line copied
// or planted beside another entry attributes nothing.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stagedWith stages a one-file proto-entry and plants name beside its tree.
func stagedWith(t *testing.T, store *Store, name string) string {
	t.Helper()
	staged, err := store.Stage("planted")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(TreeDir(staged), ".local", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(TreeDir(staged), ".local", "bin", "x"), []byte("x\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteManifest(staged, &Manifest{Schema: ManifestSchema, Home: "/home/agent", Platform: Platform(),
		Surfaces: []string{".local"}, Excluded: []string{}, Entries: []ManifestEntry{}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staged, name), []byte(`{"kind":"capture","act":"record"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return staged
}

func TestAdmissionRefusesAProtoEntryHoldingHostOwnedMetadata(t *testing.T) {
	admits := map[string]func(*Store, string) (*Entry, error){
		"AdmitEntry": func(s *Store, staged string) (*Entry, error) { return s.AdmitEntry(staged) },
		"AdmitEntryGuarded": func(s *Store, staged string) (*Entry, error) {
			return s.AdmitEntryGuarded(context.Background(), staged, func(commit func() error) error { return commit() })
		},
		"AdmitEntryValidated": func(s *Store, staged string) (*Entry, error) {
			return s.AdmitEntryValidated(context.Background(), staged, func(*Manifest) error { return nil }, nil)
		},
	}
	for _, name := range []string{ReceiptsName, completeMarker} {
		for via, admit := range admits {
			t.Run(name+" via "+via, func(t *testing.T) {
				store := &Store{Dir: t.TempDir()}
				staged := stagedWith(t, store, name)
				_, err := admit(store, staged)
				if err == nil || !strings.Contains(err.Error(), name) || !strings.Contains(err.Error(), "nothing was stored") {
					t.Fatalf("admitting a proto-entry holding %s = %v, want a refusal naming it", name, err)
				}
				if keys, _ := store.EntryKeys(); len(keys) != 0 {
					t.Errorf("the refused proto-entry was stored: %v", keys)
				}
			})
		}
	}
}

// A record whose Key names another entry is not this entry's: selection ignores it, and so does the
// materialize's platform gate.
func TestARecordKeyedToAnotherEntryAttributesNothing(t *testing.T) {
	home := t.TempDir()
	store, entry := entryFixture(t, home, "plan9/mips", nil)
	forged := func(string) ([]Record, error) {
		return []Record{{Key: "ffffffffffffffff", Bin: "vendor", Platform: "plan9/arm"}}, nil
	}
	selected, err := Select(store, forged)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 0 {
		t.Errorf("a record keyed to another entry was selected: %+v", selected)
	}
	_, err = Materialize(MaterializeOptions{Entry: entry, Home: home, Platform: "plan9/arm", PlatformRecords: forged})
	if !errors.Is(err, ErrWrongPlatform) {
		t.Errorf("a record keyed to another entry licensed its platform: %v", err)
	}

	own := func(string) ([]Record, error) {
		return []Record{{Key: entry.Key, Bin: "vendor", Platform: "plan9/arm"}}, nil
	}
	if selected, err := Select(store, own); err != nil || len(selected) != 1 {
		t.Errorf("a record keyed to its own entry was not selected: %+v %v", selected, err)
	}
	if _, err := Materialize(MaterializeOptions{Entry: entry, Home: home, Platform: "plan9/arm", PlatformRecords: own}); err != nil {
		t.Errorf("a record keyed to its own entry did not license its platform: %v", err)
	}
}
