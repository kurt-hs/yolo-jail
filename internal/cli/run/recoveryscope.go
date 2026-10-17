package run

// recoveryscope.go is WHAT A SET RECOVERY HATCH REACHES at a jail launch
// (docs/reference/patched-forks.md §8.5, PBR-D3): said once per launch, never a refusal.
//
//   - A FRESH LAUNCH on a container runtime warns, for each scoped hatch (patchrecovery.Scoped) that
//     is set (but the two below), of the names that cover none of the patched builds it selects — its patched forks and
//     its built extensions — in patchrecovery.UnmatchedLine's one shape. A hatch left in a shell
//     profile must not stop a workspace that does not select the pack it names.
//   - A FRESH macos-user LAUNCH builds its patched forks for darwin and no patched extension
//     (docs/reference/patched-forks.md PF-D97), so a hatch is matched against its forks alone, and a
//     launch that selects a patched extension is told the hatch does nothing for it.
//   - AN ATTACH, A LAUNCH IN A JAIL, OR A macos-user DRY RUN builds and delivers no patched build,
//     so a set hatch does nothing there and the launch says so in one line per hatch, except
//     patchrecovery.UseCachedGoodEnv, which keeps its own refusals (cachedgood.go), and
//     patchrecovery.SkipExtensionsEnv, whose line the extension delivery says.

import (
	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/patchrecovery"
	"github.com/mschulkind-oss/yolo-jail/internal/richtext"
)

// recoverySubjects are the patched builds this launch selects, as a hatch's names are matched.
func (o *Options) recoverySubjects() []patchrecovery.Subject {
	subs := o.forkRecoverySubjects()
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
	macosUser := rt == "macos-user" // parity: Warned — macos-user builds patched forks but no patched extension, so a hatch is matched against its forks alone and said to do nothing for its extensions
	switch {
	case attach:
		where = "on an attach; it applies at the next fresh launch"
	case config.InJail():
		where = "in a jail"
	case macosUser && o.Sealed:
		where = "on macos-user" // a fork's own build launch, which selects no patched build of its own
	case macosUser && o.DryRun:
		where = "on a dry run, which builds nothing; it applies when you launch without --dry-run"
	}
	subs := o.recoverySubjects()
	if macosUser {
		subs = o.forkRecoverySubjects()
	}
	for _, env := range noterHatches() {
		s := patchrecovery.Read(o.Getenv, env)
		if !s.Set() {
			continue
		}
		if where != "" {
			out.print("[yellow]" + richtext.Escape(env+" has no effect "+where) + "[/yellow]")
			continue
		}
		if line := patchrecovery.UnmatchedLine(env, s.Unmatched(subs), subs); line != "" {
			out.print("[yellow]" + richtext.Escape(line) + "[/yellow]")
		}
		if macosUser && len(o.patchedTrees) > 0 {
			out.print("[yellow]" + richtext.Escape(env+" has no effect on a patched extension on macos-user, "+
				"which builds none; run it on a container backend with `YOLO_RUNTIME=podman yolo`"+
				o.containerStepClause()) + "[/yellow]")
		}
	}
}

// forkRecoverySubjects are the patched forks this launch selects, recoverySubjects' first half: what
// a hatch reaches on macos-user, which builds forks and no extension (PF-D97).
func (o *Options) forkRecoverySubjects() []patchrecovery.Subject {
	var subs []patchrecovery.Subject
	for _, p := range o.forkPinned {
		if p.Fork.Patched() {
			subs = append(subs, patchrecovery.Subject{Key: p.Fork.Key(), Pack: p.Fork.Pack, Name: p.Fork.Bin})
		}
	}
	return subs
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
