package entrypoint

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// renameLoopEntry is injectable so tests can replace the inspected entry at the atomic move
// boundary and verify that a raced-in file is restored rather than unlinked.
var renameLoopEntry = renameNoReplaceBetweenAt

func removeLoopingLink(home string, found loopInspection) error {
	if !found.cycle || !found.inHome {
		return fmt.Errorf("loop is not confined to the jail home")
	}
	rel, err := relativeHomePath(home, found.link)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(home)
	if err != nil {
		return fmt.Errorf("open jail home for confined symlink repair: %w", err)
	}
	defer root.Close()

	parent, base := filepath.Split(rel)
	parent = filepath.Clean(parent)
	if parent == "" {
		parent = "."
	}
	parentRoot, err := openRealHomeParent(root, parent)
	if err != nil {
		return err
	}
	defer parentRoot.Close()
	parentDir, err := parentRoot.Open(".")
	if err != nil {
		return fmt.Errorf("open confined symlink parent %s: %w", filepath.Dir(found.link), err)
	}
	defer parentDir.Close()

	before, err := parentRoot.Lstat(base)
	if err != nil {
		return fmt.Errorf("refusing to repair changed symlink %s: %w; inspect the path and relaunch", found.link, err)
	}
	if before.Mode()&os.ModeSymlink == 0 || !os.SameFile(found.info, before) {
		return fmt.Errorf("refusing to repair changed symlink %s; preserve the current entry, inspect it, then relaunch", found.link)
	}
	if target, err := parentRoot.Readlink(base); err != nil || target != found.rawTarget {
		return fmt.Errorf("refusing to repair changed symlink %s; preserve the current entry, inspect it, then relaunch", found.link)
	}

	for range 16 {
		token := make([]byte, 12)
		if _, err := rand.Read(token); err != nil {
			return fmt.Errorf("cannot allocate a private symlink quarantine name for %s: %w; leave it untouched, inspect it, then relaunch", found.link, err)
		}
		quarantine := ".yolo-loop-quarantine-" + hex.EncodeToString(token)
		if err := parentRoot.Mkdir(quarantine, 0o700); err != nil {
			if errors.Is(err, os.ErrExist) {
				continue
			}
			return fmt.Errorf("cannot create a private quarantine beside %s: %w; leave the link untouched, inspect it, then relaunch", found.link, err)
		}
		quarantineInfo, err := parentRoot.Lstat(quarantine)
		if err != nil || !quarantineInfo.IsDir() || quarantineInfo.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("cannot verify the private quarantine beside %s; leave the link untouched, inspect it, then relaunch", found.link)
		}
		quarantineRoot, err := parentRoot.OpenRoot(quarantine)
		if err != nil {
			return fmt.Errorf("cannot open a private quarantine beside %s: %w; leave the link untouched, inspect it, then relaunch", found.link, err)
		}
		openedInfo, err := quarantineRoot.Stat(".")
		if err != nil || !os.SameFile(quarantineInfo, openedInfo) {
			_ = quarantineRoot.Close()
			return fmt.Errorf("refusing changed quarantine directory beside %s; leave the link untouched, inspect the path, then relaunch", found.link)
		}
		quarantineDir, err := quarantineRoot.Open(".")
		if err != nil {
			_ = quarantineRoot.Close()
			_ = parentRoot.Remove(quarantine)
			return fmt.Errorf("cannot open a private quarantine beside %s: %w; leave the link untouched, inspect it, then relaunch", found.link, err)
		}

		moveErr := renameLoopEntry(int(parentDir.Fd()), base, int(quarantineDir.Fd()), "entry")
		if moveErr != nil {
			_ = quarantineDir.Close()
			_ = quarantineRoot.Close()
			_ = parentRoot.Remove(quarantine)
			if errors.Is(moveErr, unix.EEXIST) {
				continue
			}
			return fmt.Errorf("could not atomically quarantine symlink %s: %w; it was not removed, inspect the path and relaunch", found.link, moveErr)
		}

		moved, statErr := quarantineRoot.Lstat("entry")
		movedTarget, targetErr := quarantineRoot.Readlink("entry")
		if statErr != nil || targetErr != nil || moved.Mode()&os.ModeSymlink == 0 ||
			!os.SameFile(found.info, moved) || movedTarget != found.rawTarget {
			restoreErr := renameLoopEntry(int(quarantineDir.Fd()), "entry", int(parentDir.Fd()), base)
			_ = quarantineDir.Close()
			_ = quarantineRoot.Close()
			if restoreErr != nil {
				preserved := filepath.Join(filepath.Dir(found.link), quarantine, "entry")
				return fmt.Errorf("refusing to remove changed entry for %s; the moved entry was preserved at %s and the concurrent destination was left untouched (%v); inspect both paths before relaunch", found.link, preserved, restoreErr)
			}
			_ = parentRoot.Remove(quarantine)
			return fmt.Errorf("refusing to remove changed entry for %s; it was restored untouched, so inspect the symlink and relaunch", found.link)
		}

		// The exact inspected symlink now sits in a private, same-parent quarantine. Recheck
		// its identity there before unlinking; no path outside the anchored home is traversed.
		current, statErr := quarantineRoot.Lstat("entry")
		currentTarget, targetErr := quarantineRoot.Readlink("entry")
		if statErr != nil || targetErr != nil || !os.SameFile(found.info, current) || currentTarget != found.rawTarget {
			restoreErr := renameLoopEntry(int(quarantineDir.Fd()), "entry", int(parentDir.Fd()), base)
			_ = quarantineDir.Close()
			_ = quarantineRoot.Close()
			if restoreErr != nil {
				preserved := filepath.Join(filepath.Dir(found.link), quarantine, "entry")
				return fmt.Errorf("refusing to remove changed entry for %s; the moved entry was preserved at %s and the concurrent destination was left untouched (%v); inspect both paths before relaunch", found.link, preserved, restoreErr)
			}
			_ = parentRoot.Remove(quarantine)
			return fmt.Errorf("refusing to remove changed entry for %s; it was restored untouched, so inspect the symlink and relaunch", found.link)
		}
		if err := quarantineRoot.Remove("entry"); err != nil {
			_ = quarantineDir.Close()
			_ = quarantineRoot.Close()
			preserved := filepath.Join(filepath.Dir(found.link), quarantine, "entry")
			return fmt.Errorf("the verified loop was moved out of %s but could not be unlinked; it is preserved at %s; inspect it, then relaunch: %w", found.link, preserved, err)
		}
		_ = quarantineDir.Close()
		_ = quarantineRoot.Close()
		if err := parentRoot.Remove(quarantine); err != nil {
			return fmt.Errorf("the verified loop at %s was removed, but its empty quarantine %s remains; inspect it and relaunch: %w", found.link, filepath.Join(filepath.Dir(found.link), quarantine), err)
		}
		return nil
	}
	return fmt.Errorf("could not allocate an unused private quarantine name beside %s; leave the link untouched, inspect it, then relaunch", found.link)
}

