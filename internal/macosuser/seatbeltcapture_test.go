package macosuser

import (
	"strings"
	"testing"
)

// seatbeltcapture_test.go pins the generated INSTALL-CAPTURE profile.
//
// What these tests measure and what they do not, stated once so nobody reads more into a green
// run than it carries: they measure the BYTES this repo emits — the clauses present, the clauses
// absent, and the ordering the last-match-wins policy depends on. They measure nothing about
// Apple Seatbelt. This profile HAS been kernel-loaded — capture.go records the run, on hardware
// 2026-09-11 — and its denials have a case of their own now, with no green run recorded:
// integration/macosusercaptureseatbelt_test.go's TestMacosUserCaptureSeatbeltProfileDeniesTheSharedHome,
// which loads a capture plan's profile as the sandbox account (macos-user.yml, on a Mac). A Linux
// jail cannot run sandbox-exec at all; what the case leaves to a human with a Mac is in
// docs/reference/install-capture.md's slice 6 hardware checklist.

const testStagingRoot = "/Users/Shared/yolo-captures/probetool"

// captureWriteAllowBlock returns the profile text from the `(allow file-write*` form to the
// blank line that ends it — the block whose contents ARE the capture's write policy.
func captureWriteAllowBlock(t *testing.T, profile string) string {
	t.Helper()
	start := strings.Index(profile, "(allow file-write*")
	if start < 0 {
		t.Fatalf("no write-allow block in profile:\n%s", profile)
	}
	rest := profile[start:]
	if end := strings.Index(rest, "\n\n"); end >= 0 {
		return rest[:end]
	}
	return rest
}

// The shared home is the whole point of the slice: a capture must not be able to write it.
func TestCaptureProfileDeniesWritesToTheSharedHome(t *testing.T) {
	p := SeatbeltCaptureProfile(testStagingRoot)
	if strings.Contains(captureWriteAllowBlock(t, p), SandboxHome()) {
		t.Errorf("the capture profile ALLOWS writes to the shared home %s — that is the "+
			"session profile's policy, not a capture's:\n%s", SandboxHome(), p)
	}
	deny := `(deny file-write* (subpath "` + SandboxHome() + `"))`
	if !contains(p, deny) {
		t.Errorf("capture profile missing %q:\n%s", deny, p)
	}
	// SBPL is last-match-wins, so a deny BEFORE the re-allow is not a deny.
	if idx(p, deny) <= idx(p, "(allow file-write*") {
		t.Error("the shared-home deny must FOLLOW the write-allow block, or the allow wins")
	}
}

// The read side is the other half: a capture that could READ /Users/_yolojail could read the
// machine's one credential store, and an installer is arbitrary vendor code.
func TestCaptureProfileDoesNotReAllowReadsOfTheSharedHome(t *testing.T) {
	p := SeatbeltCaptureProfile(testStagingRoot)
	usersDeny := `(deny file-read* (subpath "/Users"))`
	if !contains(p, usersDeny) {
		t.Fatalf("capture profile missing the /Users read deny:\n%s", p)
	}
	after := p[idx(p, usersDeny):]
	if strings.Contains(after, SandboxHome()) {
		t.Errorf("the capture profile re-allows reads under the shared home %s after the "+
			"/Users deny — a capture has no business reading the credential store:\n%s",
			SandboxHome(), p)
	}
}

