package claudeview

// These participants implement only the specified fake directory-lock protocol. They never
// run Claude, contact an upstream, or establish vendor/Keychain behavior on any backend.
import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const syntheticCredential = `{"claudeAiOauth":{"accessToken":"fake-access","refreshToken":"fake-refresh"},"mcpOAuth":{"fake":"preserve"}}`
const syntheticRefreshLock = ".oauth_refresh.lock"

// TestNativeSyntheticParticipant is selected only in a child test process. Its environment
// points at a fresh scratch directory, never the invoking user's actual credential store.
func TestNativeSyntheticParticipant(t *testing.T) {
	if os.Getenv("YOLO_TEST_FAKE_STORAGE_PARTICIPANT") != "1" {
		t.Skip("subprocess helper only")
	}
	dir := os.Getenv(SecureStorageEnv)
	if dir == "" {
		t.Fatal("missing synthetic directory")
	}
	lock := filepath.Join(dir, StorageLockDir)
	if err := os.Mkdir(lock, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(os.Stdout, "locked\n"); err != nil {
		t.Fatal(err)
	}
	var release [1]byte
	if _, err := io.ReadFull(os.Stdin, release[:]); err != nil {
		t.Fatal(err)
	}
	// A cooperating fake writer replaces the credential before releasing the lock.
	tmp := filepath.Join(dir, "fake-participant.tmp")
	if err := os.WriteFile(tmp, []byte(syntheticCredential), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, filepath.Join(dir, ViewFile)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
}

func startSyntheticParticipant(t *testing.T, dir string) (*exec.Cmd, io.WriteCloser) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeSyntheticParticipant$")
	// Minimal environment: no inherited credential-store pointer or real HOME/config.
	cmd.Env = []string{"YOLO_TEST_FAKE_STORAGE_PARTICIPANT=1", SecureStorageEnv + "=" + dir}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	// The process deadline also bounds this pipe read if the handshake never arrives.
	if line, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || line != "locked\n" {
		t.Fatalf("fake participant handshake = %q, %v; %s", line, err, stderr.String())
	}
	return cmd, stdin
}

func TestNativeSyntheticSharedFileRereadsAfterParticipantRelease(t *testing.T) {
	dir := t.TempDir()
	refresh := filepath.Join(dir, syntheticRefreshLock)
	if err := os.Mkdir(refresh, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd, release := startSyntheticParticipant(t, dir)
	// A second process reaches the same mkdir namespace, not a workspace-local lock.
	if err := os.Mkdir(filepath.Join(dir, StorageLockDir), 0o755); !errors.Is(err, os.ErrExist) {
		t.Fatalf("second storage lock acquisition = %v, want exists", err)
	}
	type result struct {
		wrote, locked bool
		err           error
	}
	started, mutated := make(chan struct{}), make(chan struct{})
	done := make(chan result, 1)
	go func() {
		close(started)
		w, l, err := UpdateSharedFile(dir, ViewFile, func(current []byte) ([]byte, error) {
			close(mutated)
			if string(current) != syntheticCredential {
				return nil, errors.New("writer did not reread the fake participant's replacement")
			}
			return bytes.Replace(current, []byte("fake-access"), []byte("fake-rotated"), 1), nil
		})
		done <- result{w, l, err}
	}()
	<-started
	select {
	case <-mutated:
		t.Fatal("shared-file mutation entered while the fake process held the lock")
	case <-time.After(100 * time.Millisecond):
	}
	if _, err := release.Write([]byte{'x'}); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-done:
		if r.err != nil || !r.wrote || !r.locked {
			t.Fatalf("shared update = %+v, want written under lock", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shared update did not finish after participant release")
	}
	want := strings.Replace(syntheticCredential, "fake-access", "fake-rotated", 1)
	if got, err := os.ReadFile(filepath.Join(dir, ViewFile)); err != nil || string(got) != want {
		t.Fatalf("shared replacement did not preserve unrelated fake data: %v", err)
	}
	if err := os.Mkdir(refresh, 0o755); !errors.Is(err, os.ErrExist) {
		t.Fatalf("storage write removed the separate fake refresh lock: %v", err)
	}
}

func TestNativeSyntheticDeadParticipantStorageLockRecovery(t *testing.T) {
	dir := t.TempDir()
	cmd, _ := startSyntheticParticipant(t, dir)
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("killed participant unexpectedly exited successfully")
	}
	lock := filepath.Join(dir, StorageLockDir)
	if _, err := os.Stat(lock); err != nil {
		t.Fatalf("process death should leave the directory lock: %v", err)
	}
	// Death alone cannot identify staleness. Advance the fixture's mtime explicitly rather
	// than claim vendor heartbeat/death detection or wait out the real stale period.
	old := time.Now().Add(-time.Minute)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	wrote, locked, err := UpdateSharedFile(dir, ViewFile, func([]byte) ([]byte, error) {
		return []byte(syntheticCredential), nil
	})
	if err != nil || !wrote || !locked {
		t.Fatalf("stale fake participant recovery = %v, %v, %v", wrote, locked, err)
	}
	if _, err := os.Stat(lock); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("recovered storage lock was not released: %v", err)
	}
}

func TestNativeSyntheticSharedFileAtomicReplacement(t *testing.T) {
	dir := t.TempDir()
	a := []byte(strings.Repeat("fake-old-", 8192))
	b := []byte(strings.Repeat("fake-new-", 8192))
	write := func(data []byte) error {
		_, _, err := UpdateSharedFile(dir, ViewFile, func([]byte) ([]byte, error) { return data, nil })
		return err
	}
	if err := write(a); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		for range 100 {
			if err := write(b); err != nil {
				done <- err
				return
			}
			if err := write(a); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	for {
		got, err := os.ReadFile(filepath.Join(dir, ViewFile))
		if err != nil || (!bytes.Equal(got, a) && !bytes.Equal(got, b)) {
			t.Errorf("reader saw neither complete fake file: length %d, error %v", len(got), err)
			<-done // Reap the writer before TempDir cleanup, even on failure.
			return
		}
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			fi, err := os.Stat(filepath.Join(dir, ViewFile))
			if err != nil || fi.Mode().Perm() != 0o600 {
				t.Fatalf("credential mode is not 0600: %v", err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 1 || entries[0].Name() != ViewFile {
				t.Fatalf("shared writer left temporary/lock files: %v, %v", entries, err)
			}
			return
		default:
		}
	}
}
