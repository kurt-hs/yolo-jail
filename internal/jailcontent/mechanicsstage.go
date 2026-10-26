package jailcontent

import (
	"fmt"
	"os"
	"path/filepath"
)

// RenderMechanicsIndexAndMap returns exactly the two applied-state files that
// may be refreshed after an effective live apply. It does not render recipes
// or any other built-in content.
func RenderMechanicsIndexAndMap(facts FactSheet) (map[string][]byte, error) {
	return renderMechanicsIndexAndMap(facts)
}

type mechanicsFileSnapshot struct {
	path   string
	data   []byte
	mode   os.FileMode
	exists bool
}

// RefreshMechanicsFiles updates only jail-mechanics/SKILL.md and host-map.md
// in the existing authenticated targets. It preserves directory inodes and
// restores the prior files if either output cannot be written.
func RefreshMechanicsFiles(existingTargetDirs []string, facts FactSheet) error {
	files, err := RenderMechanicsIndexAndMap(facts)
	if err != nil {
		return err
	}
	var snapshots []mechanicsFileSnapshot
	for _, dir := range existingTargetDirs {
		info, statErr := os.Stat(dir)
		if statErr != nil || !info.IsDir() {
			if statErr == nil {
				statErr = fmt.Errorf("not a directory")
			}
			return fmt.Errorf("mechanics target %q: %w", dir, statErr)
		}
		for _, rel := range []string{"jail-mechanics/SKILL.md", "jail-mechanics/host-map.md"} {
			path := filepath.Join(dir, filepath.FromSlash(rel))
			s := mechanicsFileSnapshot{path: path}
			info, statErr := os.Lstat(path)
			if statErr == nil {
				if !info.Mode().IsRegular() {
					return fmt.Errorf("mechanics target %q is not a regular file", path)
				}
				s.data, statErr = os.ReadFile(path)
				if statErr != nil {
					return statErr
				}
				s.mode, s.exists = info.Mode().Perm(), true
			} else if !os.IsNotExist(statErr) {
				return statErr
			}
			snapshots = append(snapshots, s)
		}
	}
	written := 0
	rollback := func() error {
		var first error
		for i := written - 1; i >= 0; i-- {
			s := snapshots[i]
			if s.exists {
				if err := replaceMechanicsFile(s.path, s.data, s.mode); err != nil && first == nil {
					first = err
				}
			} else if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) && first == nil {
				first = err
			}
		}
		return first
	}
	for _, dir := range existingTargetDirs {
		for _, rel := range []string{"jail-mechanics/SKILL.md", "jail-mechanics/host-map.md"} {
			path := filepath.Join(dir, filepath.FromSlash(rel))
			if err := replaceMechanicsFile(path, files[filepath.ToSlash(filepath.Join("jail-mechanics", filepath.Base(rel)))], 0o644); err != nil {
				rbErr := rollback()
				if rbErr != nil {
					return fmt.Errorf("write mechanics file: %w (rollback failed: %v)", err, rbErr)
				}
				return fmt.Errorf("write mechanics file: %w", err)
			}
			written++
		}
	}
	return nil
}

func replaceMechanicsFile(path string, content []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".mechanics-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(content); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
