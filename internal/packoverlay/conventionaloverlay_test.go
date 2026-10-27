package packoverlay

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/agentcfg"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

func TestCollectRefusesConventionalOverlaySubstitutedAfterDiscovery(t *testing.T) {
	for _, kind := range []string{"ancestor", "leaf", "fifo"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			overlays := filepath.Join(root, "overlays")
			if err := os.MkdirAll(overlays, 0o755); err != nil {
				t.Fatal(err)
			}
			name := "claude-settings.jsonc"
			path := filepath.Join(overlays, name)
			body := []byte(`{"answer":"safe"}`)
			if err := os.WriteFile(path, body, 0o644); err != nil {
				t.Fatal(err)
			}
			outsideDir := t.TempDir()
			outside := filepath.Join(outsideDir, name)
			if err := os.Link(path, outside); err != nil {
				t.Fatal(err)
			}
			contributor, problems := packload.LoadDir(root, "addon")
			if len(problems) != 0 {
				t.Fatalf("LoadDir: %v", problems)
			}
			owner := surfaceOwner(t, map[string]any{})
			var fifoProbe, fifoWriter *os.File
			writerReady := make(chan struct {
				file *os.File
				err  error
			}, 1)
			oldHook := conventionalOverlayBeforeOpen
			conventionalOverlayBeforeOpen = func(component string) {
				switch {
				case kind == "ancestor" && component == "overlays":
					moved := filepath.Join(root, "overlays-original")
					if err := os.Rename(overlays, moved); err != nil {
						t.Errorf("move overlays: %v", err)
						return
					}
					if err := os.Symlink(outsideDir, overlays); err != nil {
						t.Errorf("replace overlays: %v", err)
						return
					}
				case kind == "leaf" && component == "leaf":
					if err := os.Remove(path); err != nil {
						t.Errorf("remove overlay: %v", err)
						return
					}
					if err := os.Symlink(outside, path); err != nil {
						t.Errorf("replace overlay: %v", err)
						return
					}
				case kind == "fifo" && component == "leaf":
					if err := os.Remove(path); err != nil {
						t.Errorf("remove overlay: %v", err)
						return
					}
					if err := syscall.Mkfifo(path, 0o600); err != nil {
						t.Errorf("create FIFO: %v", err)
						return
					}
					probe, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
					if err != nil {
						t.Errorf("open FIFO probe: %v", err)
						return
					}
					fifoProbe = probe
					go func() {
						f, err := os.OpenFile(path, os.O_WRONLY, 0)
						writerReady <- struct {
							file *os.File
							err  error
						}{f, err}
					}()
					writer := <-writerReady
					if writer.err != nil {
						t.Errorf("open FIFO writer: %v", writer.err)
						return
					}
					fifoWriter = writer.file
				}
			}
			defer func() { conventionalOverlayBeforeOpen = oldHook }()
			set := Collect([]*packload.Pack{owner, contributor}, true, nil)
			if fifoWriter != nil {
				if err := fifoWriter.Close(); err != nil {
					t.Errorf("close FIFO writer: %v", err)
				}
			}
			if fifoProbe != nil {
				if err := fifoProbe.Close(); err != nil {
					t.Errorf("close FIFO probe: %v", err)
				}
			}
			if len(set.Problems) == 0 || !strings.Contains(strings.Join(set.Problems, "\n"), name) {
				t.Fatalf("Collect accepted %s substitution: %+v", kind, set)
			}
		})
	}
}

func TestConventionalOverlayEmptyArrayComposesAsArray(t *testing.T) {
	for _, tc := range []struct {
		name string
		base any
	}{
		{name: "absent"},
		{name: "empty", base: []any{}},
		{name: "scalar", base: "old"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, "overlays"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "overlays", "claude-settings.jsonc"), []byte(`{"items":[]}`), 0o644); err != nil {
				t.Fatal(err)
			}
			contributor, problems := packload.LoadDir(root, "addon")
			if len(problems) != 0 {
				t.Fatalf("LoadDir: %v", problems)
			}
			defaults := map[string]any{}
			if tc.name != "absent" {
				defaults["items"] = tc.base
			}
			owner := surfaceOwner(t, defaults)
			set := Collect([]*packload.Pack{owner, contributor}, true, nil)
			if len(set.Problems) != 0 {
				t.Fatalf("Collect: %v", set.Problems)
			}
			surfaces, _ := owner.SurfacesFor(true)
			result, err := agentcfg.Compose(agentcfg.Inputs{Surface: surfaces[0], Overlays: set.For("claude", "settings")})
			if err != nil {
				t.Fatal(err)
			}
			if got, ok := result.ConfigMap()["items"].([]any); !ok || got == nil || len(got) != 0 {
				t.Fatalf("composed items = %#v (%T), want non-nil empty array; encoded=%s", result.ConfigMap()["items"], result.ConfigMap()["items"], result.Encoded)
			}
			if !strings.Contains(string(result.Encoded), `"items": []`) && !strings.Contains(string(result.Encoded), `"items":[]`) {
				t.Fatalf("encoded empty array as %s", result.Encoded)
			}
		})
	}
}

