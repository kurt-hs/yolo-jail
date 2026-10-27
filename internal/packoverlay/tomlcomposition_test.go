package packoverlay

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/agentcfg"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

func TestTOMLManifestOverlayArrayDedupUsesNormalizedNumericIdentity(t *testing.T) {
	root := t.TempDir()
	body := "[[overlay]]\nsurface = \"claude/settings\"\nset.arr = [1, { n = 2 }]\n"
	if err := os.WriteFile(filepath.Join(root, packdecl.ManifestTOMLName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	contributor, problems := packload.LoadDir(root, "addon")
	if len(problems) != 0 {
		t.Fatalf("LoadDir: %v", problems)
	}
	owner := numericArraySurfaceOwner(t)
	set := Collect([]*packload.Pack{owner, contributor}, true, nil)
	if len(set.Problems) != 0 {
		t.Fatalf("Collect: %v", set.Problems)
	}
	patch := set.For("claude", "settings")[0].Data.(map[string]any)
	if _, ok := patch["arr"].([]any)[0].(float64); !ok {
		t.Fatalf("overlay decoder type = %T, want normalized JSON float64", patch["arr"].([]any)[0])
	}
	surfaces, _ := owner.SurfacesFor(true)
	surface := surfaces[0]
	// Base TOML surfaces retain integer decoder values while overlay maps are normalized
	// through JSON; canonical entry identity must treat both representations as equal.
	surface.Defaults = map[string]any{"arr": []any{int64(1), map[string]any{"n": int64(2)}}}
	result, err := agentcfg.Compose(agentcfg.Inputs{Surface: surface, Overlays: set.For("claude", "settings")})
	if err != nil {
		t.Fatal(err)
	}
	want := []any{int64(1), map[string]any{"n": int64(2)}}
	if got := result.ConfigMap()["arr"]; !reflect.DeepEqual(got, want) {
		t.Errorf("numeric/object entries duplicated across TOML and JSON decoders: got %#v want %#v", got, want)
	}
}

func TestTOMLManifestOverlayAppendsAndDeduplicatesArraysAtCollectorAndCompose(t *testing.T) {
	for _, tc := range []struct {
		name       string
		replace    bool
		want       []any
		wantNested []any
	}{
		{name: "default add", want: []any{"base", "one", "two"}, wantNested: []any{"nested"}},
		{name: "explicit replace", replace: true, want: []any{"one", "two", "one"}, wantNested: []any{"nested", "nested"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			replace := ""
			if tc.replace {
				replace = "replace = true\n"
			}
			body := "[[overlay]]\nsurface = \"claude/settings\"\nset.arr = [\"one\", \"two\", \"one\"]\nset.nested.arr = [\"nested\", \"nested\"]\n" + replace
			if err := os.WriteFile(filepath.Join(root, packdecl.ManifestTOMLName), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			contributor, problems := packload.LoadDir(root, "addon")
			if len(problems) != 0 {
				t.Fatalf("LoadDir: %v", problems)
			}
			owner := arraySurfaceOwner(t)
			set := Collect([]*packload.Pack{owner, contributor}, true, nil)
			if len(set.Problems) != 0 || len(set.Orphans) != 0 {
				t.Fatalf("Collect problems=%v orphans=%+v", set.Problems, set.Orphans)
			}
			surfaces, problems := owner.SurfacesFor(true)
			if len(problems) != 0 || len(surfaces) != 1 {
				t.Fatalf("owner surfaces=%v problems=%v", surfaces, problems)
			}
			result, err := agentcfg.Compose(agentcfg.Inputs{Surface: surfaces[0], Overlays: set.For("claude", "settings")})
			if err != nil {
				t.Fatalf("Compose: %v", err)
			}
			if got := result.ConfigMap()["arr"]; !reflect.DeepEqual(got, tc.want) {
				t.Errorf("arr = %#v, want %#v", got, tc.want)
			}
			nested, _ := result.ConfigMap()["nested"].(map[string]any)
			if got := nested["arr"]; !reflect.DeepEqual(got, tc.wantNested) {
				t.Errorf("nested arr = %#v, want %#v", got, tc.wantNested)
			}
		})
	}
}

func TestTOMLManifestOverlayReplacementMetadataRespectsInactiveProfile(t *testing.T) {
	root := t.TempDir()
	body := "[[overlay]]\nsurface = \"claude/settings\"\nprofile = \"bedrock\"\nset.arr = [\"same\"]\n\n[[overlay]]\nsurface = \"claude/settings\"\nset.arr = [\"same\"]\nreplace = true\n"
	if err := os.WriteFile(filepath.Join(root, packdecl.ManifestTOMLName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	contributor, problems := packload.LoadDir(root, "addon")
	if len(problems) != 0 {
		t.Fatalf("LoadDir: %v", problems)
	}
	owner := arraySurfaceOwner(t)
	set := Collect([]*packload.Pack{owner, contributor}, true, nil)
	if len(set.Problems) != 0 || len(set.Orphans) != 0 {
		t.Fatalf("Collect problems=%v orphans=%+v", set.Problems, set.Orphans)
	}
	surfaces, problems := owner.SurfacesFor(true)
	if len(problems) != 0 || len(surfaces) != 1 {
		t.Fatalf("owner surfaces=%v problems=%v", surfaces, problems)
	}
	result, err := agentcfg.Compose(agentcfg.Inputs{Surface: surfaces[0], Overlays: set.For("claude", "settings")})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := result.ConfigMap()["arr"], []any{"same"}; !reflect.DeepEqual(got, want) {
		t.Errorf("active replacement inherited inactive profile's append mode: got %#v want %#v", got, want)
	}
}

func TestTOMLManifestOverlayAppendModesFollowDuplicateDeclarationOrder(t *testing.T) {
	root := t.TempDir()
	body := "[[overlay]]\nsurface = \"claude/settings\"\nset.arr = [\"same\"]\n\n[[overlay]]\nsurface = \"claude/settings\"\nset.arr = [\"same\"]\nreplace = true\n"
	if err := os.WriteFile(filepath.Join(root, packdecl.ManifestTOMLName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	contributor, problems := packload.LoadDir(root, "addon")
	if len(problems) != 0 {
		t.Fatalf("LoadDir: %v", problems)
	}
	owner := arraySurfaceOwner(t)
	set := Collect([]*packload.Pack{owner, contributor}, true, nil)
	surfaces, _ := owner.SurfacesFor(true)
	result, err := agentcfg.Compose(agentcfg.Inputs{Surface: surfaces[0], Overlays: set.For("claude", "settings")})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := result.ConfigMap()["arr"], []any{"same"}; !reflect.DeepEqual(got, want) {
		t.Errorf("the later explicit replacement inherited the first identical declaration's mode: got %#v want %#v", got, want)
	}
}

func TestLegacyManifestOverlayRetainsArrayReplacement(t *testing.T) {
	root := t.TempDir()
	body := `{"contributes":[{"kind":"config-overlay","surface":"claude/settings","config":{"managed":{"arr":["legacy"]}}}]}`
	if err := os.WriteFile(filepath.Join(root, packdecl.ManifestName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	contributor, problems := packload.LoadDir(root, "legacy-addon")
	if len(problems) != 0 {
		t.Fatalf("LoadDir: %v", problems)
	}
	owner := arraySurfaceOwner(t)
	set := Collect([]*packload.Pack{owner, contributor}, true, nil)
	surfaces, _ := owner.SurfacesFor(true)
	result, err := agentcfg.Compose(agentcfg.Inputs{Surface: surfaces[0], Overlays: set.For("claude", "settings")})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := result.ConfigMap()["arr"], []any{"legacy"}; !reflect.DeepEqual(got, want) {
		t.Errorf("legacy arr = %#v, want %#v", got, want)
	}
}

func arraySurfaceOwner(t *testing.T) *packload.Pack {
	t.Helper()
	return surfaceOwner(t, map[string]any{"arr": []any{"base"}})
}

func numericArraySurfaceOwner(t *testing.T) *packload.Pack {
	t.Helper()
	return surfaceOwner(t, map[string]any{"arr": []any{float64(1), map[string]any{"n": float64(2)}}})
}

func surfaceOwner(t *testing.T, defaults map[string]any) *packload.Pack {
	t.Helper()
	raw, err := json.Marshal([]map[string]any{{
		"agent": "claude", "name": "settings", "codec": "json", "path": "~/.claude/settings.json",
		"defaults": defaults, "managed": map[string]any{},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return &packload.Pack{Name: "claude", Decl: &packdecl.Manifest{Contributes: []packdecl.Contribution{{Kind: packdecl.KindConfig, Raw: raw}}}}
}
