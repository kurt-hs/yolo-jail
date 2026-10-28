package check

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

const (
	graphicsRuntimeManifest = "/run/yolo/graphics-drivers.json"
	graphicsBakedManifest   = "/etc/yolo-jail/graphics-drivers.json"
	graphicsStoreManifest   = "share/yolo-jail/graphics-drivers.json"
	graphics64Tree          = "/run/opengl-driver"
	graphics32Tree          = "/run/opengl-driver-32"
)

type graphicsDriver struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Arch    string `json:"arch"`
	Lib     string `json:"lib"`
	Share   string `json:"share"`
}

type graphicsRuntimeDriver struct {
	graphicsDriver
	Preserved *bool `json:"preserved"`
}

type graphicsManifest struct {
	Drivers []graphicsDriver `json:"drivers"`
}

type graphicsRuntimeManifestData struct {
	Drivers []graphicsRuntimeDriver `json:"drivers"`
}

func (o *Options) sectionGPUGraphics(r *reporter, merged *jsonx.OrderedMap) {
	enabled, _, _, _ := gpuConfig(merged)
	if !enabled || o.IsMacOS || runtime.GOOS != "linux" {
		return
	}
	r.sectionHeader("GPU Graphics Driver")
	if !o.inJail() {
		o.reportHostGraphicsSelection(r, merged)
		r.blank()
		return
	}
	o.reportJailGraphicsState(r, merged, "/", o.getenv)
	r.blank()
}

func (o *Options) reportHostGraphicsSelection(r *reporter, merged *jsonx.OrderedMap) {
	packages := config.EffectivePackages(merged, config.PlatformLinux)
	names := packageNames(packages)
	if len(names) == 0 {
		r.warn("GPU graphics driver selection is not visible from the host check",
			"Declare a driver package such as `mesa` under `packages`, then run `yolo check` inside the jail to see its resolved version and exposed tree.")
		return
	}
	r.dim("Selected package declarations: " + strings.Join(names, ", "))
	r.warn("Graphics driver version and /run exposure are jail boot outcomes",
		"The host cannot verify the jail's runtime tree. Run `yolo check` inside the jail to see the resolved driver version and whether `/run/opengl-driver` is exposed.")
}

func (o *Options) reportJailGraphicsState(r *reporter, merged *jsonx.OrderedMap, root string, getenv func(string) string) {
	manifestPath := graphicsRuntimeManifest
	data, err := o.readGraphicsFile(root, manifestPath)
	if err == nil {
		drivers, parseErr := decodeGraphicsRuntimeManifest(data)
		if parseErr != nil {
			r.warn("Graphics runtime metadata is malformed", parseErr.Error()+". Rebuild/restart the jail, then run `yolo check` again.")
			return
		}
		if len(drivers) == 0 {
			r.warn("No graphics driver was selected for this jail",
				graphicsPackageHint(merged)+" If this is unexpected, rebuild and restart the jail, then run `yolo check` again.")
			return
		}
		o.reportGraphicsRuntimeDrivers(r, drivers, root)
		return
	}
	if !os.IsNotExist(err) {
		r.warn("Graphics runtime metadata could not be read",
			fmt.Sprintf("Cannot read %s: %v. Rebuild/restart the jail, then run `yolo check` again.", manifestPath, err))
		return
	}

	profileList := ""
	if getenv != nil {
		profileList = getenv("YOLO_STORE_PROFILES")
	}
	var drivers []graphicsDriver
	if profileList != "" {
		var readErr error
		drivers, readErr = readProfileGraphicsManifests(root, profileList, o.readFile)
		if readErr != nil {
			r.warn("Graphics driver profile metadata is unavailable or malformed", readErr.Error()+". Rebuild the package profile and restart the jail, then run `yolo check` again.")
			return
		}
	} else {
		data, err = o.readGraphicsFile(root, graphicsBakedManifest)
		if err != nil {
			if os.IsNotExist(err) {
				r.warn("No graphics driver metadata is available",
					graphicsPackageHint(merged)+" Runtime exposure cannot be verified until the jail is rebuilt and restarted; then run `yolo check` inside it.")
			} else {
				r.warn("Baked graphics driver metadata could not be read",
					fmt.Sprintf("Cannot read %s: %v. Rebuild the jail image, then run `yolo check` again.", graphicsBakedManifest, err))
			}
			return
		}
		drivers, err = decodeGraphicsManifest(data)
		if err != nil {
			r.warn("Baked graphics driver metadata is malformed", err.Error()+". Rebuild the jail image, then run `yolo check` again.")
			return
		}
	}
	if len(drivers) == 0 {
		r.warn("No graphics driver was found in the active metadata",
			graphicsPackageHint(merged)+" Runtime exposure cannot be verified until driver metadata is available and the jail restarted.")
		return
	}
	r.warn("Resolved graphics driver is known, but exposure was not recorded",
		"Selected driver metadata: "+formatGraphicsDrivers(drivers)+". Runtime provenance is absent, so `/run/opengl-driver` is not attributed to this driver; restart the jail and run `yolo check` again.")
	for _, driver := range drivers {
		tree := graphicsTree(driver.Arch)
		if !o.graphicsPathExists(root, filepath.Join(tree, "lib")) {
			r.warn(fmt.Sprintf("Graphics tree %s is missing lib", tree), "The selected driver was not verified as exposed. Restart the jail so its graphics tree can be created, then run `yolo check` again.")
		}
		if !o.graphicsPathExists(root, filepath.Join(tree, "share")) {
			r.warn(fmt.Sprintf("Graphics tree %s is missing share", tree), "The selected driver was not verified as exposed. Restart the jail so its graphics tree can be created, then run `yolo check` again.")
		}
	}
}

