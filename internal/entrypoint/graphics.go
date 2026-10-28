package entrypoint

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	GraphicsGPUEnabledEnv      = "YOLO_GPU_ENABLED"
	GraphicsManifestPath       = "/etc/yolo-jail/graphics-drivers.json"
	graphicsProfileManifestRel = "share/yolo-jail/graphics-drivers.json"
	graphicsRuntimeManifest    = "/run/yolo/graphics-drivers.json"
	graphicsDriver64Root       = "/run/opengl-driver"
	graphicsDriver32Root       = "/run/opengl-driver-32"
)

type graphicsManifest struct {
	Drivers []graphicsDriver `json:"drivers"`
}

type graphicsDriver struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Arch      string `json:"arch"`
	Lib       string `json:"lib"`
	Share     string `json:"share"`
	Preserved *bool  `json:"preserved,omitempty"`
}

type graphicsPaths struct {
	Root64          string
	Root32          string
	RuntimeManifest string
}

// GenerateGraphicsDrivers exposes the selected package's NixOS driver tree in
// /run. It is a container boot generator; no host or macos-user render calls it.
func GenerateGraphicsDrivers(e *Env) error {
	return generateGraphicsDriversIn(e, graphicsPaths{
		Root64:          graphicsDriver64Root,
		Root32:          graphicsDriver32Root,
		RuntimeManifest: graphicsRuntimeManifest,
	}, GraphicsManifestPath)
}

func generateGraphicsDriversIn(e *Env, paths graphicsPaths, bakedManifest string) error {
	if e.Getenv(GraphicsGPUEnabledEnv) != "1" {
		return nil
	}
	drivers, err := readSelectedGraphicsDrivers(e, bakedManifest)
	if err != nil {
		return err
	}
	drivers = firstGraphicsDriverPerArch(drivers)
	if len(drivers) == 0 {
		return nil
	}

	provenance := graphicsManifest{Drivers: make([]graphicsDriver, 0, len(drivers))}
	targets := make([]graphicsTreeTarget, 0, len(drivers))
	for _, driver := range drivers {
		root := paths.Root64
		if driver.Arch == "32" {
			root = paths.Root32
		}
		targets = append(targets, graphicsTreeTarget{root: root, driver: driver})
	}
	created, preserved, err := ensureGraphicsTrees(targets)
	if err != nil {
		return err
	}
	for _, target := range targets {
		driver := target.driver
		wasPreserved := preserved[target.root]
		driver.Preserved = &wasPreserved
		provenance.Drivers = append(provenance.Drivers, driver)
	}
	if err := writeGraphicsManifest(paths.RuntimeManifest, provenance); err != nil {
		rollbackGraphicsTrees(created)
		return fmt.Errorf("write graphics driver boot provenance %s: %w; make /run/yolo writable and retry", paths.RuntimeManifest, err)
	}
	return nil
}

func readSelectedGraphicsDrivers(e *Env, bakedManifest string) ([]graphicsDriver, error) {
	userProfiles := parseProfileList(e.Getenv(StoreProfilesEnv))
	fhsProfiles := parseProfileList(e.Getenv(StoreFHSProfilesEnv))
	if len(userProfiles) > 0 {
		var drivers []graphicsDriver
		found := false
		for _, profile := range userProfiles {
			path := filepath.Join(profile, filepath.FromSlash(graphicsProfileManifestRel))
			manifest, exists, err := readGraphicsManifest(path)
			if err != nil {
				return nil, err
			}
			if exists {
				found = true
				drivers = append(drivers, manifest.Drivers...)
			}
		}
		if !found {
			return nil, fmt.Errorf("store-delivered packages have no %s metadata in their declared profiles; rebuild the package profile with this yolo version and retry", graphicsProfileManifestRel)
		}
		return drivers, nil
	}
	if len(fhsProfiles) > 0 {
		// FHS profiles carry image extras such as Chromium, not the user's declared
		// graphics package selection. Their contents must not create driver intent.
		return nil, nil
	}
	manifest, exists, err := readGraphicsManifest(bakedManifest)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("image is missing %s; rebuild the jail image with this yolo version and retry", bakedManifest)
	}
	return manifest.Drivers, nil
}

func readGraphicsManifest(path string) (graphicsManifest, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return graphicsManifest{}, false, nil
	}
	if err != nil {
		return graphicsManifest{}, false, fmt.Errorf("read graphics driver metadata %s: %w; repair the package profile or image and retry", path, err)
	}
	var manifest graphicsManifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return graphicsManifest{}, false, fmt.Errorf("decode graphics driver metadata %s: %w; rebuild the package profile or image and retry", path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return graphicsManifest{}, false, fmt.Errorf("decode graphics driver metadata %s: trailing JSON data; rebuild the package profile or image and retry", path)
	}
	if manifest.Drivers == nil {
		return graphicsManifest{}, false, fmt.Errorf("decode graphics driver metadata %s: drivers must be an array; rebuild the package profile or image and retry", path)
	}
	for i, driver := range manifest.Drivers {
		if err := validateGraphicsDriver(driver); err != nil {
			return graphicsManifest{}, false, fmt.Errorf("invalid graphics driver metadata %s at drivers[%d]: %w; rebuild the package profile or image and retry", path, i, err)
		}
	}
	return manifest, true, nil
}

