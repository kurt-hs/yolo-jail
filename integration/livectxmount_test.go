package integration

// livectxmount_test.go is the live context-mount injector against a real podman jail
// (docs/design/live-config-apply.md §5, M1). It is THE instrument for the rootless branch: a
// nested jail runs podman with --userns host and reports rootful, so there it proves only the
// direct branch (AGENTS.md's carve-out). CI's rootless jobs are where `podman unshare` is shown.

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/ctxinject"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	naming "github.com/mschulkind-oss/yolo-jail/internal/runtime"
)

// liveCtxPodmanRootless is `podman info --format {{.Host.Security.Rootless}}`, logged with the
// result as §5.4 asks.
func liveCtxPodmanRootless(t *testing.T) bool {
	t.Helper()
	out, err := exec.Command("podman", "info", "--format", "{{.Host.Security.Rootless}}").Output()
	if err != nil {
		t.Fatalf("podman info: %v", err)
	}
	v := strings.TrimSpace(string(out))
	t.Logf("podman info --format '{{.Host.Security.Rootless}}' = %s", v)
	return v == "true"
}

// liveCtxExec runs script in the jail and returns its combined output and whether it exited 0.
func liveCtxExec(t *testing.T, cname, script string) (string, bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := jailExec(ctx, "podman", cname, script).CombinedOutput()
	return string(out), err == nil
}

