package run

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/ctxinject"
	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
)

// ctxDriveFixture is goldenOptions with an Exec that answers the driver's three calls: the pid
// inspect, `podman info` (when the launch's facts are absent), and the verb itself.
type ctxDriveFixture struct {
	o        *Options
	calls    [][]string
	timeouts []time.Duration
	info     string // `podman info --format json`'s answer; "" = it fails
	verbRC   int
	verbErr  string
	verbHang bool
	identity ctxinject.ProcessIdentity
}

func newCtxDriveFixture(t *testing.T) *ctxDriveFixture {
	t.Helper()
	f := &ctxDriveFixture{o: goldenOptions("/ws", t.TempDir()), identity: ctxinject.ProcessIdentity{
		StartTime: 11, MountNSDevice: 22, MountNSInode: 33,
	}}
	swapSeam(t, &ctxProcessIdentitySeam, func(int) (ctxinject.ProcessIdentity, error) {
		return f.identity, nil
	})
	f.o.Exec = func(argv []string, _ string, _ []string, timeout time.Duration) ExecResult {
		f.calls = append(f.calls, argv)
		f.timeouts = append(f.timeouts, timeout)
		switch {
		case slices.Equal(argv, []string{"podman", "inspect", "--format", "{{.State.Pid}}", "yolo-ws-abcd1234"}):
			return ExecResult{Ran: true, Stdout: "4242\n"}
		case slices.Equal(argv, []string{"podman", "info", "--format", "json"}):
			if f.info == "" {
				return ExecResult{Ran: true, RC: 125, Stderr: "cannot connect"}
			}
			return ExecResult{Ran: true, Stdout: f.info}
		case slices.Contains(argv, ctxAttachVerbName):
			if f.verbHang {
				return ExecResult{Ran: true, Timeout: true}
			}
			return ExecResult{Ran: true, RC: f.verbRC, Stderr: f.verbErr}
		}
		t.Errorf("unexpected exec %v", argv)
		return ExecResult{}
	}
	return f
}

const (
	rootlessInfo = `{"host":{"security":{"rootless":true}}}`
	rootfulInfo  = `{"host":{"security":{"rootless":false}}}`
)

// verbCall returns the call that ran the verb, or nil.
func (f *ctxDriveFixture) verbCall() ([]string, time.Duration) {
	for i, c := range f.calls {
		if slices.Contains(c, ctxAttachVerbName) {
			return c, f.timeouts[i]
		}
	}
	return nil, 0
}

// TestInjectCtxMountRunsTheVerbUnderPodmanUnshareOnRootless: a rootless podman's jail lives in a
// child of podman's user namespace, so the verb runs inside `podman unshare` (design §5.3), with
// the jail's pid from inspect and the 10 s deadline.
func TestInjectCtxMountRunsTheVerbUnderPodmanUnshareOnRootless(t *testing.T) {
	f := newCtxDriveFixture(t)
	f.o.acceptPodmanFacts(rootlessInfo)
	if err := f.o.injectCtxMount("podman", "yolo-ws-abcd1234", ctxWant("/h/src", "/ctx/src", false), time.Time{}); err != nil {
		t.Fatal(err)
	}
	argv, timeout := f.verbCall()
	if len(argv) < 4 || !slices.Equal(argv[:3], []string{"podman", "unshare", "--"}) {
		t.Fatalf("verb argv %v; want it under podman unshare --", argv)
	}
	tail := argv[4:]
	want := []string{"internal", "ctx-attach", "--pid", "4242", "--start-time", "11", "--mount-ns-device", "22", "--mount-ns-inode", "33", "--source", "/h/src", "--dest", "/ctx/src"}
	if !slices.Equal(tail, want) {
		t.Errorf("verb args %v; want %v", tail, want)
	}
	if timeout != ctxInjectDeadline || ctxInjectDeadline != 10*time.Second {
		t.Errorf("verb deadline %v (constant %v); want 10s", timeout, ctxInjectDeadline)
	}
}