func (o *Options) reportGraphicsRuntimeDrivers(r *reporter, drivers []graphicsRuntimeDriver, root string) {
	for _, driver := range drivers {
		tree := graphicsTree(driver.Arch)
		label := fmt.Sprintf("Selected driver %s %s", driver.Name, driver.Version)
		if driver.Preserved != nil && *driver.Preserved {
			r.warn(label+"; existing user-supplied tree preserved at "+tree,
				"The tree was left untouched and is not asserted to belong to the selected driver. Check the paths below; if they are not the intended driver tree, remove or replace the supplied tree and restart the jail.")
		} else {
			r.ok(label + " exposed at " + tree + " (lib -> " + driver.Lib + ", share -> " + driver.Share + ")")
		}
		for _, leaf := range []string{"lib", "share"} {
			if !o.graphicsPathExists(root, filepath.Join(tree, leaf)) {
				r.warn("Graphics tree path is absent: "+filepath.Join(tree, leaf),
					fmt.Sprintf("The jail did not expose %s. Restart the jail; if the problem remains, verify the selected driver package and any preexisting user-supplied tree.", filepath.Join(tree, leaf)))
			}
		}
	}
}

func readProfileGraphicsManifests(root, profiles string, readFile func(string) ([]byte, error)) ([]graphicsDriver, error) {
	if readFile == nil {
		readFile = os.ReadFile
	}
	var selected []graphicsDriver
	seenArch := map[string]bool{}
	for _, profile := range strings.Split(profiles, ":") {
		if profile == "" {
			continue
		}
		path := rootedPath(root, filepath.Join(profile, graphicsStoreManifest))
		data, err := readFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("cannot read %s: %w", filepath.Join(profile, graphicsStoreManifest), err)
		}
		manifest, err := decodeGraphicsManifest(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Join(profile, graphicsStoreManifest), err)
		}
		for _, driver := range manifest {
			if !seenArch[driver.Arch] {
				selected = append(selected, driver)
				seenArch[driver.Arch] = true
			}
		}
	}
	return selected, nil
}

func (o *Options) graphicsPathExists(root, path string) bool {
	fullPath := rootedPath(root, path)
	if root == "/" && o.PathExists != nil {
		return o.PathExists(fullPath)
	}
	_, err := os.Stat(fullPath)
	return err == nil
}

func (o *Options) readGraphicsFile(root, path string) ([]byte, error) {
	readFile := o.readFile
	if readFile == nil {
		readFile = os.ReadFile
	}
	return readFile(rootedPath(root, path))
}

