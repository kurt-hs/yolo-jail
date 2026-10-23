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
	base := tempDir(t)
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

	link := filepath.Join(tempDir(t), "rootlink")
	untrustedSymlink(t, root, link)
	entries, _, err = collect(t, link)
	if err != nil || len(entries) != 1 || entries["."].Kind != Symlink {
		t.Errorf("an untrusted symlink root = %v (%v), want one Symlink entry and nothing under it", entries, err)
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

// untrustedSymlink makes link → target owned by someone other than root: as root, it is handed to
// uid 65534, as anyone else it is theirs already.
func untrustedSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() == 0 {
		if err := os.Lchown(link, 65534, 65534); err != nil {
			t.Fatal(err)
		}
	}
}

// trustedSymlink makes link → target owned by root in a directory only root can write; skipped
// where the test is not root and so cannot.
func trustedSymlink(t *testing.T, target, link string) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("making a root-owned symlink needs root")
	}
	if err := os.Chmod(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

// THE PARENT-DIRECTORY SWAP (BG-D22): the checkout is proj/vendor/tool, and an agent able to write
// proj/ has made vendor a link to a host directory that also holds a tool/. O_NOFOLLOW on the whole
// path would cover only tool; the walk reaches the root a component at a time, refuses the link, and
// reads nothing — Resolve refuses it the same way.
func TestAnUntrustedLinkAboveTheRootRefusesTheWalk(t *testing.T) {
	base := tempDir(t)
	proj, host := filepath.Join(base, "proj"), filepath.Join(base, "host")
	for _, d := range []string{filepath.Join(proj, "real", "tool"), filepath.Join(host, "tool")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(host, "tool", "id_ed25519"), []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	untrustedSymlink(t, host, filepath.Join(proj, "vendor"))
	root := filepath.Join(proj, "vendor", "tool")
	visited := 0
	err := Walk(root, nil, func(Entry) error { visited++; return nil })
	var ul *UntrustedLinkError
	if !errors.As(err, &ul) || ul.Path != filepath.Join(proj, "vendor") || visited != 0 {
		t.Errorf("Walk through an untrusted vendor link = %v, %d visits; want an UntrustedLinkError naming it and none", err, visited)
	}
	if _, err := Resolve(root); !errors.As(err, &ul) {
		t.Errorf("Resolve through an untrusted vendor link = %v, want an UntrustedLinkError", err)
	}
	// Its last component too: Resolve refuses an untrusted link that IS the path.
	if _, err := Resolve(filepath.Join(proj, "vendor")); !errors.As(err, &ul) {
		t.Errorf("Resolve of an untrusted link = %v, want an UntrustedLinkError", err)
	}
	if _, err := Resolve(filepath.Join(proj, "nope", "x")); err == nil {
		t.Errorf("Resolve of a missing path succeeded")
	}
	if got, err := Resolve(filepath.Join(proj, "real", "tool")); err != nil || got != filepath.Join(proj, "real", "tool") {
		t.Errorf("Resolve of a link-free path = %q, %v; want it as written", got, err)
	}
}

// A ROOT-OWNED LINK in a directory only root can write is followed on the way to the root, as
// /var → /private/var on macOS is: no agent can have placed it. The same link in a directory others
// can write (not sticky) is refused, since they could replace it.
func TestATrustedLinkAboveTheRootIsFollowed(t *testing.T) {
	base := tempDir(t)
	real := filepath.Join(base, "real")
	if err := os.MkdirAll(filepath.Join(real, "tool"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "tool", "f"), []byte("plain"), 0o644); err != nil {
		t.Fatal(err)
	}
	trustedSymlink(t, "real", filepath.Join(base, "home"))
	got, err := Resolve(filepath.Join(base, "home", "tool"))
	if err != nil || got != filepath.Join(real, "tool") {
		t.Fatalf("Resolve through a trusted link = %q, %v; want %s", got, err, filepath.Join(real, "tool"))
	}
	_, bodies, err := collect(t, filepath.Join(base, "home", "tool"))
	if err != nil || bodies["f"] != "plain" {
		t.Errorf("Walk through a trusted link = %v, %v; want the tree", bodies, err)
	}

	open := filepath.Join(base, "open")
	if err := os.Mkdir(open, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(open, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(open, "home")); err != nil {
		t.Fatal(err)
	}
	var ul *UntrustedLinkError
	if _, err := Resolve(filepath.Join(open, "home", "tool")); !errors.As(err, &ul) {
		t.Errorf("Resolve through a root link in a world-writable directory = %v, want an UntrustedLinkError", err)
	}
}

// tempDir is t.TempDir with its symlinks resolved where it is minted: on macOS (and under a
// symlinked TMPDIR) the temp dir sits behind a link, which the walk resolves and these tests compare.
func tempDir(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}