// TestLiveCtxMountInjectAndRemove: a podman launch puts /ctx on a read-only tmpfs and records the
// config mounts it bound under the token it froze into the jail; then the injector adds a
// read-only directory the jail can read but not write, `mkdir /ctx/x` stays refused, removal
// takes it away, and a read-write injection writes through to the host.
func TestLiveCtxMountInjectAndRemove(t *testing.T) {
	requireJail(t)
	if runtime.GOOS != "linux" || detectRuntime() != "podman" {
		t.Skip("live context mounts are injected on podman on Linux only")
	}
	rootless := liveCtxPodmanRootless(t)
	_, statErr := os.Stat("/run/.containerenv")
	nested := statErr == nil
	if !rootless && !nested && os.Geteuid() != 0 {
		t.Skip("a rootful podman driven by a non-root user is the restart-only arm (design §5.3)")
	}

	atLaunch := t.TempDir()
	if err := os.WriteFile(filepath.Join(atLaunch, "launch.txt"), []byte("at launch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := writeProject(t, `{"mounts": ["`+atLaunch+`:/ctx/atlaunch"]}`)
	cname := naming.FromWorkspace(dir)

	const release = "release-livectx"
	first := startYoloBackground(t, "hold", dir,
		`echo LIVECTX-READY-$((40+2)); for _ in $(seq 1 1200); do [ -f /workspace/`+release+` ] && break; sleep 0.5; done`)
	t.Cleanup(func() { _ = os.WriteFile(filepath.Join(dir, release), []byte("go\n"), 0o644) })
	ready := regexp.MustCompile(`LIVECTX-READY-42`)
	deadline := time.Now().Add(jailTimeout())
	for !ready.MatchString(first.combined()) {
		select {
		case err := <-first.done:
			t.Fatalf("the launch exited (%v) before its script ran:\n%s", err, first.combined())
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("the jail never ran its script within %s:\n%s", jailTimeout(), first.combined())
		}
		time.Sleep(50 * time.Millisecond)
	}
	awaitLaunchLockReleased(t, dir, first)

	// THE LAUNCH HALF: the tmpfs, read-only to the jail, the launch-time bind still under it, and
	// the record carrying the jail's token and the mount it bound.
	if out, ok := liveCtxExec(t, cname, `stat -f -c %T /ctx; cat /ctx/atlaunch/launch.txt; mkdir /ctx/x 2>&1; true`); !ok ||
		!strings.Contains(out, "tmpfs") || !strings.Contains(out, "at launch") || !strings.Contains(out, "Read-only file system") {
		t.Fatalf("/ctx is not a read-only tmpfs holding the launch-time mount:\n%s", out)
	}
	envOut, err := exec.Command("podman", "inspect", "--format", "{{range .Config.Env}}{{println .}}{{end}}", cname).Output()
	if err != nil {
		t.Fatalf("podman inspect env: %v", err)
	}
	var token string
	for _, l := range strings.Split(string(envOut), "\n") {
		if v, ok := strings.CutPrefix(l, "YOLO_CTX_MOUNTS_TOKEN="); ok {
			token = v
		}
		if v, ok := strings.CutPrefix(l, "YOLO_CONTRACT_TAGS="); ok && !strings.Contains(v, "live-ctx-mounts") {
			t.Errorf("the jail's contract tags %q lack live-ctx-mounts", v)
		}
	}
	recBytes, err := os.ReadFile(filepath.Join(paths.AgentsDir(), cname, "ctx-mounts", "record.json"))
	if err != nil {
		t.Fatalf("the launch wrote no live-mounts record: %v", err)
	}
	var rec struct {
		Token   string `json:"token"`
		Entries []struct {
			Source, Dest string
			RW           bool
		} `json:"entries"`
	}
	if err := json.Unmarshal(recBytes, &rec); err != nil {
		t.Fatalf("record: %v\n%s", err, recBytes)
	}
	if token == "" || rec.Token != token || len(rec.Entries) != 1 || rec.Entries[0].Dest != "/ctx/atlaunch" {
		t.Errorf("record %s does not answer to the jail's token %q with its one mount", recBytes, token)
	}

	pidOut, err := exec.Command("podman", "inspect", "--format", "{{.State.Pid}}", cname).Output()
	if err != nil {
		t.Fatalf("podman inspect pid: %v", err)
	}
	pid := strings.TrimSpace(string(pidOut))
	n, err := strconv.Atoi(pid)
	if err != nil || n <= 0 {
		t.Fatalf("podman reports pid %q for %s", pid, cname)
	}
	identity, err := ctxinject.ReadProcessIdentity(n)
	if err != nil {
		t.Fatalf("read jail process identity for pid %d: %v", n, err)
	}
	identityArgs := []string{"--start-time", strconv.FormatUint(identity.StartTime, 10),
		"--mount-ns-device", strconv.FormatUint(identity.MountNSDevice, 10),
		"--mount-ns-inode", strconv.FormatUint(identity.MountNSInode, 10)}
	// The branch the host driver takes (ctxinjectdrive.go): `podman unshare` on rootless, the verb
	// directly when nested (--userns host) or rootful as root.
	ctxAttach := func(args ...string) (string, error) {
		argv := append([]string{yoloBin, "internal", "ctx-attach", "--pid", pid}, identityArgs...)
		argv = append(argv, args...)
		if rootless && !nested {
			argv = append([]string{"podman", "unshare", "--"}, argv...)
		}
		t.Logf("injector: %s", strings.Join(argv, " "))
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, argv[0], argv[1:]...).CombinedOutput()
		return string(out), err
	}

	// READ-ONLY INJECTION.
	ro := t.TempDir()
	if err := os.WriteFile(filepath.Join(ro, "file.txt"), []byte("live bytes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := ctxAttach("--source", ro, "--dest", "/ctx/live"); err != nil {
		t.Fatalf("read-only injection failed (%v): %s", err, out)
	}
	out, _ := liveCtxExec(t, cname, `cat /ctx/live/file.txt; echo w > /ctx/live/file.txt 2>&1; echo WRITE-RC=$?; `+
		`mkdir /ctx/x 2>&1; echo MKDIR-RC=$?`)
	if !strings.Contains(out, "live bytes") {
		t.Errorf("the jail cannot read the injected file:\n%s", out)
	}
	if !strings.Contains(out, "Read-only file system") || strings.Contains(out, "WRITE-RC=0") {
		t.Errorf("a write into the read-only injection was not refused with EROFS:\n%s", out)
	}
	if strings.Contains(out, "MKDIR-RC=0") {
		t.Errorf("mkdir /ctx/x succeeded after the injection; the private clone exposed /ctx:\n%s", out)
	}
	if b, _ := os.ReadFile(filepath.Join(ro, "file.txt")); string(b) != "live bytes\n" {
		t.Errorf("the host file changed through a read-only injection: %q", b)
	}

	// AN ADD IS IDEMPOTENT: the same source in the same mode again is success (a step cut off
	// after its mount landed is recorded by the next apply); another source there is refused.
	if out, err := ctxAttach("--source", ro, "--dest", "/ctx/live"); err != nil {
		t.Errorf("the same injection again failed (%v): %s", err, out)
	}
	if out, err := ctxAttach("--source", t.TempDir(), "--dest", "/ctx/live"); err == nil ||
		!strings.Contains(out, "a mount already") {
		t.Errorf("another source over /ctx/live was not refused (%v): %s", err, out)
	}
	if out, err := ctxAttach("--source", ro, "--dest", "/ctx/live", "--rw"); err == nil {
		t.Errorf("the same source read-write over its read-only mount was not refused: %s", out)
	}

	// REMOVAL.
	if out, err := ctxAttach("--dest", "/ctx/live", "--remove"); err != nil {
		t.Fatalf("removal failed (%v): %s", err, out)
	}
	if out, _ := liveCtxExec(t, cname, `test -e /ctx/live && echo STILL-THERE || echo GONE`); !strings.Contains(out, "GONE") {
		t.Errorf("/ctx/live is still in the jail after removal:\n%s", out)
	}

	// READ-WRITE INJECTION.
	rw := t.TempDir()
	if out, err := ctxAttach("--source", rw, "--dest", "/ctx/liverw", "--rw"); err != nil {
		t.Fatalf("read-write injection failed (%v): %s", err, out)
	}
	if out, ok := liveCtxExec(t, cname, `echo from-the-jail > /ctx/liverw/new.txt`); !ok {
		t.Errorf("a write into the read-write injection failed:\n%s", out)
	}
	if b, err := os.ReadFile(filepath.Join(rw, "new.txt")); err != nil || string(b) != "from-the-jail\n" {
		t.Errorf("the jail's write did not reach the host file: %q, %v", b, err)
	}
	if out, err := ctxAttach("--dest", "/ctx/liverw", "--remove"); err != nil {
		t.Errorf("removing the read-write injection failed (%v): %s", err, out)
	}
}
