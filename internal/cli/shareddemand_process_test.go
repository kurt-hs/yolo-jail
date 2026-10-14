package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/capture"
	"github.com/mschulkind-oss/yolo-jail/internal/notty"
	"github.com/mschulkind-oss/yolo-jail/internal/pidlock"
)

func sharedDemandTestProcess(t *testing.T, mode string, extra ...string) *exec.Cmd {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestSharedDemandProcessHelper$", "-test.short=true", "-test.timeout=30s")
	cmd.Env = append(os.Environ(), "YOLO_TEST_DEMAND_MODE="+mode)
	cmd.Env = append(cmd.Env, extra...)
	return cmd
}

// Only external execution and the sealed synthetic child are fixtures. The requester reaches
// hostMain --update; the detached process resolves user config and runs the production pipeline;
// runForkBuildChild owns/signals/drains a real child process. No agent is started.
func TestSharedDemandProcessHelper(t *testing.T) {
	mode := os.Getenv("YOLO_TEST_DEMAND_MODE")
	if mode == "" {
		return
	}
	switch mode {
	case "requester":
		withForkFloor(t)
		forkPinHomeSeams(t)
		prepareOpenAIAuthHost = func(hostPrelaunch, io.Writer) (managedOpenAIHostLaunch, error) { return nil, nil }
		got := captureHostExec(t)
		sharedPreparationSpawn = func(_ context.Context, d preparationDemand) error {
			cmd := sharedDemandTestProcess(t, "worker", "YOLO_TEST_DEMAND_KEY="+d.Key, "YOLO_TEST_DEMAND_RUNTIME="+d.Runtime, "YOLO_TEST_DEMAND_PLATFORM="+d.Platform)
			log, err := notty.OpenDetachLog(backgroundAdvanceLog())
			if err != nil {
				return err
			}
			defer log.Close()
			cmd.Stdout = log
			cmd.Stderr = log
			return notty.StartDetached(cmd)
		}
		code := hostMain([]string{"--update", "--", "tool"}, os.Stdout, os.Stderr, false, nil)
		if err := writeDemandJSON(os.Getenv("YOLO_TEST_DEMAND_RC"), struct {
			Code   int
			Target string
		}{code, got.target}); err != nil {
			t.Fatal(err)
		}
	case "worker":
		forkBuildChild = func(ctx context.Context, bound time.Duration, staging string, b forkBuild, s captureStreams, color bool) (int, bool) {
			return runForkBuildChild(ctx, bound, staging, b, s, color)
		}
		forkBuildChildCommand = func(argv []string) (*exec.Cmd, error) {
			staging := ""
			for _, arg := range argv {
				if value, ok := strings.CutPrefix(arg, "--workspace="); ok {
					staging = value
				}
			}
			return sharedDemandTestProcess(t, "child", "YOLO_TEST_DEMAND_STAGING="+staging), nil
		}
		removeBuildJail = func(name, rt string) bool {
			_ = os.WriteFile(os.Getenv("YOLO_TEST_DEMAND_CLEANED"), []byte(name+" "+rt), 0600)
			return true
		}
		code := runSharedPreparationCommand(backgroundArgs{keys: []string{os.Getenv("YOLO_TEST_DEMAND_KEY")}, runtime: os.Getenv("YOLO_TEST_DEMAND_RUNTIME"), platform: os.Getenv("YOLO_TEST_DEMAND_PLATFORM")}, os.Stdout)
		if code != 0 {
			t.Fatalf("worker exit=%d", code)
		}
	case "child":
		staging := os.Getenv("YOLO_TEST_DEMAND_STAGING")
		if err := writeForkBuildRuntime(staging, "podman"); err != nil {
			t.Fatal(err)
		}
		conn, err := net.Dial("unix", os.Getenv("YOLO_TEST_DEMAND_GATE"))
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		_, _ = fmt.Fprintln(conn, os.Getpid())
		var release [1]byte
		if _, err = io.ReadFull(conn, release[:]); err != nil {
			t.Fatal(err)
		}
		body := "#!/bin/sh\nexit 0\n"
		out := filepath.Join(staging, captureOutLeaf)
		writeFile(t, filepath.Join(staging, forkToolchainLeaf), "synthetic-image\n")
		tool := filepath.Join(capture.TreeDir(out), ".local", "bin", "tool")
		writeFile(t, tool, body)
		if err = os.Chmod(tool, 0755); err != nil {
			t.Fatal(err)
		}
		manifest := &capture.Manifest{Schema: capture.ManifestSchema, Home: "/home/agent", Platform: os.Getenv("YOLO_TEST_DEMAND_PLATFORM"), Surfaces: []string{".local"}, Excluded: capture.DefaultExcludes(), RefScan: capture.RefScanFull, Relocatable: true,
			Entries: []capture.ManifestEntry{{Path: ".local", Kind: capture.KindDir, Mode: "0755"}, {Path: ".local/bin", Kind: capture.KindDir, Mode: "0755"}, {Path: ".local/bin/tool", Kind: capture.KindFile, Mode: "0755", Size: int64(len(body))}}}
		if err = capture.WriteManifest(out, manifest); err != nil {
			t.Fatal(err)
		}
		if err = writeForkBuildRunReturned(staging); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("unknown helper mode %s", mode)
	}
}

