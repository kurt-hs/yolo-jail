package prune

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/image"
	"github.com/mschulkind-oss/yolo-jail/internal/runtime"
	"golang.org/x/sys/unix"
)

func mkPrefixRoot(t *testing.T, dir, storePath string, age time.Duration) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, image.ImageStoreKey(storePath))
	if err := os.Symlink(storePath, link); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	tv := []unix.Timeval{unix.NsecToTimeval(when.UnixNano()), unix.NsecToTimeval(when.UnixNano())}
	if err := unix.Lutimes(link, tv); err != nil {
		t.Fatal(err)
	}
	return link
}

// TestPrefixRootsAreHeldByLivenessNotAge is the ruling, stated as the case that
// separates this pass from its neighbour: a jail that has been running for a
// MONTH keeps its prefix root, where an image root of the same age is reaped.
// Getting this backwards costs a running process the file behind its own pid1.
func TestPrefixRootsAreHeldByLivenessNotAge(t *testing.T) {
	dir := t.TempDir()
	livePath := "/nix/store/aaaa-yolo-jail-install-prefix"
	deadPath := "/nix/store/bbbb-yolo-jail-install-prefix"
	liveLink := mkPrefixRoot(t, dir, livePath, 30*24*time.Hour)
	deadLink := mkPrefixRoot(t, dir, deadPath, 30*24*time.Hour)

	sources := map[string]bool{livePath + "/" + image.JailPrefixSubdir + "/bin": true}
	reaped := PruneOrphanPrefixRoots(dir, sources, true, true, time.Now())

	if len(reaped) != 1 || reaped[0] != deadLink {
		t.Fatalf("reaped %v, want exactly the root no jail is running from (%s)", reaped, deadLink)
	}
	if _, err := os.Lstat(liveLink); err != nil {
		t.Error("a running jail's prefix root was reaped — age must NOT apply here: the jail is " +
			"executing pid1 out of that store path, so losing it is not a rebuild (OQ-BF4). " +
			"OQ-LS1's age policy is for image closures only.")
	}
}

// TestPrefixRootsDeclineWhenLivenessUnknown: this pass HAS an authority, so it
// keeps the tri-state OQ-LS1 removed from the image roots. Unknown is not
// permission when the cost of being wrong is a dead jail.
func TestPrefixRootsDeclineWhenLivenessUnknown(t *testing.T) {
	dir := t.TempDir()
	link := mkPrefixRoot(t, dir, "/nix/store/cccc-yolo-jail-install-prefix", 30*24*time.Hour)
	if reaped := PruneOrphanPrefixRoots(dir, nil, false, true, time.Now()); len(reaped) != 0 {
		t.Fatalf("reaped %v with liveness unknown, want none", reaped)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Error("a root was removed while liveness was unknown")
	}
}

// TestPrefixRootGraceCoversTheRegistrationWindow: the root is registered in
// resolveJailPrefix, an entire image build BEFORE any container exists, so a
// concurrent pass would otherwise reap a root that is about to become live.
func TestPrefixRootGraceCoversTheRegistrationWindow(t *testing.T) {
	dir := t.TempDir()
	link := mkPrefixRoot(t, dir, "/nix/store/dddd-yolo-jail-install-prefix", time.Minute)
	if reaped := PruneOrphanPrefixRoots(dir, map[string]bool{}, true, true, time.Now()); len(reaped) != 0 {
		t.Fatalf("reaped %v — a root registered a minute ago has no container yet by construction", reaped)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Error("the grace window must spare a just-registered root")
	}
}

// TestPrefixStorePathOfIgnoresABundleMount: a launch running from an installed
// bundle mounts bin/linux-<arch> from the bundle, not from a store path, so
// there is no root to correlate and it must not be mistaken for one.
func TestPrefixStorePathOfIgnoresABundleMount(t *testing.T) {
	store := "/nix/store/eeee-yolo-jail-install-prefix"
	if got := PrefixStorePathOf(store + "/" + image.JailPrefixSubdir + "/bin"); got != store {
		t.Errorf("built prefix: got %q, want %q", got, store)
	}
	for _, notAPrefix := range []string{
		"/home/me/.local/share/yolo-jail/flake-bundle/bin/linux-amd64",
		"/nix/store/ffff-something-else",
		"",
	} {
		if got := PrefixStorePathOf(notAPrefix); got != "" {
			t.Errorf("PrefixStorePathOf(%q) = %q, want \"\"", notAPrefix, got)
		}
	}
}