// Exactly one project-specific writable subtree, plus the OS scratch dirs, and no workspace.
func TestCaptureProfileWritableSetIsTheStagingTreeAndScratchOnly(t *testing.T) {
	p := SeatbeltCaptureProfile(testStagingRoot)
	block := captureWriteAllowBlock(t, p)
	for _, want := range []string{
		`(subpath "` + testStagingRoot + `")`,
		`(subpath "/tmp")`,
		`(subpath "/private/tmp")`,
		`(subpath "/var/folders")`,
		`(subpath "/private/var/folders")`,
		`(subpath "/dev")`,
	} {
		if !strings.Contains(block, want) {
			t.Errorf("write-allow block missing %q:\n%s", want, block)
		}
	}
	// Six clauses and no more. A seventh is a widening nobody meant.
	if got := strings.Count(block, "(subpath "); got != 6 {
		t.Errorf("write-allow block has %d subpath clauses, want 6:\n%s", got, block)
	}
	if !contains(p, `(deny file-write* (subpath "/"))`) {
		t.Errorf("capture profile does not deny writes at / first:\n%s", p)
	}
	if idx(p, `(deny file-write* (subpath "/"))`) >= idx(p, "(allow file-write*") {
		t.Error("the root write deny must precede the re-allow")
	}
}

// The staging tree must be READABLE too, and every intermediate dir under /Users/Shared needs a
// traversal grant — the same gap that broke `git ls-files` for a nested workspace.
func TestCaptureProfileGrantsTraversalToTheStagingTree(t *testing.T) {
	p := SeatbeltCaptureProfile(testStagingRoot)
	for _, want := range []string{
		`(literal "/Users")`,
		`(literal "/Users/Shared")`,
		`(literal "/Users/Shared/yolo-captures")`,
		`(subpath "` + testStagingRoot + `")`,
	} {
		if !contains(p, want) {
			t.Errorf("capture profile missing read grant %q:\n%s", want, p)
		}
	}
	// The intermediate is traversal only — a subpath grant there would re-allow reads of
	// every OTHER program's capture staging tree beside this one.
	if contains(p, `(subpath "/Users/Shared/yolo-captures")`) {
		t.Error("the capture root is granted as a subpath — that exposes every other " +
			"program's staging tree to this installer")
	}
}

// A caller that lost its staging path must get a profile that cannot write, never one that can
// write everywhere. `(subpath "")` is a prefix match on every path.
func TestCaptureProfileWithNoStagingRootIsFailClosed(t *testing.T) {
	p := SeatbeltCaptureProfile("")
	if contains(p, `(subpath "")`) {
		t.Errorf("an empty staging root emitted `(subpath \"\")`, which matches every "+
			"path — the fail-closed case became fail-wide-open:\n%s", p)
	}
	block := captureWriteAllowBlock(t, p)
	if got := strings.Count(block, "(subpath "); got != 5 {
		t.Errorf("write-allow block has %d subpath clauses, want the 5 scratch dirs only:\n%s",
			got, block)
	}
}

// The same denies the session profile carries. A capture is MORE confined than a session, never
// less, so anything the session refuses this must refuse too.
func TestCaptureProfileKeepsTheSessionProfileDenies(t *testing.T) {
	p := SeatbeltCaptureProfile(testStagingRoot)
	for _, want := range []string{
		"(allow default)",
		`(deny file-read* (subpath "/Volumes"))`,
		`(allow file-read* (subpath "/Volumes/Macintosh HD"))`,
		`#"^/dev/r?disk"`,
		`#"^/private/dev/r?disk"`,
		`#"^/dev/bpf"`,
		`(deny file-read* (subpath "/Library/Keychains"))`,
		"(allow process-info*)",
		"(allow sysctl-read)",
	} {
		if !contains(p, want) {
			t.Errorf("capture profile is LESS confined than a session: missing %q:\n%s", want, p)
		}
	}
}

// SBPL escaping, same as the session profile — a staging root is a path yolo composed from a
// program name, but the escaping must not depend on that staying true.
func TestCaptureProfileEscapesTheStagingPath(t *testing.T) {
	p := SeatbeltCaptureProfile(`/Users/Shared/a"b\c`)
	if !contains(p, `\"`) || !contains(p, `\\`) {
		t.Errorf("SBPL escaping absent:\n%s", p)
	}
}