// TestInjectCtxMountRunsTheVerbDirectlyWhenNestedOrRootful: a nested launch (podman with
// --userns host) and a rootful podman driven as root run the verb with no prefix; --rw passes.
func TestInjectCtxMountRunsTheVerbDirectlyWhenNestedOrRootful(t *testing.T) {
	cases := map[string]func(f *ctxDriveFixture){
		"nested": func(f *ctxDriveFixture) {
			f.o.PathExists = func(p string) bool { return p == "/run/.containerenv" }
			f.o.acceptPodmanFacts(rootfulInfo)
		},
		"rootful as root": func(f *ctxDriveFixture) {
			f.o.acceptPodmanFacts(rootfulInfo)
			restore := ctxGeteuid
			ctxGeteuid = func() int { return 0 }
			t.Cleanup(func() { ctxGeteuid = restore })
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			f := newCtxDriveFixture(t)
			setup(f)
			if err := f.o.injectCtxMount("podman", "yolo-ws-abcd1234", ctxWant("/h/src", "/ctx/src", true), time.Time{}); err != nil {
				t.Fatal(err)
			}
			argv, _ := f.verbCall()
			if len(argv) == 0 || argv[0] == "podman" {
				t.Fatalf("verb argv %v; want the verb run directly", argv)
			}
			if !slices.Equal(argv[1:], []string{"internal", "ctx-attach", "--pid", "4242", "--start-time", "11", "--mount-ns-device", "22", "--mount-ns-inode", "33", "--source", "/h/src", "--dest", "/ctx/src", "--rw"}) {
				t.Errorf("verb args %v", argv[1:])
			}
		})
	}
}

// TestRemoveCtxMountRunsTheRemoval: removal is the same verb with --remove and no source.
func TestRemoveCtxMountRunsTheRemoval(t *testing.T) {
	f := newCtxDriveFixture(t)
	f.o.acceptPodmanFacts(rootlessInfo)
	if err := f.o.removeCtxMount("podman", "yolo-ws-abcd1234", ctxMountEntry{Source: "/h/src", Dest: "/ctx/src"}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	argv, _ := f.verbCall()
	if !slices.Equal(argv[4:], []string{"internal", "ctx-attach", "--pid", "4242", "--start-time", "11", "--mount-ns-device", "22", "--mount-ns-inode", "33", "--dest", "/ctx/src", "--remove"}) {
		t.Errorf("verb args %v", argv)
	}
}

// TestInjectCtxMountReadsPodmanInfoWhenTheLaunchHasNoFacts: `yolo config apply` runs no launch
// gate, so the driver asks podman once, BEFORE the verb, and never decides by retrying a failure.
func TestInjectCtxMountReadsPodmanInfoWhenTheLaunchHasNoFacts(t *testing.T) {
	f := newCtxDriveFixture(t)
	f.info = rootlessInfo
	if err := f.o.injectCtxMount("podman", "yolo-ws-abcd1234", ctxWant("/h/src", "/ctx/src", false), time.Time{}); err != nil {
		t.Fatal(err)
	}
	infoAt, verbAt := -1, -1
	for i, c := range f.calls {
		if slices.Contains(c, "info") {
			infoAt = i
		}
		if slices.Contains(c, ctxAttachVerbName) {
			verbAt = i
		}
	}
	if infoAt < 0 || verbAt < 0 || infoAt > verbAt {
		t.Errorf("calls %v; want podman info before the verb", f.calls)
	}
	argv, _ := f.verbCall()
	if argv[0] != "podman" || argv[1] != "unshare" {
		t.Errorf("verb argv %v; want the rootless branch from the read", argv)
	}
}

func TestInjectCtxMountRefusesChangedRuntimePIDBeforeRunningVerb(t *testing.T) {
	f := newCtxDriveFixture(t)
	f.o.acceptPodmanFacts(rootlessInfo)
	inner := f.o.Exec
	inspects := 0
	f.o.Exec = func(argv []string, dir string, env []string, timeout time.Duration) ExecResult {
		if len(argv) > 3 && argv[1] == "inspect" && argv[3] == "{{.State.Pid}}" {
			inspects++
			if inspects == 2 {
				return ExecResult{Ran: true, Stdout: "5151\n"}
			}
		}
		return inner(argv, dir, env, timeout)
	}
	err := f.o.injectCtxMount("podman", "yolo-ws-abcd1234", ctxWant("/h/src", "/ctx/src", false), time.Time{})
	if err == nil || !strings.Contains(err.Error(), "changed before mount injection") {
		t.Fatalf("error = %v, want changed target refused before mutation", err)
	}
	if argv, _ := f.verbCall(); argv != nil {
		t.Errorf("injector ran for a changed target: %v", argv)
	}
}

// TestInjectCtxMountRefusesWithoutABranch: no verb runs when the branch cannot be known or cannot
// work, and each refusal says why.
func TestInjectCtxMountRefusesWithoutABranch(t *testing.T) {
	cases := map[string]struct {
		setup func(f *ctxDriveFixture)
		rt    string
		want  string
	}{
		"podman info fails":  {func(f *ctxDriveFixture) {}, "podman", "podman info"},
		"rootful not root":   {func(f *ctxDriveFixture) { f.o.acceptPodmanFacts(rootfulInfo) }, "podman", "rootful"},
		"apple container":    {func(f *ctxDriveFixture) {}, "container", "Apple Container"},
		"podman machine":     {func(f *ctxDriveFixture) { f.o.IsMacOS, f.o.IsLinux = true, false }, "podman", "Podman Machine"},
		"rootless not known": {func(f *ctxDriveFixture) { f.o.acceptPodmanFacts(`{"host":{}}`) }, "podman", "rootless"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newCtxDriveFixture(t)
			restore := ctxGeteuid
			ctxGeteuid = func() int { return 1000 }
			t.Cleanup(func() { ctxGeteuid = restore })
			tc.setup(f)
			err := f.o.injectCtxMount(tc.rt, "yolo-ws-abcd1234", ctxWant("/h/src", "/ctx/src", false), time.Time{})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err %v; want one naming %q", err, tc.want)
			}
			if argv, _ := f.verbCall(); argv != nil {
				t.Errorf("the verb ran: %v", argv)
			}
		})
	}
}

