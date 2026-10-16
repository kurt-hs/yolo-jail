package run

// skipextensions.go is a jail launch's half of YOLO_SKIP_EXTENSIONS
// (patchrecovery.SkipExtensionsEnv; docs/design/patched-build-recovery.md §6.6): a built extension
// it covers — patched or unmodified, by key, pack or extension name — is not advanced, built, copied
// or mounted this launch; the wire marks it skipped, so the jail's boot leaves every entry loading
// it out of the owning agent's lists and stops no launcher for it; and the missing-build refusal
// passes over it (TreeDelivery.Skipped). Nothing is persisted: the next launch without the variable
// delivers it as before.
//
// Only a fresh container launch acts on it. An attach keeps what its jail booted with, and a launch
// in a jail or on macos-user delivers no tree: each says so in one line.

import (
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/patchrecovery"
	"github.com/mschulkind-oss/yolo-jail/internal/richtext"
)

// skipExtensionsScope is this launch's YOLO_SKIP_EXTENSIONS.
func (o *Options) skipExtensionsScope() patchrecovery.Scope {
	return patchrecovery.Read(o.Getenv, patchrecovery.SkipExtensionsEnv)
}

// TreeSubject is f as a hatch name is matched against it.
func TreeSubject(f packload.Fork) patchrecovery.Subject {
	return patchrecovery.Subject{Key: f.Key(), Pack: f.Pack, Name: f.Bin}
}

// SkippedLine is the one disclosure line for a skipped extension.
func SkippedLine(f packload.Fork) string {
	line := f.Label() + ": skipped (" + patchrecovery.SkipExtensionsEnv + ")"
	if f.Owner != "" {
		line += "; " + f.Owner + " starts without it"
	}
	return line
}

// skipExtensions answers in out every extension of this launch the hatch covers, says each, warns
// once about a name that covers none, and returns the rest, in order, for the tree arm.
func (o *Options) skipExtensions(out map[string]TreeDelivery) []packload.Fork {
	scope := o.skipExtensionsScope()
	if !scope.Set() {
		return o.patchedTrees
	}
	subs := make([]patchrecovery.Subject, 0, len(o.patchedTrees))
	var rest []packload.Fork
	pr := o.pr(o.Stderr)
	for _, f := range o.patchedTrees {
		sub := TreeSubject(f)
		subs = append(subs, sub)
		if !scope.Covers(sub) {
			rest = append(rest, f)
			continue
		}
		out[f.Key()] = TreeDelivery{Skipped: true, Reason: "skipped (" + patchrecovery.SkipExtensionsEnv + ")"}
		pr.print("[yellow]" + richtext.Escape(SkippedLine(f)) + "[/yellow]")
	}
	if line := patchrecovery.UnmatchedLine(scope.Env, scope.Unmatched(subs), subs); line != "" {
		pr.print("[yellow]" + richtext.Escape(line) + "[/yellow]")
	}
	return rest
}

// noteSkipExtensionsHasNoEffect is the one line where the hatch is set and this launch (where)
// delivers no tree to skip.
func (o *Options) noteSkipExtensionsHasNoEffect(where string) {
	if o.skipExtensionsScope().Set() {
		o.pr(o.Stderr).print("[dim]" + richtext.Escape(patchrecovery.SkipExtensionsEnv+": no effect "+where) + "[/dim]")
	}
}

// noteAttachSkipExtensions is an attach's one line where the hatch is set: the running jail keeps
// what it booted with.
func (o *Options) noteAttachSkipExtensions() {
	if o.skipExtensionsScope().Set() {
		o.pr(o.Stderr).print("[dim]" + richtext.Escape(patchrecovery.SkipExtensionsEnv+
			": this jail keeps its extensions; it applies at the next fresh launch") + "[/dim]")
	}
}
