package capture

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// artifactSurfaces narrows the existing capture doors to explicit program outputs.
// Selection owns this whitelist; a whole install prefix is never an artifact.
func artifactSurfaces(roots []string) ([]paths.HomeSurface, error) {
	if len(roots) == 0 {
		return nil, fmt.Errorf("program capture: no artifact paths — name the selected install's program outputs")
	}
	var out []paths.HomeSurface
	for _, root := range roots {
		if err := checkEntryPath(root); err != nil {
			return nil, fmt.Errorf("program capture: %w", err)
		}
		for _, excluded := range DefaultExcludes() {
			if within(excluded, root) || within(root, excluded) {
				return nil, fmt.Errorf("program capture: artifact %q overlaps yolo state — select only program output paths", root)
			}
		}
		matched := false
		for _, s := range paths.InstalledProgramSurfaces() {
			rel := filepath.ToSlash(s.HomeRel)
			if root == rel {
				return nil, fmt.Errorf("program capture: %q is a whole install surface — name program output paths below it", root)
			}
			if strings.HasPrefix(root, rel+"/") {
				// These are common install directories, not a single program's output.
				switch root {
				case ".local/bin", ".local/lib", ".local/share", ".npm-global/bin", ".npm-global/lib", ".npm-global/lib/node_modules", "go/bin", "go/pkg":
					return nil, fmt.Errorf("program capture: %q is a shared install directory — name the selected program's output", root)
				}
				out = append(out, paths.HomeSurface{HomeRel: root, Subtree: filepath.Join(s.Subtree, filepath.FromSlash(strings.TrimPrefix(root, rel+"/")))})
				matched = true
				break
			}
		}
		if !matched {
			return nil, fmt.Errorf("program capture: artifact %q is outside installed program surfaces — keep config and login state private", root)
		}
	}
	slices.SortFunc(out, func(a, b paths.HomeSurface) int { return strings.Compare(a.HomeRel, b.HomeRel) })
	for i := 1; i < len(out); i++ {
		if within(out[i-1].HomeRel, out[i].HomeRel) {
			return nil, fmt.Errorf("program capture: overlapping artifact paths %q and %q — name each output once", out[i-1].HomeRel, out[i].HomeRel)
		}
	}
	return out, nil
}

// checkArtifacts runs after the installer has exited, before moving any output. No
// parent may be a link, and output links may refer only to another whitelisted output.
func (d *driver) checkArtifacts() error {
	roots := d.surfaceRels()
	for _, root := range roots {
		if err := confinedParents(d.home, root, false); err != nil {
			return fmt.Errorf("program capture: %w", err)
		}
		start := d.surfacePath(paths.HomeSurface{HomeRel: root})
		err := filepath.WalkDir(start, func(p string, e fs.DirEntry, err error) error {
			if os.IsNotExist(err) && p == start {
				return nil
			}
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(start, p)
			if err != nil {
				return err
			}
			named := root
			if rel != "." {
				named = path.Join(root, filepath.ToSlash(rel))
			}
			info, err := e.Info()
			if err != nil {
				return err
			}
			if info.Mode()&(os.ModeSetuid|os.ModeSetgid) != 0 || (info.Mode()&os.ModeSymlink == 0 && info.Mode().Perm()&0o002 != 0) {
				return fmt.Errorf("program capture: output %q has unsafe permissions %s — fix the selected install recipe", named, info.Mode())
			}
			switch {
			case info.Mode().IsRegular(), info.IsDir():
				return nil
			case info.Mode()&os.ModeSymlink != 0:
				target, err := os.Readlink(p)
				if err != nil {
					return err
				}
				if err := checkArtifactTarget(d.home, named, target, roots); err != nil {
					return err
				}
				resolved, err := filepath.EvalSymlinks(p)
				if err != nil {
					return fmt.Errorf("program capture: link %q is dangling or cyclic — fix the selected recipe: %w", named, err)
				}
				realHome, err := filepath.EvalSymlinks(d.home)
				if err != nil {
					return err
				}
				return checkArtifactTarget(realHome, named, resolved, roots)
			default:
				return fmt.Errorf("program capture: output %q is not a file, directory or symlink — fix the selected recipe", named)
			}
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// programEnvironment is an allowlist, not a credential-name blacklist. The outer
// producer must independently use the sealed jail: this filters the installer's env,
// not its filesystem or network authority. Prefixes are private to that capture home.
func programEnvironment(env []string, home string) []string {
	executablePath := "PATH=/bin:/usr/bin"
	for _, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			executablePath = kv
		}
	}
	return []string{executablePath, "HOME=" + home, "NPM_CONFIG_PREFIX=" + filepath.Join(home, ".npm-global"), "NPM_CONFIG_CACHE=" + filepath.Join(home, ".cache/npm"), "NPM_CONFIG_UPDATE_NOTIFIER=false", "NPM_CONFIG_FUND=false", "GOPATH=" + filepath.Join(home, "go"), "GOCACHE=" + filepath.Join(home, ".cache/go-build"), "LANG=C.UTF-8", "PAGER=cat", "GIT_TERMINAL_PROMPT=0"}
}

// ValidateArtifacts is admission's independent check of the driver's output whitelist.
// It applies only to resolved ordinary captures, not historical installer/fork receipts.
func ValidateArtifacts(m *Manifest, roots []string) error {
	surfaces, err := artifactSurfaces(roots)
	if err != nil {
		return err
	}
	if err := checkEntries(m.Entries); err != nil {
		return err
	}
	expected := make([]string, len(surfaces))
	for i, s := range surfaces {
		expected[i] = s.HomeRel
	}
	observed := slices.Clone(m.Surfaces)
	slices.Sort(observed)
	if !slices.Equal(expected, observed) {
		return fmt.Errorf("program capture: producer captured different artifact roots — retry with the selected outputs")
	}
	have := make(map[string]ManifestEntry, len(m.Entries))
	for _, e := range m.Entries {
		have[e.Path] = e
	}
	for _, root := range expected {
		if _, ok := have[root]; !ok {
			return fmt.Errorf("program capture: selected output %q is missing — fix the install recipe", root)
		}
	}
	for _, e := range m.Entries {
		if e.Kind == KindSymlink {
			if err := checkArtifactTarget(m.Home, e.Path, e.Target, expected); err != nil {
				return err
			}
		} else {
			mode, err := strconv.ParseUint(e.Mode, 8, 32)
			if err != nil || mode&0o6002 != 0 {
				return fmt.Errorf("program capture: output %q has unsafe permissions — fix the selected producer", e.Path)
			}
		}
		if !inSurfaces(e, expected) {
			return fmt.Errorf("program capture: unselected output %q — fix the selected producer", e.Path)
		}
	}
	return nil
}

func checkArtifactTarget(home, named, target string, roots []string) error {
	if target == "" {
		return fmt.Errorf("program capture: link %q has no target — fix the selected recipe", named)
	}
	resolved := path.Clean(path.Join(path.Dir(named), target))
	if filepath.IsAbs(target) {
		if home == "" || !within(home, target) {
			return fmt.Errorf("program capture: link %q escapes the capture home — fix the selected install recipe", named)
		}
		r, err := filepath.Rel(home, target)
		if err != nil {
			return err
		}
		resolved = filepath.ToSlash(r)
	}
	for _, allowed := range roots {
		if within(allowed, resolved) {
			return nil
		}
	}
	return fmt.Errorf("program capture: link %q targets unselected output %q — include its program artifact or fix the selected recipe", named, target)
}
