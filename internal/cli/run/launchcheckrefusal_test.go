package run

// launchcheckrefusal_test.go pins OQ-HD11's refusal as HD-D21 keeps it under HD-R1
// (docs/design/host-daemon-ownership.md) THROUGH Run(): the one host-wide daemon that can still
// exist is a machine-wide copy an older yolo left running through the upgrade, and while it is
// alive a fresh launch on either arm that starts host services REFUSES, naming its stop. A fresh
// launch's own copy that lacks the check keeps the yellow line, and an attach never refuses
// (HD-D5 (1)). No launch stops the older copy itself.
//
// Each test drives a whole launch, not the boundary launchcheck_test.go drives, so deleting the
// refusal at one call site fails the test for that site: the keeper's (the container arm) or the
// macos-user arm's. Each has a control beside it, the same launch with no older copy running,
// which proceeds; without it the refusal tests would pass on a harness that never reached the
// check at all.
//
// The daemon is launchcheck_test.go's fixture: each launch spawns its own copy, a relay to the
// handler served at launchCheckFixtureSocket, and leaveAnOlderCopyRunning stands up the older
// yolo's copy at the name-keyed rendezvous. Its loophole declares no jail daemon, so every launch
// of it is asked (runLaunchChecks' served rule).

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/hostservice"
	"github.com/mschulkind-oss/yolo-jail/internal/image"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/macosuser"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	yoloruntime "github.com/mschulkind-oss/yolo-jail/internal/runtime"
)

// askedEveryLaunchManifest is the fixture loophole as a local pack ships it: one copy per jail,
// declaring the launch check, and with no jail daemon, so no payload decides whether it is asked.
func askedEveryLaunchManifest(t *testing.T) string {
	t.Helper()
	cmd, err := json.Marshal([]string{os.Args[0], "-splice-child", "{socket}", launchCheckFixtureSocket()})
	if err != nil {
		t.Fatal(err)
	}
	return `{
	"name": "` + launchCheckFixtureName + `",
	"description": "a host service that is asked the launch check",
	"default_enabled": true,
	"transport": "loopback-tls",
	"lifecycle": "spawned",
	"host_daemon": {"cmd": ` + string(cmd) + `, "publishes": "socket",
		"launch_check": true, "detach": true}
}`
}

// controlNote is the line the control's daemon makes every launch print (answersTheCheck).
const controlNote = "loophole " + launchCheckFixtureName + ": checked by the control"

// anOlderCopy is the prep that leaves the older yolo's machine-wide copy running.
func anOlderCopy(t *testing.T) { leaveAnOlderCopyRunning(t, olderThanTheCheck) }

// launchCheckRefusalHome isolates a whole launch with the fixture loophole in the conventional local pack.
func launchCheckRefusalHome(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("binds host-wide singleton paths")
	}
	home := packHome(t)
	writeLocalLoopholePack(t, home, launchCheckFixtureName, askedEveryLaunchManifest(t))
	writeUserPacks(t, home, `[]`)
	return home
}

// assertTheOlderCopyWasNotStopped: the older copy is still alive afterwards, since only the user
// may stop it.
func assertTheOlderCopyWasNotStopped(t *testing.T) {
	t.Helper()
	if !olderHostWideAlive(launchCheckFixtureName) {
		t.Error("the older yolo's copy is no longer alive: the launch stopped it")
	}
}

