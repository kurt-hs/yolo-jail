package packload

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
)

type OverlayFile struct {
	// Name is the basename under <pack>/overlays; discovery intentionally does not
	// resolve its surface or parse it before the selected owner set is known.
	Name string
}

func discoverHomeFiles(root string, followLocalLinks bool) ([]packdecl.Contribution, []string) {
	if followLocalLinks {
		return discoverLocalHomeFiles(root)
	}
	homeRoot := filepath.Join(root, "home")
	info, err := os.Lstat(homeRoot)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, []string{fmt.Sprintf("cannot inspect home/: %v", err)}
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, []string{"home/ is a symlink; place a real directory in the pack so local/fetched link policy remains controlled by staging"}
	}
	if !info.IsDir() {
		return nil, []string{"home/ is not a directory"}
	}
	rootResolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, []string{fmt.Sprintf("cannot resolve pack root: %v", err)}
	}
	var files []packdecl.Contribution
	var problems []string
	var walk func(string, string)
	walk = func(dir, rel string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			problems = append(problems, fmt.Sprintf("cannot read home/%s: %v", rel, err))
			return
		}
		for _, entry := range entries {
			childRel := filepath.ToSlash(filepath.Join(rel, entry.Name()))
			child := filepath.Join(dir, entry.Name())
			if entry.Name() == ".git" {
				info, infoErr := entry.Info()
				if infoErr == nil && info.IsDir() {
					continue
				}
			}
			if entry.Type()&os.ModeSymlink != 0 {
				target, statErr := os.Stat(child)
				if statErr != nil {
					problems = append(problems, fmt.Sprintf("home/%s is a symlink that resolves to nothing: %v", childRel, statErr))
					continue
				}
				if target.IsDir() {
					problems = append(problems, fmt.Sprintf("home/%s is a directory symlink; file-by-file home delivery does not follow directory links", childRel))
					continue
				}
				if !target.Mode().IsRegular() {
					problems = append(problems, fmt.Sprintf("home/%s is not a regular file", childRel))
					continue
				}
				resolved, resolveErr := filepath.EvalSymlinks(child)
				if resolveErr != nil {
					problems = append(problems, fmt.Sprintf("home/%s cannot be resolved: %v", childRel, resolveErr))
					continue
				}
				if !pathWithin(rootResolved, resolved) {
					problems = append(problems, fmt.Sprintf("home/%s points outside the pack; ordinary readers do not follow escaping file links", childRel))
					continue
				}
				files = append(files, packdecl.Contribution{Kind: packdecl.KindFiles,
					From: "home/" + childRel, Into: childRel})
				continue
			}
			if entry.IsDir() {
				walk(child, childRel)
				continue
			}
			info, err := entry.Info()
			if err != nil {
				problems = append(problems, fmt.Sprintf("cannot inspect home/%s: %v", childRel, err))
				continue
			}
			if !info.Mode().IsRegular() {
				problems = append(problems, fmt.Sprintf("home/%s is not a regular file", childRel))
				continue
			}
			files = append(files, packdecl.Contribution{Kind: packdecl.KindFiles,
				From: "home/" + childRel, Into: childRel})
		}
	}
	walk(homeRoot, "")
	return files, problems
}

// discoverLocalHomeFiles follows the same user-owned content links as packstage's local
// FollowSymlinks policy. Contribution paths retain the names inside home/, not the targets'
// physical paths, so aliases stage and deliver identically.
func discoverLocalHomeFiles(root string) ([]packdecl.Contribution, []string) {
	homeRoot := filepath.Join(root, "home")
	info, err := os.Lstat(homeRoot)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, []string{fmt.Sprintf("cannot inspect home/: %v", err)}
	}
	linkInfo := info
	info, err = os.Stat(homeRoot)
	if err != nil {
		if linkInfo.Mode()&os.ModeSymlink != 0 {
			return nil, []string{fmt.Sprintf("home/ is a symlink that resolves to nothing; repair or remove the link: %v", err)}
		}
		return nil, []string{fmt.Sprintf("cannot inspect home/: %v", err)}
	}
	if !info.IsDir() {
		return nil, []string{"home/ is not a directory"}
	}
	var files []packdecl.Contribution
	var problems []string
	active := map[string]bool{}
	var walk func(string, string)
	walk = func(dir, rel string) {
		real, err := filepath.EvalSymlinks(dir)
		if err != nil {
			problems = append(problems, fmt.Sprintf("cannot resolve home/%s: %v", rel, err))
			return
		}
		real, err = filepath.Abs(real)
		if err != nil {
			problems = append(problems, fmt.Sprintf("cannot resolve home/%s: %v", rel, err))
			return
		}
		if active[real] {
			problems = append(problems, fmt.Sprintf("home/%s is a directory symlink loop (it leads back into %s)", rel, real))
			return
		}
		active[real] = true
		defer delete(active, real)
		entries, err := os.ReadDir(dir)
		if err != nil {
			problems = append(problems, fmt.Sprintf("cannot read home/%s: %v", rel, err))
			return
		}
		for _, entry := range entries {
			childRel := filepath.ToSlash(filepath.Join(rel, entry.Name()))
			child := filepath.Join(dir, entry.Name())
			mode := entry.Type()
			if entry.Name() == ".git" {
				if entry.IsDir() || mode&os.ModeSymlink != 0 && dirTargetIsDirectory(child) {
					continue
				}
			}
			if mode&os.ModeSymlink != 0 {
				target, statErr := os.Stat(child)
				if statErr != nil {
					problems = append(problems, fmt.Sprintf("home/%s is a symlink that resolves to nothing: %v", childRel, statErr))
					continue
				}
				if target.IsDir() {
					walk(child, childRel)
					continue
				}
				if !target.Mode().IsRegular() {
					problems = append(problems, fmt.Sprintf("home/%s is not a regular file", childRel))
					continue
				}
				files = append(files, packdecl.Contribution{Kind: packdecl.KindFiles, From: "home/" + childRel, Into: childRel})
				continue
			}
			if entry.IsDir() {
				walk(child, childRel)
				continue
			}
			fileInfo, err := entry.Info()
			if err != nil {
				problems = append(problems, fmt.Sprintf("cannot inspect home/%s: %v", childRel, err))
				continue
			}
			if !fileInfo.Mode().IsRegular() {
				problems = append(problems, fmt.Sprintf("home/%s is not a regular file", childRel))
				continue
			}
			files = append(files, packdecl.Contribution{Kind: packdecl.KindFiles, From: "home/" + childRel, Into: childRel})
		}
	}
	walk(homeRoot, "")
	return files, problems
}

