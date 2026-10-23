package macosuser

// fontcopy.go is how `share_host_fonts` reaches the SEPARATE ACCOUNT without the account ever
// reaching into your home (the rule ctxlinks.go states for every context link): yolo, running as
// you, copies the readable font files of ~/Library/Fonts into a yolo-owned folder under the
// staging root that the sandbox account may read, and the account's fonts.conf and Seatbelt
// allow name the copy. Nothing is granted inside your home, so turning the opt-in off leaves no
// opening behind; it removes the copy (RemoveFontCopy).
//
// ONE SHARED COPY, refreshed only when the source changes: its folder is named by a digest of
// every copied file's relative path, size and modification time, so a launch whose source
// matches the copy's name reuses it. SESSIONS DO NOT RACE: a new copy is built in a `.new-*`
// folder and renamed into place; a launch that loses the rename to another uses the winner's,
// which has the same contents by construction. Older copies are removed once a new one is in
// place.
//
// THE SOURCE IS READ WITHOUT FOLLOWING LINKS: a ~/Library/Fonts that is itself a symbolic link
// is not copied at all, a link inside it is skipped and counted, and each file is opened with
// O_NOFOLLOW and checked to be a regular file. A file yolo cannot read is counted and named.
//
// READ ACCESS FOR THE ACCOUNT is one inheriting access entry on the copy root, which yolo owns,
// set when it creates the root: everything later created under it inherits read for the sandbox
// account alone, while the folders stay 0700 and the files 0600 to every other account.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// fontCopyLeaf is the copy root's name under the staging root.
const fontCopyLeaf = "fonts"

// fontCopyRoot is where the shared copy lives: <stateDir>/fonts.
func fontCopyRoot() string { return stateDir + "/" + fontCopyLeaf }

// fontCopyNewPrefix names a copy being built; one older than fontCopyStaleAfter is a launch that
// died mid-build, and the next copy removes it.
const (
	fontCopyNewPrefix  = ".new-"
	fontCopyStaleAfter = time.Hour
)

// FontCopy is what one refresh of the shared copy found.
type FontCopy struct {
	// Dir is the copy the sandbox reads, "" when there is none.
	Dir string
	// SourceIsLink is true when the source folder itself is a symbolic link, which is not copied.
	SourceIsLink bool
	// Links counts symbolic links inside the source, skipped.
	Links int
	// Unreadable names the files (relative to the source) yolo could not read, not copied.
	Unreadable []string
	// Err is why there is no copy, when something failed.
	Err string
}

// fontCopyGrant is the inheriting access entry the copy root carries: list and read for the
// sandbox account, inherited by every folder and file created under it.
func fontCopyGrant(root string) []string {
	return []string{chmodBin, "+a", "user:" + SandboxUser + " allow list,search," +
		sandboxFileReadRights + ",file_inherit,directory_inherit", root}
}

// fontSourceFile is one regular file of the source the copy will hold.
type fontSourceFile struct {
	rel     string
	size    int64
	modTime int64
}

// stageFontCopy refreshes the shared copy of src under root, running the root's access entry
// through run when it creates the root.
func stageFontCopy(src, root string, run func([]string) int) FontCopy {
	fi, err := os.Lstat(src)
	if err != nil {
		return FontCopy{Err: err.Error()}
	}
	if fi.Mode()&fs.ModeSymlink != 0 {
		return FontCopy{SourceIsLink: true}
	}
	if !fi.IsDir() {
		return FontCopy{Err: src + " is not a folder"}
	}
	files, links, unreadable, err := listFontSource(src)
	if err != nil {
		return FontCopy{Err: err.Error()}
	}
	out := FontCopy{Links: links, Unreadable: unreadable}
	if err := ensureFontCopyRoot(root, run); err != nil {
		out.Err = err.Error()
		return out
	}
	dir := filepath.Join(root, fontSourceDigest(files))
	if di, err := os.Lstat(dir); err == nil && di.IsDir() {
		out.Dir = dir
		pruneFontCopies(root, filepath.Base(dir))
		return out
	}
	tmp, err := os.MkdirTemp(root, fontCopyNewPrefix)
	if err != nil {
		out.Err = err.Error()
		return out
	}
	for _, f := range files {
		if copyErr := copyFontFile(filepath.Join(src, f.rel), filepath.Join(tmp, f.rel)); copyErr != nil {
			// Read a moment ago and not now: named with the others rather than failing the copy.
			out.Unreadable = append(out.Unreadable, f.rel)
		}
	}
	if err := os.Rename(tmp, dir); err != nil {
		_ = os.RemoveAll(tmp)
		// Another launch renamed the same contents into place first: use that one.
		if di, statErr := os.Lstat(dir); statErr != nil || !di.IsDir() {
			out.Err = err.Error()
			return out
		}
	}
	out.Dir = dir
	pruneFontCopies(root, filepath.Base(dir))
	return out
}

