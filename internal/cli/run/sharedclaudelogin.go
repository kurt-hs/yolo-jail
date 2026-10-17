package run

import (
	"fmt"
	"path/filepath"

	"github.com/mschulkind-oss/yolo-jail/internal/claudeview"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/oauthbroker"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/richtext"
)

// noteBrokenSharedClaudeLogin is a container launch's one notice about the machine's shared
// Claude login: when the file every Claude jail reads its login from (CL-D22's bridge,
// claudesecurestorage.go) holds bytes that do not parse, Claude in this jail starts signed out
// and nothing else says why. The launch names the file and the next step, and changes nothing.
//
// ONLY A BROKEN FILE. An empty one is the placeholder every machine has before its first /login
// (storage.ensureSharedCredentials), and Claude asks for that login itself; a Bedrock or API-key
// Claude never fills it. `yolo check` says it is empty, as a passing row.
//
// ONLY WHERE THIS FILE IS THE JAIL'S: the two container backends bind it from the machine store.
// A macos-user session's shared directory is the sandbox account's own, so this file is not what
// its Claude reads.
func (o *Options) noteBrokenSharedClaudeLogin(rt string, cfg *jsonx.OrderedMap, packs []*packload.Pack) {
	if rt != "podman" && rt != "container" {
		return
	}
	dir := o.claudeSecureStorageDir(rt, cfg, packs, paths.GlobalHome())
	if dir == "" {
		return
	}
	file := filepath.Join(filepath.FromSlash(dir), claudeview.ViewFile)
	cond, size := oauthbroker.SharedFileStatus(file)
	if cond != oauthbroker.SharedFileBroken {
		return
	}
	out := o.pr(o.Stderr)
	out.print("[yellow]Claude's shared login file does not parse, so Claude in this jail starts " +
		"signed out.[/yellow]")
	out.print("")
	out.print(fmt.Sprintf("  %s (%d bytes)", richtext.Escape(file), size))
	out.print("")
	out.print("  Run /login inside Claude in this jail. It writes a new login to that file, " +
		"which every Claude jail on this machine reads.")
}