// TestPrefixBinMountDestMatchesTheLauncher pins the one string this package
// shares with the run pipeline without importing it. If the launcher ever mounts
// the prefix somewhere else, this pass silently finds no live sources and reaps
// every prefix root — including the one the jail reading this is running from.
func TestPrefixBinMountDestMatchesTheLauncher(t *testing.T) {
	src, err := os.ReadFile("../cli/run/jailprefix.go")
	if err != nil {
		t.Fatalf("read jailprefix.go: %v", err)
	}
	want := `JailPrefixBinDir = JailPrefixDir + "/bin"`
	if !strings.Contains(string(src), want) || prefixBinMountDest != "/opt/yolo-jail/bin" {
		t.Fatalf("the prefix bin mount destination moved: prune has %q and jailprefix.go no longer "+
			"spells %s. These are two copies of one path; a silent divergence makes every prefix "+
			"root look orphaned.", prefixBinMountDest, want)
	}
}

// mountsJSON renders a podman `inspect --format '{{json .Mounts}}'` payload for
// a container binding `src` at `dest`. The shape is MEASURED, not invented: it
// is what podman 5.x printed on this repo's own jail on 2026-09-13, fields and
// all.
func mountsJSON(src, dest string) string {
	return `[{"Type":"bind","Source":"` + src + `","Destination":"` + dest +
		`","Driver":"","Mode":"","Options":["nosuid","nodev","rbind"],"RW":false,` +
		`"Propagation":"rprivate"}]`
}

// prefixRun builds a RunFunc that answers `inspect --format {{json .Mounts}}`
// per container name from `mounts`, and fails any name mapped to a nil entry the
// way podman fails an absent one (rc=125). An unmapped name is a test bug and is
// reported as one.
func prefixRun(t *testing.T, mounts map[string]string) RunFunc {
	t.Helper()
	return func(argv []string, _ time.Duration) ProbeResult {
		if len(argv) != 5 || argv[1] != "inspect" {
			t.Fatalf("unexpected argv %v", argv)
		}
		out, ok := mounts[argv[4]]
		if !ok {
			t.Fatalf("test fixture has no mounts entry for %q", argv[4])
		}
		if out == "" {
			// `podman inspect` on a name that no longer exists (MEASURED):
			// `Error: no such object: "..."`, rc=125.
			return ProbeResult{Ran: true, RC: 125}
		}
		return ProbeResult{Ran: true, RC: 0, Stdout: out}
	}
}

func liveSet(names ...string) runtime.LiveSet {
	set := map[string]struct{}{}
	for _, n := range names {
		set[n] = struct{}{}
	}
	return runtime.LiveSet{Known: true, Names: set}
}

// TestALiveContainerWithNoMountsIsAnAnswerNotAFailure is the defect, in the
// exact shape it was measured in.
//
// `yolo-linux-builder` (internal/containerbuilder) wears the `yolo-` prefix
// ParsePodmanLive filters on, so it lands in the live set beside every real
// jail — and it mounts NOTHING, for which podman prints `[]`: a well-formed
// empty array, rc=0. While "no mount at dest" and "could not inspect" were the
// same return value, that one container made the whole answer unusable, and both
// the prefix-root sweep and the superseded-store-output sweep skipped forever.
// On the maintainer's host that was 36.4 GiB across 572 store paths nothing
// could reclaim.
//
// The assertion is that the REAL jail's prefix is still found: the builder
// contributes nothing and vetoes nothing.
func TestALiveContainerWithNoMountsIsAnAnswerNotAFailure(t *testing.T) {
	const prefixSrc = "/nix/store/aaaa-yolo-jail-install-prefix/" + image.JailPrefixSubdir + "/bin"
	run := prefixRun(t, map[string]string{
		"yolo-linux-builder": "[]", // MEASURED: podman's answer for no mounts
		"yolo-ws-deadbeef":   mountsJSON(prefixSrc, prefixBinMountDest),
	})

	sources, known, why := LivePrefixSources("podman",
		liveSet("yolo-linux-builder", "yolo-ws-deadbeef"), prefixBinMountDest, run)

	if !known {
		t.Fatalf("declined with %q — a container that ANSWERED `[]` has told yolo it mounts "+
			"nothing, which is a fact about that container, not missing evidence. Reading it as "+
			"a failure is what disabled both prefix reapers on every machine running the Linux "+
			"builder (or any jail older than the mounted prefix).", why)
	}
	if why != "" {
		t.Errorf("known answer carried decline reason %q, want empty", why)
	}
	if len(sources) != 1 || !sources[prefixSrc] {
		t.Fatalf("sources = %v, want exactly the real jail's prefix %q", sources, prefixSrc)
	}
}

