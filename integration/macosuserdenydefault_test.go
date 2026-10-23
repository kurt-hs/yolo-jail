package integration

import (
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/macosuser"
)

// THE DENY-BY-DEFAULT PROFILE'S REGISTRY (docs/design/macos-user-model-plan.md slice 4).
//
// The same convention as macosuserseatbelt_test.go's seatbeltRules, for the profile
// macosuser.DenyDefaultProfile renders: every #seatbelt-test-id: it carries is named here,
// with either the runtime cases that prove it or a written reason none does yet. Its own
// registry, not more entries in seatbeltRules, because slice 13 deletes the old profile and
// its registry whole (the plan's Traps).
//
// The old profile's ids that this one keeps appear in both registries: the same id on a rule
// with the same effect. The ids the old profile has and this one does not are SUBSUMED by
// deny-default (write-outside-deny, volumes-read-deny, users-read-deny, file-ioctl-deny) or
// RETIRED (boot-volume-read-allow); the plan's Test-id ledger records each.
//
// Slice 10 ports the prototype runbook's checks into runtime cases here, each with its bare
// control, and clears the unproven reasons below as it does.

// denyDefaultUnproven is the reason every rule carries until slice 10.
const denyDefaultUnproven = "runtime case lands in slice 10"

// denyDefaultRules must name every id the deny-default profile carries, and only those.
var denyDefaultRules = []seatbeltRule{
	// The posture, and the allow list (the prototype's blocks, in order).
	{id: "deny-default", unproven: denyDefaultUnproven},
	{id: "system-read-allow", unproven: denyDefaultUnproven},
	{id: "host-software-hide-deny", unproven: denyDefaultUnproven},
	{id: "host-software-read-allow", unproven: denyDefaultUnproven},
	{id: "host-software-metadata-allow", unproven: denyDefaultUnproven},
	{id: "host-preferences-read-allow", unproven: denyDefaultUnproven},
	{id: "apple-toolchain-read-allow", unproven: denyDefaultUnproven},
	{id: "ancestor-literal-allow", unproven: denyDefaultUnproven},
	{id: "workspace-read-allow", unproven: denyDefaultUnproven},
	{id: "workspace-write-allow", unproven: denyDefaultUnproven},
	{id: "home-allow", unproven: denyDefaultUnproven},
	{id: "tmp-allow", unproven: denyDefaultUnproven},
	{id: "shared-tmp-allow", unproven: denyDefaultUnproven},
	{id: "machine-dir-allow", unproven: denyDefaultUnproven},
	{id: "dev-io-allow", unproven: denyDefaultUnproven},
	{id: "device-ioctl-allow", unproven: denyDefaultUnproven},
	{id: "context-write-allow", unproven: denyDefaultUnproven},
	{id: "cache-relocation-write-allow", unproven: denyDefaultUnproven},
	// The write carve-outs, after every write allow.
	{id: "workspace-readonly-deny", unproven: denyDefaultUnproven},
	{id: "home-content-write-deny", unproven: denyDefaultUnproven},
	{id: "home-content-anchor-deny", unproven: denyDefaultUnproven},
	{id: "context-readonly-deny", unproven: denyDefaultUnproven},
	{id: "file-ioctl-tty-allow", unproven: denyDefaultUnproven},
	{id: "staging-root-read-allow", unproven: denyDefaultUnproven},
	{id: "context-read-allow", unproven: denyDefaultUnproven},
	{id: "cache-relocation-read-allow", unproven: denyDefaultUnproven},
	// Processes, services, network.
	{id: "process-allow", unproven: denyDefaultUnproven},
	{id: "cross-process-procargs-deny", unproven: denyDefaultUnproven},
	{id: "cross-process-pidinfo-deny", unproven: denyDefaultUnproven},
	{id: "same-sandbox-pidinfo-allow", unproven: denyDefaultUnproven},
	{id: "mach-lookup-allow", unproven: denyDefaultUnproven},
	{id: "network-allow", unproven: denyDefaultUnproven},
	{id: "loopback-deny", unproven: denyDefaultUnproven},
	{id: "loopback-port-allow", unproven: denyDefaultUnproven},
	// The closing block of explicit denies.
	{id: "host-secrets-deny", unproven: denyDefaultUnproven},
	{id: "library-keychains-deny", unproven: denyDefaultUnproven},
	{id: "system-keychains-deny", unproven: denyDefaultUnproven},
	{id: "raw-device-deny", unproven: denyDefaultUnproven},
	{id: "macos-log-deny", unproven: denyDefaultUnproven},
	{id: "launchd-listener-deny", unproven: denyDefaultUnproven},
	{id: "ssh-agent-socket-deny", unproven: denyDefaultUnproven},
	{id: "container-socket-deny", unproven: denyDefaultUnproven},
	{id: "container-socket-connect-deny", unproven: denyDefaultUnproven},
	{id: "service-deny", unproven: denyDefaultUnproven},
	{id: "appleevent-send-deny", unproven: denyDefaultUnproven},
}

