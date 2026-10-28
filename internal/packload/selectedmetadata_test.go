package packload

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
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