// THE SEALED CAPTURE PROFILE (FP-D24) is the capture profile with the seal after it: everything the
// capture profile pins still holds, and the loopback and the nix daemon are denied AFTER
// `(allow default)` — last match wins, so a deny before it would be none.
func TestSealedCaptureProfileDeniesTheLoopbackAndTheNixDaemonAfterAllowDefault(t *testing.T) {
	p := SeatbeltSealedCaptureProfile(testStagingRoot)
	if problems := captureProfileProblems(p); len(problems) > 0 {
		t.Errorf("the sealed profile breaks the capture profile's own rules: %v", problems)
	}
	if !strings.HasPrefix(p, SeatbeltCaptureProfile(testStagingRoot)) {
		t.Error("the sealed profile is not the capture profile with the seal appended")
	}
	allow := idx(p, "(allow default)")
	for _, deny := range []string{
		`(deny network-outbound (remote ip "localhost:*"))`,
		`(deny network-outbound (remote unix-socket (path-literal "/nix/var/nix/daemon-socket/socket")))`,
		`(deny file-read* file-write* (subpath "/nix/var/nix/daemon-socket"))`,
	} {
		if !contains(p, deny) {
			t.Errorf("the sealed profile lacks %q:\n%s", deny, p)
		} else if idx(p, deny) < allow {
			t.Errorf("%q precedes `(allow default)`, so it denies nothing", deny)
		}
	}
	if problems := sealedProfileProblems(p); len(problems) > 0 {
		t.Errorf("the sealed profile fails its own check: %v", problems)
	}
	// The network beyond the loopback stays: a build fetches its dependencies.
	if contains(p, `(deny network-outbound (remote ip "*:*"))`) || contains(p, "(deny network*") {
		t.Errorf("the sealed profile denies the network a build fetches from:\n%s", p)
	}
}

// The plain capture profile, an installer's, is NOT sealed: the seal is a fork build's alone (FP-D9
// scopes it to the fork route), and the check says so of it.
func TestTheInstallerCaptureProfileIsNotSealed(t *testing.T) {
	if problems := sealedProfileProblems(SeatbeltCaptureProfile(testStagingRoot)); len(problems) != 3 {
		t.Errorf("the installer's capture profile reads as sealed: %v", problems)
	}
}

// testSelfHomes is a run-as-you capture's homes for the invoking user /Users/you: the machine root
// and the shared store LaunchHomes places under that home, HOME the staging home.
func testSelfHomes() Homes {
	h := LaunchHomes(ModeSelf, testStagingRoot, "/Users/you")
	h.Home = CaptureStagingHome(testStagingRoot)
	return h
}

