package cli

// hostfloorpatchedmac_test.go pins the Mac host floor's PATCHED fork at the floor's two advance call
// sites (docs/reference/patched-forks.md PF-D97, forked-programs-as-packs.md FP-D25): on a Mac the
// floor's advance — floorAdvance, and floorResolvePatched's — runs under the macos-user runtime, so
// its build is the sealed macos-user fork-build act's (macosuser.RunForkBuildAct, FP-D24), and names
// the darwin platform a plain fork's floor build is filed under (floorForkBuild), so the floor's
// lookups and its builds name one platform. On Linux both stay what they were: the host's platform,
// and no runtime, the advance's capture jail resolving its own.

import (
	"context"
	"io"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/hostfloor"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// onFloorPlatform makes the floor's capture identity platform for this test: the one its fork
// builds are filed under (floorForkBuild) and its patched advance is handed (floorPatchedPlatform).
func onFloorPlatform(t *testing.T, platform string) {
	t.Helper()
	orig := hostFloorCapturePlatform
	hostFloorCapturePlatform = func() string { return platform }
	t.Cleanup(func() { hostFloorCapturePlatform = orig })
}

// macPatchedProgram is a patched fork's floor program; its series is unread here.
func macPatchedProgram() hostfloor.Program {
	return hostfloor.Program{Pack: "basepack", Install: packdecl.Install{Kind: packdecl.InstallKindSource, Bin: "tool",
		ForkedBy: "forkpack", Source: "git+https://example.invalid/tool?ref=main", Build: "sh build.sh",
		Produces: []string{".npm-global/bin/tool"}, Patches: "patches", ForkRoot: unreadForkRoot}}
}

// unreadForkRoot is a fork root no series is read from in these tests.
const unreadForkRoot = "/nonexistent/forkpack"

// THE FLOOR'S ADVANCE ON A MAC RUNS UNDER MACOS-USER, for the darwin platform the floor files its
// fork builds under; on Linux under no runtime, for the host's. Drop the runtime from
// floorPatchedOptions, or have floorAdvance build its own options, and the Mac case fails.
func TestTheFloorsPatchedAdvanceOnAMacRunsAsTheSandboxAccount(t *testing.T) {
	for _, c := range []struct{ platform, runtime string }{
		{"darwin/arm64", "macos-user"},
		{"linux/amd64", ""},
	} {
		onFloorPlatform(t, c.platform)
		var got []advanceOptions
		orig := floorAdvancePatchedFork
		floorAdvancePatchedFork = func(_ packload.Fork, o advanceOptions) advanceResult {
			got = append(got, o)
			return advanceResult{}
		}
		floorAdvance(context.Background(), floorForkBuild(macPatchedProgram(), "").Fork, io.Discard, nil, nil)
		floorAdvancePatchedFork = orig
		if len(got) != 1 {
			t.Fatalf("on %s: %d advances, want one", c.platform, len(got))
		}
		o := got[0]
		if o.runtime != c.runtime || o.platform != c.platform || !o.launch || !o.host {
			t.Errorf("on %s the floor's advance runs under runtime %q for platform %q (launch=%v host=%v), "+
				"want runtime %q for %s, a launch at the host", c.platform, o.runtime, o.platform, o.launch, o.host,
				c.runtime, c.platform)
		}
		if b := floorForkBuild(macPatchedProgram(), ""); b.Platform != o.platform {
			t.Errorf("on %s the floor files a fork's build under %s but advances for %s", c.platform, b.Platform,
				o.platform)
		}
	}
}

// THE FLOOR'S PREPARATION READS THE ADVANCE IT WOULD RUN under the same runtime and platform: on a
// Mac macos-user and darwin, so the advance it opens (newAdvance) names the build a Mac makes.
// Revert floorResolvePatched to its own options and the Mac case fails.
func TestTheFloorsPatchedPreparationOnAMacOpensTheSandboxAccountsAdvance(t *testing.T) {
	for _, c := range []struct{ platform, runtime string }{
		{"darwin/arm64", "macos-user"},
		{"linux/amd64", ""},
	} {
		onFloorPlatform(t, c.platform)
		var got []advanceOptions
		orig := floorNewAdvance
		floorNewAdvance = func(_ packload.Fork, o advanceOptions) (*advance, *advanceResult) {
			got = append(got, o)
			return nil, &advanceResult{problem: "stopped by the test"}
		}
		state := floorResolvePatched(context.Background(), macPatchedProgram(), io.Discard, nil, true)
		floorNewAdvance = orig
		if len(got) != 1 {
			t.Fatalf("on %s: %d advances opened, want one", c.platform, len(got))
		}
		o := got[0]
		if o.runtime != c.runtime || o.platform != c.platform || !o.launch || !o.host || !o.readOnlyInitialization {
			t.Errorf("on %s the floor's preparation opens an advance under runtime %q for platform %q "+
				"(launch=%v host=%v readOnly=%v), want runtime %q for %s", c.platform, o.runtime, o.platform,
				o.launch, o.host, o.readOnlyInitialization, c.runtime, c.platform)
		}
		if state.Reason != "stopped by the test" {
			t.Errorf("on %s the early stop's reason was not kept: %q", c.platform, state.Reason)
		}
	}
}
