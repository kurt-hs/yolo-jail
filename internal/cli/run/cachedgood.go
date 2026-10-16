package run

import (
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/patchrecovery"
	"github.com/mschulkind-oss/yolo-jail/internal/richtext"
)

// CachedGoodEnv selects patched programs' and extensions' exact recorded good builds for one fresh
// jail launch.
const CachedGoodEnv = patchrecovery.UseCachedGoodEnv

// selectCachedGood validates the launch-only selector against the final selected fork and
// extension sets. It does not grant a build, host-floor, attach, or in-jail capability.
//
// THE GRAMMAR is patchrecovery's: `1`, or names matching a patched fork or a built extension by key,
// pack or name (docs/reference/patched-forks.md §8.5). A name covering nothing selected
// is a warning line. The one refusal for coverage is a value that covers nothing at all and is a
// single exact owner key (`<pack>/<name>`): what used to be the selector's only form, and a typo of
// one names a build the user asked for by its key.
func (o *Options) selectCachedGood(rt string) bool {
	selector := strings.TrimSpace(o.Getenv(CachedGoodEnv))
	if selector == "" {
		return true
	}
	if o.Update {
		o.pr(o.Stderr).print("--update cannot combine with historical cached-good recovery — drop --update for this recovery launch, or unset " + CachedGoodEnv + " and retry fresh preparation")
		return false
	}
	refuse := func(why string) bool {
		o.pr(o.Stderr).printf("[bold red]Refusing cached-good recovery: %s.[/bold red]", richtext.Escape(why))
		return false
	}
	if o.inJail() || config.InJail() {
		return refuse("it applies only to a fresh host-to-container launch")
	}
	if rt != "podman" && rt != "container" { // parity: Refused — macos-user has no capture-store bind to deliver an old build through
		return refuse("it applies only to a fresh podman or Apple Container launch")
	}
	if o.CapturesDir() == "" {
		return refuse("this launch has no host capture store to deliver from")
	}
	if why := o.roBindsUnsupported(rt); why != "" {
		return refuse("this runtime cannot mount the capture store read-only: " + why)
	}
	scope := patchrecovery.Parse(selector)
	scope.Env = CachedGoodEnv
	var subs []patchrecovery.Subject
	var forks, trees []string
	for _, pin := range o.forkPinned {
		if !pin.Fork.Patched() {
			continue
		}
		sub := patchrecovery.Subject{Key: pin.Fork.Key(), Pack: pin.Fork.Pack, Name: pin.Fork.Bin}
		subs = append(subs, sub)
		if scope.Covers(sub) {
			forks = append(forks, sub.Key)
		}
	}
	for _, f := range o.patchedTrees {
		sub := TreeSubject(f)
		subs = append(subs, sub)
		if scope.Covers(sub) {
			trees = append(trees, sub.Key)
		}
	}
	if len(forks)+len(trees) == 0 && len(scope.Names()) == 1 && strings.Contains(selector, "/") {
		return refuse(selector + " is not exactly one selected patched program or extension")
	}
	if line := patchrecovery.UnmatchedLine(CachedGoodEnv, scope.Unmatched(subs), subs); line != "" {
		o.pr(o.Stderr).print("[yellow]" + richtext.Escape(line) + "[/yellow]")
	}
	if len(forks)+len(trees) == 0 {
		return true // nothing covered: the strict launch path, as with the variable unset
	}
	o.cachedGoodOwner = strings.Join(append(append([]string(nil), forks...), trees...), ",")
	o.cachedGoodForks, o.cachedGoodTrees = forks, trees
	return true
}

// cachedGoodForkOwners is ForkBuildRequest.CachedGoodOwner: the fork keys the selector covers.
func (o *Options) cachedGoodForkOwners() string { return strings.Join(o.cachedGoodForks, ",") }
