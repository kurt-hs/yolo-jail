package packdecl

import (
	"io/fs"
	"path"
	"strings"
	"testing"
)

// The shipped packs are pack.toml files, and a few examples and fixtures are still pack.json:
// these readers take a manifest's bytes and its file name and use the reader that name selects,
// so a test over "every manifest the repository ships" reads each one the way the loader would.

// decodeManifestNamed is Decode for pack.json and DecodeTOML for pack.toml.
func decodeManifestNamed(name string, data []byte) (*Manifest, []string) {
	if strings.HasSuffix(name, ManifestTOMLName) {
		return DecodeTOML(data)
	}
	return Decode(data)
}

// decodeManifestNamedTolerant is DecodeTolerant for pack.json and DecodeTOMLTolerant for pack.toml.
func decodeManifestNamedTolerant(name string, data []byte) (*Manifest, []string, []string) {
	if strings.HasSuffix(name, ManifestTOMLName) {
		return DecodeTOMLTolerant(data)
	}
	return DecodeTolerant(data)
}

// decodeManifestNamedForUse is DecodeForUse for pack.json and DecodeTOMLForUse for pack.toml.
func decodeManifestNamedForUse(name string, data []byte) (*Manifest, []string, []string) {
	if strings.HasSuffix(name, ManifestTOMLName) {
		return DecodeTOMLForUse(data)
	}
	return DecodeForUse(data)
}

// readManifestIn reads dir's manifest from fsys under whichever name it ships (pack.toml first),
// and returns the name it found with the bytes; ok is false when the directory has neither.
func readManifestIn(fsys fs.FS, dir string) (name string, data []byte, ok bool) {
	for _, candidate := range []string{ManifestTOMLName, ManifestName} {
		if b, err := fs.ReadFile(fsys, path.Join(dir, candidate)); err == nil {
			return candidate, b, true
		}
	}
	return "", nil, false
}

// decodeShippedPack decodes dir's shipped manifest strictly, failing the test on a problem.
func decodeShippedPack(t *testing.T, fsys fs.FS, dir string) *Manifest {
	t.Helper()
	name, data, ok := readManifestIn(fsys, dir)
	if !ok {
		t.Fatalf("%s ships no manifest", dir)
	}
	m, problems := decodeManifestNamed(name, data)
	if len(problems) != 0 {
		t.Fatalf("%s/%s: %v", dir, name, problems)
	}
	return m
}
