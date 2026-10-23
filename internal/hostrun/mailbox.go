package hostrun

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// The mailbox's host side (design §5.2). Every create is exclusive and no-follow beneath a held
// root, so a link, hardlink or FIFO the jail plants at any name fails the create instead of
// being followed or truncated (HX-D7). The host never reads a file here (HX-P3).

// createFlags is every host create in the mailbox.
const createFlags = os.O_WRONLY | os.O_CREATE | os.O_EXCL | syscall.O_NOFOLLOW | syscall.O_NONBLOCK

// createExclusive creates name below r, failing if anything is there.
func createExclusive(r *os.Root, name string) (*os.File, error) {
	return r.OpenFile(name, createFlags, 0o600)
}

func writeExclusive(r *os.Root, name string, data []byte) error {
	f, err := createExclusive(r, name)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func marshal(v any) []byte {
	b, _ := json.MarshalIndent(v, "", "  ")
	return append(b, '\n')
}

// writeJSONRename writes v to final below r by an exclusive temporary name and a rename, so a
// reader sees the whole file or none.
func writeJSONRename(r *os.Root, final string, v any) error {
	tmp := "." + final + ".tmp"
	if err := writeExclusive(r, tmp, marshal(v)); err != nil {
		return err
	}
	if err := r.Rename(tmp, final); err != nil {
		_ = r.Remove(tmp)
		return err
	}
	return nil
}

// buildDir is a run directory under construction: made as `.<id>.tmp`, filled, then renamed
// to `<id>` by publish. root stays valid across the rename: it holds a descriptor.
type buildDir struct {
	mb   *os.Root
	id   string
	root *os.Root
}

// newBuildDir mints an id and makes its build directory, minting once more on a collision.
func newBuildDir(mb *os.Root, mbPath string, now time.Time, rnd io.Reader) (*buildDir, error) {
	var lastErr error
	for range 2 {
		id, err := NewRunID(now, rnd)
		if err != nil {
			return nil, err
		}
		tmp := TmpName(id)
		if err := mb.Mkdir(tmp, 0o700); err != nil {
			lastErr = err
			if errors.Is(err, fs.ErrExist) {
				continue
			}
			return nil, err
		}
		r, err := openSub(mb, tmp, filepath.Join(mbPath, tmp))
		if err != nil {
			return nil, err
		}
		return &buildDir{mb: mb, id: id, root: r}, nil
	}
	return nil, lastErr
}

// publish renames the build directory into place.
func (b *buildDir) publish() error { return b.mb.Rename(TmpName(b.id), b.id) }

// abandon removes a build directory that will not be published.
func (b *buildDir) abandon() {
	b.root.Close()
	_ = b.mb.RemoveAll(TmpName(b.id))
}

// deliverRefusal writes a run directory holding only refused.json (HX-D39).
func deliverRefusal(mb *os.Root, mbPath string, ref Refusal, now time.Time, rnd io.Reader) (string, error) {
	b, err := newBuildDir(mb, mbPath, now, rnd)
	if err != nil {
		return "", err
	}
	ref.ID = b.id
	if err := writeExclusive(b.root, RefusedFile, marshal(ref)); err != nil {
		b.abandon()
		return "", err
	}
	if err := b.publish(); err != nil {
		b.abandon()
		return "", err
	}
	b.root.Close()
	return b.id, nil
}

// pruneAge is how old a run directory gets before a host form removes it (HX-D43).
const pruneAge = 24 * time.Hour

// prune removes every run directory, and leftover build directory, whose id time is more than
// pruneAge before now, beneath the held mailbox root. It removes nothing else.
func prune(mb *os.Root, now time.Time) error {
	d, err := mb.Open(".")
	if err != nil {
		return err
	}
	names, err := d.Readdirnames(-1)
	d.Close()
	if err != nil {
		return err
	}
	var first error
	for _, n := range names {
		id := n
		if tid, ok := ParseTmpName(n); ok {
			id = tid
		}
		at, ok := ParseRunID(id)
		if !ok || now.Sub(at) <= pruneAge {
			continue
		}
		if err := mb.RemoveAll(n); err != nil && first == nil {
			first = err
		}
	}
	return first
}