// listFontSource walks src without following links: the regular files it can open, the links it
// skipped, and the files it could not read.
func listFontSource(src string) (files []fontSourceFile, links int, unreadable []string, err error) {
	err = filepath.WalkDir(src, func(p string, d fs.DirEntry, walkErr error) error {
		rel, _ := filepath.Rel(src, p)
		if walkErr != nil {
			if p == src {
				return walkErr
			}
			unreadable = append(unreadable, rel)
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			links++
			return nil
		case d.IsDir() || !d.Type().IsRegular():
			return nil
		}
		f, openErr := openNoFollow(p)
		if openErr != nil {
			unreadable = append(unreadable, rel)
			return nil
		}
		info, statErr := f.Stat()
		_ = f.Close()
		if statErr != nil || !info.Mode().IsRegular() {
			unreadable = append(unreadable, rel)
			return nil
		}
		files = append(files, fontSourceFile{rel: rel, size: info.Size(), modTime: info.ModTime().UnixNano()})
		return nil
	})
	return files, links, unreadable, err
}

// openNoFollow opens p for reading, refusing a symbolic link at its last component.
func openNoFollow(p string) (*os.File, error) {
	return os.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
}

// fontSourceDigest names a copy by its files' relative paths, sizes and modification times.
func fontSourceDigest(files []fontSourceFile) string {
	h := sha256.New()
	for _, f := range files {
		fmt.Fprintf(h, "%s\x00%d\x00%d\n", f.rel, f.size, f.modTime)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// ensureFontCopyRoot makes the copy root, 0700 with the sandbox account's inheriting read entry,
// when it is not there. A root that exists is used as it is; one that is a link is refused.
//
// THE ROOT APPEARS WITH ITS ENTRY ALREADY ON IT: it is made beside its final name, granted, then
// renamed into place, so a launch racing this one never builds a copy in a root whose entry is
// not there yet (what is created there would not inherit it). The loser of the rename uses the
// winner's root.
func ensureFontCopyRoot(root string, run func([]string) int) error {
	usable := func() (bool, error) {
		fi, err := os.Lstat(root)
		if err != nil {
			return false, nil
		}
		if !fi.IsDir() || fi.Mode()&fs.ModeSymlink != 0 {
			return false, errors.New(root + " is not a folder yolo made")
		}
		return true, nil
	}
	if ok, err := usable(); ok || err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(root), "."+filepath.Base(root)+fontCopyNewPrefix)
	if err != nil {
		return err
	}
	if run == nil || run(fontCopyGrant(tmp)) != 0 {
		_ = os.RemoveAll(tmp)
		return errors.New("could not grant " + SandboxUser + " read on " + root)
	}
	if err := os.Rename(tmp, root); err != nil {
		_ = os.RemoveAll(tmp)
		if ok, uerr := usable(); !ok {
			if uerr != nil {
				return uerr
			}
			return err
		}
	}
	return nil
}

// copyFontFile copies one regular file, not following a link at src, creating dst's folders.
func copyFontFile(src, dst string) error {
	in, err := openNoFollow(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if info, err := in.Stat(); err != nil || !info.Mode().IsRegular() {
		return errors.New(src + " is not a regular file")
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// pruneFontCopies removes every copy under root but keep, and a `.new-*` build older than
// fontCopyStaleAfter. Best-effort: a copy that cannot be removed now is removed next time.
func pruneFontCopies(root, keep string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if name == keep {
			continue
		}
		if strings.HasPrefix(name, fontCopyNewPrefix) {
			if info, err := e.Info(); err != nil || time.Since(info.ModTime()) < fontCopyStaleAfter {
				continue
			}
		}
		_ = os.RemoveAll(filepath.Join(root, name))
	}
}

// removeFontCopy removes the shared copy, when the opt-in is off. Best-effort.
func removeFontCopy(root string) {
	if fi, err := os.Lstat(root); err == nil && fi.IsDir() && fi.Mode()&fs.ModeSymlink == 0 {
		_ = os.RemoveAll(root)
	}
}
