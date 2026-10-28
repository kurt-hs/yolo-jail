package packload

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packstage"
)

func TestSelectedPackFSBindsDeclarationAndModuleFromOneFilteredView(t *testing.T) {
	files := fstest.MapFS{
		"fixture/pack.json":                      {Data: []byte(`{"name":"fixture","contributes":[{"kind":"loophole","from":"loopholes/audio"}]}`), Mode: 0o444},
		"fixture/loopholes/audio/manifest.jsonc": {Data: []byte(`{"name":"audio","version":1}`), Mode: 0o444},
	}
	selected, problems, err := ReadSelectedPackFS(files, "fixture", "/diagnostic/fixture", "fixture", nil, nil)
	if err != nil || len(problems) != 0 {
		t.Fatalf("construct selected view: problems=%v err=%v", problems, err)
	}
	modules, refusals, warnings, err := ReadableLoopholeModules(selected)
	if err != nil || len(refusals) != 0 || len(warnings) != 0 || len(modules) != 1 || modules[0].Decl.Name != "audio" {
		t.Fatalf("unfiltered source modules=%#v refusals=%v warnings=%v err=%v", modules, refusals, warnings, err)
	}

	filtered, problems, err := ReadSelectedPackFS(files, "fixture", "/diagnostic/selected/fixture", "fixture", nil, []string{"loopholes/audio/manifest.jsonc"})
	if err != nil || len(problems) != 0 {
		t.Fatalf("construct filtered view: problems=%v err=%v", problems, err)
	}
	modules, refusals, warnings, err = ReadableLoopholeModules(filtered)
	if err != nil || len(modules) != 0 || len(refusals) != 1 || len(warnings) != 0 {
		t.Fatalf("filtered module remains visible: modules=%#v refusals=%v warnings=%v err=%v", modules, refusals, warnings, err)
	}
	manifestless, problems, err := ReadSelectedPackFS(files, "fixture", "/diagnostic/manifestless", "fixture", []string{"loopholes/audio/**"}, nil)
	if err != nil || len(problems) != 0 || manifestless.Decl.Name != "" || len(manifestless.Decl.LoopholeSources()) != 0 {
		t.Fatalf("excluded manifest escaped filtered view: pack=%#v problems=%v err=%v", manifestless, problems, err)
	}
}

func TestSelectedPackFSFiltersManifestAndConventionsWithTheWholeView(t *testing.T) {
	files := fstest.MapFS{
		"fixture/home/.config/tool/settings": {Data: []byte("ok"), Mode: 0o444},
		"fixture/overlays/claude.json":       {Data: []byte("{}"), Mode: 0o444},
		"fixture/README.md":                  {Data: []byte("fixture description\nsecond line\n"), Mode: 0o444},
		"fixture/briefing/AGENTS.md":         {Data: []byte("reserved"), Mode: 0o444},
		"fixture/briefing/ordinary.md":       {Data: []byte("allowed"), Mode: 0o444},
	}
	selected, problems, err := ReadSelectedPackFS(files, "fixture", "/diagnostic/fixture", "fixture", []string{"home/**"}, nil)
	if err != nil {
		t.Fatalf("construct filtered view: %v", err)
	}
	if selected.Decl.Description != "" || len(selected.Decl.Contributes) != 1 || len(selected.OverlayFiles) != 0 || len(problems) != 0 {
		t.Fatalf("filter escaped into conventions: description=%q contributions=%#v overlays=%#v problems=%v", selected.Decl.Description, selected.Decl.Contributes, selected.OverlayFiles, problems)
	}
	whole, problems, err := ReadSelectedPackFS(files, "fixture", "/diagnostic/fixture", "fixture", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if whole.Decl.Description != "fixture description" || len(whole.Decl.Contributes) != 1 || len(whole.OverlayFiles) != 1 {
		t.Fatalf("unfiltered conventions=%#v overlays=%#v", whole.Decl, whole.OverlayFiles)
	}
	if len(problems) != 1 || problems[0] == "" {
		t.Fatalf("reserved briefing problem = %v, want one refusal", problems)
	}
}

func TestReadSelectedPackPathAppliesFiltersWithoutMaterializing(t *testing.T) {
	root := t.TempDir()
	packManifest := []byte(`{"name":"fixture","contributes":[{"kind":"loophole","from":"loopholes/audio"}]}`)
	moduleManifest := []byte(`{"name":"audio","version":1}`)
	for name, body := range map[string][]byte{
		"pack.json": packManifest, filepath.Join("loopholes", "audio", "manifest.jsonc"): moduleManifest,
	} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	selected, problems, err := ReadSelectedPackPath(root, "fixture", nil, []string{"loopholes/audio/manifest.jsonc"}, false)
	if err != nil || len(problems) != 0 {
		t.Fatalf("read filtered disk source: problems=%v err=%v", problems, err)
	}
	modules, refusals, warnings, err := ReadableLoopholeModules(selected)
	if err != nil || len(modules) != 0 || len(refusals) != 1 || len(warnings) != 0 {
		t.Fatalf("excluded disk module remains visible: modules=%#v refusals=%v warnings=%v err=%v", modules, refusals, warnings, err)
	}
	for name, want := range map[string][]byte{
		"pack.json": packManifest, filepath.Join("loopholes", "audio", "manifest.jsonc"): moduleManifest,
	} {
		got, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("disk source changed at %s: got %q err=%v", name, got, err)
		}
	}
}