func decodeGraphicsManifest(data []byte) ([]graphicsDriver, error) {
	var manifest graphicsManifest
	if err := decodeStrictGraphicsJSON(data, &manifest); err != nil {
		return nil, err
	}
	if manifest.Drivers == nil {
		return nil, fmt.Errorf("metadata must contain a drivers array")
	}
	for i, driver := range manifest.Drivers {
		if err := validateGraphicsDriver(driver); err != nil {
			return nil, fmt.Errorf("driver %d: %w", i, err)
		}
	}
	return firstGraphicsDriverPerArch(manifest.Drivers), nil
}

func decodeGraphicsRuntimeManifest(data []byte) ([]graphicsRuntimeDriver, error) {
	var manifest graphicsRuntimeManifestData
	if err := decodeStrictGraphicsJSON(data, &manifest); err != nil {
		return nil, err
	}
	if manifest.Drivers == nil {
		return nil, fmt.Errorf("runtime metadata must contain a drivers array")
	}
	for i, driver := range manifest.Drivers {
		if err := validateGraphicsDriver(driver.graphicsDriver); err != nil {
			return nil, fmt.Errorf("driver %d: %w", i, err)
		}
		if driver.Preserved == nil {
			return nil, fmt.Errorf("driver %d: preserved must be true or false", i)
		}
	}
	first := map[string]bool{}
	out := make([]graphicsRuntimeDriver, 0, len(manifest.Drivers))
	for _, driver := range manifest.Drivers {
		if !first[driver.Arch] {
			out = append(out, driver)
			first[driver.Arch] = true
		}
	}
	return out, nil
}

func decodeStrictGraphicsJSON(data []byte, dst any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return fmt.Errorf("decode graphics metadata: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		if err == nil {
			return fmt.Errorf("metadata contains more than one JSON value")
		}
		return fmt.Errorf("decode graphics metadata trailer: %w", err)
	}
	return nil
}

func validateGraphicsDriver(driver graphicsDriver) error {
	if strings.TrimSpace(driver.Name) == "" || strings.TrimSpace(driver.Version) == "" {
		return fmt.Errorf("name and version are required")
	}
	if driver.Arch != "64" && driver.Arch != "32" {
		return fmt.Errorf("arch must be 64 or 32, got %q", driver.Arch)
	}
	if !filepath.IsAbs(driver.Lib) || !filepath.IsAbs(driver.Share) {
		return fmt.Errorf("lib and share must be absolute resolved output paths")
	}
	return nil
}

func firstGraphicsDriverPerArch(drivers []graphicsDriver) []graphicsDriver {
	seen := map[string]bool{}
	out := make([]graphicsDriver, 0, len(drivers))
	for _, driver := range drivers {
		if !seen[driver.Arch] {
			out = append(out, driver)
			seen[driver.Arch] = true
		}
	}
	return out
}

func graphicsTree(arch string) string {
	if arch == "32" {
		return graphics32Tree
	}
	return graphics64Tree
}

func rootedPath(root, path string) string {
	if root == "" || root == "/" {
		return path
	}
	return filepath.Join(root, strings.TrimLeft(filepath.FromSlash(path), string(filepath.Separator)))
}

func packageNames(packages []any) []string {
	var names []string
	for _, raw := range packages {
		switch pkg := raw.(type) {
		case string:
			names = append(names, pkg)
		case *jsonx.OrderedMap:
			if name, ok := pkg.Get("name"); ok {
				if value, ok := name.(string); ok {
					names = append(names, value)
				}
			}
		}
	}
	return names
}

func formatGraphicsDrivers(drivers []graphicsDriver) string {
	items := make([]string, 0, len(drivers))
	for _, driver := range drivers {
		items = append(items, fmt.Sprintf("%s %s (%s-bit)", driver.Name, driver.Version, driver.Arch))
	}
	return strings.Join(items, ", ")
}

func graphicsPackageHint(merged *jsonx.OrderedMap) string {
	packages := packageNames(config.EffectivePackages(merged, config.PlatformLinux))
	if len(packages) > 0 {
		return "Configured packages: " + strings.Join(packages, ", ") + ". Add the intended graphics driver (for example `mesa`) under `packages`; pin its `nixpkgs` source there to hold the resolved version steady."
	}
	return "Add the intended graphics driver (for example `mesa`) under `packages`; pin its `nixpkgs` source there to hold the resolved version steady."
}
