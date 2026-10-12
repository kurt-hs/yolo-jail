package cli

// captureproviderkeys.go keeps the PROVIDER KEYS out of every capture of in-jail edits — `yolo
// config capture` and the fold at a jail's teardown, both through captureSurfaceAt — by the
// maintainer's 2026-10-11 ruling PP-D15 (docs/design/providers-and-profiles-redesign.md): "we
// should in fact overwrite that. That's just the way precedence works… the profile should [win]
// … you can set your profile if you want and restarting the jail shouldn't change that". A
// provider key (packdecl.ProviderKey, declared by the agent's own pack) behaves like a managed
// key: the boot re-asserts the selection at every render (internal/entrypoint's providerkeys.go,
// which drops a captured value), and a capture never records an edit at one in the first place.
//
// WHAT IS NOT BLOCKED. Claude Code rereads its settings file while it runs, so an in-jail edit at
// a provider key takes effect at once and lasts the session; nothing here intercepts it. It lasts
// only until the next boot, which renders the selection's value and names the edit it refused.
//
// ONLY THE UNCONDITIONAL KEYS: a value-conditional key (`matches`, the Bedrock-only model ids) is
// a provider key only while the selection is off its platform, and the capture does not read
// the selection, so it records those and the next boot decides them against the selection, as it
// always has. The keys come from the shipped packs (packload.Embedded): the capture runs on the
// host at teardown as well as in the jail, and reads no selection; a key a configured pack alone
// declares is still dropped by the boot.

import (
	"fmt"

	"github.com/mschulkind-oss/yolo-jail/internal/agentcfg"
	"github.com/mschulkind-oss/yolo-jail/internal/agentcfg/manifest"
	"github.com/mschulkind-oss/yolo-jail/internal/capturelog"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonptr"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"strings"
)

// captureProviderKeys is every unconditional provider key a shipped pack declares on s.
func captureProviderKeys(s manifest.Surface) []agentcfg.OwnedLeaf {
	var out []agentcfg.OwnedLeaf
	seen := map[string]bool{}
	for _, p := range packload.Embedded() {
		if p == nil || p.Decl == nil {
			continue
		}
		for _, k := range p.Decl.ProviderKeysFor(s.Agent + "/" + s.Name) {
			if k.Matches != "" || seen[k.Pointer] {
				continue
			}
			seen[k.Pointer] = true
			out = append(out, agentcfg.OwnedLeaf{Pointer: k.Pointer})
		}
	}
	return out
}

// providerKeyLabel spells a pointer dotted, as the capture log spells a leaf.
func providerKeyLabel(pointer string) string {
	steps, err := jsonptr.Parse(pointer)
	if err != nil || len(steps) == 0 {
		return pointer
	}
	return strings.Join(steps, ".")
}

// noteCaptureProviderKeyDrops hands note one line per provider key the capture refused: the key,
// its value when not credential-shaped, and the way to set it.
func noteCaptureProviderKeyDrops(note func(string), s manifest.Surface, drops []agentcfg.OwnedDrop) {
	if note == nil {
		return
	}
	for _, d := range drops {
		key := providerKeyLabel(d.Pointer)
		note(fmt.Sprintf("%s/%s: not captured: %s = %s in %s is a provider key, which %s's "+
			"profile selection decides; set it through the profile (yolo config-ref: profile/providers). "+
			"The edit lasts only until the next boot, which names it again.", s.Agent, s.Name, key,
			capturelog.Value(key, d.Value), s.Path, s.Agent))
	}
}
