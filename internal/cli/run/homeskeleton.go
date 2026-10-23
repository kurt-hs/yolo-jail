package run

// homeskeleton.go builds the per-jail HOME SKELETON a podman jail binds read-only at
// /home/agent (docs/reference/jail-home.md#why-the-home-root-is-a-read-only-skeleton-with-symlink-hatches).
//
// A skeleton — the design's coined term — is a directory holding ONLY the mountpoints and
// redirect links this launch's binds need, and no file content. It replaces the one
// machine-wide base, <state>/home, that every podman jail used to share: that base was a
// union of every shipped pack's dirs and every mountpoint any launch had ever made, so one
// workspace's `writable_home_dirs` and `host_files` links, every unselected pack's directory,
// and the machine's shared credential dirs showed up in every jail. A skeleton is built from
// THIS launch's selected packs and config and nothing else, so a pack the jail did not select
// has no effect inside it (the design's DIR-BH1).
//
// <state>/home stays, as the machine store: the rw source of the selected packs' shared dirs
// and the Claude login seed. storage.EnsureGlobalStorage still provisions those; everything
// else it used to create in <state>/home is created here instead.
//
// THE THREE RULES the shared base obeyed by accident, stated because a per-jail directory has
// to obey them on purpose (docs/reference/jail-home.md#the-three-rules-a-skeleton-obeys):
//
//  1. BUILT OUTSIDE THE WORKSPACE, under paths.HomeSkeletonRoot. <workspace>/.yolo/home is
//     writable from inside the jail, so a skeleton there would be read-only in name only, and
//     the MkdirAll/Symlink calls below would follow links the jail planted.
//  2. NEVER A MOUNTPOINT REMOVED UNDER A LIVE JAIL: a host-side rmdir silently detaches the
//     bind inside. So every fresh launch builds a NEW directory and nothing ever edits or
//     removes an old one (the maintainer's OQ-BH10 ruling). Old skeletons go when the jail's
//     whole AGENTS_DIR/<cname> does, through prune.PruneOrphanAgentStaging, which declines
//     when liveness is unknown and never reaps a live or TRACKED jail's entry. That reaper
//     can reach an entry only because the launch now drops the jail's tracking file once it
//     observes the container gone (forgetGoneContainer, trackingcleanup.go); before that, a
//     --rm jail's tracking file outlived every normal exit and its skeletons piled up.
//     A launch removes only its OWN skeleton, and only when no container holds it: its
//     partial directory when building fails, the one it built before a refusal or a runtime
//     that never started, and the one its container booted from once the runtime answers
//     that container is gone (discardUnheldSkeleton; OQ-BH16).
//  3. <state>/home STAYS as the machine store (above).
//
// FATAL AND BEST-EFFORT, split the way the writers this replaces split it
// (docs/reference/jail-home.md#when-a-skeleton-is-built): the entries
// that used to come from storage.EnsureGlobalStorage — the core dirs, the pack dirs, the
// single-file mountpoints and the redirects — fail the launch, because a jail without them
// does not boot; the config- and pack-driven ones that used to come from prepareWsState,
// preparePackFilesGlobal and prepareHostFiles still only degrade, as they always did, but
// with a warning where they used to be silent. Either way the message names the PATH, which
// podman's own error (`conmon bytes "": readObjectStart`) never does.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// homeSkeleton is one fresh launch's skeleton.
type homeSkeleton struct {
	// dir is the directory podmanBaseMounts binds read-only at /home/agent.
	dir string
	// warnings are the best-effort entries that could not be created, one line each, each
	// naming its path.
	warnings []string
}

// homeSkeletonTimeLayout prefixes each skeleton's directory name, so a human listing
// AGENTS_DIR/<cname>/home can tell the launches apart. The random suffix os.MkdirTemp adds
// is what makes the name unique; the stamp only orders it.
const homeSkeletonTimeLayout = "20060102T150405Z"

