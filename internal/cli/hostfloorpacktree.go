package cli

// hostfloorpacktree.go wires a PACK-TREE PROGRAM (docs/design/pack-buildable-programs.md, `via:
// "pack"`) into the host floor on Linux (§9's `yolo host`, Linux row; the plan's Task 9): the floor's
// PackTree resolver (hostfloor's packtree.go) is answered here, through the same resolution and the
// same build act a jail launch and `yolo host apply` share with `yolo pack build`.
//
//   - THE TREE IS THE STAGED ONE. The pack is staged into this process's pack tree exactly as a launch
//     stages it (resolveStagedPack, `yolo pack build`'s resolver), and its `from` digested there, so the
//     floor's key is the key a jail launch on this machine looks up, and the build is handed the bytes
//     the digest read (§6.1). A local pack read in place would key a build no launch ever asks for.
//   - WHICH BYTES is packload.PackTreeResolve's answer for the floor's platform: the build of the tree,
//     or the prebuilt artifact where the pack ships no source. On Linux a build can always run in
//     principle (a runtime that is not installed is a step, not "permanently unavailable", OQ-BG2), so
//     an artifact never stands in for a present source.
//   - THE ACT is buildFork, the one every launch runs: the sealed build jail, the host's copy of a tree
//     with no build line, or the verified fetch, under the build's lock. Its failure names the build's
//     log and the retry, `yolo pack build <pack> --at host` (§8).

import (
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/hostfloor"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/pidlock"
)

// floorStagePack resolves a selected pack by name, staged as a launch stages it. A var so a test can
// hand the floor a pack of its own.
var floorStagePack = func(name string) (*packload.Pack, error) {
	entries, _, err := config.LoadPackEntries()
	if err != nil {
		return nil, err
	}
	entry := config.EmbeddedPackEntry(name)
	for _, e := range entries {
		if e.Name == name {
			entry = e
			break
		}
	}
	return resolveStagedPack(entry)
}

// floorBuildFork is the build act the floor's pack-tree programs run (buildFork), a var so a test can
// stand a fake act in.
var floorBuildFork = buildFork

// floorPackTrees is a floor's PackTree resolver: each pack staged at most once per floor, however many
// of its programs ask.
func floorPackTrees(store *capture.Store, out io.Writer) func(p hostfloor.Program) hostfloor.PackTreeDelivery {
	var mu sync.Mutex
	staged := map[string]*packload.Pack{}
	stageErr := map[string]error{}
	return func(p hostfloor.Program) hostfloor.PackTreeDelivery {
		mu.Lock()
		pack, seen := staged[p.Pack]
		err := stageErr[p.Pack]
		if !seen && err == nil {
			pack, err = floorStagePack(p.Pack)
			staged[p.Pack], stageErr[p.Pack] = pack, err
		}
		mu.Unlock()
		if err != nil {
			return hostfloor.PackTreeDelivery{Reason: "pack " + p.Pack + " could not be staged to read " + p.Bin() +
				"'s source (" + err.Error() + ") — fix what that names, then run `yolo host` again"}
		}
		return floorPackTree(p, pack, store, out)
	}
}

// floorPackTree is one pack-tree program's delivery on the floor, from its pack staged.
func floorPackTree(p hostfloor.Program, pack *packload.Pack, store *capture.Store, out io.Writer) hostfloor.PackTreeDelivery {
	var fork *packload.Fork
	for _, f := range packload.PackTreePrograms([]*packload.Pack{pack}) {
		if f.Bin == p.Bin() {
			f := f
			fork = &f
			break
		}
	}
	if fork == nil {
		return hostfloor.PackTreeDelivery{Reason: "pack " + p.Pack + " no longer declares " + p.Bin() +
			" as a program built from its own tree — run `yolo host` again to read its current declaration"}
	}
	platform := hostFloorCapturePlatform()
	pin := packload.PackTreeResolve(*fork, platform, true)
	if pin.Reason != "" {
		return hostfloor.PackTreeDelivery{Reason: pin.Reason}
	}
	b := forkBuild{Fork: pin.Fork, Commit: pin.Commit, Platform: platform}
	f := b.Fork
	d := hostfloor.PackTreeDelivery{
		Revision: b.Commit,
		Recipe:   b.recipe(),
		What:     packTreeWhat(b),
		Version:  "tree " + shortSHA(b.Commit),
		Jail:     !f.IsPrebuilt() && strings.TrimSpace(f.Build) != "",
	}
	if f.IsPrebuilt() {
		d.Version = "prebuilt sha256 " + shortSHA(b.Commit)
		d.Disclosure = run.PrebuiltLine(f.Bin, f.Pack, f.PrebuiltWhy, f.Prebuilt.SHA256, f.Prebuilt.URL)
	}
	d.Hit = func() (*capture.Entry, error) {
		entry, _, err := resolveForkBuild(store, f.Bin, b.Platform, b.selectSource(), b.Commit, b.recipe())
		return entry, err
	}
	d.Build = func() (*capture.Entry, error) {
		entry, err := floorBuildFork(b, buildMode{lock: pidlock.Mode{Wait: true, Bound: forkBuildWaitBound},
			jailStdout: hostJailStdout(), keepJailOutput: true}, out, out, false)
		if err != nil {
			return nil, floorPackTreeFailure(b, store, err)
		}
		return entry, nil
	}
	return d
}

// floorPackTreeFailure is a failed floor build's error in §8's terms: what failed, the build's log
// when it kept one, and the retry on the host. The readiness act's refusal adds the bypass.
func floorPackTreeFailure(b forkBuild, store *capture.Store, err error) error {
	why := err.Error()
	if log := buildJailLogPath(store.StagingDir("fork-" + b.id())); fileExists(log) {
		why += withBuildLog(log)
	}
	verb := "building"
	switch {
	case b.Fork.IsPrebuilt():
		verb = "fetching"
	case strings.TrimSpace(b.Fork.Build) == "":
		verb = "copying"
	}
	return fmt.Errorf("%s %s from %s failed: %s — retry: `yolo pack build %s --at host`", verb, b.Fork.Bin,
		packTreeWhat(b), why, b.Fork.Pack)
}