func pathWithin(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func dirTargetIsDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// discoverOverlayFiles records direct files in the conventional overlays directory. Ordinary
// readers refuse links because they lack origin context; only the explicit local-use reader may
// follow local links. Fetched sources retain staged-tree containment enforcement.
func discoverOverlayFiles(root string, followLocalLinks bool) ([]OverlayFile, []string) {
	dir := filepath.Join(root, "overlays")
	info, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, []string{fmt.Sprintf("cannot inspect overlays/: %v", err)}
	}
	if info.Mode()&os.ModeSymlink != 0 {
		if !followLocalLinks {
			return nil, []string{"overlays/ is a symlink; use a directory within the pack so staged-tree containment remains enforceable"}
		}
		info, err = os.Stat(dir)
		if err != nil {
			return nil, []string{fmt.Sprintf("overlays/ is a symlink that resolves to nothing: %v", err)}
		}
	}
	if !info.IsDir() {
		return nil, []string{"overlays/ is not a directory"}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, []string{fmt.Sprintf("cannot read overlays/: %v", err)}
	}
	var files []OverlayFile
	var problems []string
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			if !followLocalLinks {
				problems = append(problems, fmt.Sprintf("overlays/%s is a symlink; symlinked overlay sources are not followed during discovery", entry.Name()))
				continue
			}
			fileInfo, err := os.Stat(filepath.Join(dir, entry.Name()))
			if err != nil {
				problems = append(problems, fmt.Sprintf("overlays/%s is a symlink that resolves to nothing: %v", entry.Name(), err))
				continue
			}
			if !fileInfo.Mode().IsRegular() {
				problems = append(problems, fmt.Sprintf("overlays/%s is not a regular file", entry.Name()))
				continue
			}
			files = append(files, OverlayFile{Name: entry.Name()})
			continue
		}
		if entry.IsDir() {
			problems = append(problems, fmt.Sprintf("overlays/%s is a directory; overlay files must be direct children", entry.Name()))
			continue
		}
		fileInfo, err := entry.Info()
		if err != nil {
			problems = append(problems, fmt.Sprintf("cannot inspect overlays/%s: %v", entry.Name(), err))
			continue
		}
		if !fileInfo.Mode().IsRegular() {
			problems = append(problems, fmt.Sprintf("overlays/%s is not a regular file; symlinks and special files are not followed", entry.Name()))
			continue
		}
		files = append(files, OverlayFile{Name: entry.Name()})
	}
	return files, problems
}

// discoverManifest selects one declaration file and refuses the transitional ambiguity
// between the new format and either legacy spelling. Legacy-only selection order remains
// pack.jsonc before pack.json.
func discoverManifest(root string) (data []byte, name, problem string) {
	newPath := filepath.Join(root, packdecl.ManifestTOMLName)
	newExists, err := pathExists(newPath)
	if err != nil {
		return nil, "", fmt.Sprintf("cannot inspect %s: %v", packdecl.ManifestTOMLName, err)
	}
	legacy := []string{packdecl.ManifestLegacyJSONCName, packdecl.ManifestLegacyJSONName}
	var legacyFound []string
	for _, candidate := range legacy {
		exists, err := pathExists(filepath.Join(root, candidate))
		if err != nil {
			return nil, "", fmt.Sprintf("cannot inspect %s: %v", candidate, err)
		}
		if exists {
			legacyFound = append(legacyFound, candidate)
		}
	}
	if newExists && len(legacyFound) > 0 {
		return nil, "", fmt.Sprintf("both %s and %s are present; keep one manifest and remove the other", packdecl.ManifestTOMLName, strings.Join(legacyFound, " and "))
	}
	if newExists {
		name = packdecl.ManifestTOMLName
	} else {
		for _, candidate := range legacyFound {
			data, err = os.ReadFile(filepath.Join(root, candidate))
			if os.IsNotExist(err) {
				continue // retain the legacy reader's dangling/vanished sibling fallback
			}
			if err != nil {
				return nil, "", err.Error()
			}
			return data, candidate, ""
		}
		return nil, "", ""
	}
	data, err = os.ReadFile(filepath.Join(root, name))
	if err != nil {
		return nil, "", err.Error()
	}
	return data, name, ""
}

func pathExists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func packDescription(root string, decl *packdecl.Manifest, manifestName string) string {
	if manifestName != "" {
		return decl.Description
	}
	data, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		return ""
	}
	first, _, _ := strings.Cut(string(data), "\n")
	return strings.TrimSpace(strings.TrimSuffix(first, "\r"))
}
