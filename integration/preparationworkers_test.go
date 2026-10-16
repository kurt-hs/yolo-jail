package integration

// preparationworkers_test.go stops, at the end of every test that launched yolo, the detached
// preparation processes that launch left running under the test's temp HOME.
//
// # What a launch leaves behind
//
// A launch on the HOST (YOLO_VERSION unset, which is CI) whose selected packs declare a
// program prepares it through yolo's background preparation (internal/cli's
// preparationscheduler.go and shareddemand.go): it registers the program with the long-lived
// `yolo internal preparation-scheduler`, starting one if none runs, and waits for a detached
// `yolo internal background-advance --shared-demand` worker to answer. Both outlive the launch
// by design, and both run with the launch's environment: in this suite, a temp HOME that
// t.TempDir deletes when the test ends. Inventoried on 2026-10-16 by running
// TestAnUnsatisfiableNodeFloorRefusesTheLaunch with YOLO_VERSION unset and listing every process
// whose environment names the test's temp dir once the launch had returned: the scheduler
// (still alive after the test binary exited, its cwd the deleted workspace) and two
// shared-demand workers, one still finishing after the refusal reached the terminal.
//
// A writer like that, writing through a HOME that is being deleted, recreates a directory under
// t.TempDir's RemoveAll: CI run 38856922948 failed that test with only
// `TempDir RemoveAll cleanup: unlinkat <tempdir>: directory not empty`. A jail never shows it
// (registerOrdinaryPreparations and waitOrdinaryPreparation stay in the foreground when
// config.InJail), which is why it surfaces in CI alone.
//
// # How a worker is identified as this test's
//
// Through the pid locks it holds, the way the launch that started it finds it: every lock under
// the background-advance directory of the test's HOME records its last holder's pid
// (internal/pidlock). The scheduler's worker.lock, a shared demand's worker.lock and a key's
// advance lock all live there, so the walk takes every *.lock rather than spelling their
// layout, which internal/cli owns. Then by HOME, as homedaemons_test.go does: a pid is this
// test's only if the process runs with HOME set to the test's temp home, so a stale lock naming
// a recycled pid, or a worker another test started, is left alone. On a host without /proc
// nothing is identified and nothing is stopped.
//
// The stop is SIGTERM, which every one of these processes takes as a request to wind down
// (internal/cli's backgroundSignals), and SIGKILL only after a grace long enough for a worker
// to end the jail it was running.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/pidlock"
)

// preparationWorkerTermGrace bounds how long a stopped worker may take to end what it runs, a
// capture jail included, before it is killed.
const preparationWorkerTermGrace = 30 * time.Second

// stopHomePreparationWorkers stops every detached preparation process that holds (or last held)
// a lock under home's background-advance directory and runs with HOME=home, waiting for each to
// exit, and names the ones it stopped ("pid <n> (<lock>)").
func stopHomePreparationWorkers(home string) ([]string, error) {
	if home == "" {
		return nil, nil
	}
	dir := paths.BackgroundAdvanceDirUnder(home)
	pids := map[int]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.Type().IsRegular() && strings.HasSuffix(d.Name(), ".lock") {
			if pid := pidlock.Holder(path); pid > 0 && pid != os.Getpid() {
				if _, seen := pids[pid]; !seen {
					rel, _ := filepath.Rel(dir, path)
					pids[pid] = rel
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("reading the preparation locks under %s: %w", dir, err)
	}
	var stopped []string
	var errs []error
	for pid, lock := range pids {
		if processGone(pid) || !processRunsWithHome(pid, home) {
			continue
		}
		_ = syscall.Kill(pid, syscall.SIGTERM)
		for deadline := time.Now().Add(preparationWorkerTermGrace); !processGone(pid) && time.Now().Before(deadline); {
			time.Sleep(50 * time.Millisecond)
		}
		if !processGone(pid) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			for deadline := time.Now().Add(2 * time.Second); !processGone(pid) && time.Now().Before(deadline); {
				time.Sleep(50 * time.Millisecond)
			}
		}
		if !processGone(pid) {
			errs = append(errs, fmt.Errorf("preparation process pid %d (%s) survived SIGTERM and SIGKILL", pid, lock))
			continue
		}
		stopped = append(stopped, fmt.Sprintf("pid %d (%s)", pid, lock))
	}
	sort.Strings(stopped)
	return stopped, errors.Join(errs...)
}

// --- the tests ---

// fakePreparationWorker starts a stand-in for a detached preparation process running under
// home, recorded as the holder of lock (relative to home's background-advance directory).
func fakePreparationWorker(t *testing.T, home, lock string) int {
	t.Helper()
	cmd := fakeHostDaemonProcess(t, "preparation worker", home)
	path := filepath.Join(paths.BackgroundAdvanceDirUnder(home), lock)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(fmt.Sprintf("%d\n", cmd.Process.Pid)), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	return cmd.Process.Pid
}

// Only this home's workers are stopped, wherever under the directory their lock sits; a lock
// naming another home's process is left alone.
func TestStopHomePreparationWorkersStopsOnlyTheHomesOwn(t *testing.T) {
	mine, theirs := t.TempDir(), t.TempDir()
	scheduler := fakePreparationWorker(t, mine, "worker.lock")
	demand := fakePreparationWorker(t, mine, filepath.Join("demands", "abc", "worker.lock"))
	other := fakePreparationWorker(t, theirs, "worker.lock")
	// The other home's process, named by a lock under MY home: a recycled pid or a worker
	// another test started.
	if err := os.WriteFile(filepath.Join(paths.BackgroundAdvanceDirUnder(mine), "key.lock"),
		[]byte(fmt.Sprintf("%d\n", other)), 0o600); err != nil {
		t.Fatal(err)
	}

	stopped, err := stopHomePreparationWorkers(mine)
	if err != nil {
		t.Fatal(err)
	}
	if len(stopped) != 2 {
		t.Errorf("stopped = %v, want this home's scheduler and demand worker", stopped)
	}
	for _, pid := range []int{scheduler, demand} {
		if !processGone(pid) {
			t.Errorf("preparation process %d running under %s is still alive", pid, mine)
		}
	}
	if processGone(other) {
		t.Errorf("a preparation process running under ANOTHER home (%s) was stopped", theirs)
	}
	if stopped, err := stopHomePreparationWorkers(filepath.Join(mine, "absent")); err != nil || len(stopped) != 0 {
		t.Errorf("a home with no background-advance directory: stopped %v (%v), want nothing", stopped, err)
	}
}

// THE CALL SITE, BY BEHAVIOR: a runCommand launch's cleanup stops a preparation worker running
// under the launch's HOME. The launch is `yolo --version`, since what is pinned is the
// harness's stop and not the spawn. Delete the stopHomePreparationWorkers call from
// awaitDetachedWriters and the stand-in outlives the subtest.
//
// The stand-in's lock has a name of its own: the isolated HOME's state dir is this run's store,
// whose real worker.lock a scheduler another test started may hold.
func TestALaunchesCleanupStopsItsHomesPreparationWorkers(t *testing.T) {
	requireJail(t)
	ws := t.TempDir()
	worker := fakePreparationWorker(t, os.Getenv("HOME"), "integration-stand-in.lock")
	t.Run("launch", func(t *testing.T) {
		if res := runYoloCLI(t, ws, "--version"); res.rc != 0 {
			t.Fatalf("yolo --version: rc=%d\n%s", res.rc, res.combined())
		}
	})
	if !processGone(worker) {
		t.Error("a preparation worker running under the launch's HOME outlived the launch's " +
			"cleanup: awaitDetachedWriters no longer calls stopHomePreparationWorkers")
	}
}
