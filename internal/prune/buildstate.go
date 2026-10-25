package prune

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/runtime"
)

// buildstate.go reaps the build dir's GC-root links that no reaper used to reach
// (disk-footprint.md "Build-state links"). Each one roots a store closure, so a link
// nothing removes is a closure nix can never collect: measured 2026-10-24 in one
// jail's build dir, 938 prefix roots, 413 retired `jail-prefix-*` out-links pinning
// about 392 install prefixes, 170 image-copier link pairs and six extras roots.
//
// The policy of each class follows the one question prefixroots.go asks: can losing
// the link cost only a rebuild?
//
//   - Retired prefix out-links (`jail-prefix-<sha16>`): no yolo writes them since the
//     launch stopped building an install prefix. A running jail an older yolo started
//     may still execute from one, so the class is held by liveness, like prefix roots.
//   - Image-copier links (`image-copier-<sha16>` and its `-man` twin): only the launcher
//     executes the copier, never a running jail, and a launch that copies rebuilds and
//     re-links it first. Losing one costs a rebuild, so age, like image roots (OQ-LS1).
//   - Package-profile roots (`package-roots/extras-*`, `package-roots/jail-*`): a running
//     jail reads its profiles from the host store, so the profiles a running container
//     was started with are held by liveness; the rest age out.

// RetiredPrefixOutLinkPrefix names the out-links the launch wrote for its install
// prefix (BUILD_DIR/jail-prefix-<sha16 of the source root>) until it stopped
// building one. Nothing reads or writes them now.
const RetiredPrefixOutLinkPrefix = "jail-prefix-"

// ImageCopierLinkPrefix is image.ImageCopierOutLink's leaf prefix. Spelled here rather
// than imported because the out-link's name is the contract; the two are pinned by
// TestImageCopierLinkPrefixMatchesTheWriter.
const ImageCopierLinkPrefix = "image-copier-"

// BuildLinkRetention is how long an age-policy link of this file is kept after it was
// last written. The same horizon as ImageRootRetention, for the same reason: losing one
// costs a rebuild, and a week covers a workspace used weekly.
const BuildLinkRetention = ImageRootRetention

// copierLinkName matches an image-copier out-link and its man-page twin, capturing the
// key.
var copierLinkName = regexp.MustCompile(`^image-copier-([0-9a-f]{16})(-man)?$`)

// isSymlink reports whether p is a symlink right now.
func isSymlink(p string) bool {
	st, err := os.Lstat(p)
	return err == nil && st.Mode()&os.ModeSymlink != 0
}

// PruneOrphanPrefixRootsGuarded is PruneOrphanPrefixRoots with each removal bracketed by
// guard. The recheck re-reads the link: it must still be a symlink, past the grace. A
// root no running jail holds cannot gain a holder during the pass, because no launch
// mounts a store prefix any more; the grace keeps the rule for an older launcher's
// registration window.
func PruneOrphanPrefixRootsGuarded(rootsDir string, liveSources map[string]bool, liveKnown bool, apply bool, now time.Time, guard Guard) []string {
	reaped := []string{}
	if !liveKnown {
		return reaped
	}
	liveKeys := livePrefixKeys(liveSources)
	entries, err := os.ReadDir(rootsDir)
	if err != nil {
		return reaped
	}
	oldEnough := func(link string) bool {
		st, err := os.Lstat(link)
		return err == nil && st.Mode()&os.ModeSymlink != 0 && now.Sub(st.ModTime()) >= PrefixRootGrace
	}
	for _, e := range entries {
		link := filepath.Join(rootsDir, e.Name())
		if liveKeys[e.Name()] || !oldEnough(link) {
			continue
		}
		if !apply {
			reaped = append(reaped, link)
			continue
		}
		if guard.Do(func() bool { return oldEnough(link) }, func() { _ = os.Remove(link) }) {
			reaped = append(reaped, link)
		}
	}
	return reaped
}

// livePrefixStorePaths maps the live prefix mount sources to the store paths they are
// mounted out of.
func livePrefixStorePaths(liveSources map[string]bool) map[string]bool {
	out := map[string]bool{}
	for src := range liveSources {
		if sp := PrefixStorePathOf(src); sp != "" {
			out[sp] = true
		}
	}
	return out
}

