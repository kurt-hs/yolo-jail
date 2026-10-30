package packmigrate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// DeliveredFile is one file a pack lays into the home: its content and its permission bits, so
// a migration that drops an executable bit is a difference, not a pass.
type DeliveredFile struct {
	SHA256 string
	Mode   fs.FileMode
}

// Delivery is the home-relative landing → file map of every `files` contribution that copies a
// pack tree to a path (declared, or discovered under home/ by packload). Addressed trees, slots
// and built trees are not in it: a migration leaves those declared exactly as they were, so the
// manifest comparison already covers them.
func Delivery(pack *packload.Pack) (map[string]DeliveredFile, error) {
	out := map[string]DeliveredFile{}
	if pack == nil || pack.Decl == nil {
		return out, nil
	}
	for _, c := range pack.Decl.Contributions() {
		if c.Kind != packdecl.KindFiles || c.From == "" || c.Into == "" || c.Agent != "" || len(c.Agents) != 0 || c.IsBuiltTree() {
			continue
		}
		source := filepath.Join(pack.Root, filepath.FromSlash(c.From))
		err := walkFiles(source, func(rel string, info fs.FileInfo, full string) error {
			landing := c.Into
			if rel != "" {
				landing = path.Join(c.Into, rel)
			}
			file, err := deliveredFile(full, info)
			if err != nil {
				return err
			}
			if previous, dup := out[landing]; dup && previous != file {
				return fmt.Errorf("two files contributions land different content at %s", landing)
			}
			out[landing] = file
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// walkFiles calls fn for source itself when it is a file (rel ""), or for every file beneath it,
// skipping a .git directory as packload's home discovery does.
func walkFiles(source string, fn func(rel string, info fs.FileInfo, full string) error) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fn("", info, source)
	}
	return filepath.WalkDir(source, func(full string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" && full != source {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(source, full)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return fn(filepath.ToSlash(rel), info, full)
	})
}

func deliveredFile(full string, info fs.FileInfo) (DeliveredFile, error) {
	if info.Mode()&fs.ModeSymlink != 0 {
		target, err := os.Readlink(full)
		if err != nil {
			return DeliveredFile{}, err
		}
		return DeliveredFile{SHA256: "symlink:" + target, Mode: info.Mode()}, nil
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return DeliveredFile{}, err
	}
	sum := sha256.Sum256(data)
	return DeliveredFile{SHA256: hex.EncodeToString(sum[:]), Mode: info.Mode().Perm()}, nil
}

// DeliveryDifferences lists every landing whose content or mode differs, or that only one side
// delivers.
func DeliveryDifferences(old, got map[string]DeliveredFile) []string {
	var diffs []string
	for _, landing := range sortedKeys(old) {
		after, ok := got[landing]
		switch {
		case !ok:
			diffs = append(diffs, fmt.Sprintf("~/%s is no longer delivered", landing))
		case after.SHA256 != old[landing].SHA256:
			diffs = append(diffs, fmt.Sprintf("~/%s changes content", landing))
		case after.Mode != old[landing].Mode:
			diffs = append(diffs, fmt.Sprintf("~/%s changes mode %v → %v", landing, old[landing].Mode, after.Mode))
		}
	}
	for _, landing := range sortedKeys(got) {
		if _, ok := old[landing]; !ok {
			diffs = append(diffs, fmt.Sprintf("~/%s is newly delivered", landing))
		}
	}
	return diffs
}