// denyDefaultCaseIDs is the rule id of every runtime case against the deny-default profile.
// Empty until slice 10 ports the runbook's checks.
func denyDefaultCaseIDs() map[string]int { return map[string]int{} }

// denyDefaultFixtureParams sets every optional input, so every conditional rule renders.
func denyDefaultFixtureParams() macosuser.ProfileParams {
	ws := "/Users/Shared/proj"
	home := ws + "/.yolo/home"
	return macosuser.ProfileParams{
		Workspace:         ws,
		Home:              home,
		HostHome:          "/Users/you",
		SandboxTmp:        "/private/var/folders/ab/xyz/T/yolo-proj",
		DarwinUserTempDir: "/private/var/folders/ab/xyz/T",
		StagingRoot:       macosuser.StagingRoot(),
		MachineDirs:       []string{"/Users/you/.local/share/yolo-jail/macos-user/.cache"},
		Context: []macosuser.ContextLink{
			{Dest: "/ctx/lib", Source: "/Users/Shared/ci/lib", Dir: true},
			{Dest: "/ctx/data", Source: "/Users/Shared/yolo/data", RW: true, Dir: true},
		},
		Relocations:  []macosuser.CacheRelocation{{Subdir: "huggingface", Target: "/Volumes/Data/hf"}},
		ReadonlyRels: []string{"vendored"},
		HomeReadonly: macosuser.ResolveHomeReadonly(home, ws,
			[]string{".claude"}, []string{".claude/skills", ".claude/CLAUDE.md"}),
		Devices:      seatbeltFixtureDevices,
		RunAsUser:    "you",
		Loopback:     macosuser.LoopbackPolicy{Ports: []int{31053}},
		Tmp:          macosuser.TmpShared,
		HostSoftware: []string{"/opt/homebrew", "/usr/local"},
	}
}

// TestMacosUserDenyDefaultRegistryMatchesTheProfile mirrors
// TestMacosUserSeatbeltRegistryMatchesTheProfile for the deny-default profile, in both
// directions, on Linux under -short. It renders the profile twice, once under each value of
// every pending ruling (loopback denied and allowed, /tmp shared and private, host software
// visible and hidden), and takes the union of the ids, since each ruling renders rules the
// other omits.
func TestMacosUserDenyDefaultRegistryMatchesTheProfile(t *testing.T) {
	prototype := denyDefaultFixtureParams()
	alternate := denyDefaultFixtureParams()
	alternate.Loopback = macosuser.LoopbackPolicy{AllowAll: true}
	alternate.Tmp = macosuser.TmpPrivate
	alternate.HostSoftware = nil

	inProfile := map[string]bool{}
	for _, pp := range []macosuser.ProfileParams{prototype, alternate} {
		for _, m := range seatbeltIDPattern.FindAllStringSubmatch(macosuser.DenyDefaultProfile(pp), -1) {
			inProfile[m[1]] = true
		}
	}
	if len(inProfile) == 0 {
		t.Fatal("the deny-default profile carries no #seatbelt-test-id:…# markers at all")
	}

	registered := map[string]seatbeltRule{}
	for _, r := range denyDefaultRules {
		if _, dup := registered[r.id]; dup {
			t.Errorf("rule %q is registered twice", r.id)
		}
		registered[r.id] = r
		if !inProfile[r.id] {
			t.Errorf("rule %q is registered here but is in neither rendering of the deny-default "+
				"profile; either the directive was deleted and this entry outlived it, or the id was "+
				"renamed on one side only", r.id)
		}
	}
	for id := range inProfile {
		if _, ok := registered[id]; !ok {
			t.Errorf("the deny-default profile carries #seatbelt-test-id:%s# and nothing here claims "+
				"it. Add a denyDefaultRules entry, with a runtime case or the reason none proves it yet.", id)
		}
	}

	proved := denyDefaultCaseIDs()
	for id := range proved {
		if _, ok := registered[id]; !ok {
			t.Errorf("a case names rule %q, which is in no registry entry", id)
		}
	}
	for _, r := range denyDefaultRules {
		switch {
		case r.unproven == "" && proved[r.id] == 0:
			t.Errorf("rule %q claims to be proved at runtime and no case carries its id", r.id)
		case r.unproven != "" && proved[r.id] > 0:
			t.Errorf("rule %q is registered as unproven and %d case(s) carry its id; delete "+
				"the reason or delete the cases, but the file must not say both", r.id, proved[r.id])
		}
	}
}