func TestConventionalOverlayUsesSelectedSurfaceOwnerCodec(t *testing.T) {
	for _, tc := range []struct {
		name, filename string
		wantProblem    bool
	}{
		{name: "declared TOML", filename: "claude-settings.toml"},
		{name: "wrong extension", filename: "claude-settings.jsonc", wantProblem: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, "overlays"), 0o755); err != nil {
				t.Fatal(err)
			}
			body := `answer = "selected owner"`
			if filepath.Ext(tc.filename) == ".jsonc" {
				body = `{"answer":"selected owner"}`
			}
			if err := os.WriteFile(filepath.Join(root, "overlays", tc.filename), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			contributor, problems := packload.LoadDir(root, "addon")
			if len(problems) != 0 {
				t.Fatalf("LoadDir: %v", problems)
			}
			owner := customSurfaceOwnerWithCodec(t, "claude", "settings", "toml")
			set := Collect([]*packload.Pack{owner, contributor}, true, nil)
			if tc.wantProblem {
				if !strings.Contains(strings.Join(set.Problems, "\n"), `owner codec is "toml"`) {
					t.Fatalf("wrong extension was not rejected against the selected owner: %v", set.Problems)
				}
				return
			}
			if len(set.Problems) != 0 || len(set.Orphans) != 0 {
				t.Fatalf("selected-owner overlay problems=%v orphans=%+v", set.Problems, set.Orphans)
			}
			surfaces, surfaceProblems := owner.SurfacesFor(true)
			if len(surfaceProblems) != 0 || len(surfaces) != 1 {
				t.Fatalf("owner surfaces=%v problems=%v", surfaces, surfaceProblems)
			}
			result, err := agentcfg.Compose(agentcfg.Inputs{Surface: surfaces[0], Overlays: set.For("claude", "settings")})
			if err != nil {
				t.Fatal(err)
			}
			if got := result.ConfigMap()["answer"]; got != "selected owner" {
				t.Errorf("composed answer = %#v", got)
			}
		})
	}
}

