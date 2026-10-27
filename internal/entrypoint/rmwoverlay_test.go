package entrypoint

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/agentcfg"
	"github.com/mschulkind-oss/yolo-jail/internal/agentcfg/manifest"
	"github.com/mschulkind-oss/yolo-jail/internal/render"
)

func TestRMWAppendArraysOverlayRepeatedRender(t *testing.T) {
	home := t.TempDir()
	e := &Env{Home: home, Vars: map[string]string{}, hostTarget: true, hostOwnership: render.OwnershipOwn}
	s := manifest.Surface{
		Agent: "example", Name: "settings", Path: "~/.example.json", Codec: "json", Mode: manifest.ModeRMW,
		Defaults: map[string]any{"nested": map[string]any{"default": true, "shared": "default"}},
		Managed:  map[string]any{"nested": map[string]any{"managed": true, "shared": "managed"}},
	}
	path := expandHomePath(e, s.Path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	initial := map[string]any{
		"items":   []any{float64(1), map[string]any{"b": float64(2), "a": float64(1)}, "user"},
		"nested":  map[string]any{"user": "keep", "shared": "file"},
		"servers": map[string]any{"user-server": map[string]any{"command": "user"}},
	}
	data, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	overlay := agentcfg.Overlay{Pack: "append-pack", AppendArrays: true, Data: map[string]any{
		"items":   []any{float64(1), map[string]any{"a": int64(1), "b": int64(2)}, "overlay"},
		"nested":  map[string]any{"fromOverlay": true, "shared": "overlay"},
		"servers": map[string]any{"overlay-server": map[string]any{"command": "overlay"}},
	}}
	contribs := &surfaceContribs{overlays: []agentcfg.Overlay{overlay}}
	computed := map[string]any{"servers": map[string]any{"computed-server": map[string]any{"command": "computed"}}}
	var firstRender []byte

	for renderNo := 1; renderNo <= 2; renderNo++ {
		if err := renderSurfaceRMWSurface(e, s, computed, contribs); err != nil {
			t.Fatalf("render %d: %v", renderNo, err)
		}
		gotBytes, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if renderNo == 1 {
			firstRender = append([]byte(nil), gotBytes...)
		} else if !bytes.Equal(gotBytes, firstRender) {
			t.Fatalf("second render is not byte-identical:\\nfirst: %s\\nsecond: %s", firstRender, gotBytes)
		}
		var got map[string]any
		if err := json.Unmarshal(gotBytes, &got); err != nil {
			t.Fatal(err)
		}
		want := map[string]any{
			"items":   []any{float64(1), map[string]any{"a": float64(1), "b": float64(2)}, "user", "overlay"},
			"nested":  map[string]any{"user": "keep", "fromOverlay": true, "default": true, "managed": true, "shared": "managed"},
			"servers": map[string]any{"computed-server": map[string]any{"command": "computed"}},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("render %d config mismatch:\n got: %#v\nwant: %#v", renderNo, got, want)
		}
	}

	provenance, err := os.ReadFile(prismProvenancePath(e, s.Agent, s.Name))
	if err != nil {
		t.Fatal(err)
	}
	recorded := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(provenance)), "\n") {
		key, value, ok := strings.Cut(line, "\t")
		if ok {
			recorded[key] = value
		}
	}
	for key, want := range map[string]string{
		"items":   "config-overlay:append-pack",
		"nested":  "managed",
		"servers": "computed",
	} {
		if recorded[key] != want {
			t.Errorf("provenance[%q] = %q, want %q", key, recorded[key], want)
		}
	}
}

func TestRMWOverlayArrayReplacementRemainsWholeArray(t *testing.T) {
	for _, tc := range []struct {
		name   string
		append bool
	}{
		{name: "explicit replacement"},
		{name: "legacy replacement"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := modeEnv(t)
			s := manifest.Surface{Agent: "example", Name: "settings", Path: "~/.example.json", Codec: "json", Mode: manifest.ModeRMW}
			path := expandHomePath(e, s.Path)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(`{"items":["user"]}`), 0o644); err != nil {
				t.Fatal(err)
			}
			contribs := &surfaceContribs{overlays: []agentcfg.Overlay{{Pack: "replace-pack", AppendArrays: tc.append, Data: map[string]any{"items": []any{"overlay"}}}}}
			if err := renderSurfaceRMWSurface(e, s, nil, contribs); err != nil {
				t.Fatal(err)
			}
			var got map[string]any
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			want := []any{"overlay"}
			if tc.append {
				want = []any{"user", "overlay"}
			}
			if !reflect.DeepEqual(got["items"], want) {
				t.Fatalf("items = %#v, want %#v", got["items"], want)
			}
		})
	}
}