func openRealHomeParent(home *os.Root, rel string) (*os.Root, error) {
	current, err := home.OpenRoot(".")
	if err != nil {
		return nil, fmt.Errorf("open confined jail home: %w", err)
	}
	if rel == "." {
		return current, nil
	}
	for _, part := range splitPath(rel) {
		info, err := current.Lstat(part)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			_ = current.Close()
			return nil, fmt.Errorf("refusing symlink repair through non-directory or linked parent %s; preserve it, inspect the path, then relaunch", filepath.Join(home.Name(), rel))
		}
		next, err := current.OpenRoot(part)
		if err != nil {
			_ = current.Close()
			return nil, fmt.Errorf("open confined symlink parent %s: %w; inspect it and relaunch", filepath.Join(home.Name(), rel), err)
		}
		opened, err := next.Stat(".")
		_ = current.Close()
		if err != nil || !os.SameFile(info, opened) {
			_ = next.Close()
			return nil, fmt.Errorf("refusing symlink repair through changed parent %s; preserve it, inspect the path, then relaunch", filepath.Join(home.Name(), rel))
		}
		current = next
	}
	return current, nil
}

func relativeHomePath(home, path string) (string, error) {
	homeAbs, err := filepath.Abs(home)
	if err != nil {
		return "", err
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(filepath.Clean(homeAbs), filepath.Clean(pathAbs))
	if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("refusing symlink repair outside the jail home at %s; leave it untouched, repair or remove the link yourself, then relaunch", path)
	}
	return rel, nil
}

func splitPath(rel string) []string {
	var parts []string
	for rel != "." && rel != string(filepath.Separator) {
		part := filepath.Base(rel)
		parts = append([]string{part}, parts...)
		rel = filepath.Dir(rel)
	}
	return parts
}
