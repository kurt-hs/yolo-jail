//go:build linux

package ctxinject

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAttachRejectsAChangedProcessOrMountNamespaceBeforeSourceMutation(t *testing.T) {
	identity, err := ReadProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	identity.MountNSInode++
	err = Attach(os.Getpid(), identity, "/does/not/exist", "/ctx/ctxinject-test", false)
	if err == nil || !strings.Contains(err.Error(), "target process or mount namespace changed") {
		t.Fatalf("Attach error = %v, want changed namespace refusal before opening the source", err)
	}
}

// TestAttachFollowsNoLinkInTheSource: the host resolved the source before the injector ran, so a
// resolved path has no link in it, and one that has a link now was swapped since. It is refused
// at the open, in the caller's namespace, before anything is copied; so is a source that is not
// a directory, which the injector could only mount on a directory it made.
func TestAttachFollowsNoLinkInTheSource(t *testing.T) {
	identity, err := ReadProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(base, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, src := range []string{link, filepath.Join(link, "."), file} {
		err := Attach(os.Getpid(), identity, src, "/ctx/ctxinject-test", false)
		if err == nil || !strings.Contains(err.Error(), "no link followed") {
			t.Errorf("Attach(%s) = %v; want the source refused at its open", src, err)
		}
	}
}