func validateGraphicsDriver(driver graphicsDriver) error {
	if strings.TrimSpace(driver.Name) == "" || strings.TrimSpace(driver.Version) == "" {
		return errors.New("name and resolved version must be non-empty")
	}
	if driver.Arch != "64" && driver.Arch != "32" {
		return fmt.Errorf("arch %q is not 64 or 32", driver.Arch)
	}
	if driver.Preserved != nil {
		return errors.New("build metadata must not contain runtime preserved state")
	}
	for label, path := range map[string]string{"lib": driver.Lib, "share": driver.Share} {
		if !filepath.IsAbs(path) {
			return fmt.Errorf("%s path %q is not absolute", label, path)
		}
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("declared %s output %s is not materialized: %w", label, path, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("declared %s output %s is not a directory", label, path)
		}
	}
	return nil
}

func firstGraphicsDriverPerArch(drivers []graphicsDriver) []graphicsDriver {
	seen := map[string]bool{}
	selected := make([]graphicsDriver, 0, len(drivers))
	for _, driver := range drivers {
		if seen[driver.Arch] {
			continue
		}
		seen[driver.Arch] = true
		selected = append(selected, driver)
	}
	return selected
}

type graphicsTreeTarget struct {
	root   string
	driver graphicsDriver
}

type graphicsStagedTree struct {
	root    string
	staging string
	created bool
	driver  graphicsDriver
}

type graphicsRollbackTree struct {
	root  string
	lib   string
	share string
}

// ensureGraphicsTrees validates and stages all absent architecture roots before
// claiming any destination. A later failure rolls back only roots this attempt
// created; pre-existing user paths are never repaired or removed.
func ensureGraphicsTrees(targets []graphicsTreeTarget) (created []graphicsRollbackTree, preserved map[string]bool, err error) {
	return ensureGraphicsTreesWithRename(targets, os.Rename)
}

func ensureGraphicsTreesWithRename(targets []graphicsTreeTarget, rename func(string, string) error) (created []graphicsRollbackTree, preserved map[string]bool, err error) {
	preserved = make(map[string]bool, len(targets))
	staged := make([]*graphicsStagedTree, 0, len(targets))
	defer func() {
		for _, tree := range staged {
			_ = os.RemoveAll(tree.staging)
		}
		if err != nil {
			rollbackGraphicsTrees(created)
		}
	}()

	for _, target := range targets {
		if _, statErr := os.Lstat(target.root); statErr == nil {
			preserved[target.root] = true
			continue
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return created, nil, fmt.Errorf("inspect %s for driver %s: %w", target.root, target.driver.Name, statErr)
		}
		parent := filepath.Dir(target.root)
		if mkdirErr := os.MkdirAll(parent, 0o755); mkdirErr != nil {
			return created, nil, fmt.Errorf("prepare parent of %s: %w", target.root, mkdirErr)
		}
		tmp, tempErr := os.MkdirTemp(parent, ".opengl-driver-*")
		if tempErr != nil {
			return created, nil, fmt.Errorf("stage driver %s at %s: %w", target.driver.Name, target.root, tempErr)
		}
		tree := &graphicsStagedTree{root: target.root, staging: tmp, driver: target.driver}
		staged = append(staged, tree)
		for _, child := range []struct{ name, value string }{{"lib", target.driver.Lib}, {"share", target.driver.Share}} {
			if linkErr := os.Symlink(child.value, filepath.Join(tmp, child.name)); linkErr != nil {
				return created, nil, fmt.Errorf("stage %s link for %s: %w", child.name, target.root, linkErr)
			}
		}
	}

	for _, tree := range staged {
		if mkdirErr := os.Mkdir(tree.root, 0o755); mkdirErr != nil {
			if errors.Is(mkdirErr, os.ErrExist) {
				preserved[tree.root] = true
				continue
			}
			return created, nil, fmt.Errorf("create graphics root %s: %w; remove or repair that path and retry", tree.root, mkdirErr)
		}
		tree.created = true
		created = append(created, graphicsRollbackTree{root: tree.root, lib: tree.driver.Lib, share: tree.driver.Share})
	}
	for _, tree := range staged {
		if !tree.created {
			continue
		}
		for _, child := range []string{"lib", "share"} {
			if moveErr := rename(filepath.Join(tree.staging, child), filepath.Join(tree.root, child)); moveErr != nil {
				return created, nil, fmt.Errorf("publish graphics %s link at %s: %w", child, tree.root, moveErr)
			}
		}
	}
	return created, preserved, nil
}

func rollbackGraphicsTrees(trees []graphicsRollbackTree) {
	for i := len(trees) - 1; i >= 0; i-- {
		tree := trees[i]
		for _, child := range []struct{ name, target string }{{"lib", tree.lib}, {"share", tree.share}} {
			path := filepath.Join(tree.root, child.name)
			if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
				if target, readErr := os.Readlink(path); readErr == nil && target == child.target {
					_ = os.Remove(path)
				}
			}
		}
		// Remove only an empty directory that was exclusively created by this attempt.
		_ = os.Remove(tree.root)
	}
}

func writeGraphicsManifest(path string, manifest graphicsManifest) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".graphics-drivers-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