// buildHomeSkeleton creates a NEW skeleton directory under root and fills it from this
// launch's selected packs, its config and its resolved host_files entries.
//
// Podman only, and only on the fresh-launch path, under the workspace flock, after
// prepareWsState and prepareHostFiles (runContainer). An attach never calls it: the running
// jail already holds its own skeleton, and editing that one is exactly what rule 2 forbids.
//
// Every -v destination directly under /home/agent must either exist here or sit inside
// another bind; TestEveryHomeBindHasASkeletonEntry checks that over the assembled argv.
//
// trees is the launch's per-launch copy of each patched extension it delivered, by extension key
// (Options.patchedTreeDirs): a delivered one gets its directory mountpoint, and one with no copy
// none.
func buildHomeSkeleton(root string, packs []*packload.Pack, cfg *jsonx.OrderedMap,
	hostFiles []config.HostFileEntry, trees map[string]string) (homeSkeleton, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return homeSkeleton{}, fmt.Errorf("cannot create the jail's home skeleton root %s: %w", root, err)
	}
	dir, err := os.MkdirTemp(root, time.Now().UTC().Format(homeSkeletonTimeLayout)+"-")
	if err != nil {
		return homeSkeleton{}, fmt.Errorf("cannot create a home skeleton under %s: %w", root, err)
	}
	// A FATAL FAILURE TAKES ITS PARTIAL DIRECTORY WITH IT. The launch refuses, so no
	// container ever binds this directory, and nothing else knows its name: left behind it
	// is one more skeleton for the reaper, from a launch that never ran.
	fail := func(err error) (homeSkeleton, error) {
		_ = os.RemoveAll(dir)
		return homeSkeleton{}, err
	}
	// os.MkdirTemp creates 0700. The shared base this replaces was 0755, and /home/agent's
	// mode is what the jail's own tools see, so it stays what it was.
	if err := os.Chmod(dir, 0o755); err != nil {
		return fail(fmt.Errorf("cannot set the mode of the home skeleton %s: %w", dir, err))
	}
	b := skeletonBuilder{dir: dir, asked: &[]string{}}

	// --- FATAL: what storage.EnsureGlobalStorage used to put in the shared base ---
	for _, rel := range skeletonDirs(packs) {
		if err := b.mkdir(rel); err != nil {
			return fail(err)
		}
	}
	for _, rel := range paths.HomeFileMountpoints() {
		if err := b.touch(rel); err != nil {
			return fail(err)
		}
	}
	// The three redirects, links into per-workspace binds (paths.HomeFileRedirects). A link
	// whose target dir is not bound in this jail — `.claude.json` in a jail that did not
	// select claude — dangles and reads as absent, which is the correct answer there.
	//
	// BEFORE every best-effort entry, the order EnsureGlobalStorage created them in, so that a
	// pack entry landing on a redirect NAME (a dotfiles pack's `files` into `.gitconfig`) is
	// the one that fails, as a warning. Built after them, the pack's file got there first and
	// this fatal link then refused every fresh launch. Their targets are fixed, relative and
	// inside the skeleton, so a later MkdirAll that runs into one stays inside it too.
	for _, r := range paths.HomeFileRedirects() {
		if err := b.symlink(r.Name, r.Target); err != nil {
			return fail(err)
		}
	}

	// --- BEST-EFFORT: the config- and pack-driven mountpoints ---
	sk := homeSkeleton{dir: dir}
	warn := func(err error) {
		if err != nil {
			sk.warnings = append(sk.warnings, err.Error())
		}
	}
	// writable_home_dirs: a rw bind of <wsState>/writable-home/<rel> nests inside the :ro
	// root, and the OCI runtime cannot mkdirat inside a read-only bind (crun EROFS, surfacing
	// as `conmon bytes "": readObjectStart`).
	writableHomeDirs := config.WritableHomeDirs(cfg, packs)
	for _, rel := range writableHomeDirs {
		warn(b.mkdir(rel))
	}
	// host_files: the writable subtree each new top-level destination is staged in, bar the
	// ones a writable_home_dirs bind above already covers.
	for _, rel := range hostFileWritableDirs(hostFiles, packs, writableHomeDirs) {
		warn(b.mkdir(rel))
	}
	// Pack `files` targets no pack writable dir covers; the leaf type follows the source,
	// because a dir bind over a file (or the reverse) aborts container creation.
	fileDirs, fileFiles := packFilesSkeletonEntries(packs, trees)
	for _, rel := range fileDirs {
		warn(b.mkdir(rel))
	}
	for _, rel := range fileFiles {
		warn(b.touch(rel))
	}
	for _, target := range packSkillTargets(packs) {
		warn(b.mkdir(target.Dest))
	}
	for _, d := range briefingDestinations(packs) {
		warn(b.touch(d.Into))
	}

	// --- BEST-EFFORT LINKS, last, so no MkdirAll above ever runs through one: their targets
	// are config-driven ---
	//
	// host_files home-root FILE destinations (`~/.npmrc`), each a RELATIVE link into the
	// writable ~/.config overlay, left DANGLING on purpose — HostFileModeOnce seeds only a
	// file it cannot stat, so a pre-created target would suppress the seed for good.
	for _, entry := range hostFiles {
		if entry.StagingFor(packs) != config.HostFileStagingSymlink {
			continue
		}
		warn(b.symlink(entry.Path, entry.SymlinkTarget()))
	}

	// THE CONTEXT LINK, LAST OF ALL (OQ-MM6). The home root is read-only in this jail, so the
	// agent's ~/ctx link is laid here from the host side, pointing at the container's context
	// dir (the string YOLO_CONTEXT_DIR names on podman; absolute on purpose, because the
	// entrypoint compares raw link strings). It comes after every other entry, because a
	// host-side MkdirAll or write through the link would land on the HOST's /ctx. A config or
	// pack entry that claims ctx keeps its own directory, and the link is skipped with a line.
	if w := b.layContextLink(paths.ContextLinkName, paths.ContainerContextDir); w != "" {
		sk.warnings = append(sk.warnings, w)
	}
	return sk, nil
}

