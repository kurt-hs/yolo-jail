package capture

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// VerifyMaterialized checks that the declared outputs in home still match the admitted entry.
// The content-addressed key authenticates the immutable source tree; the capture manifest names
// the installed paths, and the file comparisons ensure a home's key stamp cannot vouch for stale
// or missing bytes.
func VerifyMaterialized(entry *Entry, home, programPath string, produces []string) error {
	if entry == nil || home == "" || !filepath.IsAbs(home) {
		return fmt.Errorf("capture verify: entry and absolute home are required")
	}
	if programPath == "" || len(produces) == 0 {
		return fmt.Errorf("capture verify: program path and declared outputs are required")
	}
	if err := checkEntryPath(programPath); err != nil {
		return fmt.Errorf("capture verify: invalid program path %q: %w", programPath, err)
	}
	key, err := KeyForTree(entry.Tree)
	if err != nil {
		return fmt.Errorf("capture verify: digesting admitted tree: %w", err)
	}
	if key != entry.Key {
		return fmt.Errorf("capture verify: admitted tree digest does not match key %s", entry.Key)
	}
	m, err := ReadManifest(entry.Root)
	if err != nil {
		return fmt.Errorf("capture verify: reading admitted manifest: %w", err)
	}
	if got, _, err := describeTree(entry.Tree, m.Home); err != nil {
		return fmt.Errorf("capture verify: describing admitted tree: %w", err)
	} else if !sameManifestEntries(got, m.Entries) {
		return fmt.Errorf("capture verify: admitted manifest does not describe key %s", entry.Key)
	}
	var relocation *relocation
	if m.Home != "" && filepath.Clean(m.Home) != filepath.Clean(home) {
		var reasons []string
		relocation, reasons, err = planRelocation(m, entry.Tree, home)
		if err != nil {
			return fmt.Errorf("capture verify: planning relocation: %w", err)
		}
		if len(reasons) != 0 {
			return fmt.Errorf("capture verify: entry cannot be relocated: %s", strings.Join(reasons, "; "))
		}
	}

	entries := make(map[string]ManifestEntry, len(m.Entries))
	for _, e := range m.Entries {
		entries[e.Path] = e
	}
	checked := map[string]bool{}
	selected := map[string]bool{}
	for _, output := range produces {
		if err := checkEntryPath(output); err != nil {
			return fmt.Errorf("capture verify: invalid declared output %q: %w", output, err)
		}
		if checked[output] {
			return fmt.Errorf("capture verify: duplicate declared output %q", output)
		}
		checked[output] = true
		if _, ok := entries[output]; !ok {
			if programPath == output || strings.HasPrefix(programPath, output+"/") {
				return fmt.Errorf("capture verify: program output %q is absent from the admitted manifest", output)
			}
			// The host can deliberately select a prior admitted Good after a recipe edit.
			// Its manifest, not the failed current recipe, is the inventory for this key.
			continue
		}
		expected := map[string]bool{}
		for path := range entries {
			if path == output || strings.HasPrefix(path, output+"/") {
				expected[path] = true
			}
		}
		observed, err := materializedPaths(home, filepath.Join(home, filepath.FromSlash(output)), entries)
		if err != nil {
			return fmt.Errorf("capture verify: walking declared output %q: %w", output, err)
		}
		if !samePaths(expected, observed) {
			return fmt.Errorf("capture verify: installed output %q has paths different from the admitted manifest", output)
		}
		for path := range expected {
			selected[path] = true
		}
	}
	programDeclared := false
	for _, output := range produces {
		if programPath == output || strings.HasPrefix(programPath, output+"/") {
			programDeclared = true
			break
		}
	}
	if !programDeclared {
		return fmt.Errorf("capture verify: program path %q is outside the admitted output inventory", programPath)
	}
	if direct, exists := entries[programPath]; exists {
		if direct.Kind == KindDir {
			return fmt.Errorf("capture verify: program path %q is a directory", programPath)
		}
		selected[programPath] = true
	} else {
		resolvedProgram, programChain, ok := resolveManifestPath(programPath, entries, m.Home, map[string]bool{})
		if !ok || entries[resolvedProgram].Kind == KindDir {
			return fmt.Errorf("capture verify: program path %q is absent from the admitted output inventory", programPath)
		}
		for _, path := range programChain {
			selected[path] = true
		}
	}
	closure, dirTargets := admittedTargetClosure(entries, selected, m.Home)
	for path := range closure {
		if err := verifyMaterializedEntry(entry, m, entries[path], filepath.Join(home, filepath.FromSlash(path)), relocation); err != nil {
			return fmt.Errorf("capture verify: %s: %w", path, err)
		}
	}
	for target := range dirTargets {
		expected := map[string]bool{}
		for path := range entries {
			if path == target || strings.HasPrefix(path, target+"/") {
				expected[path] = true
			}
		}
		observed, err := materializedPaths(home, filepath.Join(home, filepath.FromSlash(target)), entries)
		if err != nil {
			return fmt.Errorf("capture verify: walking admitted symlink target %q: %w", target, err)
		}
		if !samePaths(expected, observed) {
			return fmt.Errorf("capture verify: installed symlink target %q has paths different from the admitted manifest", target)
		}
	}
	return nil
}

