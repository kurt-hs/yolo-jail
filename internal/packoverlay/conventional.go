package packoverlay

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/mschulkind-oss/yolo-jail/internal/agentcfg"
	"github.com/mschulkind-oss/yolo-jail/internal/agentcfg/codec"
	"github.com/mschulkind-oss/yolo-jail/internal/agentcfg/manifest"
	"github.com/mschulkind-oss/yolo-jail/internal/json5"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/packdecl"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// conventionalOverlayBeforeOpen is a deterministic substitution seam for package tests.
var conventionalOverlayBeforeOpen func(component string)

func placeConventionalOverlay(set *OverlaySet, pack *packload.Pack, file packload.OverlayFile,
	owners map[manifest.SurfaceKey]string, known map[manifest.SurfaceKey]manifest.Surface,
	autonomy bool, activeProfiles map[string][]string) {
	identity, profile, format, err := conventionalIdentity(file.Name, known)
	if err != nil {
		set.Problems = append(set.Problems, fmt.Sprintf("pack %s: overlays/%s: %v", pack.Name, file.Name, err))
		return
	}
	body, err := readConventionalOverlay(filepath.Join(pack.Root, "overlays", file.Name), format, pack.Local)
	if err != nil {
		set.Problems = append(set.Problems, fmt.Sprintf("pack %s: overlays/%s: %v", pack.Name, file.Name, err))
		return
	}
	if surface, ok := known[identity]; ok && codecName(format) != surface.Codec {
		set.Problems = append(set.Problems, fmt.Sprintf("pack %s: overlays/%s targets %s whose owner codec is %q, but the extension selects %q",
			pack.Name, file.Name, identity.String(), surface.Codec, codecName(format)))
		return
	}
	if profile != "" && !profileActive(activeProfiles[identity.Agent], profile) {
		return
	}
	if _, owned := owners[identity]; !owned {
		_, coreOwned := agentcfg.BuiltinManifest().Lookup(identity.Agent, identity.Name)
		set.Orphans = append(set.Orphans, OrphanOverlay{
			Kind: packdecl.KindConfigOverlay, Pack: pack.Name, Target: identity.String(),
			Owner: shippedOwnerOf(identity, autonomy), CoreOwned: coreOwned,
		})
		return
	}
	set.byTarget[identity] = append(set.byTarget[identity], agentcfg.Overlay{
		Pack: pack.Name, Data: body, AppendArrays: true,
	})
}