// THE RUN-AS-YOU CAPTURE'S "SHARED HOME" IS THE MACHINE ROOT AND THE SHARED STORE (slice 9 of
// docs/design/macos-user-model-plan.md): the sibling of TestCaptureProfileDeniesWritesToTheSharedHome
// for a capture that runs as you. Deny-by-default, so no allow grants either; and each is denied by
// name, for reads and writes, AFTER every allow, the statement of intent that survives a later edit
// widening the allow list. Fails if the builder takes its denied roots from SandboxHome() again, or
// grants the store as a machine dir the way a launch does.
func TestSelfModeCaptureProfileDeniesTheMachineRootAndSharedDirs(t *testing.T) {
	h := testSelfHomes()
	if h.Machine == "" || h.Shared == "" || h.Machine == SandboxHome() || h.Shared == SandboxHome() {
		t.Fatalf("the fixture's homes are not the run-as-you ones: %+v", h)
	}
	p := SeatbeltSelfCaptureProfile(testStagingRoot, h, "you", "/private/var/folders/ab/xyz/T/")
	if !contains(p, "\n(deny default)\n") || contains(p, "(allow default)") {
		t.Fatalf("the run-as-you capture profile is not deny-by-default:\n%s", p)
	}
	deny := "(deny file-read* file-write*\n" +
		"    (subpath " + sbplStr(h.Machine) + ")\n" +
		"    (subpath " + sbplStr(h.Shared) + "))\n"
	if !contains(p, deny) {
		t.Fatalf("the run-as-you capture profile does not deny the machine root and shared store:\n%s", p)
	}
	at := idx(p, deny)
	if last := strings.LastIndex(p, "(allow "); last > at {
		t.Errorf("an allow follows the store deny; SBPL is last-match-wins, so it could re-open it:\n%s", p[last:])
	}
	for _, root := range []string{h.Machine, h.Shared} {
		if n := strings.Count(p, sbplStr(root)); n != 1 {
			t.Errorf("%s is named %d times; the closing deny must be the only rule naming it:\n%s", root, n, p)
		}
	}
	if problems := selfCaptureProfileProblems(p, h); len(problems) > 0 {
		t.Errorf("the run-as-you capture profile fails its own check: %v", problems)
	}
	// The staging tree is the writable root; the account's home is no part of this profile.
	if !contains(p, `(allow file-write* (subpath "`+testStagingRoot+`"))`) {
		t.Errorf("the staging tree is not writable:\n%s", p)
	}
	if contains(p, SandboxHome()) {
		t.Errorf("the run-as-you capture profile names the account's home %s:\n%s", SandboxHome(), p)
	}
	// The deny carries its id, as every deny of the generated profiles does.
	requireEveryDenyCarriesATestID(t, "the run-as-you capture profile", p)
}

// The check fails for each way the store could be reachable: the account's profile, a profile that
// grants the store as a launch grants its machine dirs, a profile with no closing deny, and homes
// with no machine root.
func TestSelfModeCaptureProfileProblemsCatchAReachableStore(t *testing.T) {
	h := testSelfHomes()
	launchLike := DenyDefaultProfile(ProfileParams{Workspace: testStagingRoot,
		MachineDirs: []string{h.Machine, h.Shared}}.WithProductionRulings()) + captureStoreDeny(h)
	for name, tc := range map[string]struct {
		profile string
		homes   Homes
		want    string
	}{
		"account profile":  {SeatbeltCaptureProfile(testStagingRoot), h, "deny-by-default"},
		"store as a grant": {launchLike, h, "before its closing deny"},
		"no closing deny":  {DenyDefaultProfile(ProfileParams{Workspace: testStagingRoot}.WithProductionRulings()), h, "last"},
		"no machine root":  {SeatbeltSelfCaptureProfile(testStagingRoot, h, "you", ""), Homes{Shared: h.Shared}, "no absolute machine root"},
	} {
		problems := strings.Join(selfCaptureProfileProblems(tc.profile, tc.homes), "\n")
		if !strings.Contains(problems, tc.want) {
			t.Errorf("%s: no problem names %q: %s", name, tc.want, problems)
		}
	}
}

// A fork's build in the run-as-you mode carries the seal after the deny-default profile's own
// nix-daemon allow, which it narrows, and the store deny too.
func TestSealedSelfCaptureProfileClosesTheNixDaemonAfterItsAllow(t *testing.T) {
	h := testSelfHomes()
	p := SeatbeltSealedSelfCaptureProfile(testStagingRoot, h, "you", "")
	if problems := append(sealedProfileProblems(p), selfCaptureProfileProblems(p, h)...); len(problems) > 0 {
		t.Errorf("the sealed run-as-you profile fails its checks: %v", problems)
	}
	if strings.LastIndex(p, sealedBuildNixSocketDeny) < strings.LastIndex(p, "#seatbelt-test-id:nix-daemon-allow#") {
		t.Error("the seal's nix deny precedes the nix-daemon allow it must narrow")
	}
	// Unsealed, the nix allow stands, and the seal's check says so.
	if len(sealedProfileProblems(SeatbeltSelfCaptureProfile(testStagingRoot, h, "you", ""))) == 0 {
		t.Error("the unsealed run-as-you capture profile passes the seal check")
	}
}
