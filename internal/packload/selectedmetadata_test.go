package packload

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
)

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