func conventionalIdentity(filename string, known map[manifest.SurfaceKey]manifest.Surface) (manifest.SurfaceKey, string, string, error) {
	ext := filepath.Ext(filename)
	format := strings.TrimPrefix(ext, ".")
	if codecName(format) == "" {
		return manifest.SurfaceKey{}, "", "", fmt.Errorf("unsupported overlay extension %q; use .jsonc, .json, .toml, .yaml or .yml", ext)
	}
	stem := strings.TrimSuffix(filename, ext)
	base, profile, gated := strings.Cut(stem, "@")
	if gated && (profile == "" || strings.Contains(profile, "@")) {
		return manifest.SurfaceKey{}, "", "", fmt.Errorf("profile suffix must be one non-empty @profile")
	}
	keys := make([]manifest.SurfaceKey, 0)
	for key := range known {
		if overlaySlug(key) == base {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
	if len(keys) > 1 {
		return manifest.SurfaceKey{}, "", "", fmt.Errorf("ambiguous surface identity %q; declared alternatives are %s", base, surfaceNames(keys))
	}
	if len(keys) == 1 {
		return keys[0], profile, format, nil
	}
	// Do not guess where a hyphen splits agent and surface. If a known agent prefix
	// uniquely resolves the token, the remaining suffix is an unowned surface identity;
	// otherwise name every viable split so a typo cannot pick an arbitrary owner.
	agents := map[string]bool{}
	for key := range known {
		if strings.HasPrefix(base, strings.ReplaceAll(key.Agent, "/", "-")+"-") {
			agents[key.Agent] = true
		}
	}
	var alternatives []manifest.SurfaceKey
	for agent := range agents {
		prefix := strings.ReplaceAll(agent, "/", "-") + "-"
		name := strings.TrimPrefix(base, prefix)
		if name != "" {
			alternatives = append(alternatives, manifest.SurfaceKey{Agent: agent, Name: name})
		}
	}
	sort.Slice(alternatives, func(i, j int) bool { return alternatives[i].String() < alternatives[j].String() })
	if len(alternatives) == 1 {
		return alternatives[0], profile, format, nil
	}
	if len(alternatives) > 1 {
		return manifest.SurfaceKey{}, "", "", fmt.Errorf("ambiguous surface identity %q; possible declared-agent alternatives are %s", base, surfaceNames(alternatives))
	}
	return manifest.SurfaceKey{}, "", "", fmt.Errorf("cannot resolve surface identity %q; select its owner pack and use agent-surface spelling", base)
}

func overlaySlug(key manifest.SurfaceKey) string {
	return strings.ReplaceAll(key.Agent+"/"+key.Name, "/", "-")
}

func surfaceNames(keys []manifest.SurfaceKey) string {
	values := make([]string, len(keys))
	for i, key := range keys {
		values[i] = key.String()
	}
	return strings.Join(values, ", ")
}

func codecName(format string) string {
	switch format {
	case "jsonc", "json":
		return "json"
	case "toml":
		return "toml"
	case "yaml", "yml":
		return "yaml"
	default:
		return ""
	}
}

func readConventionalOverlay(path, format string, followLocalLinks bool) (map[string]any, error) {
	var data []byte
	var err error
	if followLocalLinks {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return nil, fmt.Errorf("cannot resolve local overlay: %w", err)
		}
		fd, err := unix.Open(resolved, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
		if err != nil {
			return nil, fmt.Errorf("cannot read local overlay: %w", err)
		}
		file := os.NewFile(uintptr(fd), resolved)
		defer file.Close()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("local overlay is not a regular file")
		}
		data, err = io.ReadAll(file)
		if err != nil {
			return nil, fmt.Errorf("cannot read local overlay: %w", err)
		}
	} else {
		data, err = readConventionalOverlayBeneath(path)
		if err != nil {
			return nil, err
		}
	}
	var value any
	if format == "jsonc" {
		parsed, err := json5.Decode(data)
		if err != nil {
			return nil, fmt.Errorf("invalid JSONC overlay: %w", err)
		}
		value = jsonx.Plain(parsed)
	} else {
		c, ok := codec.LookupCodec(codecName(format))
		if !ok {
			return nil, fmt.Errorf("no decoder for extension .%s", format)
		}
		value, err = c.Decode(data)
		if err != nil {
			return nil, fmt.Errorf("invalid %s overlay: %w", strings.ToUpper(format), err)
		}
	}
	object, ok := value.(map[string]any)
	if !ok || len(object) == 0 {
		return nil, fmt.Errorf("overlay must contain a non-empty object")
	}
	return object, nil
}

func readConventionalOverlayBeneath(path string) ([]byte, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve overlay path: %w", err)
	}
	root, err := filepath.EvalSymlinks(filepath.Dir(filepath.Dir(abs)))
	if err != nil {
		return nil, fmt.Errorf("cannot resolve pack root: %w", err)
	}
	rootFD, err := openConventionalDirectory(root)
	if err != nil {
		return nil, fmt.Errorf("cannot open pack root: %w", err)
	}
	defer unix.Close(rootFD)
	if conventionalOverlayBeforeOpen != nil {
		conventionalOverlayBeforeOpen("overlays")
	}
	dirFD, err := unix.Openat(rootFD, "overlays", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("cannot open overlay directory without following links: %w", err)
	}
	defer unix.Close(dirFD)
	if conventionalOverlayBeforeOpen != nil {
		conventionalOverlayBeforeOpen("leaf")
	}
	fd, err := unix.Openat(dirFD, filepath.Base(abs), unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("cannot open overlay as a regular file without following symlinks: %w", err)
	}
	file := os.NewFile(uintptr(fd), abs)
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("cannot inspect opened overlay: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("overlay is not a regular file")
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("cannot read overlay: %w", err)
	}
	return data, nil
}

func openConventionalDirectory(path string) (int, error) {
	fd, err := unix.Open(string(filepath.Separator), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}
	for _, component := range strings.Split(strings.TrimPrefix(filepath.Clean(path), string(filepath.Separator)), string(filepath.Separator)) {
		if component == "" {
			continue
		}
		next, err := unix.Openat(fd, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		unix.Close(fd)
		if err != nil {
			return -1, err
		}
		fd = next
	}
	return fd, nil
}