// TestInjectCtxMountReportsTheVerbsFailureAndDeadline: the verb's one stderr line is the error,
// and a verb that outlives its deadline says so.
func TestInjectCtxMountReportsTheVerbsFailureAndDeadline(t *testing.T) {
	f := newCtxDriveFixture(t)
	f.o.acceptPodmanFacts(rootlessInfo)
	f.verbRC, f.verbErr = 1, "yolo internal ctx-attach: move_mount onto /ctx/src: operation not permitted\n"
	err := f.o.injectCtxMount("podman", "yolo-ws-abcd1234", ctxWant("/h/src", "/ctx/src", false), time.Time{})
	if err == nil || !strings.Contains(err.Error(), "move_mount onto /ctx/src: operation not permitted") {
		t.Errorf("err %v; want the verb's line", err)
	}
	f = newCtxDriveFixture(t)
	f.o.acceptPodmanFacts(rootlessInfo)
	f.verbHang = true
	err = f.o.injectCtxMount("podman", "yolo-ws-abcd1234", ctxWant("/h/src", "/ctx/src", false), time.Time{})
	if err == nil || !strings.Contains(err.Error(), "10s") {
		t.Errorf("err %v; want the deadline named", err)
	}
}

// liveEnv is a podman jail's inspect env carrying this build's tags and token tok.
func liveEnv(tok string) []string {
	return []string{entrypoint.ContractTagsEnv + "=" + launchContractTagsValue(), ctxMountsTokenEnv + "=" + tok}
}

// TestCtxMountsLiveReasons: when a mount change can reach a running jail, and the outcome line's
// words when it cannot. The backend is decided before the tag, since every container backend
// freezes the same tag list.
func TestCtxMountsLiveReasons(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const cname = "yolo-ws-abcd1234"
	if err := writeCtxMountRecord(cname, "tok", nil); err != nil {
		t.Fatal(err)
	}
	oldTags := []string{entrypoint.ContractTagsEnv + "=entry-channel,agent-env-files,profile-sets,session-hangup", ctxMountsTokenEnv + "=tok"}
	cases := []struct {
		name   string
		rt     string
		macOS  bool
		env    []string
		ok     bool
		reason string
	}{
		{"live", "podman", false, liveEnv("tok"), true, ""},
		{"apple container", "container", false, liveEnv("tok"), false, "Apple Container shares are fixed when its VM boots"},
		{"podman machine", "podman", true, liveEnv("tok"), false, "the Podman Machine path is not built yet"},
		{"no tag", "podman", false, oldTags, false, "this jail started before live mounts existed"},
		{"no record match", "podman", false, liveEnv("other"), false, "the host's live-mount record is missing or does not match this jail's launch token"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newCtxDriveFixture(t)
			f.o.acceptPodmanFacts(rootlessInfo)
			f.o.IsMacOS, f.o.IsLinux = tc.macOS, !tc.macOS
			ok, reason := f.o.ctxMountsLive(tc.rt, cname, tc.env)
			if ok != tc.ok || reason != tc.reason {
				t.Errorf("ctxMountsLive = %v, %q; want %v, %q", ok, reason, tc.ok, tc.reason)
			}
		})
	}
}

// TestLaunchFreezesTheLiveCtxMountsTag: the tag is in every launch's list, so an apply can tell a
// jail that has the /ctx tmpfs from one that predates it.
func TestLaunchFreezesTheLiveCtxMountsTag(t *testing.T) {
	if !slices.Contains(launchContractTags, contractLiveCtxMounts) || contractLiveCtxMounts != "live-ctx-mounts" {
		t.Errorf("launchContractTags %v lacks live-ctx-mounts", launchContractTags)
	}
}
