package hostfloor

// packtree.go is the floor's arm for a PACK-TREE PROGRAM (docs/design/pack-buildable-programs.md, a
// `program` delivered `via: "pack"`): its host copy is the capture store's build of the pack's own
// source directory — or, where the pack ships no source or the user opted into it (BG-D18), its
// verified prebuilt artifact — relocated
// into the floor exactly as a fork's build is (FP-D16; §9's `yolo host` rows: Linux, and macOS,
// whose build is the macos-user sealed act and whose blockers are that act's, BuildActUnavailable).
//
// # What is a fork's, and what is not
//
// The store entry, the confined relocating materialize (materializeBuild), a not-relocatable build
// refused rather than rebuilt (buildUnusableAs), and a Node script started by the floor's own Node
// (execRecord) are the fork arm's, unchanged. What differs:
//
//   - NO PIN. The build's revision is the source directory's tree digest, or the artifact's sha256,
//     which the caller's PackTree resolves from the pack staged as a launch stages it, so the floor
//     and a jail launch on this machine name one build (§6.1). Nothing here reads, makes or asks for a
//     fork pin: ForkPin, ForkPinnable and PinFork are never called for one.
//   - RESOLVED ONLY BY AN INSTALL. The resolution digests the source directory, which §10 forbids on
//     `yolo check`, a dry run or a timer, so Status never asks it: Ensure resolves once per Floor
//     (resolvePackTree) and every status after reads that answer, as a fork's reads the pin its
//     install made (madePins). A status with no resolution knows only the declaration.
//   - NEVER SERVED PAST ITS KEY (§10, P5): an installed copy of another digest, recipe or artifact is
//     a near-miss, so a failed reinstall takes it out of bin/ as a fork's moved pin does.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
)

// PackTreeDelivery is one pack-tree program's resolved delivery for this floor (Floor.PackTree): which
// bytes serve it, under which key, and the two acts that find or make them. Reason set means there are
// none, and says why with the next step.
type PackTreeDelivery struct {
	// Revision and Recipe are the build's key beside its source: the tree digest (or the artifact's
	// sha256) and the recipe hash. A record of another pair is a near-miss.
	Revision, Recipe string
	// What names the bytes, for a line: "pack fx's tools/x at tree digest 0123456789ab".
	What string
	// Version is the Record.Version an install of it writes.
	Version string
	// Jail reports whether making it boots a build jail: a build line, not a copy or a prebuilt.
	Jail bool
	// Disclosure is the "which bytes" line a launch prints on every launch while it serves (a prebuilt
	// artifact's, run.PrebuiltLine), "" when there is none to print.
	Disclosure string
	// Reason is why this program has no delivery here, "" when it has one.
	Reason string
	// PrebuiltOptInStep is the clause a refusal of a build that cannot run here adds: the explicit
	// opt-in to the pack's prebuilt artifact for this platform (packdecl.PrebuiltOptInStep, OQ-BG2),
	// "" when the pack declares none. Never a substitution: the floor refuses either way.
	PrebuiltOptInStep string
	// Hit is the store's entry under this key, the hit a jail launch asks for; an error when there is
	// none.
	Hit func() (*capture.Entry, error)
	// Build runs the act that admits it — the sealed build, the host's copy of a tree with no build
	// line, or the verified fetch — waiting, bounded, for the same key another process is making, and
	// returns the entry it admitted (never a second lookup: selection is newest-wins on a one-second
	// stamp). Its error names the build's log and the retry.
	Build func() (*capture.Entry, error)
}

// cannotBuildReason is the refusal of a build of bin that cannot run on this machine (why, with its
// setup step), and the prebuilt opt-in when the pack declares one here.
func (d PackTreeDelivery) cannotBuildReason(bin, why string) string {
	r := "there is no build of " + bin + " from " + d.What + " on this machine, and " + why
	if d.PrebuiltOptInStep != "" {
		r += "; " + d.PrebuiltOptInStep
	}
	return r
}

// resolvePackTree is PackTree's answer for p, asked once per Floor and kept for every later status.
// Only Ensure calls it (the resolution digests the source directory).
func (f *Floor) resolvePackTree(p Program) PackTreeDelivery {
	if d, ok := f.packTrees[p.Bin()]; ok {
		return d
	}
	var d PackTreeDelivery
	if f.PackTree == nil {
		d.Reason = "this yolo builds no program from a pack's own tree for its host floor"
	} else {
		d = f.PackTree(p)
	}
	if f.packTrees == nil {
		f.packTrees = map[string]PackTreeDelivery{}
	}
	f.packTrees[p.Bin()] = d
	return d
}

