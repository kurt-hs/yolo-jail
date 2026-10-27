package modelcatalog

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func ids(c Catalog) []string {
	var out []string
	for id := range c.IDs {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// Every `id` string at any depth of every matched file is a known id, one segment matched at a
// time: pi 0.99.1's shape, {"<api>": {"chat:<id>": {"id": "<id>", …}}}, and a list of objects.
func TestReadCollectsOfflineCanonicalModelFactsWithoutFillingDefaults(t *testing.T) {
	pkg := t.TempDir()
	writeFile(t, filepath.Join(pkg, "catalog.json"), `{"models":[
		{"id":"global.anthropic.opus-5","vendor":"anthropic","api":"anthropic","context":1000000,"accepts":["text","image"],"price":{"input":1,"output":2,"cache_read":0.1,"cache_write":0.2}},
		{"id":"vendor/unknown","name":"Unknown","base":"model-base","extends":"shared","compact_at":1000,"openrouter_routing":{"order":["anthropic"]}}
	]}`)
	got, err := ReadCanonicalFacts(pkg, []string{"catalog.json"})
	if err != nil {
		t.Fatal(err)
	}
	legacy := Read(pkg, []string{"catalog.json"})
	if len(legacy.Facts) != 0 || !legacy.IDs["global.anthropic.opus-5"] {
		t.Fatalf("legacy agent catalog reader must remain ids-only: %#v", legacy)
	}
	facts, ok := got.Facts["global.anthropic.opus-5"]
	if !ok || facts.API != "anthropic" || facts.Context != 1_000_000 || facts.Price == nil || facts.Price.CacheWrite == nil || *facts.Price.CacheWrite != .2 {
		t.Fatalf("offline model facts = %#v", got.Facts)
	}
	unknown, ok := got.Facts["vendor/unknown"]
	if !ok || unknown.Name != "Unknown" || unknown.Base != "model-base" || unknown.Extends != "shared" || unknown.CompactAt != 1000 || unknown.OpenRouterRouting["order"] == nil || unknown.MaxTokens != 0 || unknown.Price != nil {
		t.Fatalf("catalog facts invented defaults or lost an authored field: %#v", unknown)
	}
}

func TestReadCanonicalFactsRefusesMalformedAuthoredFacts(t *testing.T) {
	pkg := t.TempDir()
	writeFile(t, filepath.Join(pkg, "facts.json"), `{"id":"model-x","price":{"input":"free"}}`)
	if _, err := ReadCanonicalFacts(pkg, []string{"facts.json"}); err == nil || !strings.Contains(err.Error(), "model-x") {
		t.Fatalf("malformed canonical fact source should name its model and fail: %v", err)
	}
}

func TestReadCollectsEveryIDTheMatchedFilesName(t *testing.T) {
	pkg := t.TempDir()
	writeFile(t, filepath.Join(pkg, "package.json"), `{"name": "agent", "version": "1.2.3"}`)
	data := filepath.Join(pkg, "node_modules", "@s", "ai", "dist", "providers", "data")
	writeFile(t, filepath.Join(data, "zai.json"),
		`{"openai-completions": {"chat:glm-5.3": {"id": "glm-5.3", "compat": {"x": 1}}}}`)
	writeFile(t, filepath.Join(data, "list.json"), `[{"id": "alpha-1"}, {"nested": [{"id": "beta-2"}]}]`)
	writeFile(t, filepath.Join(data, "notes.txt"), `{"id": "not-a-catalog-file"}`)
	writeFile(t, filepath.Join(data, "broken.json"), `{"id": `)
	c := Read(pkg, []string{"node_modules/@s/ai/dist/providers/data/*.json"})
	if want := []string{"alpha-1", "beta-2", "glm-5.3"}; !reflect.DeepEqual(ids(c), want) {
		t.Errorf("ids = %v, want %v (only matched files, a broken one skipped)", ids(c), want)
	}
	if c.Files != 2 {
		t.Errorf("Files = %d, want 2: the broken file does not parse and is not counted", c.Files)
	}
	if !Installed(pkg) || Version(pkg) != "1.2.3" {
		t.Errorf("Installed = %v, Version = %q, want true and 1.2.3", Installed(pkg), Version(pkg))
	}
}

// A glob that matches nothing is a catalog that could not be read: zero files, whatever else is
// on disk, which is what tells "could not ask" from "not found".
func TestReadOfAMovedCatalogReadsNothing(t *testing.T) {
	pkg := t.TempDir()
	writeFile(t, filepath.Join(pkg, "dist", "models.generated.js"), `export const M = {id: "x"}`)
	c := Read(pkg, []string{"node_modules/@s/ai/dist/providers/data/*.json"})
	if c.Files != 0 || len(c.IDs) != 0 {
		t.Errorf("a glob matching nothing read %d files and %v", c.Files, ids(c))
	}
	if Installed(filepath.Join(pkg, "absent")) {
		t.Error("a directory with no package.json is not an installed package")
	}
}

// A pattern character in the package directory's own path is never read as one: only the
// declared segments are patterns.
func TestReadMatchesOnlyTheDeclaredSegments(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "odd[dir]")
	writeFile(t, filepath.Join(pkg, "data", "a.json"), `{"id": "one"}`)
	if got := ids(Read(pkg, []string{"data/*.json"})); !reflect.DeepEqual(got, []string{"one"}) {
		t.Errorf("ids = %v, want [one]", got)
	}
}
