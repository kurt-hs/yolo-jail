package packoverlay

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/agentcfg"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

func TestReadablePostureOverlayUsesItsDecodedRowMetadata(t *testing.T) {
	owner := arraySurfaceOwner(t)
	for _, tc := range []struct {
		name    string
		replace bool
		want    []any
	}{
		{name: "append selected posture", want: []any{"base", "posture"}},
		{name: "explicit replace", replace: true, want: []any{"posture"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			replace := ""
			if tc.replace {
				replace = "\nreplace = true"
			}
			root := filepath.Join(t.TempDir(), "foreign")
			if err := os.MkdirAll(root, 0o755); err != nil {
				t.Fatal(err)
			}
			body := `[[autonomy]]
autonomous = { config = [{ agent = "claude", name = "settings", codec = "json", path = "~/.claude/settings.json", managed = { arr = ["posture"] } }] }` + replace + "\n"
			if err := os.WriteFile(filepath.Join(root, packdecl.ManifestTOMLName), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			foreign, problems := packload.LoadDir(root, "foreign")
			if len(problems) != 0 {
				t.Fatalf("LoadDir: %v", problems)
			}
			set := Collect([]*packload.Pack{owner, foreign}, true, nil)
			if len(set.Problems) != 0 || len(set.Orphans) != 0 {
				t.Fatalf("Collect problems=%v orphans=%+v", set.Problems, set.Orphans)
			}
			overlays := set.For("claude", "settings")
			if len(overlays) != 1 || overlays[0].AppendArrays == tc.replace {
				t.Fatalf("selected posture metadata = %+v; replace=%v", overlays, tc.replace)
			}
			surfaces, problems := owner.SurfacesFor(true)
			if len(problems) != 0 || len(surfaces) != 1 {
				t.Fatalf("owner surfaces=%v problems=%v", surfaces, problems)
			}
			result, err := agentcfg.Compose(agentcfg.Inputs{Surface: surfaces[0], Overlays: overlays})
			if err != nil {
				t.Fatalf("Compose: %v", err)
			}
			if got := result.ConfigMap()["arr"]; !reflect.DeepEqual(got, tc.want) {
				t.Errorf("composed foreign posture array = %#v, want %#v", got, tc.want)
			}
			if host := Collect([]*packload.Pack{owner, foreign}, false, nil); len(host.For("claude", "settings")) != 0 {
				t.Errorf("autonomous posture row leaked into the host notch: %+v", host.For("claude", "settings"))
			}
		})
	}
}

func TestReadableForeignPostureArrayYieldsToOwnerManagedLayer(t *testing.T) {
	root := filepath.Join(t.TempDir(), "foreign")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `[[autonomy]]
autonomous = { config = [{ agent = "claude", name = "settings", codec = "json", path = "~/.claude/settings.json", managed = { arr = ["foreign"] } }] }
`
	if err := os.WriteFile(filepath.Join(root, packdecl.ManifestTOMLName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	foreign, problems := packload.LoadDir(root, "foreign")
	if len(problems) != 0 {
		t.Fatalf("LoadDir: %v", problems)
	}
	owner := arraySurfaceOwner(t)
	surfaces, problems := owner.SurfacesFor(true)
	if len(problems) != 0 || len(surfaces) != 1 {
		t.Fatalf("owner surfaces=%v problems=%v", surfaces, problems)
	}
	surfaces[0].Managed = map[string]any{"arr": []any{"managed"}}
	set := Collect([]*packload.Pack{owner, foreign}, true, nil)
	overlays := set.For("claude", "settings")
	if len(overlays) != 1 || !overlays[0].AppendArrays {
		t.Fatalf("grouped posture overlay must retain its append metadata: %+v", overlays)
	}
	result, err := agentcfg.Compose(agentcfg.Inputs{Surface: surfaces[0], Overlays: overlays})
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if got, want := result.ConfigMap()["arr"], []any{"managed"}; !reflect.DeepEqual(got, want) {
		t.Errorf("foreign posture array overruled owner's managed layer: got %#v want %#v", got, want)
	}
}

func TestReadableDuplicateOverlayRowsKeepIndependentMetadata(t *testing.T) {
	root := filepath.Join(t.TempDir(), "duplicate")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `[[overlay]]
surface = "claude/settings"
set.arr = ["same"]

[[overlay]]
surface = "claude/settings"
set.arr = ["same"]
replace = true
`
	if err := os.WriteFile(filepath.Join(root, packdecl.ManifestTOMLName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	pack, problems := packload.LoadDir(root, "duplicate")
	if len(problems) != 0 {
		t.Fatalf("LoadDir: %v", problems)
	}
	set := Collect([]*packload.Pack{arraySurfaceOwner(t), pack}, true, nil)
	got := set.For("claude", "settings")
	if len(got) != 2 || !got[0].AppendArrays || got[1].AppendArrays {
		t.Fatalf("duplicate rows collapsed or rematched metadata: %+v", got)
	}
}
