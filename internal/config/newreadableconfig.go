package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/configdoc"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

const readableConfigFormat = 2

// loadConfigFile dispatches by the selected filename. Legacy JSONC/JSON reads stay on their
// existing decoder and error path; TOML goes through the shared positioned document contract.
func loadConfigFile(path, label string, strict bool, warn Warn, record bool, scope SchemaScope, at readAt) (*jsonx.OrderedMap, *srcFile, *srcNode, error) {
	if strings.EqualFold(filepath.Ext(path), ".toml") {
		return loadTOMLConfigFile(path, label, strict, warn, record, scope, at)
	}
	return loadJSONCFile(path, label, strict, warn, record, at)
}

func loadTOMLConfigFile(path, label string, strict bool, warn Warn, record bool, scope SchemaScope, at readAt) (*jsonx.OrderedMap, *srcFile, *srcNode, error) {
	if warn == nil {
		warn = defaultWarn
	}
	data, rel, err := at.ws.read(path)
	if err != nil {
		if os.IsNotExist(err) {
			return jsonx.NewOrderedMap(), nil, nil, nil
		}
		m, parseErr := handleParseFailure(label, err, strict, warn)
		return m, nil, nil, parseErr
	}
	doc, err := configdoc.Parse(data, configdoc.TOML)
	if err != nil {
		m, parseErr := handleParseFailure(label, err, strict, warn)
		return m, nil, nil, parseErr
	}
	if err := normalizeTOMLNumbers(doc.Values); err != nil {
		return nil, nil, nil, err
	}
	if err := consumeReadableFormat(doc.Values, label); err != nil {
		return nil, nil, nil, err
	}
	if err := validateReadableTOML(doc.Values, scope, label, doc.Index); err != nil {
		return nil, nil, nil, err
	}
	file := &srcFile{path: path, data: data, format: configdoc.TOML, document: doc,
		contained: rel != "", rel: rel, via: at.via, seq: at.ws.nextSeq()}
	if !record {
		return doc.Values, file, nil, nil
	}
	return doc.Values, file, fileTree(file, doc.Values), nil
}

func normalizeTOMLNumbers(value any) error {
	switch current := value.(type) {
	case *jsonx.OrderedMap:
		for _, key := range current.Keys() {
			v, _ := current.Get(key)
			normalized, err := normalizeTOMLNumber(v)
			if err != nil {
				return err
			}
			current.Set(key, normalized)
		}
	case []any:
		for i, v := range current {
			normalized, err := normalizeTOMLNumber(v)
			if err != nil {
				return err
			}
			current[i] = normalized
		}
	}
	return nil
}

func normalizeTOMLNumber(value any) (any, error) {
	switch current := value.(type) {
	case int64:
		normalized, ok := jsonx.IntLiteral(strconv.FormatInt(current, 10))
		if !ok {
			return nil, fmt.Errorf("invalid TOML integer %d", current)
		}
		return normalized, nil
	case *jsonx.OrderedMap, []any:
		if err := normalizeTOMLNumbers(current); err != nil {
			return nil, err
		}
		return current, nil
	default:
		return value, nil
	}
}

func consumeReadableFormat(values *jsonx.OrderedMap, label string) error {
	version, present := values.Get("format")
	if !present {
		return nil
	}
	n, ok := jsonx.AsInt(version)
	if !ok {
		return configErr("%s: format must be the integer 2 (got %s)", label, reprAny(version))
	}
	if n > readableConfigFormat {
		return configErr("%s is format %d; this yolo reads format 2. Upgrade with `yolo update`, or `just install` from a checkout. Nothing was started", label, n)
	}
	if n != readableConfigFormat {
		return configErr("%s: format %d is unsupported in a TOML file; TOML config files use format 2. Run `yolo config migrate` for the supported format", label, n)
	}
	values.Delete("format")
	return nil
}

func resolveUserConfigPath(legacyPath string) (path, label string, toml bool, err error) {
	dir := filepath.Dir(legacyPath)
	candidates := []string{paths.UserConfigTOMLName, paths.UserConfigLegacyJSONCName, paths.UserConfigLegacyJSONName}
	var found []string
	for _, candidate := range candidates {
		if pathExists(filepath.Join(dir, candidate)) {
			found = append(found, candidate)
		}
	}
	if len(found) > 1 && found[0] == paths.UserConfigTOMLName {
		return "", "", false, configErr("both %s and %s exist; config formats are never merged. Keep one file and run `yolo config migrate` to convert the legacy file", found[0], found[1])
	}
	name := paths.UserConfigTOMLName
	if len(found) > 0 {
		name = found[0]
	}
	return filepath.Join(dir, name), filepath.Join(dir, name), name == paths.UserConfigTOMLName, nil
}

func loadUserConfigFragments(dir string, strict bool, warn Warn, seen map[string]struct{}, record bool) (*jsonx.OrderedMap, *srcNode, error) {
	fragmentDir := filepath.Join(dir, "config.d")
	entries, err := os.ReadDir(fragmentDir)
	if err != nil {
		if os.IsNotExist(err) {
			return jsonx.NewOrderedMap(), nil, nil
		}
		m, parseErr := handleParseFailure(fragmentDir, err, strict, warn)
		if parseErr != nil {
			return nil, nil, parseErr
		}
		return m, nil, nil
	}
	var names []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".toml") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	merged := jsonx.NewOrderedMap()
	var mergedNode *srcNode
	for _, name := range names {
		path := filepath.Join(fragmentDir, name)
		info, statErr := os.Stat(path) // Follow conventional symlinked fragments.
		if statErr != nil {
			_, parseErr := handleParseFailure(path, statErr, strict, warn)
			if parseErr != nil {
				return nil, nil, parseErr
			}
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		fragment, node, loadErr := loadWithIncludesScope(path, path, strict, warn, seen, record, SchemaScopeUserOnly, readAt{})
		if loadErr != nil {
			return nil, nil, loadErr
		}
		merged, mergedNode = mergeReadableConfig(merged, fragment, mergedNode, node, nil)
	}
	return merged, mergedNode, nil
}

func mergeUserConfigFragments(base *jsonx.OrderedMap, baseNode *srcNode, dir string, strict bool, warn Warn, seen map[string]struct{}, record bool) (*jsonx.OrderedMap, *srcNode, error) {
	fragments, fragmentNode, err := loadUserConfigFragments(dir, strict, warn, seen, record)
	if err != nil {
		return nil, nil, err
	}
	merged, node := mergeReadableConfig(base, fragments, baseNode, fragmentNode, nil)
	return merged, node, nil
}
