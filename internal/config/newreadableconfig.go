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
	"github.com/mschulkind-oss/yolo-jail/internal/shquote"
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
		backup := shquote.Join([]string{"cp", "-p", label, label + ".backup"})
		return configErr("%s: format %d is unsupported in a TOML file; TOML config files use format 2. First confirm a non-config backup path does not already exist and preserve this file with `%s`; then manually rewrite its contents as TOML format 2, checking every key against the accepted TOML schema before using it. changing or removing the format marker alone is not a conversion; no lossless or equivalence-checked migration is available", label, n, backup)
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
		keepTOML := shquote.Join([]string{"mv", filepath.Join(dir, found[1]), filepath.Join(dir, found[1]+".backup")})
		keepLegacy := shquote.Join([]string{"mv", filepath.Join(dir, found[0]), filepath.Join(dir, found[0]+".backup")})
		return "", "", false, configErr("both %s and %s exist; config formats are never merged. Choose which file to keep active, first confirm the backup path does not already exist, then rerun the command. To keep TOML, move the legacy file with `%s`; to keep the legacy file, move TOML with `%s`. This preserves the moved file; neither file is converted or merged", found[0], found[1], keepTOML, keepLegacy)
	}
	name := paths.UserConfigTOMLName
	if len(found) > 0 {
		name = found[0]
	}
	return filepath.Join(dir, name), filepath.Join(dir, name), name == paths.UserConfigTOMLName, nil
}

func loadUserConfigFragments(base *jsonx.OrderedMap, baseNode *srcNode, dir string, strict bool, warn Warn, seen map[string]struct{}, record bool) (*jsonx.OrderedMap, *srcNode, error) {
	fragmentDir := filepath.Join(dir, "config.d")
	entries, err := os.ReadDir(fragmentDir)
	if err != nil {
		if os.IsNotExist(err) {
			if base == nil {
				base = jsonx.NewOrderedMap()
			}
			return base, baseNode, nil
		}
		_, parseErr := handleParseFailure(fragmentDir, err, strict, warn)
		if parseErr != nil {
			return nil, nil, parseErr
		}
		if base == nil {
			base = jsonx.NewOrderedMap()
		}
		return base, baseNode, nil
	}
	var names []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".toml") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	merged := base
	if merged == nil {
		merged = jsonx.NewOrderedMap()
	}
	mergedNode := baseNode
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
	fragments, fragmentNode, err := loadUserConfigFragments(base, baseNode, dir, strict, warn, seen, record)
	if err != nil {
		return nil, nil, err
	}
	return fragments, fragmentNode, nil
}
