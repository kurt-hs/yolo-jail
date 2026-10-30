package entrypoint

import (
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// movedDelivery returns the files contribution of p that delivers to into, the destination
// under the jail home. A shipped pack delivers its plain-copy files through its home/ tree,
// which packload turns into one files contribution per file, so this is the declaration the
// renderer consumes whichever way the file reached it.
func movedDelivery(p *packload.Pack, into string) (packdecl.Contribution, bool) {
	for _, c := range p.Decl.Contributions() {
		if c.Kind == packdecl.KindFiles && c.Into == into {
			return c, true
		}
	}
	return packdecl.Contribution{}, false
}

// movedShippedFile returns the on-disk source of the file p delivers to into, resolved from
// the pack's own declaration rather than a hard-coded path inside the pack, and fails the
// test when the pack delivers nothing there.
func movedShippedFile(t *testing.T, p *packload.Pack, into string) string {
	t.Helper()
	c, ok := movedDelivery(p, into)
	if !ok {
		t.Fatalf("pack %s delivers nothing to %s", p.Name, into)
	}
	return filepath.Join(p.Root, filepath.FromSlash(c.From))
}

// movedCompatibilityHomeFile is where pack packDir keeps the compatibility data it delivers
// for bin through its home/ tree: the file lands at the same path under the jail home, which
// is the destination selectedProgramCompatibility looks a declaration up by.
func movedCompatibilityHomeFile(packDir, bin string) string {
	return filepath.Join(packDir, "home", filepath.FromSlash(programCompatibilityDir), bin+".json")
}

// movedDeclareCompatibility takes the staged copilot pack's compatibility data out of its
// home/ tree, writes the same bytes to compatibility.json at the pack root, and appends count
// explicit files declarations of from into the compatibility destination to its manifest. It
// returns the pack directory. With the home/ copy gone, each appended declaration is the only
// thing delivering there, so a test can exercise the refusals a declared source still meets.
func movedDeclareCompatibility(t *testing.T, root, from string, count int) string {
	t.Helper()
	packDir := filepath.Join(root, "_official", "copilot")
	homeFile := movedCompatibilityHomeFile(packDir, "copilot")
	data, err := os.ReadFile(homeFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(homeFile); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packDir, "compatibility.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	declaration := "\n[[files]]\nfrom = " + strconv.Quote(from) + "\ninto = " +
		strconv.Quote(path.Join(programCompatibilityDir, "copilot.json")) + "\n"
	movedAppendManifest(t, packDir, strings.Repeat(declaration, count))
	return packDir
}

// movedAppendManifest appends text to the TOML manifest of the staged pack at packDir.
func movedAppendManifest(t *testing.T, packDir, text string) {
	t.Helper()
	manifest := filepath.Join(packDir, packdecl.ManifestTOMLName)
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, append(data, text...), 0o644); err != nil {
		t.Fatal(err)
	}
}

// movedReplaceManifest replaces the one occurrence of old in the TOML manifest of the staged
// pack at packDir, failing the test when the manifest no longer holds it.
func movedReplaceManifest(t *testing.T, packDir, old, replacement string) {
	t.Helper()
	manifest := filepath.Join(packDir, packdecl.ManifestTOMLName)
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), old) != 1 {
		t.Fatalf("%s does not hold exactly one %q", manifest, old)
	}
	if err := os.WriteFile(manifest, []byte(strings.Replace(string(data), old, replacement, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}
