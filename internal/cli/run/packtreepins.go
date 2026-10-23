package run

// packtreepins.go is the launch's half of a PACK-TREE PROGRAM (docs/design/pack-buildable-programs.md,
// a `program` delivered `via: "pack"`): each one this launch's packs carry, keyed on the tree
// digest of its source directory in the launch's OWN staged pack tree, which nothing edits after
// staging (§6.1). The digest stands where a fork's pinned commit stands (packload.ForkPin.Commit),
// so the fork route's trigger, build slot and delivery (forkbuild.go) carry the program unchanged:
// a store entry for this digest is a hit and prints nothing, and anything else is built once.

import (
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// packTreePins are this launch's pack-tree programs with the digest each is built at, or why it has
// none. Like noteForkPins, nothing is decided inside a capture or build jail (CapturesDir ""),
// whose own launch never builds.
//
// The digest reads the source directory whole on every fresh launch: the same order as the staging
// copy every launch already makes of the pack (the design's §6.1 warning), and a hit costs nothing
// more.
func (o *Options) packTreePins(packs []*packload.Pack) []packload.ForkPin {
	if o.CapturesDir() == "" {
		return nil
	}
	var out []packload.ForkPin
	for _, f := range packload.PackTreePrograms(packs) {
		p := packload.ForkPin{Fork: f}
		digest, err := f.Digest()
		switch {
		case err != nil:
			p.Reason = "pack " + f.Pack + "'s source directory " + f.From + " could not be read (" + err.Error() +
				") — fix the pack's tree and launch again"
		case digest == "":
			p.Reason = "pack " + f.Pack + " ships no " + f.From + " to build " + f.Bin + " from — add that " +
				"directory to the pack, or drop the program from its manifest"
		default:
			p.Commit = digest
		}
		out = append(out, p)
	}
	return out
}
