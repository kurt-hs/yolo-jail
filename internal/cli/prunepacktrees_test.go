package cli

import (
	"errors"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/macosuser"
)

// `yolo prune` reaps the guest pack trees macos-user launches had to keep: the front door wires
// the sweep, and hands its trees over intact. MUTATION: delete `opts.GuestPackTrees = …` in
// pruneOptions and this fails.
func TestPruneOptionsWireTheGuestPackTreeSweep(t *testing.T) {
	if pruneOptions([]string{"prune"}).GuestPackTrees == nil {
		t.Fatal("pruneOptions wires no guest-pack-tree sweep; `yolo prune` would never reap a kept tree")
	}
	got := guestPackTreeReap(macosuser.PackTreeReap{
		Applicable: true,
		Removed: []macosuser.PackTreeReapEntry{{Path: "/s/packs/a.1", Bytes: 10},
			{Path: "/s/packs/b.2", Bytes: 5, Err: errors.New("rm failed")}},
		Kept: []macosuser.PackTreeReapEntry{{Path: "/s/packs/c.3", Reason: "a running launch's"}},
	})
	if got.Bytes != 10 || got.RemovedCount() != 1 || len(got.Removed) != 2 || got.Removed[1].Err == nil {
		t.Errorf("removed trees did not carry over: %+v", got)
	}
	if len(got.Kept) != 1 || got.Kept[0].Why != "a running launch's" || got.Kept[0].Path != "/s/packs/c.3" {
		t.Errorf("kept trees did not carry over: %+v", got.Kept)
	}
}
