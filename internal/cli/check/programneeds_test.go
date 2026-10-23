package check

// programneeds_test.go pins `yolo check`'s two call sites for a `program` need
// (docs/design/provisioner-sets.md OQ-PS5, PS-D18): the "provided by" line for a need another
// selected pack satisfies, and the FAIL for a program two selected packs carry a recipe for.
// AgentNameCollisions skips a clash made only of `program` claims, so the provider FAIL is the
// only thing standing between a two-provider config and a passing check.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

// programPackDir writes a local pack whose contributes list is contributions, and returns its
// dir; the basename is the pack name.
func programPackDir(t *testing.T, name, contributions string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"` + name + `","contributes":[` + contributions + `]}`
	if err := os.WriteFile(filepath.Join(dir, "pack.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

const (
	xtoolNeed   = `{"kind":"program","bin":"xtool"}`
	xtoolRecipe = `{"kind":"program","bin":"xtool","via":"npm","package":"xtool-pkg"}`
)

func TestSectionPacksNamesTheProviderOfANeed(t *testing.T) {
	needer := programPackDir(t, "needer", xtoolNeed)
	provb := programPackDir(t, "provb", xtoolRecipe)
	packsFixture(t, `{"packs": ["file://`+needer+`", "file://`+provb+`"]}`)

	var buf bytes.Buffer
	r := &reporter{w: &buf}
	(&Options{}).sectionPacks(r, jsonx.NewOrderedMap())

	if r.failed != 0 {
		t.Fatalf("a need with one provider must not fail check:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "program xtool, needed by pack needer, is provided by pack provb") {
		t.Errorf("check must name the provider, in the launch's words:\n%s", buf.String())
	}
}

func TestSectionPacksFailsOnTwoProvidersOfOneProgram(t *testing.T) {
	needer := programPackDir(t, "needer", xtoolNeed)
	provb := programPackDir(t, "provb", xtoolRecipe)
	provc := programPackDir(t, "provc", xtoolRecipe)
	packsFixture(t, `{"packs": ["file://`+needer+`", "file://`+provb+`", "file://`+provc+`"]}`)

	var buf bytes.Buffer
	r := &reporter{w: &buf}
	(&Options{}).sectionPacks(r, jsonx.NewOrderedMap())

	if r.failed == 0 {
		t.Fatalf("two providers of one program must FAIL check — the launch refuses it:\n%s", buf.String())
	}
	out := buf.String()
	for _, want := range []string{"program xtool has more than one provider", "packs provb, provc",
		"drop all but one of provb, provc from `packs`"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}