type sharedDemandProcessFixture struct {
	fx            *patchedAdvanceFixture
	listener      *net.UnixListener
	gate, cleaned string
}

func newSharedDemandProcessFixture(t *testing.T) *sharedDemandProcessFixture {
	t.Helper()
	fx := patchedFloorFixture(t)
	fx.commit(t, "v1.1.0", map[int]string{14: "fourteen"})
	if rc, _, out := fx.hostLaunch(t); rc != 0 {
		t.Fatalf("seed rc=%d\n%s", rc, out)
	}
	fx.commit(t, "v1.2.0", map[int]string{14: "fourteen", 20: "twenty"})
	// A short owned temporary path keeps AF_UNIX usable on both hosts; resolve where minted.
	dir, err := os.MkdirTemp("", "yolo-demand-")
	if err != nil {
		t.Fatal(err)
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	gate := filepath.Join(real, "gate")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: gate, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	cleaned := filepath.Join(real, "cleaned")
	t.Setenv("YOLO_TEST_DEMAND_GATE", gate)
	t.Setenv("YOLO_TEST_DEMAND_CLEANED", cleaned)
	return &sharedDemandProcessFixture{fx, listener, gate, cleaned}
}
func (f *sharedDemandProcessFixture) requester(t *testing.T) (*exec.Cmd, string, *syncBuffer) {
	t.Helper()
	result := filepath.Join(t.TempDir(), "result.json")
	cmd := sharedDemandTestProcess(t, "requester", "YOLO_TEST_DEMAND_RC="+result)
	out := &syncBuffer{}
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	return cmd, result, out
}
func (f *sharedDemandProcessFixture) child(t *testing.T) (net.Conn, int) {
	t.Helper()
	_ = f.listener.SetDeadline(time.Now().Add(10 * time.Second))
	conn, err := f.listener.Accept()
	if err != nil {
		t.Fatalf("no synthetic child: %v; worker log: %s", err, mustRead(t, backgroundAdvanceLog()))
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatal(err)
	}
	return conn, pid
}
func readSharedRequester(t *testing.T, cmd *exec.Cmd, path string, out *syncBuffer, want int) {
	t.Helper()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("requester process: %v\n%s", err, out.String())
	}
	var result struct {
		Code   int
		Target string
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("requester result: %v\n%s", err, out.String())
	}
	if err = json.Unmarshal(b, &result); err != nil {
		t.Fatal(err)
	}
	if result.Code != want || want != 0 && result.Target != "" || want == 0 && result.Target == "" {
		t.Fatalf("requester result=%+v want code=%d\n%s", result, want, out.String())
	}
}

