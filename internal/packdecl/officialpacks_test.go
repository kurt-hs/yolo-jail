package packdecl

import (
	"io/fs"
	"path"
	"testing"

	officialpacks "github.com/mschulkind-oss/yolo-jail/packs"
)

// TestNoOfficialPackDeclaresAPrebuilt is the census beside TestNoOfficialPackDeclaresABinary: a
// `prebuilt` download is a pinned file a pack fetches from a host, and an official pack ships
// its programs as source or as a vendor installer, never as a binary it hosts (BG-D7, carrying
// OQ-BP8). The validator cannot see whether a manifest is official, so the refusal is here, over
// every embedded manifest. It reads the embedded packs rather than a list, so a pack added
// tomorrow is covered by construction.
func TestNoOfficialPackDeclaresAPrebuilt(t *testing.T) {
	entries, err := fs.ReadDir(officialpacks.FS, ".")
	if err != nil {
		t.Fatalf("reading the embedded packs: %v", err)
	}
	checked := 0
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		raw, err := fs.ReadFile(officialpacks.FS, path.Join(ent.Name(), ManifestName))
		if err != nil {
			continue // not every embedded pack ships a manifest at the root
		}
		m, problems := Decode(raw)
		if len(problems) != 0 {
			t.Errorf("Decode refused the shipped %s manifest: %v", ent.Name(), problems)
			continue
		}
		checked++
		for i, c := range m.Contributes {
			if c.Prebuilt != nil {
				t.Errorf("%s contributes[%d] (%s) declares \"prebuilt\": an official pack may not host a "+
					"download; ship the program as source (\"from\") or a vendor installer", ent.Name(), i, c.Bin)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no embedded manifest was read, so this census checks nothing")
	}
}
