package entrypoint

// ctxlink.go lays the agent's `~/ctx` link (OQ-MM6, docs/reference/context-mounts.md#the-context-dir):
// a symbolic link from the home directory to $YOLO_CONTEXT_DIR, so the context mounts are
// browsable from home on every backend. It names what is already there and grants nothing:
// the link is a name, and the mounts and their access are decided elsewhere.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// ContextLinkOwnerEnv names the directory every workspace's staged context tree sits in, on
// macos-user (macosuser.StagedCtxParent). The sandbox home is one machine-wide home but the
// context dir is per workspace, so a ~/ctx link into this directory is yolo's own, left by an
// earlier launch, and is retargeted. Set by the macos-user bootstrap only; the container
// backends leave it unset, so nothing there is ever yolo's by this test.
const ContextLinkOwnerEnv = "YOLO_CTX_STAGED_PARENT"

// LinkContextDir makes $HOME/ctx a symbolic link to the value of $YOLO_CONTEXT_DIR, when the
// launch names one. A link already pointing there is left as it is. A link into yolo's own
// staged context tree (ContextLinkOwnerEnv) is yolo's, so it is replaced: only the link is
// removed. Anything else at that path (a file, a directory, a link anywhere else) is never
// removed or overwritten: the path is left alone and one line says so.
func LinkContextDir(e *Env) error {
	target := e.Getenv(paths.ContextDirEnv)
	if target == "" {
		return nil
	}
	p := filepath.Join(e.Home, paths.ContextLinkName)
	cur, rerr := os.Readlink(p)
	if rerr == nil && cur == target {
		return nil
	}
	if rerr == nil && e.ownsContextLink(cur) {
		if err := os.Remove(p); err != nil {
			e.warn(fmt.Sprintf("yolo: could not retarget ~/%s from an earlier workspace: %v; leaving it.",
				paths.ContextLinkName, err))
			return nil
		}
	} else if _, lerr := os.Lstat(p); lerr == nil {
		e.warn(fmt.Sprintf("yolo: ~/%s already exists and is not yolo's link to $%s (%s); leaving it.",
			paths.ContextLinkName, paths.ContextDirEnv, target))
		return nil
	}
	// Not fatal: the link is a convenience, and a boot that cannot make it must still start.
	// The mounts are reachable at $YOLO_CONTEXT_DIR either way.
	if err := os.Symlink(target, p); err != nil {
		e.warn(fmt.Sprintf("yolo: could not link ~/%s to $%s (%s): %v; the mounts stay at $%s.",
			paths.ContextLinkName, paths.ContextDirEnv, target, err, paths.ContextDirEnv))
	}
	return nil
}

// ownsContextLink reports whether the link target cur lies inside yolo's staged context
// parent. An unset parent owns nothing.
func (e *Env) ownsContextLink(cur string) bool {
	parent := e.Getenv(ContextLinkOwnerEnv)
	if parent == "" {
		return false
	}
	return strings.HasPrefix(filepath.Clean(cur), filepath.Clean(parent)+string(filepath.Separator))
}