// resolvedPackTree is the resolution an install already made of p on this Floor, if any.
func (f *Floor) resolvedPackTree(p Program) (PackTreeDelivery, bool) {
	d, ok := f.packTrees[p.Bin()]
	return d, ok
}

// packTreeNoEntry is why the floor holds no pack-tree program on a platform it does not build one for:
// the floor builds one on Linux (a sealed build jail) and on a Mac (the macos-user sealed act, the
// plan's Task 10), and on no other platform. The next step is a jail's, whose launch builds it, or the
// floor's `host_floor`.
func packTreeNoEntry(p Program, goos string) string {
	return "it is built from pack " + p.Pack + "'s own tree (" + p.Install.PackFrom + "), which the host " +
		"floor builds on Linux and macOS, and this machine is " + goos + " — run it in a jail (`yolo -- " + p.Bin() + "`), whose " +
		"launch builds it, or leave pack " + p.Pack + " out of the floor with the user config's `host_floor`"
}

// packTreeRecipeNoEntry is recipeNoEntryReason's pack-tree arm: no entry on a platform the floor builds
// none for, on a floor given no resolver, or where the install's own resolution found none.
func (f *Floor) packTreeRecipeNoEntry(p Program) string {
	if f.GOOS != "linux" && f.GOOS != "darwin" {
		return packTreeNoEntry(p, f.GOOS)
	}
	if f.PackTree == nil {
		return "it is built from pack " + p.Pack + "'s own tree (" + p.Install.PackFrom + "), and this floor " +
			"builds none — run it in a jail (`yolo -- " + p.Bin() + "`), or leave pack " + p.Pack + " out of " +
			"the floor with the user config's `host_floor`"
	}
	if d, ok := f.resolvedPackTree(p); ok && d.Reason != "" {
		return d.Reason
	}
	return ""
}

// packTreeSourcePresent reports whether p's source directory is in its pack as loaded, by a stat and
// never a digest; ok is false when the loader named no root to look under.
func packTreeSourcePresent(in packdecl.Install) (present, ok bool) {
	if in.PackRoot == "" {
		return false, false
	}
	fi, err := os.Stat(filepath.Join(in.PackRoot, filepath.FromSlash(in.PackFrom)))
	return err == nil && fi.IsDir(), true
}

// packTreeNoSourceReason is the §4 refusal for a pack that ships no source directory and no prebuilt
// for this platform, read off the declaration alone: the pack, `from`, the platform, and what
// `prebuilt` does declare.
func (f *Floor) packTreeNoSourceReason(p Program) string {
	in := p.Install
	platform := f.GOOS + "/" + f.GOARCH
	if _, ok := in.Prebuilt[platform]; ok {
		return ""
	}
	var have []string
	for k := range in.Prebuilt {
		have = append(have, k)
	}
	sort.Strings(have)
	declared := "none"
	if len(have) > 0 {
		declared = strings.Join(have, ", ")
	}
	return "pack " + p.Pack + " ships no " + in.PackFrom + " to build " + p.Bin() + " from, and its `prebuilt` " +
		"declares no artifact for " + platform + " (it declares: " + declared + ") — add that directory to the " +
		"pack, or a `prebuilt` entry for " + platform
}

// packTreeProvisionable is provisionable's pack-tree arm. With the install's resolution in hand it is
// exact: the store's entry under the key is judged as the floor's copy, and with none, a build that
// needs a jail on a machine that cannot boot one is no entry. Without it (a status no install
// preceded: `yolo check`, a dry run) only the declaration is read: a pack that ships no source and no
// artifact for this platform is no entry, and anything else is left to the install, which digests.
func (f *Floor) packTreeProvisionable(st Status) Status {
	p := st.Program
	d, ok := f.resolvedPackTree(p)
	if !ok {
		if present, known := packTreeSourcePresent(p.Install); known && !present {
			if why := f.packTreeNoSourceReason(p); why != "" {
				st.Disposition, st.Reason = NoEntry, why
			}
		}
		return st
	}
	if d.Hit != nil {
		if entry, err := d.Hit(); err == nil {
			if why := buildUnusableAs(p, d.What+"'s build of "+p.Bin(), entry); why != "" {
				st.Disposition, st.Reason = NoEntry, why
			} else if why := f.storeProgramLoaderProblem(entry, p.Install.ProgramPath()); why != "" {
				st.Disposition, st.Reason = NoEntry, d.What+"'s build of "+p.Bin()+" "+why
			}
			return st
		}
	}
	if d.Jail {
		if why := f.cannotBuild(p.Bin(), "builds it"); why != "" {
			st.Disposition, st.Reason = NoEntry, d.cannotBuildReason(p.Bin(), why)
		}
	}
	return st
}

