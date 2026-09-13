//go:build !linux

package capture

import (
	"fmt"
	"os"
)

// clone_other.go is the non-Linux half of the reflink primitive: it reports that reflink is
// unavailable, so materialize falls through to hardlink and then to copy.
//
// It is a REFUSAL and not a stub-with-a-TODO, because the answer differs per platform and
// only one of them is worth building blind. macOS has `clonefile(2)`, which does the same
// job on APFS — but it takes PATHS rather than file descriptors and creates its own
// destination, so it is a different call shape rather than a different constant, and the
// backend that would use it (macos-user) is install-capture.md slice 6's, unverified on
// hardware, and the one place a capture also has to be RELOCATED before it can be
// materialized at all. Wiring a clone here that nothing can exercise would be a second
// unverified mechanism sitting under the first.
//
// A CALLER MUST STILL CHECK THE ERROR, even though this half never returns nil:
// clone_linux.go's cloneFile can succeed, and reflinkOne is compiled against whichever
// half the GOOS selects. That makes `cerr != nil` provably true under GOOS=darwin and
// only there, which is why the darwin lint pass in the Justfile's `lint` recipe drops
// SA4023 — the reasoning is written out there. Nothing here needs changing for it, and
// nothing here may be restructured to placate it.

// errCloneUnsupported reports that this platform, filesystem or mount pair cannot reflink.
var errCloneUnsupported = fmt.Errorf("reflink is not supported here")

// cloneFile always fails with errCloneUnsupported on this platform. See the file comment.
func cloneFile(dst, src *os.File) error {
	return fmt.Errorf("%w: no reflink call is wired for this platform", errCloneUnsupported)
}

// fsName cannot name a filesystem on this platform, and says so by answering "". The copy
// fallback's message degrades to "reflink is unavailable" without the filesystem clause.
func fsName(path string) string { return "" }