func customSurfaceOwnerWithCodec(t *testing.T, agent, name, codec string) *packload.Pack {
	t.Helper()
	raw, err := json.Marshal([]map[string]any{{
		"agent": agent, "name": name, "codec": codec, "path": "~/.custom/settings.toml", "managed": map[string]any{},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return &packload.Pack{Name: "selected-custom-owner", Decl: &packdecl.Manifest{Contributes: []packdecl.Contribution{{Kind: packdecl.KindConfig, Raw: raw}}}}
}

func TestConventionalOverlayLoadsGatesByProfileAndPreservesComposePrecedence(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "overlays"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "overlays", "claude-settings@bedrock.jsonc"), []byte("{ // overlay\n \"arr\": [\"one\", \"two\", \"one\"] }"), 0o644); err != nil {
		t.Fatal(err)
	}
	contributor, problems := packload.LoadDir(root, "addon")
	if len(problems) != 0 {
		t.Fatalf("LoadDir: %v", problems)
	}
	owner := arraySurfaceOwner(t)
	set := Collect([]*packload.Pack{owner, contributor}, true, map[string][]string{"claude": {"bedrock"}})
	if len(set.Problems) != 0 || len(set.Orphans) != 0 {
		t.Fatalf("Collect problems=%v orphans=%+v", set.Problems, set.Orphans)
	}
	if overlays := set.For("claude", "settings"); len(overlays) != 1 || !overlays[0].AppendArrays {
		t.Fatalf("conventional overlay = %+v, want one additive overlay", overlays)
	}
	surfaces, _ := owner.SurfacesFor(true)
	base := agentcfg.Inputs{Surface: surfaces[0], Overlays: set.For("claude", "settings")}
	plain, err := agentcfg.Compose(base)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := plain.ConfigMap()["arr"], []any{"base", "one", "two"}; !reflect.DeepEqual(got, want) {
		t.Errorf("composed array = %#v, want %#v", got, want)
	}
	if got := plain.Provenance["arr"]; got != agentcfg.OverlayLayer("addon") {
		t.Errorf("provenance = %q, want overlay contributor", got)
	}

	captured := base
	captured.Overlay = map[string]any{"arr": []any{"captured"}}
	capturedResult, err := agentcfg.Compose(captured)
	if err != nil {
		t.Fatal(err)
	}
	if got := capturedResult.ConfigMap()["arr"]; !reflect.DeepEqual(got, []any{"captured"}) {
		t.Errorf("capture did not outrank config overlay: %#v", got)
	}
	computed := base
	computed.Computed = map[string]any{"arr": []any{"computed"}}
	computedResult, err := agentcfg.Compose(computed)
	if err != nil {
		t.Fatal(err)
	}
	if got := computedResult.ConfigMap()["arr"]; !reflect.DeepEqual(got, []any{"computed"}) {
		t.Errorf("computed did not outrank config overlay: %#v", got)
	}
	managed := base
	managed.Surface.Managed = map[string]any{"arr": []any{"managed"}}
	managedResult, err := agentcfg.Compose(managed)
	if err != nil {
		t.Fatal(err)
	}
	if got := managedResult.ConfigMap()["arr"]; !reflect.DeepEqual(got, []any{"managed"}) || managedResult.Provenance["arr"] != agentcfg.LayerManaged {
		t.Errorf("managed precedence/provenance = %#v / %q", got, managedResult.Provenance["arr"])
	}

	inactive := Collect([]*packload.Pack{owner, contributor}, true, nil)
	if len(inactive.Problems) != 0 || len(inactive.Orphans) != 0 || len(inactive.For("claude", "settings")) != 0 {
		t.Errorf("inactive profile was not a clean skip: problems=%v orphans=%+v overlays=%+v", inactive.Problems, inactive.Orphans, inactive.For("claude", "settings"))
	}
}

func TestMalformedConventionalOverlayIsFatalWithoutSelectedOwner(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "overlays"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "overlays", "claude-settings.jsonc"), []byte("{ broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	contributor, problems := packload.LoadDir(root, "addon")
	if len(problems) != 0 {
		t.Fatalf("LoadDir: %v", problems)
	}
	set := Collect([]*packload.Pack{contributor}, true, nil)
	if len(set.Problems) == 0 || !strings.Contains(strings.Join(set.Problems, "\n"), "claude-settings.jsonc") {
		t.Fatalf("malformed ownerless overlay was not fatal/actionable: %v", set.Problems)
	}
}

func TestConventionalJSONCOverlayRecursesIntoNewObjects(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "overlays"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"nested":{"removed":null,"items":[1,1]}}`
	if err := os.WriteFile(filepath.Join(root, "overlays", "claude-settings.jsonc"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	contributor, problems := packload.LoadDir(root, "addon")
	if len(problems) != 0 {
		t.Fatalf("LoadDir: %v", problems)
	}
	owner := surfaceOwner(t, map[string]any{})
	set := Collect([]*packload.Pack{owner, contributor}, true, nil)
	if len(set.Problems) != 0 {
		t.Fatalf("Collect: %v", set.Problems)
	}
	surfaces, _ := owner.SurfacesFor(true)
	result, err := agentcfg.Compose(agentcfg.Inputs{Surface: surfaces[0], Overlays: set.For("claude", "settings")})
	if err != nil {
		t.Fatal(err)
	}
	nested, _ := result.ConfigMap()["nested"].(map[string]any)
	if _, ok := nested["removed"]; ok {
		t.Errorf("new nested object retained a null tombstone: %#v", nested)
	}
	if got, want := nested["items"], []any{float64(1)}; !reflect.DeepEqual(got, want) {
		t.Errorf("new nested array = %#v, want recursively deduplicated %#v", got, want)
	}
}

func TestConventionalOverlayRejectsAmbiguousHyphenatedSurfaceIdentity(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "overlays"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "overlays", "a-b-c.jsonc"), []byte(`{"x":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	contributor, problems := packload.LoadDir(root, "addon")
	if len(problems) != 0 {
		t.Fatalf("LoadDir: %v", problems)
	}
	packs := []*packload.Pack{contributor, customSurfaceOwner(t, "a-b", "c"), customSurfaceOwner(t, "a", "b-c")}
	set := Collect(packs, true, nil)
	joined := strings.Join(set.Problems, "\n")
	if !strings.Contains(joined, "a-b/c") || !strings.Contains(joined, "a/b-c") || !strings.Contains(joined, "ambiguous") {
		t.Fatalf("hyphen identity ambiguity did not name both alternatives: %v", set.Problems)
	}
}

func customSurfaceOwner(t *testing.T, agent, name string) *packload.Pack {
	t.Helper()
	raw, err := json.Marshal([]map[string]any{{"agent": agent, "name": name, "codec": "json", "path": "~/.custom/settings.json", "managed": map[string]any{}}})
	if err != nil {
		t.Fatal(err)
	}
	return &packload.Pack{Name: agent + "-owner", Decl: &packdecl.Manifest{Contributes: []packdecl.Contribution{{Kind: packdecl.KindConfig, Raw: raw}}}}
}