// layContextLink lays the link rel -> target inside the skeleton and returns the one line to
// print about it, or "" when the link is laid. It never fails the launch: a home that cannot
// take the link, or one that a declared entry claims, boots without it, and the mounts stay
// where they are at $YOLO_CONTEXT_DIR.
func (b skeletonBuilder) layContextLink(rel, target string) string {
	if claimsPath(b.asked, rel) {
		return claimedContextLinkLine(rel, target)
	}
	p := filepath.Join(b.dir, rel)
	if _, err := os.Lstat(p); err == nil {
		return claimedContextLinkLine(rel, target)
	}
	if err := os.Symlink(target, p); err != nil {
		return fmt.Sprintf("yolo: could not link ~/%s to $%s (%s): %v; the mounts stay at $%s.",
			rel, paths.ContextDirEnv, target, err, paths.ContextDirEnv)
	}
	return ""
}

// claimsPath reports whether any path asked of the builder is rel itself or under it.
func claimsPath(asked *[]string, rel string) bool {
	if asked == nil {
		return false
	}
	for _, a := range *asked {
		c := filepath.ToSlash(filepath.Clean(filepath.FromSlash(a)))
		if c == rel || strings.HasPrefix(c, rel+"/") {
			return true
		}
	}
	return false
}

func claimedContextLinkLine(rel, target string) string {
	return fmt.Sprintf("yolo: ~/%s is claimed by a home entry of this launch, so the context link is "+
		"not laid; $%s still names %s.", rel, paths.ContextDirEnv, target)
}

// discardUnheldSkeleton removes a skeleton that no container ever held: the one this launch
// built and then started no container on, because a pre-flight after the build refused the
// launch or the runtime never started. Without it every such launch left one more directory
// under AGENTS_DIR/<cname>/home for the reaper, which reaches it only once the whole entry is
// neither live nor tracked.
//
// Rule 2 does not stand in the way: it forbids removing a mountpoint under a LIVE jail, and
// this directory was made by os.MkdirTemp for this launch alone and never named in a started
// container's argv. It removes only a direct child of paths.HomeSkeletonRoot(cname), so a
// wrong argument cannot reach anything else, and a failure only leaves the directory to the
// reaper, so it is not reported.
//
// forgetGoneContainer calls it too, for the skeleton of a container the runtime has answered
// is gone (OQ-BH16): no container holds it any more, which is what the guard needs.
func discardUnheldSkeleton(cname, dir string) {
	if dir == "" || filepath.Dir(dir) != paths.HomeSkeletonRoot(cname) {
		return
	}
	_ = os.RemoveAll(dir)
}