// containerLaunch runs a fresh podman launch, its keeper in-process, to its container's start, and
// reports what it printed, its status, and whether the runtime was asked to run the container.
// Each of prep runs once the daemon is up (anOlderCopy).
func containerLaunch(t *testing.T, handler hostservice.Handler,
	prep ...func(*testing.T)) (out string, rc int, ranContainer bool) {
	t.Helper()
	launchCheckRefusalHome(t)
	serveLaunchCheckDaemon(t, handler)
	for _, p := range prep {
		p(t)
	}
	ws := t.TempDir()
	cname := yoloruntime.FromWorkspace(ws)
	bin, ran := t.TempDir(), filepath.Join(t.TempDir(), "ran")
	script := "#!/bin/sh\nif [ \"$1\" = run ]; then : > '" + ran + "'; fi\nexit 3\n"
	if err := os.WriteFile(filepath.Join(bin, "podman"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":/bin:/usr/bin")
	o := dispatchOptions(t, ws, "podman", new(bytes.Buffer), new(bytes.Buffer), nil)
	var stdout, stderr lockedBuffer
	o.Stdout, o.Stderr = &stdout, &stderr
	repo, _ := o.RepoRoot()
	o.PathExists = func(p string) bool {
		return p == filepath.Join(prebuiltBinDir(repo.Root), "yolo-entrypoint")
	}
	o.Exec = func(argv []string, _ string, _ []string, _ time.Duration) ExecResult {
		return ExecResult{Ran: true, RC: 0} // `ps`: no container of the name is running
	}
	o.autoLoad = func(image.AutoLoadOptions) image.LoadResult {
		return image.LoadResult{OK: true, Ref: goldenImageRef}
	}
	t.Cleanup(func() { _ = os.RemoveAll(hostServiceSocketsDir(cname, false)) })
	done := make(chan int, 1)
	go func() { done <- Run(*o) }()
	select {
	case rc = <-done:
	case <-time.After(sealedLaunchBound):
		t.Fatalf("the launch had not returned after %s\nstdout:\n%s\nstderr:\n%s",
			sealedLaunchBound, stdout.String(), stderr.String())
	}
	_, err := os.Stat(ran)
	return stdout.String() + stderr.String(), rc, err == nil
}

// TestAFreshContainerLaunchRefusesBesideAnOlderYolosCopy is the container arm, whose host services
// its keeper starts: with an older yolo's machine-wide copy alive, the keeper refuses before the
// container, and the launch exits 1.
func TestAFreshContainerLaunchRefusesBesideAnOlderYolosCopy(t *testing.T) {
	out, rc, ran := containerLaunch(t, answersTheCheck, anOlderCopy)
	if ran {
		t.Errorf("the launch started its container beside an older yolo's machine-wide copy:\n%s", out)
	}
	if rc != 1 {
		t.Errorf("Run() = %d, want 1 for a refused launch:\n%s", rc, out)
	}
	assertTheOlderCopyRefusal(t, out, "Refusing this launch")
	assertTheOlderCopyWasNotStopped(t)
}

// TestAFreshContainerLaunchProceedsPastADaemonThatAnswers is its control: the same launch with no
// older copy reaches the check, prints the daemon's note, and starts its container.
func TestAFreshContainerLaunchProceedsPastADaemonThatAnswers(t *testing.T) {
	out, _, ran := containerLaunch(t, answersTheCheck)
	if !strings.Contains(out, controlNote) {
		t.Fatalf("the launch never asked the daemon, so its refusal twin proves nothing:\n%s", out)
	}
	if !ran {
		t.Errorf("a launch whose daemon answers the check did not start its container:\n%s", out)
	}
	if strings.Contains(out, "Refusing") {
		t.Errorf("a launch whose daemon answers the check refused:\n%s", out)
	}
}

// TestAFreshContainerLaunchWarnsWhenItsOwnDaemonLacksTheCheck: the daemon a fresh launch asks is
// its own copy, this yolo's build, so `unknown action: launch-check` means its manifest declares a
// check its program lacks. No restart or stop fixes that, so the launch warns, naming none, and
// starts its container.
func TestAFreshContainerLaunchWarnsWhenItsOwnDaemonLacksTheCheck(t *testing.T) {
	out, _, ran := containerLaunch(t, olderThanTheCheck)
	if strings.Contains(out, "Refusing") {
		t.Errorf("the launch refused a daemon no restart can fix:\n%s", out)
	}
	if !ran {
		t.Errorf("the launch did not start its container:\n%s", out)
	}
	if !strings.Contains(out, "loophole "+launchCheckFixtureName+": its daemon does not answer the "+
		"launch check its manifest declares") {
		t.Errorf("the launch does not say the manifest declares a check its program lacks:\n%s", out)
	}
}

// macosUserLaunchAgainst runs a macos-user launch against a stub backend and reports whether the
// sandboxed command was started.
func macosUserLaunchAgainst(t *testing.T, handler hostservice.Handler,
	prep ...func(*testing.T)) (out string, rc int, started bool) {
	t.Helper()
	launchCheckRefusalHome(t)
	serveLaunchCheckDaemon(t, handler)
	for _, p := range prep {
		p(t)
	}
	ws := t.TempDir()
	var stdout, stderr bytes.Buffer
	o := dispatchOptions(t, ws, "macos-user", &stdout, &stderr, nil)
	o.MacosUserRun = func(_ *jsonx.OrderedMap, _ string, _, _ []string, _, _ string, _ macosuser.HomeOverlay,
		_ macosuser.HostContext, _ bool, _ *jsonx.OrderedMap, _ []packload.BlockedTool, _ macosuser.JailDaemons) int {
		started = true
		return 0
	}
	rc = Run(*o)
	return stdout.String() + stderr.String(), rc, started
}

// TestAMacosUserLaunchRefusesBesideAnOlderYolosCopy is the arm that starts its own host services:
// it refuses before the sandboxed command runs.
func TestAMacosUserLaunchRefusesBesideAnOlderYolosCopy(t *testing.T) {
	out, rc, started := macosUserLaunchAgainst(t, answersTheCheck, anOlderCopy)
	if started {
		t.Errorf("the macos-user launch ran its command beside an older yolo's machine-wide copy:\n%s", out)
	}
	if rc != 1 {
		t.Errorf("Run() = %d, want 1 for a refused launch:\n%s", rc, out)
	}
	assertTheOlderCopyRefusal(t, out, "Refusing the macos-user launch")
	assertTheOlderCopyWasNotStopped(t)
}

// TestAMacosUserLaunchProceedsPastADaemonThatAnswers is its control.
func TestAMacosUserLaunchProceedsPastADaemonThatAnswers(t *testing.T) {
	out, rc, started := macosUserLaunchAgainst(t, answersTheCheck)
	if !strings.Contains(out, controlNote) {
		t.Fatalf("the launch never asked the daemon, so its refusal twin proves nothing:\n%s", out)
	}
	if !started || rc != 0 {
		t.Errorf("a launch whose daemon answers the check did not run its command (rc %d):\n%s", rc, out)
	}
}

// attachAgainst runs an attach into a running jail whose launch published a front to the fixture
// daemon. Each of prep runs once that front is up, so an older copy it leaves stands for one the
// jail's own launch never met. The jail's pack tree is this workspace's own staging, so the attach
// reads the fixture loophole from it.
func attachAgainst(t *testing.T, handler hostservice.Handler, prep ...func(*testing.T)) packSkewRun {
	t.Helper()
	launchCheckRefusalHome(t)
	serveLaunchCheckDaemon(t, handler)
	ws := t.TempDir()
	cname := yoloruntime.FromWorkspace(ws)
	var out bytes.Buffer
	fresh := dispatchOptions(t, ws, "podman", &out, &out, nil)
	cfg, ok := fresh.loadAndValidateConfig()
	if !ok {
		t.Fatalf("config:\n%s", out.String())
	}
	fresh.stagingCfg = cfg
	staged, ok := fresh.stageRunPacks(cname)
	if !ok {
		t.Fatalf("staging:\n%s", out.String())
	}
	if err := writeLivePackTree(cname, staged.root); err != nil {
		t.Fatal(err)
	}
	// The running jail's front, published by its launch and still open.
	t.Cleanup(func() { _ = os.RemoveAll(hostServiceSocketsDir(cname, false)) })
	launcher := &Options{}
	fillDefaults(launcher)
	var launchOut bytes.Buffer
	launcher.Stderr, launcher.Stdout = &launchOut, &launchOut
	launcher.PathExists = func(string) bool { return false }
	handles, _ := launcher.startLoopholesDisclosed(cname, "podman", newConfig(), staged.packs, nil)
	t.Cleanup(func() {
		for _, h := range handles {
			if h.stop != nil {
				h.stop()
			}
		}
	})
	if !startedLoophole(handles, launchCheckFixtureName) {
		t.Fatalf("the running jail's front was never published:\n%s", launchOut.String())
	}
	for _, p := range prep {
		p(t)
	}
	current := "YOLO_VERSION=9.9.9-test\n" + entrypointContractTagsLine() + "\n"
	return runPackSkew(t, ws, current, false, "", nil, nil)
}

// TestAnAttachKeepsTheLineAndEntersBesideAnOlderYolosCopy is HD-D5 (1) through Run(): an attach
// starts nothing, and refusing it would leave the running jail as it is, so beside an older yolo's
// copy that does not answer the check it prints the yellow line naming that copy's stop and the
// relaunch, delivers its channel and enters the jail.
func TestAnAttachKeepsTheLineAndEntersBesideAnOlderYolosCopy(t *testing.T) {
	r := attachAgainst(t, olderThanTheCheck, anOlderCopy)
	out := r.stdout + r.stderr
	if !r.execed || r.rc != 0 {
		t.Errorf("the attach did not enter the jail (rc %d):\n%s", r.rc, out)
	}
	if strings.Contains(out, "Refusing") {
		t.Errorf("the attach refused, which HD-D5 (1) keeps a warning:\n%s", out)
	}
	if _, err := os.Stat(r.envFile); err != nil {
		t.Errorf("the attach did not deliver its channel (%v)", err)
	}
	for _, want := range []string{"loophole " + launchCheckFixtureName + ": this jail was launched by an older yolo",
		"launch it again with this yolo"} {
		if !strings.Contains(out, want) {
			t.Errorf("the attach does not say %q:\n%s", want, out)
		}
	}
	assertTheOlderCopyWasNotStopped(t)
}

// TestAnAttachProceedsPastADaemonThatAnswers is its control: the attach asks, delivers its channel
// and enters the jail.
func TestAnAttachProceedsPastADaemonThatAnswers(t *testing.T) {
	r := attachAgainst(t, answersTheCheck)
	out := r.stdout + r.stderr
	if !strings.Contains(out, controlNote) {
		t.Fatalf("the attach never asked the daemon, so its twin's line proves nothing:\n%s", out)
	}
	if !r.execed || r.rc != 0 {
		t.Errorf("an attach whose daemon answers the check did not enter the jail (rc %d):\n%s", r.rc, out)
	}
	if _, err := os.Stat(r.envFile); err != nil {
		t.Errorf("the attach did not deliver its channel (%v)", err)
	}
}