// PruneRetiredPrefixOutLinks removes the retired BUILD_DIR/jail-prefix-* out-links,
// keeping any whose target is the prefix a running jail is executing from. Returns the
// links removed (or that would be, in dry-run), sorted.
//
// liveKnown=false removes nothing: a link might be the only root of a store prefix an
// older jail still runs on. The store paths themselves are deleted, if ever, by the
// store-output reaper, which keeps a running jail's prefix on its own evidence.
func PruneRetiredPrefixOutLinks(buildDir string, liveSources map[string]bool, liveKnown, apply bool, now time.Time, guard Guard) []string {
	removed := []string{}
	if !liveKnown {
		return removed
	}
	live := livePrefixStorePaths(liveSources)
	entries, err := os.ReadDir(buildDir)
	if err != nil {
		return removed
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), RetiredPrefixOutLinkPrefix) {
			continue
		}
		link := filepath.Join(buildDir, e.Name())
		if !isSymlink(link) {
			continue // not ours
		}
		// PrefixRootGrace, as for the prefix roots: an older launcher still mid-build may
		// have written this link moments ago, before its container exists.
		if st, err := os.Lstat(link); err != nil || now.Sub(st.ModTime()) < PrefixRootGrace {
			continue
		}
		if target, err := os.Readlink(link); err == nil && live[target] {
			continue // a running jail executes from this prefix
		}
		if !apply {
			removed = append(removed, link)
			continue
		}
		if guard.Do(func() bool { return isSymlink(link) }, func() { _ = os.Remove(link) }) {
			removed = append(removed, link)
		}
	}
	sort.Strings(removed)
	return removed
}

// PruneImageCopierLinks removes image-copier out-links (and each one's `-man` twin)
// that are dangling, orphaned (a `-man` link whose main link is gone), or older than
// retention — always keeping the newest main link, so the copier the last launch built
// stays rooted. Returns the links removed (or that would be), sorted.
//
// NO LIVENESS GATE, by the rule above: no running jail executes the copier. A launch
// that is about to copy runs `nix build --out-link` first, which rewrites its link and
// so makes it the newest. The link is keyed by a hash of the source root, which cannot
// be reversed, so "its source root no longer exists" is decided by age instead.
func PruneImageCopierLinks(buildDir string, retention time.Duration, apply bool, now time.Time, guard Guard) []string {
	removed := []string{}
	entries, err := os.ReadDir(buildDir)
	if err != nil {
		return removed
	}
	type pair struct{ main, man string }
	keys := map[string]*pair{}
	for _, e := range entries {
		m := copierLinkName.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		link := filepath.Join(buildDir, e.Name())
		if !isSymlink(link) {
			continue
		}
		p := keys[m[1]]
		if p == nil {
			p = &pair{}
			keys[m[1]] = p
		}
		if m[2] == "" {
			p.main = link
		} else {
			p.man = link
		}
	}
	// The newest main link that still resolves is never removed.
	var newest string
	var newestTime time.Time
	for _, p := range keys {
		if p.main == "" {
			continue
		}
		if _, err := os.Stat(p.main); err != nil {
			continue
		}
		if st, err := os.Lstat(p.main); err == nil && st.ModTime().After(newestTime) {
			newest, newestTime = p.main, st.ModTime()
		}
	}
	// reapable re-reads the main link: missing, dangling, or old. Run once to select and
	// again under the guard, so a launch that rebuilt this copier meanwhile keeps it.
	reapable := func(main string) bool {
		if main == "" {
			return true // a `-man` link with no main link is an orphan
		}
		st, err := os.Lstat(main)
		if err != nil {
			return true
		}
		if _, err := os.Stat(main); err != nil {
			return true // dangling
		}
		return main != newest && now.Sub(st.ModTime()) >= retention
	}
	for _, p := range keys {
		if !reapable(p.main) {
			continue
		}
		for _, link := range []string{p.main, p.man} {
			if link == "" {
				continue
			}
			if !apply {
				removed = append(removed, link)
				continue
			}
			main := p.main
			seen, err := os.Lstat(link)
			if err != nil {
				continue
			}
			// The link removed must be the one judged: a launch rebuilding this copier
			// replaces the link with a new inode (nix renames a fresh symlink over it), so
			// a link compared by identity right before the removal is left alone if it
			// changed. A rebuild landing between that comparison and the unlink is the
			// residual race, a window of two syscalls: it costs that launch's root on the
			// copier until its next rebuild, never a running jail anything, and nix does
			// not collect a path a running process holds open.
			var gone bool
			guard.Do(func() bool { return reapable(main) }, func() {
				if cur, err := os.Lstat(link); err == nil && os.SameFile(seen, cur) {
					gone = os.Remove(link) == nil
				}
			})
			if gone {
				removed = append(removed, link)
			}
		}
	}
	sort.Strings(removed)
	return removed
}

// Store-profile env names: the launch→boot contract entrypoint.StoreProfilesEnv and
// entrypoint.StoreFHSProfilesEnv, spelled here because prune must not depend on the boot
// path. TestStoreProfileEnvNamesMatchTheEntrypoint pins them together.
const (
	storeProfilesEnv    = "YOLO_STORE_PROFILES"
	storeFHSProfilesEnv = "YOLO_STORE_FHS_PROFILES"
)

