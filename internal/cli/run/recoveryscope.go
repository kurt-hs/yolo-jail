package run

// recoveryscope.go is WHAT A SET RECOVERY HATCH REACHES at a jail launch (docs/design/
// patched-build-recovery.md §6.1, PBR-D3): said once per launch, never a refusal.
//
//   - A FRESH LAUNCH on a container runtime warns, for each scoped hatch (patchrecovery.Scoped) that
//     is set (but the two below), of the names that cover none of the patched builds it selects — its patched forks and
//     its built extensions — in patchrecovery.UnmatchedLine's one shape. A hatch left in a shell
//     profile must not stop a workspace that does not select the pack it names.
//   - AN ATTACH, A LAUNCH IN A JAIL, OR macos-user builds and delivers no patched build, so a set
//     hatch does nothing there and the launch says so in one line per hatch, except
//     patchrecovery.UseCachedGoodEnv, which keeps its own refusals (cachedgood.go), and
//     patchrecovery.SkipExtensionsEnv, whose line the extension delivery says.

import (
	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/patchrecovery"
	"github.com/mschulkind-oss/yolo-jail/internal/richtext"
)

// recoverySubjects are the patched builds this launch selects, as a hatch's names are matched.
func (o *Options) recoverySubjects() []patchrecovery.Subject {
	var subs []patchrecovery.Subject
	for _, p := range o.forkPinned {
		if p.Fork.Patched() {
			subs = append(subs, patchrecovery.Subject{Key: p.Fork.Key(), Pack: p.Fork.Pack, Name: p.Fork.Bin})
		}
	}
	for _, f := range o.patchedTrees {
		subs = append(subs, patchrecovery.Subject{Key: f.Key(), Pack: f.Pack, Name: f.Bin})
	}
	return subs
}

// noteRecoveryHatches says, once per launch, what each set recovery hatch reaches here. attach is
// an attach to a running jail.
func (o *Options) noteRecoveryHatches(rt string, attach bool) {
	if o.recoveryHatchesNoted {
		return
	}
	o.recoveryHatchesNoted = true
	out := o.pr(o.Stderr)
	where := ""
	switch {
	case attach:
		where = "on an attach; it applies at the next fresh launch"
	case config.InJail():
		where = "in a jail"
	case rt == "macos-user": // parity: Warned — macos-user delivers no patched build, so a set recovery hatch is said to do nothing there
		where = "on macos-user"
	}
	subs := o.recoverySubjects()
	for _, env := range noterHatches() {
		s := patchrecovery.Read(o.Getenv, env)
		if !s.Set() {
			continue
		}
		if where == "" {
			if line := patchrecovery.UnmatchedLine(env, s.Unmatched(subs), subs); line != "" {
				out.print("[yellow]" + richtext.Escape(line) + "[/yellow]")
			}
			continue
		}
		out.print("[yellow]" + richtext.Escape(env+" has no effect "+where) + "[/yellow]")
	}
}

// noterHatches are the scoped hatches this file speaks for: every one but
// patchrecovery.UseCachedGoodEnv (selectCachedGood says its own) and patchrecovery.SkipExtensionsEnv
// (skipextensions.go says its own).
func noterHatches() []string {
	var out []string
	for _, env := range patchrecovery.Scoped {
		if env != patchrecovery.UseCachedGoodEnv && env != patchrecovery.SkipExtensionsEnv {
			out = append(out, env)
		}
	}
	return out
}

// NoterHatches is noterHatches for `yolo host`'s half (internal/cli's hostrecoveryscope.go).
func NoterHatches() []string { return noterHatches() }