// skeletonDirs is every directory a skeleton gets whatever the config says: core's own
// (paths.HomeSkeletonCoreDirs, which names no pack's dir) and the mountpoints of the SELECTED
// packs' writable and shared dirs. The shared base carried every SHIPPED pack's here
// (packload.Embedded*), because it was provisioned before the config was loaded; that is how
// a claude-only jail came to have a ~/.codex and a codex-only jail a
// ~/.claude-shared-credentials.
func skeletonDirs(packs []*packload.Pack) []string {
	out := append([]string{}, paths.HomeSkeletonCoreDirs()...)
	out = append(out, packload.WritableDirs(packs)...)
	return append(out, packload.SharedDirs(packs)...)
}

// skeletonBuilder creates entries inside one skeleton directory.
type skeletonBuilder struct {
	dir string
	// asked is every home-relative path a builder method was asked for, refused or not, so
	// the claim check sees each destination the config and packs declared.
	asked *[]string
}

// path resolves a home-relative path inside the skeleton. Every input is validated upstream
// (config validation, packdecl), and this refuses an absolute or escaping one anyway: the
// builder runs on the host, and a path that leaves the skeleton is a host write.
func (b skeletonBuilder) path(rel string) (string, error) {
	if b.asked != nil {
		*b.asked = append(*b.asked, rel)
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if rel == "" || filepath.IsAbs(clean) || clean == "." || clean == ".." ||
		strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("home skeleton: refusing %q, which is not a path inside the home", rel)
	}
	return filepath.Join(b.dir, clean), nil
}

// mkdir creates the directory rel (and its parents).
func (b skeletonBuilder) mkdir(rel string) error {
	p, err := b.path(rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(p, 0o755); err != nil {
		return fmt.Errorf("home skeleton: cannot create the mountpoint %s: %w", p, err)
	}
	return nil
}

// touch creates the empty file rel (and its parent dirs), leaving an existing regular file
// alone. Anything else already there — a redirect link, a directory — is refused, not
// passed over: a file bind onto it is not the file mountpoint the caller asked for, and a
// dir under a file bind aborts container creation.
func (b skeletonBuilder) touch(rel string) error {
	p, err := b.path(rel)
	if err != nil {
		return err
	}
	if info, err := os.Lstat(p); err == nil {
		if info.Mode().IsRegular() {
			return nil
		}
		return fmt.Errorf("home skeleton: cannot create the file mountpoint %s: a %s is already there",
			p, entryKind(info))
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("home skeleton: cannot create the parent of the mountpoint %s: %w", p, err)
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("home skeleton: cannot create the mountpoint %s: %w", p, err)
	}
	return f.Close()
}

// entryKind names what an existing entry is, for touch's refusal.
func entryKind(info os.FileInfo) string {
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		return "link"
	case info.IsDir():
		return "directory"
	default:
		return "non-regular file"
	}
}

// symlink creates rel as a link to target. It never replaces an existing entry: in a new
// skeleton the only thing that can already be there is another entry of this launch's, and
// silently replacing a mountpoint with a link would break that bind instead.
func (b skeletonBuilder) symlink(rel, target string) error {
	p, err := b.path(rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("home skeleton: cannot create the parent of the link %s: %w", p, err)
	}
	if err := os.Symlink(target, p); err != nil {
		if cur, rerr := os.Readlink(p); rerr == nil && cur == target {
			return nil
		}
		return fmt.Errorf("home skeleton: cannot create the link %s -> %s: %w", p, target, err)
	}
	return nil
}
