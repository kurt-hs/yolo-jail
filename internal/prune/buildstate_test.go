package prune

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/image"
	"github.com/mschulkind-oss/yolo-jail/internal/runtime"
	"golang.org/x/sys/unix"
)

// mkLink makes link -> target with its own mtime age ago.
func mkLink(t *testing.T, target, link string, age time.Duration) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	tv := []unix.Timeval{unix.NsecToTimeval(when.UnixNano()), unix.NsecToTimeval(when.UnixNano())}
	if err := unix.Lutimes(link, tv); err != nil {
		t.Fatal(err)
	}
	return link
}

// mkTarget makes a directory standing in for a store path that still exists.
func mkTarget(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// --- retired prefix out-links ---

func TestRetiredPrefixOutLinksDeclineWhenLivenessUnknown(t *testing.T) {
	dir := t.TempDir()
	link := mkLink(t, "/nix/store/aaaa-yolo-jail-install-prefix", filepath.Join(dir, "jail-prefix-0123456789abcdef"), 30*24*time.Hour)
	if got := PruneRetiredPrefixOutLinks(dir, nil, false, true, time.Now(), nil); len(got) != 0 {
		t.Fatalf("removed %v with liveness unknown — an older jail may still execute from that prefix", got)
	}
	if !exists(link) {
		t.Fatal("a retired out-link was removed while liveness was unknown")
	}
}

func TestRetiredPrefixOutLinksKeepARunningJailsPrefixAndRemoveTheRest(t *testing.T) {
	dir := t.TempDir()
	live := "/nix/store/aaaa-yolo-jail-install-prefix"
	keep := mkLink(t, live, filepath.Join(dir, "jail-prefix-aaaaaaaaaaaaaaaa"), 30*24*time.Hour)
	gone := mkLink(t, "/nix/store/bbbb-yolo-jail-install-prefix", filepath.Join(dir, "jail-prefix-bbbbbbbbbbbbbbbb"), 2*time.Hour)
	young := mkLink(t, "/nix/store/dddd-yolo-jail-install-prefix", filepath.Join(dir, "jail-prefix-dddddddddddddddd"), time.Minute)
	other := mkLink(t, "/nix/store/cccc-skopeo", filepath.Join(dir, "restore-result"), 30*24*time.Hour)
	notLink := filepath.Join(dir, "jail-prefix-notalink")
	if err := os.WriteFile(notLink, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	sources := map[string]bool{live + "/" + image.JailPrefixSubdir + "/bin": true}

	if dry := PruneRetiredPrefixOutLinks(dir, sources, true, false, time.Now(), nil); !reflect.DeepEqual(dry, []string{gone}) || !exists(gone) {
		t.Fatalf("dry run = %v, want [%s] reported and nothing removed", dry, gone)
	}
	got := PruneRetiredPrefixOutLinks(dir, sources, true, true, time.Now(), nil)
	if !reflect.DeepEqual(got, []string{gone}) || exists(gone) {
		t.Fatalf("removed %v, want exactly the retired link no jail runs from (%s)", got, gone)
	}
	if !exists(young) {
		t.Error("a link younger than PrefixRootGrace was removed; an older launcher may be mid-build")
	}
	if !exists(keep) {
		t.Error("the out-link of a prefix a running jail executes from was removed")
	}
	if !exists(other) || !exists(notLink) {
		t.Error("the sweep reached past jail-prefix-* symlinks")
	}
}

func TestRetiredPrefixOutLinksHaveNoWriter(t *testing.T) {
	// The class is "retired" only while nothing writes the name. The writer lived in
	// internal/cli/run as JailPrefixOutLink; a revival must take this class out too.
	src, err := os.ReadFile("../cli/run/jailprefix.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(src), RetiredPrefixOutLinkPrefix) {
		t.Fatalf("jailprefix.go writes %q again — the retired-out-link sweep would delete a live "+
			"launch's prefix root", RetiredPrefixOutLinkPrefix)
	}
}

// --- orphaned prefix roots, guarded ---

func TestGuardedPrefixRootsRecheckUnderTheGuard(t *testing.T) {
	dir := t.TempDir()
	link := mkPrefixRoot(t, dir, "/nix/store/eeee-yolo-jail-install-prefix", 30*24*time.Hour)
	declineAll := Guard(func(recheck func() bool, del func()) bool {
		// A pass that lost the race: the root was re-registered moments ago.
		now := time.Now()
		_ = unix.Lutimes(link, []unix.Timeval{unix.NsecToTimeval(now.UnixNano()), unix.NsecToTimeval(now.UnixNano())})
		if !recheck() {
			return false
		}
		del()
		return true
	})
	if got := PruneOrphanPrefixRootsGuarded(dir, map[string]bool{}, true, true, time.Now(), declineAll); len(got) != 0 {
		t.Fatalf("removed %v though the recheck saw a fresh registration", got)
	}
	if !exists(link) {
		t.Fatal("the recheck did not stop the deletion")
	}
	if got := PruneOrphanPrefixRootsGuarded(dir, nil, false, true, time.Now().Add(48*time.Hour), nil); len(got) != 0 {
		t.Fatalf("removed %v with liveness unknown", got)
	}
}

// --- image-copier links ---

func TestImageCopierLinksRemoveDanglingOrphanedAndAgedButKeepTheNewest(t *testing.T) {
	dir := t.TempDir()
	store := t.TempDir()
	skopeo := mkTarget(t, store, "skopeo")
	man := mkTarget(t, store, "skopeo-man")
	day := 24 * time.Hour

	newest := mkLink(t, skopeo, filepath.Join(dir, "image-copier-1111111111111111"), 40*day)
	newestMan := mkLink(t, man, filepath.Join(dir, "image-copier-1111111111111111-man"), 40*day)
	older := mkLink(t, skopeo, filepath.Join(dir, "image-copier-2222222222222222"), 50*day)
	olderMan := mkLink(t, man, filepath.Join(dir, "image-copier-2222222222222222-man"), 50*day)
	dangling := mkLink(t, filepath.Join(store, "gone"), filepath.Join(dir, "image-copier-3333333333333333"), time.Minute)
	orphanMan := mkLink(t, man, filepath.Join(dir, "image-copier-4444444444444444-man"), time.Minute)
	other := mkLink(t, filepath.Join(store, "gone"), filepath.Join(dir, "image-copier-notakey"), 50*day)

	got := PruneImageCopierLinks(dir, BuildLinkRetention, true, time.Now(), nil)
	want := []string{older, olderMan, dangling, orphanMan}
	if !reflect.DeepEqual(got, sortedCopy(want)) {
		t.Fatalf("removed %v, want %v", got, sortedCopy(want))
	}
	for _, p := range want {
		if exists(p) {
			t.Errorf("%s survived", p)
		}
	}
	if !exists(newest) || !exists(newestMan) {
		t.Error("the newest copier link was removed — the next copy would rebuild skopeo from source " +
			"after a nix GC, though it is the one the last launch built")
	}
	if !exists(other) {
		t.Error("a name the writer never produces was removed")
	}
}

func TestImageCopierLinksKeepRecentOnes(t *testing.T) {
	dir := t.TempDir()
	skopeo := mkTarget(t, t.TempDir(), "skopeo")
	a := mkLink(t, skopeo, filepath.Join(dir, "image-copier-aaaaaaaaaaaaaaaa"), time.Hour)
	b := mkLink(t, skopeo, filepath.Join(dir, "image-copier-bbbbbbbbbbbbbbbb"), 2*time.Hour)
	if got := PruneImageCopierLinks(dir, BuildLinkRetention, true, time.Now(), nil); len(got) != 0 {
		t.Fatalf("removed %v, want none within the retention", got)
	}
	if !exists(a) || !exists(b) {
		t.Fatal("a recent copier link was removed")
	}
}

func TestImageCopierLinkPrefixMatchesTheWriter(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	leaf := filepath.Base(image.ImageCopierOutLink("/some/root"))
	if !copierLinkName.MatchString(leaf) || !copierLinkName.MatchString(leaf+"-man") ||
		!strings.HasPrefix(leaf, ImageCopierLinkPrefix) {
		t.Fatalf("image.ImageCopierOutLink writes %q, which the copier sweep does not match — the "+
			"links would accumulate again", leaf)
	}
}

// --- package-profile roots ---

func TestPackageRootsDeclineWhenLivenessUnknown(t *testing.T) {
	dir := t.TempDir()
	link := mkLink(t, "/nix/store/x-yolo-image-extras", filepath.Join(dir, "extras-0000000000000001"), 90*24*time.Hour)
	_ = mkLink(t, "/nix/store/y-yolo-image-extras", filepath.Join(dir, "extras-0000000000000002"), time.Hour)
	if got := PrunePackageRoots(dir, nil, false, BuildLinkRetention, true, time.Now(), nil); len(got) != 0 {
		t.Fatalf("removed %v with liveness unknown", got)
	}
	if !exists(link) {
		t.Fatal("a package root was removed while liveness was unknown")
	}
}

func TestPackageRootsKeepInUseNewestAndRecentAndAgeOutTheRest(t *testing.T) {
	dir := t.TempDir()
	day := 24 * time.Hour
	inUse := mkLink(t, "/nix/store/a-yolo-image-extras", filepath.Join(dir, "extras-aaaaaaaaaaaaaaaa"), 60*day)
	aged := mkLink(t, "/nix/store/b-yolo-image-extras", filepath.Join(dir, "extras-bbbbbbbbbbbbbbbb"), 30*day)
	recent := mkLink(t, "/nix/store/c-yolo-image-extras", filepath.Join(dir, "extras-cccccccccccccccc"), 2*day)
	newest := mkLink(t, "/nix/store/d-yolo-image-extras", filepath.Join(dir, "extras-dddddddddddddddd"), time.Hour)
	jailAgedNewest := mkLink(t, "/nix/store/e-yolo-noncontainer-packages", filepath.Join(dir, "jail-eeeeeeeeeeeeeeee"), 20*day)
	jailAged := mkLink(t, "/nix/store/f-yolo-noncontainer-packages", filepath.Join(dir, "jail-ffffffffffffffff"), 40*day)
	stranger := mkLink(t, "/nix/store/g", filepath.Join(dir, "something-else"), 90*day)

	live := map[string]bool{"/nix/store/a-yolo-image-extras": true}
	if dry := PrunePackageRoots(dir, live, true, BuildLinkRetention, false, time.Now(), nil); !exists(aged) || len(dry) != 2 {
		t.Fatalf("dry run = %v and must remove nothing", dry)
	}
	got := PrunePackageRoots(dir, live, true, BuildLinkRetention, true, time.Now(), nil)
	if want := sortedCopy([]string{aged, jailAged}); !reflect.DeepEqual(got, want) {
		t.Fatalf("removed %v, want %v", got, want)
	}
	for _, p := range []string{inUse, recent, newest, jailAgedNewest, stranger} {
		if !exists(p) {
			t.Errorf("%s was removed; in use, newest of its family, recent, or not ours", p)
		}
	}
}

// --- the liveness evidence for package roots ---

func TestLiveStoreProfilesReadsEachRunningJailsEnvironment(t *testing.T) {
	live := runtime.LiveSet{Known: true, Names: map[string]struct{}{"yolo-a": {}, "yolo-b": {}}}
	run := func(argv []string, _ time.Duration) ProbeResult {
		switch argv[len(argv)-1] {
		case "yolo-a":
			return ProbeResult{Ran: true, Stdout: `["PATH=/bin","YOLO_STORE_PROFILES=/nix/store/p1:/nix/store/p2","YOLO_STORE_FHS_PROFILES=/nix/store/x-yolo-image-extras"]`}
		default:
			return ProbeResult{Ran: true, Stdout: `["PATH=/bin"]`}
		}
	}
	got, known, why := LiveStoreProfiles("podman", live, run)
	want := map[string]bool{"/nix/store/p1": true, "/nix/store/p2": true, "/nix/store/x-yolo-image-extras": true}
	if !known || why != "" || !reflect.DeepEqual(got, want) {
		t.Fatalf("LiveStoreProfiles = %v, %v, %q; want %v, true", got, known, why, want)
	}
}

func TestLiveStoreProfilesIsUnknownWhenAnyContainerCannotBeRead(t *testing.T) {
	live := runtime.LiveSet{Known: true, Names: map[string]struct{}{"yolo-a": {}, "yolo-b": {}}}
	run := func(argv []string, _ time.Duration) ProbeResult {
		if argv[len(argv)-1] == "yolo-b" {
			return ProbeResult{Ran: true, RC: 125}
		}
		return ProbeResult{Ran: true, Stdout: `[]`}
	}
	if _, known, why := LiveStoreProfiles("podman", live, run); known || !strings.Contains(why, "yolo-b") {
		t.Fatalf("known=%v why=%q; one unreadable container must make the answer unknown, naming it", known, why)
	}
	if _, known, _ := LiveStoreProfiles("podman", runtime.LiveSet{Known: false}, run); known {
		t.Fatal("an unenumerable runtime must be unknown")
	}
}

func TestStoreProfileEnvNamesMatchTheEntrypoint(t *testing.T) {
	if storeProfilesEnv != entrypoint.StoreProfilesEnv || storeFHSProfilesEnv != entrypoint.StoreFHSProfilesEnv {
		t.Fatalf("prune reads %q/%q but the launch sets %q/%q — every package root would look unused",
			storeProfilesEnv, storeFHSProfilesEnv, entrypoint.StoreProfilesEnv, entrypoint.StoreFHSProfilesEnv)
	}
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// TestPruneRunsTheBuildStateSection is `yolo prune`'s call-site pin: the recovery tool
// runs the same build-state reapers as the slot. Fails if the section's call is deleted.
func TestPruneRunsTheBuildStateSection(t *testing.T) {
	o, gs := baseOpts(t)
	o.Now = time.Now
	o.Apply = true
	build := filepath.Join(gs, "build")
	month := 30 * 24 * time.Hour
	skopeo := mkTarget(t, t.TempDir(), "skopeo")
	outLink := mkLink(t, "/nix/store/aaaa-yolo-jail-install-prefix", filepath.Join(build, "jail-prefix-aaaaaaaaaaaaaaaa"), month)
	oldCopier := mkLink(t, skopeo, filepath.Join(build, "image-copier-1111111111111111"), month)
	newCopier := mkLink(t, skopeo, filepath.Join(build, "image-copier-2222222222222222"), time.Hour)
	oldRoot := mkLink(t, "/nix/store/b-yolo-image-extras", filepath.Join(build, "package-roots", "extras-1111111111111111"), month)
	newRoot := mkLink(t, "/nix/store/c-yolo-image-extras", filepath.Join(build, "package-roots", "extras-2222222222222222"), time.Hour)

	var buf strings.Builder
	o.Out = &buf
	if rc := Run(o); rc != 0 {
		t.Fatalf("rc=%d\n%s", rc, buf.String())
	}
	out := buf.String()
	for _, p := range []string{outLink, oldCopier, oldRoot} {
		if exists(p) {
			t.Errorf("%s survived `yolo prune --apply`:\n%s", p, out)
		}
	}
	if !exists(newCopier) || !exists(newRoot) {
		t.Error("prune removed the newest copier link or package root")
	}
	for _, want := range []string{"removed: 1 retired jail-prefix out-link(s)", "removed: 1 image-copier link(s)",
		"removed: 1 package-profile root(s)"} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q:\n%s", want, out)
		}
	}
}

// acDoc is Apple Container's `container inspect` document in the measured top-level shape
// (docs/plans/setup-support-gaps.md §5.1 row 5). The mount entry's field names are NOT
// measured on a Mac (disk-footprint.md BF-D4).
func acDoc(mountSrc, mountDest string, env ...string) string {
	envJSON := `"PATH=/bin"`
	for _, e := range env {
		envJSON += `,"` + e + `"`
	}
	mounts := ""
	if mountDest != "" {
		mounts = `{"source":"` + mountSrc + `","destination":"` + mountDest + `","options":["ro"]}`
	}
	return `[{"id":"yolo-a","status":{"state":"running"},"configuration":{"mounts":[` + mounts +
		`],"labels":{},"initProcess":{"environment":[` + envJSON + `]}}}]`
}

// TestAppleContainerLivenessIsAnswered: Apple Container's inspect takes no --format, so the
// liveness readers must read its whole document. Asked with --format, every pass declined on
// that runtime forever.
func TestAppleContainerLivenessIsAnswered(t *testing.T) {
	live := runtime.LiveSet{Known: true, Names: map[string]struct{}{"yolo-a": {}}}
	src := "/nix/store/aaaa-yolo-jail-install-prefix/" + image.JailPrefixSubdir + "/bin"
	run := func(argv []string, _ time.Duration) ProbeResult {
		if strings.Join(argv, " ") != "container inspect yolo-a" {
			return ProbeResult{Ran: true, RC: 64} // AC rejects --format
		}
		return ProbeResult{Ran: true, Stdout: acDoc(src, prefixBinMountDest, "YOLO_STORE_FHS_PROFILES=/nix/store/x-yolo-image-extras")}
	}
	sources, known, why := LivePrefixSources("container", live, prefixBinMountDest, run)
	if !known || !sources[src] {
		t.Fatalf("LivePrefixSources on Apple Container = %v, %v, %q; want the mounted prefix", sources, known, why)
	}
	profiles, known, pwhy := LiveStoreProfiles("container", live, run)
	if !known || !profiles["/nix/store/x-yolo-image-extras"] {
		t.Fatalf("LiveStoreProfiles on Apple Container = %v, %v, %q", profiles, known, pwhy)
	}

	// A container with no mount at the prefix destination answered: it holds no prefix.
	noMount := func([]string, time.Duration) ProbeResult { return ProbeResult{Ran: true, Stdout: acDoc("", "")} }
	if s, known, _ := LivePrefixSources("container", live, prefixBinMountDest, noMount); !known || len(s) != 0 {
		t.Fatalf("a container with no prefix mount: %v, %v; want known and empty", s, known)
	}
	// A document without a mounts array is not an answer.
	bad := func([]string, time.Duration) ProbeResult {
		return ProbeResult{Ran: true, Stdout: `[{"configuration":{}}]`}
	}
	if _, known, _ := LivePrefixSources("container", live, prefixBinMountDest, bad); known {
		t.Fatal("a document with no mounts array must read as could-not-ask")
	}
}

// TestImageCopierLinkReplacedMidPassIsKept: a launch that rebuilt the copier after the sweep
// judged its link replaced the link with a new inode; the removal compares identity first.
func TestImageCopierLinkReplacedMidPassIsKept(t *testing.T) {
	dir := t.TempDir()
	skopeo := mkTarget(t, t.TempDir(), "skopeo")
	_ = mkLink(t, skopeo, filepath.Join(dir, "image-copier-1111111111111111"), time.Hour)
	old := mkLink(t, skopeo, filepath.Join(dir, "image-copier-2222222222222222"), 30*24*time.Hour)
	rebuild := Guard(func(recheck func() bool, del func()) bool {
		// The launch's `nix build --out-link` renames a fresh link over the old one, keeping
		// the old mtime here so the recheck alone would still pass.
		tmp := old + ".tmp"
		mkLink(t, skopeo, tmp, 30*24*time.Hour)
		if err := os.Rename(tmp, old); err != nil {
			t.Fatal(err)
		}
		if !recheck() {
			return false
		}
		del()
		return true
	})
	if got := PruneImageCopierLinks(dir, BuildLinkRetention, true, time.Now(), rebuild); len(got) != 0 {
		t.Fatalf("removed %v; the link was replaced after it was judged", got)
	}
	if !exists(old) {
		t.Fatal("a copier link a launch just rewrote was removed")
	}
}
