package packload

// skipextensions_test.go pins ApplyTreeLists' skip (YOLO_SKIP_EXTENSIONS,
// docs/reference/pi-extensions.md §9.1): a skipped tree's entries — its own and any path
// inside it — leave the contributing pack's config-lists and posture lists, it takes no fallback,
// another pack's identical entry stays, and the packs handed in are never written.

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
)

func TestASkippedTreeLeavesItsOwnPacksListsAndTakesNoFallback(t *testing.T) {
	const tree = "~/.pi/agent/yolo-ext/web/node_modules/pi-web-access"
	surfaceOwner := &Pack{Name: "pi", Decl: &packdecl.Manifest{Contributes: []packdecl.Contribution{
		{Kind: packdecl.KindConfig, Raw: json.RawMessage(`[{"agent":"pi","name":"settings","codec":"json",` +
			`"path":"~/.pi/agent/settings.json"}]`)},
	}}}
	matt := &Pack{Name: "matt", Decl: &packdecl.Manifest{Contributes: []packdecl.Contribution{
		{Kind: packdecl.KindFiles, Into: ".pi/agent/yolo-ext/web", Source: "npm:pi-web-access", Fallback: "npm:pi-web-access"},
		{Kind: packdecl.KindFiles, Into: ".pi/agent/yolo-ext/goal", Source: "git+https://h/o/goal?ref=main"},
		{Kind: packdecl.KindConfigList, Surface: "pi/settings", Path: "/packages",
			Add: rawList(t, "npm:a", tree, tree+"/index.ts", 7, "~/.pi/agent/yolo-ext/goal")},
		{Kind: packdecl.KindAutonomy, Autonomous: &packdecl.AutonomyPosture{Lists: []packdecl.PostureList{
			{Surface: "pi/settings", Path: "/packages", Add: rawList(t, tree, "npm:b")},
		}}},
	}}}
	other := &Pack{Name: "other", Decl: &packdecl.Manifest{Contributes: []packdecl.Contribution{
		{Kind: packdecl.KindConfigList, Surface: "pi/settings", Path: "/packages", Add: rawList(t, tree)},
	}}}
	packs := []*Pack{surfaceOwner, matt, other}
	before := string(matt.Decl.Contributes[2].Add)

	out, taken, skipped := ApplyTreeLists(packs, func(Fork) bool { return false },
		func(f Fork) bool { return f.Key() == "matt/web" })
	if len(taken) != 0 {
		t.Errorf("a skipped tree took its fallback: %+v", taken)
	}
	if len(skipped) != 1 || skipped[0].Key() != "matt/web" {
		t.Fatalf("skipped = %+v, want matt/web", skipped)
	}
	if got := listOf(t, out[1].Decl.Contributes[2].Add); !reflect.DeepEqual(got,
		[]any{"npm:a", float64(7), "~/.pi/agent/yolo-ext/goal"}) {
		t.Errorf("the config-list = %v, want every entry loading the tree gone and the rest as written", got)
	}
	if got := listOf(t, out[1].Decl.Contributes[3].Autonomous.Lists[0].Add); !reflect.DeepEqual(got, []any{"npm:b"}) {
		t.Errorf("the autonomous posture list = %v, want the tree's entry gone", got)
	}
	if out[2] != other {
		t.Error("another pack's identical entry was touched")
	}
	if string(matt.Decl.Contributes[2].Add) != before {
		t.Error("the packs handed in were written")
	}
}