// admittedTargetClosure follows only symlink targets that resolve through entries in the admitted
// manifest. External and escaping targets are checked as link text but never opened or traversed.
func admittedTargetClosure(entries map[string]ManifestEntry, selected map[string]bool, home string) (map[string]bool, map[string]bool) {
	closure := make(map[string]bool, len(selected))
	queue := make([]string, 0, len(selected))
	for path := range selected {
		closure[path] = true
		queue = append(queue, path)
	}
	dirTargets := map[string]bool{}
	for len(queue) > 0 {
		current := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		entry := entries[current]
		if entry.Kind == KindSymlink {
			target, ok := manifestLinkPath(current, entry.Target, home)
			if !ok {
				continue
			}
			target, chain, ok := resolveManifestPath(target, entries, home, map[string]bool{})
			if !ok {
				continue
			}
			for _, path := range chain {
				if !closure[path] {
					closure[path] = true
					queue = append(queue, path)
				}
			}
			if entries[target].Kind == KindDir {
				dirTargets[target] = true
				for path := range entries {
					if (path == target || strings.HasPrefix(path, target+"/")) && !closure[path] {
						closure[path] = true
						queue = append(queue, path)
					}
				}
			}
		}
	}
	return closure, dirTargets
}

func manifestLinkPath(link, target, home string) (string, bool) {
	var rel string
	if filepath.IsAbs(target) {
		if home == "" || !filepath.IsAbs(home) {
			return "", false
		}
		var err error
		rel, err = filepath.Rel(filepath.Clean(home), filepath.Clean(target))
		if err != nil {
			return "", false
		}
	} else {
		rel = filepath.Clean(filepath.Join(filepath.Dir(filepath.FromSlash(link)), filepath.FromSlash(target)))
	}
	rel = filepath.ToSlash(rel)
	if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") || filepath.IsAbs(rel) {
		return "", false
	}
	return rel, true
}

// resolveManifestPath resolves symlinks in path components without touching the filesystem.
// It returns every admitted component it traversed; a missing target, escape, or cycle is not
// followed, so verification never reads a path outside the captured entry.
func resolveManifestPath(path string, entries map[string]ManifestEntry, home string, seen map[string]bool) (string, []string, bool) {
	path = filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if path == "." || path == ".." || strings.HasPrefix(path, "../") || filepath.IsAbs(path) {
		return "", nil, false
	}
	parts := strings.Split(path, "/")
	var chain []string
	for i := range parts {
		component := strings.Join(parts[:i+1], "/")
		entry, ok := entries[component]
		if !ok {
			return "", nil, false
		}
		chain = append(chain, component)
		if entry.Kind == KindSymlink {
			if seen[component] {
				return "", nil, false
			}
			seen[component] = true
			target, ok := manifestLinkPath(component, entry.Target, home)
			if !ok {
				return "", nil, false
			}
			rest := strings.Join(parts[i+1:], "/")
			if rest != "" {
				target = filepath.ToSlash(filepath.Clean(filepath.Join(filepath.FromSlash(target), filepath.FromSlash(rest))))
			}
			resolved, suffix, ok := resolveManifestPath(target, entries, home, seen)
			if !ok {
				return "", nil, false
			}
			return resolved, append(chain, suffix...), true
		}
		if i < len(parts)-1 && entry.Kind != KindDir {
			return "", nil, false
		}
	}
	return path, chain, true
}

