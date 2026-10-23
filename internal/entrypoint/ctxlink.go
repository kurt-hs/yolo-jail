package entrypoint

// ctxlink.go lays the agent's `~/ctx` link (OQ-MM6, docs/reference/context-mounts.md#the-context-dir):
// a symbolic link from the home directory to $YOLO_CONTEXT_DIR, so the context mounts are
// browsable from home on every backend. It names what is already there and grants nothing:
// the link is a name, and the mounts and their access are decided elsewhere.

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// LinkContextDir makes $HOME/ctx a symbolic link to the value of $YOLO_CONTEXT_DIR, when the
// launch names one. A link already pointing there is left as it is. Anything else at that
// path (a file, a directory, a link to somewhere else) is never removed or overwritten: the
// path is left alone and one line says so.
func LinkContextDir(e *Env) error {
	target := e.Getenv(paths.ContextDirEnv)
	if target == "" {
		return nil
	}
	p := filepath.Join(e.Home, paths.ContextLinkName)
	if cur, err := os.Readlink(p); err == nil && cur == target {
		return nil
	}
	if _, lerr := os.Lstat(p); lerr == nil {
		e.warn(fmt.Sprintf("yolo: ~/%s already exists and is not yolo's link to $%s (%s); leaving it.",
			paths.ContextLinkName, paths.ContextDirEnv, target))
		return nil
	}
	// Not fatal: the link is a convenience, and a boot that cannot make it (the container's
	// home root is a read-only skeleton, OQ-MM6) must still start. The mounts are reachable at
	// $YOLO_CONTEXT_DIR either way.
	if err := os.Symlink(target, p); err != nil {
		e.warn(fmt.Sprintf("yolo: could not link ~/%s to $%s (%s): %v; the mounts stay at $%s.",
			paths.ContextLinkName, paths.ContextDirEnv, target, err, paths.ContextDirEnv))
	}
	return nil
}
