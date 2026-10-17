package prune

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/outfmt"
)

// The macos-user guest-pack-tree sweep is WIRED: the seam is called with --apply's value, its
// trees are in the report, the total and the JSON document, a declined sweep fails the command,
// and a machine with no packs directory prints nothing.
func TestGuestPackTreeSectionIsWired(t *testing.T) {
	o, _ := baseOpts(t)
	var asked []bool
	o.GuestPackTrees = func(apply bool) (EmbeddedReap, bool) {
		asked = append(asked, apply)
		return EmbeddedReap{
			Removed: []EmbeddedEntry{{Name: "/Users/Shared/yolo-staged/packs/yolo-a.1", Bytes: 4096}},
			Kept:    []EmbeddedEntry{{Name: "/Users/Shared/yolo-staged/packs/yolo-b.2", Why: "a running launch's"}},
			Bytes:   4096,
		}, true
	}
	var buf bytes.Buffer
	o.Out = &buf
	o.Apply = true
	if rc := Run(o); rc != 0 {
		t.Fatalf("rc=%d\n%s", rc, buf.String())
	}
	if len(asked) != 1 || !asked[0] {
		t.Errorf("the sweep was asked %v, want once with apply", asked)
	}
	for _, want := range []string{"macos-user guest pack trees", "removed: ", "yolo-a.1", "a running launch's"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("the report does not say %q:\n%s", want, buf.String())
		}
	}

	buf.Reset()
	o.Apply = false
	o.Format = outfmt.JSON
	if rc := Run(o); rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	var rep Report
	if err := json.Unmarshal(buf.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range rep.Categories {
		if c.Name == "macos_guest_pack_trees" {
			found = c.Count == 1 && c.Bytes == 4096
		}
	}
	if !found || rep.TotalBytes < 4096 {
		t.Errorf("the JSON report does not carry the sweep: %+v", rep)
	}

	o.Format = ""
	o.GuestPackTrees = func(bool) (EmbeddedReap, bool) {
		return EmbeddedReap{Declined: errors.New("no table").Error()}, true
	}
	buf.Reset()
	if rc := Run(o); rc != 1 {
		t.Errorf("a declined sweep returned %d, want 1", rc)
	}

	o.GuestPackTrees = func(bool) (EmbeddedReap, bool) { return EmbeddedReap{}, false }
	buf.Reset()
	if rc := Run(o); rc != 0 || strings.Contains(buf.String(), "guest pack trees") {
		t.Errorf("a machine with no packs directory printed the section (rc %d):\n%s", rc, buf.String())
	}
}
