package nativehome

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

func completed(t *testing.T) string {
	t.Helper()
	dir, err := Prepare(t.TempDir(), t.TempDir(), "0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	if err := Complete(dir, map[string]string{"YOLO_SESSION_ID": "0123456789abcdef", "NATIVE_TEST_RELOCATION": dir}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YOLO_SESSION_ID", "")
	t.Setenv("NATIVE_TEST_RELOCATION", "")
	return dir
}

func TestNativeHomeRequiresLaunchAndLeaseGone(t *testing.T) {
	dir := completed(t)
	if release, gone := ClaimGone(dir); gone {
		release()
		t.Fatal("unlaunched home eligible")
	}
	f, err := Enter(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if release, gone := ClaimGone(dir); gone {
		release()
		t.Fatal("provisioning marked a launch")
	}
	f, err = Enter(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if release, gone := ClaimGone(dir); gone {
		release()
		t.Fatal("live command eligible")
	}
	_ = f.Close()
	release, gone := ClaimGone(dir)
	if !gone {
		t.Fatal("ended command not eligible")
	}
	release()
	if err := os.Remove(filepath.Join(dir, packload.EmbeddedLeaseName)); err != nil {
		t.Fatal(err)
	}
	if release, gone := ClaimGone(dir); gone {
		release()
		t.Fatal("absent lease treated as gone")
	}
}

func TestNativeHomeIdentityAndNamespaceAreStrict(t *testing.T) {
	home, ws := t.TempDir(), t.TempDir()
	for _, id := range []string{"", "dry-run", "12345678", "0123456789ABCDEF", "../0123456789abcdef"} {
		if _, err := Prepare(home, ws, id); err == nil {
			t.Fatalf("accepted ID %q", id)
		}
	}
	a, err := Dir(home, ws, "0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Dir(home, t.TempDir(), "0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("workspace namespace missing")
	}
	dir := completed(t)
	if err := Complete(dir, map[string]string{"YOLO_SESSION_ID": "fedcba9876543210"}); err != nil {
		t.Fatal(err)
	}
	if f, err := Enter(dir, true); err == nil {
		_ = f.Close()
		t.Fatal("command identity mismatch accepted")
	}
}

func TestNativeHomeDoesNotFollowNamespaceOrLeaseLinks(t *testing.T) {
	home, ws := t.TempDir(), t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(home, ".yolo-native-sessions")); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(home, ws, "0123456789abcdef"); err == nil {
		t.Fatal("namespace symlink followed")
	}
	dir := completed(t)
	if err := os.Remove(filepath.Join(dir, packload.EmbeddedLeaseName)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "missing"), filepath.Join(dir, packload.EmbeddedLeaseName)); err != nil {
		t.Fatal(err)
	}
	if f, err := Enter(dir, true); err == nil {
		_ = f.Close()
		t.Fatal("lease symlink followed")
	}
}

func TestNativeHomeRetirementKeepsUnknownNamespaceSiblings(t *testing.T) {
	dir := completed(t)
	f, err := Enter(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	stray := filepath.Join(dir, "user-note")
	if err := os.WriteFile(stray, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	release, gone := ClaimGone(dir)
	if !gone {
		t.Fatal("fixture not gone")
	}
	defer release()
	if err := Retire(dir); err == nil {
		t.Fatal("unknown sibling not reported")
	}
	b, err := os.ReadFile(stray)
	if err != nil || string(b) != "keep" {
		t.Fatalf("unknown sibling lost: %q %v", b, err)
	}
}
