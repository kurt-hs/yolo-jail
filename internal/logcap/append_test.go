package logcap

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func TestAppendBeneathSerializesTrimAndConcurrentAppends(t *testing.T) {
	ws := t.TempDir()
	if err := os.Mkdir(filepath.Join(ws, ".yolo"), 0o755); err != nil {
		t.Fatal(err)
	}
	open := func(name string, flag int, perm os.FileMode) (*os.File, error) {
		return paths.OpenWorkspaceStateFile(ws, name, flag, perm)
	}
	seed := bytes.Repeat([]byte("old diagnostic line\n"), MaxBytes/20+10)
	f, err := open("receipts.jsonl", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(seed); err != nil {
		t.Fatal(err)
	}
	f.Close()
	const writers, each = 8, 50
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < each; j++ {
				if err := AppendBeneath(open, "receipts.jsonl", []byte("record\n")); err != nil {
					t.Error(err)
					return
				}
			}
		}(i)
	}
	wg.Wait()
	active, err := os.ReadFile(filepath.Join(ws, ".yolo", "receipts.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	archive, err := os.ReadFile(filepath.Join(ws, ".yolo", "receipts.jsonl.1"))
	if err != nil {
		t.Fatal(err)
	}
	if len(active) > MaxBytes || len(archive) > MaxBytes {
		t.Fatalf("retention exceeded: active=%d archive=%d", len(active), len(archive))
	}
	if got := bytes.Count(append(archive, active...), []byte("record\n")); got != writers*each {
		t.Fatalf("kept %d records, want %d", got, writers*each)
	}
}

func TestAppendBeneathRefusesLinkedSink(t *testing.T) {
	ws := t.TempDir()
	if err := os.Mkdir(filepath.Join(ws, ".yolo"), 0o755); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(ws, ".yolo", "receipts.jsonl")); err != nil {
		t.Fatal(err)
	}
	open := func(name string, flag int, perm os.FileMode) (*os.File, error) {
		return paths.OpenWorkspaceStateFile(ws, name, flag, perm)
	}
	if err := AppendBeneath(open, "receipts.jsonl", []byte("record\n")); err == nil {
		t.Fatal("linked sink was accepted")
	}
	if got, _ := os.ReadFile(victim); string(got) != "safe" {
		t.Fatalf("linked sink modified victim: %q", got)
	}
}

func TestAppendBeneathNeverFollowsArchiveOrLockLinks(t *testing.T) {
	for _, name := range []string{"receipts.jsonl.1", "receipts.jsonl.lock"} {
		t.Run(name, func(t *testing.T) {
			ws := t.TempDir()
			if err := os.Mkdir(filepath.Join(ws, ".yolo"), 0755); err != nil {
				t.Fatal(err)
			}
			victim := filepath.Join(t.TempDir(), "victim")
			if err := os.WriteFile(victim, []byte("safe"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(victim, filepath.Join(ws, ".yolo", name)); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(ws, ".yolo", "receipts.jsonl"), bytes.Repeat([]byte("old\n"), MaxBytes/4+1), 0600); err != nil {
				t.Fatal(err)
			}
			open := func(name string, flag int, perm os.FileMode) (*os.File, error) {
				return paths.OpenWorkspaceStateFile(ws, name, flag, perm)
			}
			if err := AppendBeneath(open, "receipts.jsonl", []byte("record\n")); err != nil {
				t.Fatal(err)
			}
			if got, _ := os.ReadFile(victim); string(got) != "safe" {
				t.Fatalf("followed %s: %q", name, got)
			}
		})
	}
}

// A LOCK CREATE THAT LOSES A RACE: on macOS an O_CREATE open of the sidecar lock, racing other
// first appends to the same sink, has failed with ENOENT although its directory exists (the
// maintainer's Mac, 2026-10-16: TestAppendBeneathSerializesTrimAndConcurrentAppends lost the
// records of the appends that hit it). The append opens the lock again rather than dropping
// its record.
func TestAppendBeneathRetriesALockCreateThatReportsNoSuchFile(t *testing.T) {
	ws := t.TempDir()
	if err := os.Mkdir(filepath.Join(ws, ".yolo"), 0o755); err != nil {
		t.Fatal(err)
	}
	failures := 2
	open := func(name string, flag int, perm os.FileMode) (*os.File, error) {
		if name == "receipts.jsonl.lock" && flag&os.O_CREATE != 0 && failures > 0 {
			failures--
			return nil, &fs.PathError{Op: "openat", Path: name, Err: syscall.ENOENT}
		}
		return paths.OpenWorkspaceStateFile(ws, name, flag, perm)
	}
	if err := AppendBeneath(open, "receipts.jsonl", []byte("record\n")); err != nil {
		t.Fatalf("the append gave up on a lock create that lost a race: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(ws, ".yolo", "receipts.jsonl")); string(got) != "record\n" {
		t.Fatalf("the log holds %q, want the record", got)
	}
}

// A LOCK THAT CANNOT BE OPENED AT ALL is still the open's error, after a bounded number of tries.
func TestAppendBeneathReportsALockThatNeverOpens(t *testing.T) {
	calls := 0
	open := func(name string, flag int, perm os.FileMode) (*os.File, error) {
		calls++
		return nil, &fs.PathError{Op: "openat", Path: name, Err: syscall.ENOENT}
	}
	if err := AppendBeneath(open, "receipts.jsonl", []byte("record\n")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want the open's ENOENT", err)
	}
	if calls > 10 {
		t.Fatalf("the lock open was tried %d times, want a small bound", calls)
	}
}