func TestSelectedPathFSKeepsSourceAliasNames(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "actual.txt"), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("actual.txt", filepath.Join(root, "alias.txt")); err != nil {
		t.Fatal(err)
	}
	check, err := packstage.Check(packstage.Spec{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	view := newSelectedPathFS(root, check.Staged)
	entries, err := view.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range entries {
		if entry.Name() != "alias.txt" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			t.Fatal(err)
		}
		if info.Name() != "alias.txt" {
			t.Fatalf("DirEntry.Info name = %q, want selected source alias", info.Name())
		}
		file, err := view.Open("alias.txt")
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		named, ok := file.(interface{ Name() string })
		if !ok || named.Name() != "alias.txt" {
			t.Fatalf("File.Name = %v, want selected source alias", named)
		}
		stat, err := file.Stat()
		if err != nil || stat.Name() != "alias.txt" {
			t.Fatalf("File.Stat name = %v, err=%v", stat.Name(), err)
		}
		data, err := fs.ReadFile(view, "alias.txt")
		if err != nil || string(data) != "content" {
			t.Fatalf("selected alias content = %q err=%v", data, err)
		}
		found = true
	}
	if !found {
		t.Fatal("selected view omitted the symlink's source spelling")
	}
}

func TestReadSelectedPackPathOmitsFilteredConventionRoots(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "pack.json"), []byte(`{"name":"fixture"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"home", "overlays"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("not a directory"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	selected, problems, err := ReadSelectedPackPath(root, "fixture", nil, []string{"home", "overlays"}, false)
	if err != nil || len(problems) != 0 {
		t.Fatalf("excluded convention roots leaked into selected view: problems=%v err=%v", problems, err)
	}
	if len(selected.Decl.Contributes) != 0 || len(selected.OverlayFiles) != 0 {
		t.Fatalf("excluded conventions remain selected: contributions=%#v overlays=%#v", selected.Decl.Contributes, selected.OverlayFiles)
	}
}

func TestReadSelectedPackPathChecksWholeRootBeforeApplyingFilters(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("host bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "excluded-link")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadSelectedPackPath(root, "fixture", nil, []string{"excluded-link"}, false); err == nil {
		t.Fatal("whole-root source check was skipped for an excluded path")
	}
	got, err := os.ReadFile(outside)
	if err != nil || string(got) != "host bytes" {
		t.Fatalf("read-only source check changed outside bytes: %q err=%v", got, err)
	}
}

func TestReadSelectedPackFSPreservesHostHomeProblemOrderAndText(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"home", "overlays"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("not a directory"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pathPack, pathProblems := LoadDir(root, "fixture")
	files := fstest.MapFS{
		"fixture/home":     {Data: []byte("not a directory"), Mode: 0o444},
		"fixture/overlays": {Data: []byte("not a directory"), Mode: 0o444},
	}
	fsPack, fsProblems, err := ReadSelectedPackFS(files, "fixture", root, "fixture", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if pathPack == nil || fsPack == nil || !reflect.DeepEqual(pathProblems, fsProblems) {
		t.Fatalf("host/FS conventions differ: path pack=%#v problems=%v; FS pack=%#v problems=%v", pathPack, pathProblems, fsPack, fsProblems)
	}
}

func TestSelectedPackFSBindingRejectsChangedFullLoadedDeclaration(t *testing.T) {
	files := fstest.MapFS{
		"fixture/pack.json":                      {Data: []byte(`{"name":"fixture","contributes":[{"kind":"loophole","from":"loopholes/audio"}]}`), Mode: 0o444},
		"fixture/loopholes/audio/manifest.jsonc": {Data: []byte(`{"name":"audio","version":1}`), Mode: 0o444},
	}
	selected, problems, err := ReadSelectedPackFS(files, "fixture", "/diagnostic/fixture", "fixture", nil, nil)
	if err != nil || len(problems) != 0 {
		t.Fatalf("construct selected view: problems=%v err=%v", problems, err)
	}
	selected.Decl.Description = "changed outside the selected source"
	if _, _, _, err := ReadableLoopholeModules(selected); err == nil {
		t.Fatal("selected metadata was used after a non-module field of its parsed declaration changed")
	}
}

func TestSelectedPackFSBindingRejectsSelectedNameDrift(t *testing.T) {
	files := fstest.MapFS{
		"fixture/pack.json":                      {Data: []byte(`{"name":"fixture","contributes":[{"kind":"loophole","from":"loopholes/audio"}]}`), Mode: 0o444},
		"fixture/loopholes/audio/manifest.jsonc": {Data: []byte(`{"name":"audio","version":1}`), Mode: 0o444},
	}
	selected, problems, err := ReadSelectedPackFS(files, "fixture", "/diagnostic/fixture", "fixture", nil, nil)
	if err != nil || len(problems) != 0 {
		t.Fatalf("construct selected view: problems=%v err=%v", problems, err)
	}
	selected.Name = "spoofed"
	if _, _, _, err := ReadableLoopholeModules(selected); err == nil {
		t.Fatal("selected metadata was used after the effective selected name changed")
	}
}

func TestSelectedPackFSBindingSurvivesTrustedForkCopy(t *testing.T) {
	files := fstest.MapFS{
		"fixture/pack.json":                      {Data: []byte(`{"name":"fixture","contributes":[{"kind":"loophole","from":"loopholes/audio"}]}`), Mode: 0o444},
		"fixture/loopholes/audio/manifest.jsonc": {Data: []byte(`{"name":"audio","version":1}`), Mode: 0o444},
	}
	selected, problems, err := ReadSelectedPackFS(files, "fixture", "/diagnostic/fixture", "fixture", nil, nil)
	if err != nil || len(problems) != 0 {
		t.Fatalf("construct selected view: problems=%v err=%v", problems, err)
	}
	clone := copyPackDecl(selected)
	modules, refusals, warnings, err := ReadableLoopholeModules(clone)
	if err != nil || len(refusals) != 0 || len(warnings) != 0 || len(modules) != 1 {
		t.Fatalf("trusted copy lost selected metadata: modules=%#v refusals=%v warnings=%v err=%v", modules, refusals, warnings, err)
	}
	clone.Decl.Description = "untrusted follow-up mutation"
	if _, _, _, err := ReadableLoopholeModules(clone); err == nil {
		t.Fatal("trusted copy binding accepted an unrelated declaration mutation")
	}
}

func TestApplyForksDoesNotRebindUnchangedSelectedPackAfterMutation(t *testing.T) {
	files := fstest.MapFS{
		"extra/pack.json":                      {Data: []byte(`{"name":"extra","contributes":[{"kind":"loophole","from":"loopholes/audio"}]}`), Mode: 0o444},
		"extra/loopholes/audio/manifest.jsonc": {Data: []byte(`{"name":"audio","version":1}`), Mode: 0o444},
	}
	extra, problems, err := ReadSelectedPackFS(files, "extra", "/diagnostic/extra", "extra", nil, nil)
	if err != nil || len(problems) != 0 {
		t.Fatalf("construct selected extra pack: problems=%v err=%v", problems, err)
	}
	extra.Decl.Description = "mutated after source parsing"
	base := claimPack(t, "base", packdecl.Contribution{Kind: packdecl.KindProgram, Bin: "tool", Via: "npm", Package: "tool"})
	fork := claimPack(t, "fork", forkContribution("tool", "base"))
	out, err := ApplyForks([]*Pack{extra, base, fork})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := ReadableLoopholeModules(out[0]); err == nil {
		t.Fatal("ApplyForks refreshed an unchanged pack's stale source binding")
	}
}

func TestApplyForksPreservesSourceBindingForRewrittenBase(t *testing.T) {
	files := fstest.MapFS{
		"base/pack.json":                      {Data: []byte(`{"name":"base","contributes":[{"kind":"program","bin":"tool","via":"npm","package":"tool"},{"kind":"loophole","from":"loopholes/audio"}]}`), Mode: 0o444},
		"base/loopholes/audio/manifest.jsonc": {Data: []byte(`{"name":"audio","version":1}`), Mode: 0o444},
	}
	base, problems, err := ReadSelectedPackFS(files, "base", "/diagnostic/base", "base", nil, nil)
	if err != nil || len(problems) != 0 {
		t.Fatalf("construct selected base pack: problems=%v err=%v", problems, err)
	}
	fork := claimPack(t, "fork", forkContribution("tool", "base"))
	out, err := ApplyForks([]*Pack{base, fork})
	if err != nil {
		t.Fatal(err)
	}
	modules, refusals, warnings, err := ReadableLoopholeModules(out[0])
	if err != nil || len(refusals) != 0 || len(warnings) != 0 || len(modules) != 1 || modules[0].Decl.Name != "audio" {
		t.Fatalf("fork rewrite lost source-bound metadata: modules=%#v refusals=%v warnings=%v err=%v", modules, refusals, warnings, err)
	}
}

func TestLoopholeModulesFromFSReadsTheSelectedModuleWithoutMaterializing(t *testing.T) {
	decl, problems := packdecl.Decode([]byte(`{"name":"fixture","contributes":[{"kind":"loophole","from":"loopholes/audio"}]}`))
	if len(problems) != 0 {
		t.Fatalf("decode pack declaration: %v", problems)
	}
	p := &Pack{Name: "fixture", Root: "/diagnostic/fixture", Decl: decl}
	files := fstest.MapFS{
		"fixture/loopholes/audio/manifest.jsonc": &fstest.MapFile{Data: []byte(`{"name":"audio","version":1}`), Mode: fs.FileMode(0o444)},
	}
	modules, refusals, warnings := p.LoopholeModulesFromFS(files, "fixture")
	if len(refusals) != 0 || len(warnings) != 0 || len(modules) != 1 {
		t.Fatalf("modules=%#v refusals=%v warnings=%v", modules, refusals, warnings)
	}
	if modules[0].Decl == nil || modules[0].Decl.Name != "audio" || modules[0].Dir != "/diagnostic/fixture/loopholes/audio" {
		t.Fatalf("module did not retain decoded declaration and diagnostic identity: %#v", modules[0])
	}
}