func verifyMaterializedEntry(entry *Entry, m *Manifest, expected ManifestEntry, installed string, relocation *relocation) error {
	source := filepath.Join(entry.Tree, filepath.FromSlash(expected.Path))
	sourceInfo, err := os.Lstat(source)
	if err != nil {
		return err
	}
	installedInfo, err := os.Lstat(installed)
	if err != nil {
		return err
	}
	switch expected.Kind {
	case KindDir:
		// Materialize leaves existing directory bindings (including macos-user's
		// home-to-sidecar links) intact. Verify the directory they bind, not the
		// binding's inode; files and captured symlinks remain Lstat-strict below.
		if !sourceInfo.IsDir() || !dirExists(installed) {
			return fmt.Errorf("expected a directory")
		}
	case KindSymlink:
		if sourceInfo.Mode()&fs.ModeSymlink == 0 || installedInfo.Mode()&fs.ModeSymlink == 0 {
			return fmt.Errorf("expected a symlink")
		}
		want, err := os.Readlink(source)
		if err != nil {
			return err
		}
		if relocation != nil {
			if rewritten, ok := relocation.links[expected.Path]; ok {
				want = rewritten
			}
		}
		got, err := os.Readlink(installed)
		if err != nil {
			return err
		}
		if got != want {
			return fmt.Errorf("symlink target differs from admitted manifest")
		}
	case KindFile:
		if !sourceInfo.Mode().IsRegular() || !installedInfo.Mode().IsRegular() {
			return fmt.Errorf("expected a regular file")
		}
		mode, err := strconv.ParseUint(expected.Mode, 8, 32)
		if err != nil {
			return fmt.Errorf("invalid manifest mode %q", expected.Mode)
		}
		if installedInfo.Mode().Perm()&0o111 != fs.FileMode(mode)&0o111 {
			return fmt.Errorf("executable mode differs from admitted manifest")
		}
		if err := sameMaterializedFile(source, installed, expected.Path, relocation); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown manifest entry kind %q", expected.Kind)
	}
	return nil
}

func sameMaterializedFile(source, installed, path string, relocation *relocation) error {
	sourceFile, err := os.Open(source)
	if err != nil {
		return err
	}
	defer sourceFile.Close()
	installedFile, err := os.Open(installed)
	if err != nil {
		return err
	}
	defer installedFile.Close()
	actual := sha256.New()
	if relocation != nil && relocation.content[path] {
		if _, err := rewriteStream(actual, sourceFile, []byte(relocation.from), []byte(relocation.to)); err != nil {
			return err
		}
	} else if _, err := io.Copy(actual, sourceFile); err != nil {
		return err
	}
	got := sha256.New()
	if _, err := io.Copy(got, installedFile); err != nil {
		return err
	}
	if !strings.EqualFold(hex.EncodeToString(actual.Sum(nil)), hex.EncodeToString(got.Sum(nil))) {
		return fmt.Errorf("file bytes differ from admitted artifact")
	}
	return nil
}

func materializedPaths(home, root string, entries map[string]ManifestEntry) (map[string]bool, error) {
	paths := map[string]bool{}
	var visit fs.WalkDirFunc
	visit = func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(home, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		paths[rel] = true
		// Materialize permits existing directory bindings, including nested ones.
		// WalkDir does not follow links, so resolve only an admitted KindDir via
		// a trailing dot. Captured symlinks and unadmitted links stay link entries;
		// their targets are never traversed by this inventory walk.
		if d.Type()&fs.ModeSymlink != 0 && entries[rel].Kind == KindDir {
			return filepath.WalkDir(path+string(filepath.Separator)+".", visit)
		}
		return nil
	}
	err := filepath.WalkDir(root, visit)
	return paths, err
}

func samePaths(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for path := range a {
		if !b[path] {
			return false
		}
	}
	return true
}

// sameManifestEntries reports whether manifest describes the tree actual was read from, in the
// terms the entry's content address covers: names, kinds, sizes, link targets and a file's exec
// bits. A directory's mode is not among them (treedigest hashes none), and a macos-user launch
// verifies against a sealed copy whose directories are all a-w,a+rX (macosuser's stage script),
// so comparing it would refuse every staged entry while authenticating nothing.
func sameManifestEntries(actual, manifest []ManifestEntry) bool {
	if len(actual) != len(manifest) {
		return false
	}
	for i := range actual {
		a, m := actual[i], manifest[i]
		if a.Path != m.Path || a.Kind != m.Kind || a.Size != m.Size || a.Target != m.Target {
			return false
		}
		if m.Kind == KindFile {
			actualMode, aerr := strconv.ParseUint(a.Mode, 8, 32)
			manifestMode, merr := strconv.ParseUint(m.Mode, 8, 32)
			if aerr != nil || merr != nil || actualMode&0o111 != manifestMode&0o111 {
				return false
			}
		}
	}
	return true
}
