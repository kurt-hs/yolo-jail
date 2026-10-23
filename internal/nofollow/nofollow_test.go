package nofollow

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

const secret = "SECRET-HOST-BYTES"

// fixture is a tree beside an "outside" directory holding a secret, the stand-in for ~/.ssh.
func fixture(t *testing.T) (root, outside string) {
	t.Helper()
	base := t.TempDir()
	root, outside = filepath.Join(base, "tree"), filepath.Join(base, "outside")
	for _, d := range []string{filepath.Join(root, "sub"), outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(p, body string) {
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(outside, "id_ed25519"), secret)
	write(filepath.Join(root, "f"), "plain")
	write(filepath.Join(root, "sub", "g"), "nested")
	return root, outside
}

// collect walks root and returns each entry's rel, kind and (for a file) content.
func collect(t *testing.T, root string) (map[string]Entry, map[string]string, error) {
	t.Helper()
	entries, bodies := map[string]Entry{}, map[string]string{}
	err := Walk(root, nil, func(e Entry) error {
		entries[e.Rel] = e
		if e.Kind == File {
			b, err := io.ReadAll(e.File)
			if err != nil {
				return err
			}
			bodies[e.Rel] = string(b)
		}
		return nil
	})
	return entries, bodies, err
}

// A SYMLINK IS REPORTED BY ITS TARGET AND NEVER FOLLOWED: links to a directory and to a file outside
// the tree are each one Symlink entry, and nothing under or behind them is read. A root that is a
// symlink is one Symlink entry too.
func TestAWalkNeverFollowsASymlink(t *testing.T) {
	root, outside := fixture(t)
	if err := os.Symlink(outside, filepath.Join(root, "dirlink")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "id_ed25519"), filepath.Join(root, "filelink")); err != nil {
		t.Fatal(err)
	}
	entries, bodies, err := collect(t, root)
	if err != nil {
		t.Fatal(err)
	}
	for rel, target := range map[string]string{"dirlink": outside, "filelink": filepath.Join(outside, "id_ed25519")} {
		if e := entries[rel]; e.Kind != Symlink || e.Target != target {
			t.Errorf("%s = %+v, want a Symlink to %s", rel, e, target)
		}
	}
	for rel, body := range bodies {
		if strings.Contains(body, secret) {
			t.Errorf("the walk read the secret through %s", rel)
		}
	}
	if _, ok := entries[filepath.Join("dirlink", "id_ed25519")]; ok {
		t.Errorf("the walk descended through a directory symlink")
	}
	if bodies["f"] != "plain" || bodies[filepath.Join("sub", "g")] != "nested" || entries["sub"].Kind != Dir {
		t.Errorf("the tree's own entries were not walked: %v", bodies)
	}

	link := filepath.Join(t.TempDir(), "rootlink")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	entries, _, err = collect(t, link)
	if err != nil || len(entries) != 1 || entries["."].Kind != Symlink {
		t.Errorf("a symlink root = %v (%v), want one Symlink entry and nothing under it", entries, err)
	}
}

// THE RACE: a directory or file swapped for a symlink to the outside after it was listed and
// lstat'ed fails the walk with ErrChanged; the secret is never read.
func TestAnEntrySwappedForASymlinkMidWalkFailsTheWalk(t *testing.T) {
	for _, victim := range []string{"sub", "f"} {
		t.Run(victim, func(t *testing.T) {
			root, outside := fixture(t)
			swapTo := outside
			if victim == "f" {
				swapTo = filepath.Join(outside, "id_ed25519")
			}
			testHookAfterLstat = func(rel string) {
				if rel != victim {
					return
				}
				p := filepath.Join(root, victim)
				if err := os.RemoveAll(p); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(swapTo, p); err != nil {
					t.Fatal(err)
				}
			}
			t.Cleanup(func() { testHookAfterLstat = nil })
			_, bodies, err := collect(t, root)
			if !errors.Is(err, ErrChanged) {
				t.Errorf("the walk = %v, want ErrChanged", err)
			}
			for rel, body := range bodies {
				if strings.Contains(body, secret) {
					t.Errorf("the walk read the secret through %s", rel)
				}
			}
		})
	}
}

// A FIFO is reported, never opened, so it cannot hang the walk.
func TestAFIFOIsReportedNotOpened(t *testing.T) {
	root, _ := fixture(t)
	if err := syscall.Mkfifo(filepath.Join(root, "pipe"), 0o644); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	entries, _, err := collect(t, root)
	if err != nil || entries["pipe"].Kind != Other {
		t.Errorf("a FIFO = %+v (%v), want an Other entry", entries["pipe"], err)
	}
}
