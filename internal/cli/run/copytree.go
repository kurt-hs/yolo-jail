package run

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// copyTree copies a staged tree to dest at mode 0o644, or 0o755 for a file whose source
// carries an execute bit, keeping every symbolic link as the same link (copyTreeWith). Pack
// staging itself does not use it: an embedded pack is copied into the launch's fresh tree by
// treesync, which applies this same mode rule; copyTree is left for trees nothing binds (the
// macos-user home overlay's skills and pack `files` trees).
//
// The exec bit is PRESERVED here, matching packstage's rule for a configured pack: the
// trust question an exec bit raises is "may this content ship an executable at all", and
// for an embedded pack the answer is yes by construction — it ships with yolo and is
// reviewed with the release. Stripping the bit at copy time would only mean a shipped pack
// cannot carry a working script while a user's own pack can.
func copyTree(src, dest string) error {
	return copyTreeWith(src, dest, func(perm fs.FileMode) fs.FileMode {
		if perm&0o111 != 0 {
			return 0o755
		}
		return 0o644
	})
}

// copyTreeKeepingLinks is copyTree with each regular file's own permission bits, owner read and
// write added, for a patched extension's per-launch copy (buildMacosHomeOverlayTree).
func copyTreeKeepingLinks(src, dest string) error {
	return copyTreeWith(src, dest, func(perm fs.FileMode) fs.FileMode { return perm | 0o600 })
}

// copyTreeWith copies the tree src to dest, recreating each symbolic link as the same link and
// NEVER READING THROUGH ONE (docs/reference/macos-user-nix-and-features.md, CD-1): directories
// 0755, regular files at mode(their permission bits), and anything else (a socket, a FIFO, a
// device) refused, since skipping it would report a partial copy as a whole one.
//
// WHY A LINK STAYS A LINK. The macos-user overlay is copied by the host user, outside the sandbox,
// so a copy that read through a link would hand the sandbox account the bytes of any file the host
// user can read: a fetched pack's `skills/x.md -> ~/.ssh/id_ed25519`, were it ever to reach this
// copy. A link kept as a link is resolved later by the sandbox account, with its own rights, so it
// reaches nothing that account could not open by path. The next hop (macosuser's `cp -R` into the
// staged root) keeps links as links too, so the rule holds end to end.
//
// A regular file is opened O_NOFOLLOW and non-blocking and checked by fstat after the open, so an
// entry swapped for a link or a FIFO between the listing and the open is refused, never read
// through and never blocked on.
func copyTreeWith(src, dest string, mode func(fs.FileMode) fs.FileMode) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			return os.Symlink(link, target)
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case d.Type().IsRegular():
			return copyRegularNoFollow(p, target, mode)
		default:
			return fmt.Errorf("%s is a %s, which the overlay copy cannot reproduce", p, d.Type())
		}
	})
}

// copyRegularNoFollow copies the regular file p to target at mode(p's permission bits). It opens p
// without following a link and without blocking, and refuses it unless fstat says it is a regular
// file.
func copyRegularNoFollow(p, target string, mode func(fs.FileMode) fs.FileMode) error {
	in, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return fmt.Errorf("opening %s without following a link: %w", p, err)
	}
	defer in.Close()
	fi, err := in.Stat()
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%s is a %s, which the overlay copy cannot reproduce", p, fi.Mode().Type())
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	m := mode(fi.Mode().Perm())
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, m)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	// Explicit chmod: OpenFile's mode is masked by umask and ignored entirely for an existing
	// file, so neither path reliably lands the exec bit on its own.
	return os.Chmod(target, m)
}