// LiveStoreProfiles returns the store-delivered profiles running jails were started with
// (their YOLO_STORE_PROFILES and YOLO_STORE_FHS_PROFILES), whether that could be
// determined, and, when it could not, which evidence was missing.
//
// The same all-or-nothing rule as LivePrefixSources: one running container whose
// environment cannot be read makes the answer unknown, and every caller then removes
// nothing. A container that answered without either variable bakes its packages and
// holds no profile.
func LiveStoreProfiles(rt string, live runtime.LiveSet, run RunFunc) (map[string]bool, bool, string) {
	if !live.Known {
		return nil, false, fmt.Sprintf("could not enumerate running jails from %s", rt)
	}
	names := make([]string, 0, len(live.Names))
	for name := range live.Names {
		names = append(names, name)
	}
	sort.Strings(names)
	profiles := map[string]bool{}
	for _, name := range names {
		env, ok := inspectEnv(rt, name, run)
		if !ok {
			return nil, false, fmt.Sprintf("could not read running container %s's environment from %s", name, rt)
		}
		for _, kv := range env {
			k, v, ok := strings.Cut(kv, "=")
			if !ok || (k != storeProfilesEnv && k != storeFHSProfilesEnv) {
				continue
			}
			for _, p := range strings.Split(v, ":") {
				if p = strings.TrimSpace(p); p != "" {
					profiles[p] = true
				}
			}
		}
	}
	return profiles, true, ""
}

// inspectEnv reads one container's environment. Apple Container's inspect takes no
// --format, so its whole document is read (runtime.EnvFromContainerInspectJSON); every
// other runtime answers `inspect --format {{json .Config.Env}}`.
func inspectEnv(rt, name string, run RunFunc) ([]string, bool) {
	if rt == "container" {
		res := run([]string{"container", "inspect", name}, inspectTimeout)
		if !res.Ran || res.RC != 0 {
			return nil, false
		}
		return runtime.EnvFromContainerInspectJSON(res.Stdout)
	}
	res := run([]string{rt, "inspect", "--format", "{{json .Config.Env}}", name}, inspectTimeout)
	var env []string
	if !res.Ran || res.RC != 0 || json.Unmarshal([]byte(strings.TrimSpace(res.Stdout)), &env) != nil || env == nil {
		return nil, false
	}
	return env, true
}

// packageRootFamilies are the leaf prefixes under BUILD_DIR/package-roots: the image
// extras' profile and the store-delivered `packages:` profile.
var packageRootFamilies = []string{"extras-", "jail-"}

// PackageRootFamily is the family prefix a package-roots leaf belongs to, or "" for a name
// no writer produces. TestPackageRootFamiliesMatchTheWriters pins it against the launch's
// two writers.
func PackageRootFamily(name string) string {
	for _, f := range packageRootFamilies {
		if strings.HasPrefix(name, f) {
			return f
		}
	}
	return ""
}

// PrunePackageRoots ages out package-profile roots under rootsDir. Kept: a root whose
// target a running jail was started with (liveProfiles), the newest root of each family
// (the current profile), and any root written within retention. Returns the roots
// removed (or that would be), sorted.
//
// liveKnown=false removes nothing: the profile a running jail links its tools from might
// be the one a removed root was the last to hold.
func PrunePackageRoots(rootsDir string, liveProfiles map[string]bool, liveKnown bool, retention time.Duration, apply bool, now time.Time, guard Guard) []string {
	removed := []string{}
	if !liveKnown {
		return removed
	}
	entries, err := os.ReadDir(rootsDir)
	if err != nil {
		return removed
	}
	newest := map[string]string{}
	newestTime := map[string]time.Time{}
	var links []string
	for _, e := range entries {
		fam := PackageRootFamily(e.Name())
		link := filepath.Join(rootsDir, e.Name())
		if fam == "" || !isSymlink(link) {
			continue
		}
		links = append(links, link)
		if st, err := os.Lstat(link); err == nil && st.ModTime().After(newestTime[fam]) {
			newest[fam], newestTime[fam] = link, st.ModTime()
		}
	}
	reapable := func(link string) bool {
		st, err := os.Lstat(link)
		if err != nil || st.Mode()&os.ModeSymlink == 0 {
			return false
		}
		if target, err := os.Readlink(link); err == nil && liveProfiles[target] {
			return false
		}
		if newest[PackageRootFamily(filepath.Base(link))] == link {
			return false
		}
		return now.Sub(st.ModTime()) >= retention
	}
	for _, link := range links {
		if !reapable(link) {
			continue
		}
		if !apply {
			removed = append(removed, link)
			continue
		}
		if guard.Do(func() bool { return reapable(link) }, func() { _ = os.Remove(link) }) {
			removed = append(removed, link)
		}
	}
	sort.Strings(removed)
	return removed
}
