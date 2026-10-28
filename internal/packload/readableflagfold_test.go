package packload

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/agentcfg"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
)

func TestReadableOwnPostureManagedArrayMetadataUsesAppendAndReplace(t *testing.T) {
	load := func(t *testing.T, name, body string) *Pack {
		t.Helper()
		root := filepath.Join(t.TempDir(), name)
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, packdecl.ManifestTOMLName), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		p, problems := LoadDir(root, name)
		if len(problems) != 0 {
			t.Fatalf("LoadDir: %v", problems)
		}
		return p
	}
	for _, tc := range []struct {
		name    string
		replace bool
		want    []any
	}{
		{name: "append", want: []any{"base", "posture"}},
		{name: "replace", replace: true, want: []any{"posture"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			replace := ""
			if tc.replace {
				replace = "replace = true\n"
			}
			p := load(t, "owner", `[[config]]
config = [{ agent = "pi", name = "settings", codec = "json", path = "~/.pi/settings.json", managed = { arr = ["base"] } }]

[[autonomy]]
autonomous = { config = [{ agent = "pi", name = "settings", codec = "json", path = "~/.pi/settings.json", managed = { arr = ["posture"] } }] }
`+replace)
			surfaces, problems := p.SurfacesFor(true)
			if len(problems) != 0 || len(surfaces) != 1 {
				t.Fatalf("SurfacesFor: surfaces=%v problems=%v", surfaces, problems)
			}
			result, err := agentcfg.Compose(agentcfg.Inputs{Surface: surfaces[0]})
			if err != nil {
				t.Fatalf("Compose: %v", err)
			}
			if got := result.ConfigMap()["arr"]; !reflect.DeepEqual(got, tc.want) {
				t.Errorf("own posture managed array = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestReadableLaunchRowsFoldAppendReplaceLegacyAndNotchFromDecodedPacks(t *testing.T) {
	writePack := func(name, file, body string) *Pack {
		t.Helper()
		root := filepath.Join(t.TempDir(), name)
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, file), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		p, problems := LoadDir(root, name)
		if len(problems) != 0 {
			t.Fatalf("LoadDir(%s): %v", name, problems)
		}
		return p
	}

	permission := writePack("permission", packdecl.ManifestTOMLName, `[[autonomy]]
autonomous = { launch = [{ bin = "agy", flags = ["--dangerously-skip-permissions"] }] }
guarded = { launch = [{ bin = "agy", flags = ["--confirm"] }] }

[[launch]]
bin = "agy"
flags = ["--audit"]
`)
	model := writePack("model", packdecl.ManifestTOMLName, `[[autonomy]]
autonomous = { launch = [{ bin = "agy", flags = ["--model", "gemini-3.8-flash-medium"] }] }
guarded = { launch = [{ bin = "agy", flags = ["--guarded-model"] }] }
`)

	for _, tc := range []struct {
		name     string
		autonomy bool
		want     []string
	}{
		{name: "autonomous appended rows", autonomy: true, want: []string{"agy", "--dangerously-skip-permissions", "--audit", "--model", "gemini-3.8-flash-medium", "prompt"}},
		{name: "guarded rows are independently selected", autonomy: false, want: []string{"agy", "--confirm", "--audit", "--guarded-model", "prompt"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, inj := InjectLaunchFlags([]*Pack{permission, model}, tc.autonomy, []string{"agy", "prompt"})
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("argv = %#v, want %#v", got, tc.want)
			}
			if inj == nil || len(inj.Sources) != 3 {
				t.Fatalf("injection attribution = %#v, want all three selected TOML rows", inj)
			}
			if inj.Pack != "" {
				t.Errorf("singular Pack = %q for a multi-pack append; must not misattribute", inj.Pack)
			}
			wantPacks := []string{"permission", "permission", "model"}
			wantFlags := [][]string{{tc.want[1]}, {tc.want[2]}, {tc.want[3]}}
			if tc.autonomy {
				wantFlags[2] = tc.want[3:5]
			}
			for i, source := range inj.Sources {
				wantPack := wantPacks[i]
				if source.Pack != wantPack || source.Origin.Format != packdecl.ManifestFormatTOML ||
					source.Location.File != packdecl.ManifestTOMLName || source.Location.Line == 0 ||
					!reflect.DeepEqual(source.Flags, wantFlags[i]) {
					t.Errorf("source %d = %#v; want pack %s row and actual added flags %#v", i, source, wantPack, wantFlags[i])
				}
			}
			if inj.Sources[0].Path != filepath.Join(permission.Root, packdecl.ManifestTOMLName) ||
				inj.Sources[1].Path != filepath.Join(permission.Root, packdecl.ManifestTOMLName) ||
				inj.Sources[2].Path != filepath.Join(model.Root, packdecl.ManifestTOMLName) {
				t.Errorf("source paths must derive from each pack root: %#v", inj.Sources)
			}
		})
	}

	replacer := writePack("replace", packdecl.ManifestTOMLName, `[[launch]]
bin = "agy"
flags = ["--model", "only-model"]
replace = true
`)
	got, inj := InjectLaunchFlags([]*Pack{permission, model, replacer}, true, []string{"agy", "--model=typed", "prompt"})
	if want := []string{"agy", "only-model", "--model=typed", "prompt"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("explicit replace argv = %#v, want %#v", got, want)
	}
	if inj == nil || len(inj.Sources) != 1 || inj.Sources[0].Pack != "replace" || len(inj.Sources[0].Flags) != 1 || inj.Sources[0].Flags[0] != "only-model" {
		t.Fatalf("explicit replace attribution = %#v, want only actual added flags from replacement row", inj)
	}

	legacy := writePack("legacy", packdecl.ManifestName, `{"contributes":[{"kind":"autonomy","autonomous":{"launch":[{"bin":"agy","flags":["--legacy"]}]}}]}`)
	got, inj = InjectLaunchFlags([]*Pack{permission, model, legacy}, true, []string{"agy", "prompt"})
	if want := []string{"agy", "--legacy", "prompt"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("later legacy replacement argv = %#v, want %#v", got, want)
	}
	if inj == nil || inj.Pack != "legacy" || len(inj.Sources) != 1 || inj.Sources[0].Origin.Format != packdecl.ManifestFormatJSON {
		t.Errorf("later legacy replacement attribution = %#v", inj)
	}

	// Identical flags already present are not attributed to the row that declared them.
	_, inj = InjectLaunchFlags([]*Pack{permission, model}, true,
		[]string{"agy", "--dangerously-skip-permissions", "--audit", "--model", "gemini-3.8-flash-medium"})
	if inj != nil {
		t.Errorf("fully typed flags produced invented attribution: %#v", inj)
	}
}
