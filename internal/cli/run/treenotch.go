package run

// treenotch.go is a jail launch's half of PPX-D35 (docs/reference/pi-extensions.md): a patched
// extension whose list entry reaches no jail — the owning agent pack's entry naming it is in a
// guarded posture list, which reaches the host alone — is built, copied and mounted in no jail. The
// launch's block still names it (patchedTreeLine), and the tree arm, the mount, YOLO_PATCHED_TREES,
// the delivery record and the lines of a notch that builds none all read the trees this returns, so
// none of them sees it.

import (
	goruntime "runtime"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// jailDeliveredTrees is trees without those a jail is not delivered (packload.Fork.DeliveredInJail),
// in their order.
func jailDeliveredTrees(trees []packload.Fork) []packload.Fork {
	var out []packload.Fork
	for _, f := range trees {
		if f.DeliveredInJail() {
			out = append(out, f)
		}
	}
	return out
}

// hostBuildsOwnTrees reports whether this host's own render builds patched extensions, as the host
// apply's does: a Linux host through its build jail, and a macOS host through the macos-user build
// act, for darwin (docs/reference/pi-extensions.md §11). Inside the macos-user sandbox (a darwin
// jail) nothing builds a tree, so there a tree listed for the host alone is delivered nowhere, and the
// block says so (PPX-D38). A var so a test can be either host.
var hostBuildsOwnTrees = func() bool { return hostBuildsOwnTreesOn(goruntime.GOOS, config.InJail()) }

// hostBuildsOwnTreesOn is hostBuildsOwnTrees for a host of goos, inside a jail or not.
func hostBuildsOwnTreesOn(goos string, inJail bool) bool {
	switch goos {
	case "linux":
		return true
	case "darwin":
		return !inJail
	}
	return false
}
