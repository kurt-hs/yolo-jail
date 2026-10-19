package prune

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ReapHostSessionHomes' rule, cell by cell: an ended launch's directory goes, a running one's
// stays, a young one is not judged, an unleased one goes only past its floor, and a link into the
// real directory is removed as a link, never followed.
func TestReapHostSessionHomes(t *testing.T) {
	now := time.Now()
	root := t.TempDir()
	real := t.TempDir()
	if err := os.WriteFile(filepath.Join(real, "auth.json"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	ended := mkEmbeddedDir(t, filepath.Join(root, "pi-ended"), true, 2*time.Hour, now)
	if err := os.Symlink(filepath.Join(real, "auth.json"), filepath.Join(ended, "auth.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(ended, now.Add(-2*time.Hour), now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	running := mkEmbeddedDir(t, filepath.Join(root, "pi-running"), true, 2*time.Hour, now)
	holdShared(t, running)
	young := mkEmbeddedDir(t, filepath.Join(root, "pi-young"), true, time.Second, now)
	unleasedYoung := mkEmbeddedDir(t, filepath.Join(root, "pi-unleased-young"), false, 10*time.Minute, now)
	unleasedOld := mkEmbeddedDir(t, filepath.Join(root, "pi-unleased-old"), false, 2*time.Hour, now)

	calls := 0
	dry := ReapHostSessionHomes(root, false, now, func(string) error { calls++; return nil })
	if dry.RemovedCount() != 2 || !exists(ended) || !exists(unleasedOld) || calls != 0 {
		t.Fatalf("a dry run must name two, remove nothing and not capture: %+v calls=%d", dry.Removed, calls)
	}
	var order []string
	r := ReapHostSessionHomes(root, true, now, func(path string) error {
		if !exists(path) {
			t.Errorf("capture ran after the source was removed: %s", path)
		}
		order = append(order, filepath.Base(path))
		return nil
	})
	var removed []string
	for _, e := range r.Removed {
		removed = append(removed, e.Name)
	}
	if strings.Join(removed, ",") != "pi-ended,pi-unleased-old" || strings.Join(order, ",") != strings.Join(removed, ",") {
		t.Errorf("removed %v after captures %v, want ended homes captured before removal", removed, order)
	}
	for _, kept := range []string{running, young, unleasedYoung} {
		if !exists(kept) {
			t.Errorf("%s was reaped", kept)
		}
	}
	if exists(ended) || exists(unleasedOld) {
		t.Errorf("an ended launch's directory survived")
	}
	if data, err := os.ReadFile(filepath.Join(real, "auth.json")); err != nil || string(data) != "secret" {
		t.Errorf("the reap reached through a link into the real directory: %q, %v", data, err)
	}
	if got := ReapHostSessionHomes(filepath.Join(root, "absent"), true, now, nil); got.Declined != "" || len(got.Removed) > 0 {
		t.Errorf("an absent root is no work: %+v", got)
	}
}

func TestReapHostSessionHomesKeepsSourceWhenCaptureFails(t *testing.T) {
	now := time.Now()
	root := t.TempDir()
	ended := mkEmbeddedDir(t, filepath.Join(root, "pi-ended"), true, 2*time.Hour, now)
	called := false
	r := ReapHostSessionHomes(root, true, now, func(path string) error {
		called = true
		if path != ended || !exists(path) {
			t.Fatalf("capture received %s after its source disappeared", path)
		}
		return errors.New("capture refused")
	})
	if !called || !exists(ended) || r.RemovedCount() != 0 || len(r.Removed) != 1 || r.Removed[0].Err == nil {
		t.Fatalf("capture failure must retain and report the source: called=%v exists=%v reap=%+v",
			called, exists(ended), r)
	}
}

// THE `yolo prune` CALL SITE: an ended launch's session home is named and removed under --apply,
// and a running one's is kept. Delete the ReapHostSessionHomes call in Run and this fails.
func TestYoloPruneReapsAnEndedHostLaunchsSessionHome(t *testing.T) {
	o, gs := baseOpts(t)
	now := o.Now()
	root := filepath.Join(gs, HostSessionHomesLeaf)
	ended := mkEmbeddedDir(t, filepath.Join(root, "pi-ended"), true, 2*time.Hour, now)
	running := mkEmbeddedDir(t, filepath.Join(root, "pi-running"), true, 2*time.Hour, now)
	holdShared(t, running)
	o.CaptureHostSessionHome = func(string) error { return nil }
	var buf bytes.Buffer
	o.Out = &buf
	Run(o)
	if !strings.Contains(buf.String(), "pi-ended") || !exists(ended) {
		t.Fatalf("a dry run must name the ended launch's home and keep it:\n%s", buf.String())
	}
	o.Apply = true
	buf.Reset()
	Run(o)
	if exists(ended) {
		t.Errorf("--apply left the ended launch's session home:\n%s", buf.String())
	}
	if !exists(running) {
		t.Errorf("prune removed a running launch's session home")
	}
}