func TestSharedDemandPublicHostProcessesProducerInterruptSecondRemains(t *testing.T) {
	f := newSharedDemandProcessFixture(t)
	first, firstRC, firstOut := f.requester(t)
	conn, pid := f.child(t)
	defer conn.Close()
	second, secondRC, secondOut := f.requester(t)
	waitFor(t, "second public host attached", func() bool {
		dir := sharedDemandDir("forkpack/tool", "podman", floorPatchedPlatform())
		var live []livePreparationDemand
		err := withDemandState(context.Background(), dir, false, func() error { var e error; live, e = livePreparationDemands(dir); return e })
		return err == nil && len(live) == 2
	})
	if err := first.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	readSharedRequester(t, first, firstRC, firstOut, 130)
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("second requester lost the original child: %v\nfirst=%s\nsecond=%s\nworker=%s", err, firstOut.String(), secondOut.String(), mustRead(t, backgroundAdvanceLog()))
	}
	_, _ = conn.Write([]byte{1})
	readSharedRequester(t, second, secondRC, secondOut, 0)
	// STARTED by a later requester may require a follow-up check, never a second identical build.
	if got := f.fx.record(t).Good; got == nil || got.Entry == "" {
		t.Fatal("no guarded admission")
	}
	dir := sharedDemandDir("forkpack/tool", "podman", floorPatchedPlatform())
	waitFor(t, "shared worker ended", func() bool { return !pidlock.Held(filepath.Join(dir, "worker.lock")) })
	_ = f.listener.SetDeadline(time.Now())
	if extra, err := f.listener.Accept(); err == nil {
		extra.Close()
		t.Fatal("a second child replaced the shared producer")
	}
}

func TestSharedDemandPublicHostProcessExitZeroDemandCleansOwnedChild(t *testing.T) {
	f := newSharedDemandProcessFixture(t)
	before := f.fx.record(t).Good.Entry
	first, _, _ := f.requester(t)
	conn, pid := f.child(t)
	defer conn.Close()
	if err := first.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = first.Wait()
	waitFor(t, "requester exit tears down owned runtime", func() bool { _, err := os.Stat(f.cleaned); return err == nil })
	dir := sharedDemandDir("forkpack/tool", "podman", floorPatchedPlatform())
	waitFor(t, "zero-demand worker ends", func() bool { return !pidlock.Held(filepath.Join(dir, "worker.lock")) })
	if err := syscall.Kill(pid, 0); err == nil {
		t.Fatal("owned child survived requester exit")
	}
	if got := f.fx.record(t).Good.Entry; got != before {
		t.Fatalf("zero-demand exit admitted %s instead of retaining %s", got, before)
	}
}

func TestSharedDemandPublicHostProcessesRequesterExitSecondRemains(t *testing.T) {
	f := newSharedDemandProcessFixture(t)
	first, _, _ := f.requester(t)
	conn, pid := f.child(t)
	defer conn.Close()
	second, secondRC, secondOut := f.requester(t)
	waitFor(t, "second public host attached", func() bool {
		dir := sharedDemandDir("forkpack/tool", "podman", floorPatchedPlatform())
		var live []livePreparationDemand
		err := withDemandState(context.Background(), dir, false, func() error { var e error; live, e = livePreparationDemands(dir); return e })
		return err == nil && len(live) == 2
	})
	if err := first.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = first.Wait()
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("exiting first requester killed shared child: %v", err)
	}
	_, _ = conn.Write([]byte{1})
	readSharedRequester(t, second, secondRC, secondOut, 0)
}

func TestSharedDemandPublicHostRejectsLateSourceRecipeAndSelectionChanges(t *testing.T) {
	for _, change := range []string{"recipe", "source", "selection"} {
		t.Run(change, func(t *testing.T) {
			f := newSharedDemandProcessFixture(t)
			before := f.fx.record(t).Good.Entry
			requester, result, out := f.requester(t)
			conn, _ := f.child(t)
			defer conn.Close()
			switch change {
			case "recipe":
				b, err := os.ReadFile(f.fx.manifest)
				if err != nil {
					t.Fatal(err)
				}
				writeFile(t, f.fx.manifest, strings.Replace(string(b), "sh build.sh", "sh changed-build.sh", 1))
			case "source":
				f.fx.writeManifest(t, "different-source-ref", "")
			case "selection":
				writeFile(t, filepath.Join(f.fx.home, ".config", "yolo-jail", "config.jsonc"), `{"packs":[]}`)
			}
			// Releasing the child races the watcher intentionally: the late admission guard must still
			// reject the output if the watcher has not yet observed revocation.
			_, _ = conn.Write([]byte{1})
			readSharedRequester(t, requester, result, out, 1)
			if got := f.fx.record(t).Good.Entry; got != before {
				t.Fatalf("late %s change admitted stale output: %s", change, got)
			}
		})
	}
}
