package packs_test

import (
	"io/fs"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/packs"
)

// TestNoShippedPackHasAJSONManifest pins the conversion of the shipped packs to pack.toml: every
// shipped pack carries a pack.toml and no legacy manifest. There are no exceptions: the meanings
// TOML once could not say, "no package for this manager", "declared with no default" and "delete
// this managed key", are `false` and `unset` (docs/design/readable-config.md RC-D25).
func TestNoShippedPackHasAJSONManifest(t *testing.T) {
	entries, err := fs.ReadDir(packs.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	converted := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		for _, legacy := range []string{"pack.json", "pack.jsonc"} {
			if _, err := fs.Stat(packs.FS, name+"/"+legacy); err == nil {
				t.Errorf("packs/%s ships a %s: convert it with `yolo pack migrate packs/%s --yes "+
					"--keep-install-hints`", name, legacy, name)
			}
		}
		if _, err := fs.Stat(packs.FS, name+"/pack.toml"); err != nil {
			t.Errorf("packs/%s ships no pack.toml", name)
			continue
		}
		converted++
	}
	if converted == 0 {
		t.Fatal("no shipped pack carries a pack.toml: the test is not reading the tree it exists for")
	}
}

// TestShippedProfilesAreNotProviderNames pins OQ-RC3: a provider is selectable by its own name
// (config.LoadSelectableProfiles), so a shipped profile named for the provider it selects is a
// second spelling of the same selection and is deleted. A profile with a different name is a
// variant (bedrock-bridge adds a via) and stays.
func TestShippedProfilesAreNotProviderNames(t *testing.T) {
	entries, err := fs.ReadDir(packs.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	profiles := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		for _, profile := range shippedManifest(t, entry.Name()).Profiles() {
			profiles++
			if profile.Name == profile.Provider {
				t.Errorf("packs/%s ships profile %q over provider %q: delete it, the provider is "+
					"selectable by that name (OQ-RC3)", entry.Name(), profile.Name, profile.Provider)
			}
		}
	}
	if profiles == 0 {
		t.Fatal("no shipped pack declares a profile: the test is not reading the tree it exists for")
	}
}