// packTreePending is why a provisioned pack-tree copy is not the one its pack asks for now, "" when
// it is: another digest, recipe or artifact than the install's resolution names, or — before any
// resolution — a recipe its declaration no longer makes.
func (f *Floor) packTreePending(p Program, rec *Record) string {
	in := p.Install
	if d, ok := f.resolvedPackTree(p); ok && d.Reason == "" {
		switch {
		case rec.Recipe != d.Recipe:
			return "pack " + p.Pack + " now delivers it as " + d.What + " (installed: " + rec.Version + ")"
		case rec.Revision != d.Revision:
			return "pack " + p.Pack + "'s " + in.PackFrom + " changed: it is now " + d.What + " (installed: " +
				rec.Version + ")"
		}
	} else if rec.Recipe != in.PackTreeRecipe() && rec.Recipe != packdecl.PackTreePrebuiltRecipe(in.Bin) {
		return "pack " + p.Pack + "'s declaration of " + p.Bin() + " changed since " + rec.Version + " was installed"
	}
	if rec.Node != "" && !packdecl.SatisfiesNodeFloor(rec.Node, in.NodeFloor) {
		return "it runs on Node " + rec.Node + ", below the pack's node_floor " + in.NodeFloor
	}
	return ""
}

// packTreeServesANearMiss reports whether rec, installed for p, is not the copy the install's
// resolution names: any copy is, when there is no resolution to compare.
func (f *Floor) packTreeServesANearMiss(p Program, rec *Record) bool {
	d, ok := f.resolvedPackTree(p)
	if !ok || d.Reason != "" {
		return true
	}
	return rec.Via != packdecl.ViaPack || rec.Revision != d.Revision || rec.Recipe != d.Recipe
}

// installFromPackTree materializes the store's entry under p's resolved key into dir/home, making it
// first (Build) when the store has none: a sealed build, the host's copy of a tree with no build line,
// or the verified fetch of its prebuilt.
func (f *Floor) installFromPackTree(ctx context.Context, p Program, dir string) (*Record, error) {
	bin := p.Bin()
	d := f.resolvePackTree(p)
	if d.Reason != "" {
		return nil, &noEntryError{reason: d.Reason}
	}
	if d.Hit == nil || d.Build == nil {
		return nil, errors.New("this machine has no capture store")
	}
	entry, err := d.Hit()
	if err != nil {
		if d.Jail {
			if why := f.cannotBuild(bin, "builds it"); why != "" {
				return nil, &noEntryError{reason: d.cannotBuildReason(bin, why)}
			}
			f.say("no build of %s from %s on this machine yet; building it %s", bin, d.What, f.buildHowPer("tree digest"))
		}
		if entry, err = d.Build(); err != nil {
			return nil, err
		}
		if entry == nil {
			return nil, fmt.Errorf("the build of %s from %s ran and stored nothing", bin, d.What)
		}
	}
	what := d.What + "'s build of " + bin
	if why := buildUnusableAs(p, what, entry); why != "" {
		return nil, &noEntryError{reason: why}
	}
	entryPath, err := f.materializeBuild(p, entry, dir, what, d.What+" ("+entry.Key+")")
	if err != nil {
		return nil, err
	}
	rec := &Record{Entry: entryPath, Capture: entry.Key, Revision: d.Revision, Recipe: d.Recipe, Version: d.Version}
	defer f.span(bin, "exec_record")()
	return f.execRecord(ctx, p.Install, rec)
}

// sayPackTreeDisclosure prints p's "which bytes" line when its resolution has one: on every launch
// while the floor serves it (§4, BG-D10), a store hit included.
func (f *Floor) sayPackTreeDisclosure(p Program) {
	if d, ok := f.resolvedPackTree(p); ok && d.Disclosure != "" {
		f.say("%s", d.Disclosure)
	}
}
