package ctxinject

import (
	"os"
	"strings"
	"testing"
)

// TestRelDestAcceptsOnlyPathsStrictlyUnderTheContextDir: the injector's one jail-side input is a
// destination, and it may name nothing but a child of /ctx (design §5.6).
func TestRelDestAcceptsOnlyPathsStrictlyUnderTheContextDir(t *testing.T) {
	good := map[string]string{
		"/ctx/a":        "a",
		"/ctx/a/b":      "a/b",
		"/ctx/live-1.x": "live-1.x",
	}
	for dest, want := range good {
		got, err := RelDest(dest)
		if err != nil || got != want {
			t.Errorf("RelDest(%q) = %q, %v; want %q", dest, got, err, want)
		}
	}
	for _, dest := range []string{"", "/ctx", "/ctx/", "ctx/a", "/ctxa", "/ctx/../etc", "/ctx/a/../b",
		"/ctx//a", "/ctx/a/", "/ctx/./a", "/etc/passwd", "/ctx/a/.."} {
		if got, err := RelDest(dest); err == nil {
			t.Errorf("RelDest(%q) = %q, nil; want a refusal", dest, got)
		}
	}
}

// TestAttachRefusesBadArgumentsBeforeTouchingAnything: a bad pid or destination is refused before
// any syscall, so nothing is cloned or created.
func TestAttachRefusesBadArgumentsBeforeTouchingAnything(t *testing.T) {
	src := t.TempDir()
	if err := Attach(0, src, "/ctx/x", false); err == nil || !strings.Contains(err.Error(), "pid") {
		t.Errorf("Attach(pid 0) = %v; want a pid refusal", err)
	}
	if err := Attach(os.Getpid(), src, "/etc/x", false); err == nil || !strings.Contains(err.Error(), "/ctx") {
		t.Errorf("Attach(dest /etc/x) = %v; want a destination refusal", err)
	}
	if err := Attach(os.Getpid(), "relative/src", "/ctx/x", false); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Errorf("Attach(relative source) = %v; want an absolute-path refusal", err)
	}
	if err := Detach(-1, "/ctx/x"); err == nil || !strings.Contains(err.Error(), "pid") {
		t.Errorf("Detach(pid -1) = %v; want a pid refusal", err)
	}
	if err := Detach(os.Getpid(), "/ctx"); err == nil {
		t.Errorf("Detach(/ctx) = nil; want a destination refusal")
	}
}

// TestAttachIntoANamespaceWithoutTheTmpfsIsRefused: this test process's own mount namespace has
// no /ctx tmpfs (a dev machine has no /ctx at all; this jail's /ctx is the image's), so an attach
// into it never mounts anything. Whatever step stops it — no privilege to setns, no /ctx, or
// /ctx not a tmpfs — it is an error, and /ctx/ctxinject-test is never created.
func TestAttachIntoANamespaceWithoutTheTmpfsIsRefused(t *testing.T) {
	src := t.TempDir()
	err := Attach(os.Getpid(), src, "/ctx/ctxinject-test", false)
	if err == nil {
		t.Fatal("Attach into a namespace with no /ctx tmpfs succeeded; want a refusal")
	}
	if _, statErr := os.Lstat("/ctx/ctxinject-test"); statErr == nil {
		t.Errorf("the refused attach created /ctx/ctxinject-test (err was %v)", err)
	}
	t.Logf("refusal: %v", err)
}