// TestOneUninspectableContainerStillDeclinesEverything: the all-or-nothing rule
// SURVIVES the split above, and this is the case it was written for. A container
// that vanished between enumeration and inspect answers rc=125 — yolo genuinely
// does not know what it was mounting, and a root it needed might be the one that
// cannot be seen. Widening "no mount" to a known answer must not widen this.
func TestOneUninspectableContainerStillDeclinesEverything(t *testing.T) {
	const prefixSrc = "/nix/store/bbbb-yolo-jail-install-prefix/" + image.JailPrefixSubdir + "/bin"
	run := prefixRun(t, map[string]string{
		"yolo-ws-00000000": mountsJSON(prefixSrc, prefixBinMountDest),
		"yolo-ws-11111111": "", // rc=125, `no such object`
	})

	sources, known, why := LivePrefixSources("podman",
		liveSet("yolo-ws-00000000", "yolo-ws-11111111"), prefixBinMountDest, run)

	if known {
		t.Fatalf("answered %v with one container uninspectable — a root that container needs "+
			"might be the one yolo cannot see, and the caller deletes store paths with this", sources)
	}
	if sources != nil {
		t.Errorf("sources = %v on a decline, want nil (a partial answer invites partial deletion)", sources)
	}
	if !strings.Contains(string(why), "yolo-ws-11111111") {
		t.Errorf("decline reason %q does not name the container that failed. Naming it is the "+
			"whole point: the undifferentiated message hid this fault for weeks.", why)
	}
}

// TestAnUnrecognisedInspectShapeIsNotNoMounts guards the widening from becoming
// a sweep on a runtime nothing here has measured.
//
// The concrete worry is Apple Container, whose `inspect` this repo always calls
// WITHOUT `--format` and whose output runtime.WorkspaceFromContainerInspectJSON
// documents as a container document ("a single object or a list"). What AC does
// when handed podman's `--format` is not measured here — but if it ignores the
// flag, that payload decodes to a JSON array perfectly well, and a naive "it
// parsed, therefore there is no mount at dest" would answer known=true for every
// AC container and reap every prefix root on the machine. `null` is the same
// class. Neither is an answer.
func TestAnUnrecognisedInspectShapeIsNotNoMounts(t *testing.T) {
	for name, payload := range map[string]string{
		"apple-container document list": `[{"status":"running","configuration":{"id":"yolo-x"}}]`,
		"null":                          `null`,
		"an object":                     `{"Mounts":[]}`,
		"not json":                      `Error: unknown flag: --format`,
	} {
		run := prefixRun(t, map[string]string{"yolo-ws-22222222": payload})
		_, known, why := LivePrefixSources("podman", liveSet("yolo-ws-22222222"), prefixBinMountDest, run)
		if known {
			t.Errorf("%s: read as a known answer. Only a payload recognisable as podman's mounts "+
				"array (an array of objects each carrying a string Destination, possibly empty) "+
				"may be read as 'no mount at dest'; anything else is 'could not ask'.", name)
		}
		if !strings.Contains(string(why), "yolo-ws-22222222") {
			t.Errorf("%s: reason %q does not name the container", name, why)
		}
	}
}

// TestUnenumerableRuntimeDeclinesAndSaysSo: the other decline, and it must be
// DISTINGUISHABLE from the per-container one — a reader's next move differs
// (a machine problem vs. one odd container), which is exactly what the single
// undifferentiated message took away.
func TestUnenumerableRuntimeDeclinesAndSaysSo(t *testing.T) {
	run := func([]string, time.Duration) ProbeResult {
		t.Fatal("must not inspect anything when the live set is unknown")
		return ProbeResult{}
	}
	sources, known, why := LivePrefixSources("podman", runtime.LiveSet{Known: false}, prefixBinMountDest, run)
	if known || sources != nil {
		t.Fatalf("known=%v sources=%v, want a decline", known, sources)
	}
	if !strings.Contains(string(why), "enumerate") {
		t.Errorf("reason %q should say the ENUMERATION failed", why)
	}
	if strings.Contains(string(why), "mounts") {
		t.Errorf("reason %q reads like the per-container failure; the two must not be confusable", why)
	}
}

// TestNoLiveContainersIsAKnownEmptyAnswer: zero live jails is not an unknown.
// Nothing is executing from any prefix, so every root is orphaned and the sweep
// is entitled to run. (The grace window, not this, is what protects a root
// registered moments ago — see TestPrefixRootGraceCoversTheRegistrationWindow.)
func TestNoLiveContainersIsAKnownEmptyAnswer(t *testing.T) {
	run := func([]string, time.Duration) ProbeResult {
		t.Fatal("nothing to inspect")
		return ProbeResult{}
	}
	sources, known, why := LivePrefixSources("podman", liveSet(), prefixBinMountDest, run)
	if !known || len(sources) != 0 || why != "" {
		t.Fatalf("known=%v sources=%v why=%q, want a known empty answer", known, sources, why)
	}
}
