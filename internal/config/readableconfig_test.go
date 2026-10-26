package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func TestLoadWorkspaceConfigReadsReadableInstall(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, paths.WorkspaceConfigTOMLName), "[install]\nnix = ['just']\nmise = { python = '3.12' }\n")

	got, err := LoadWorkspaceConfig(dir, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, exists := got.Get("install")
	install, valid := raw.(*jsonx.OrderedMap)
	if !exists || !valid {
		t.Fatalf("install missing: %#v", got)
	}
	nix, _ := install.Get("nix")
	if !reflect.DeepEqual(nix, []any{"just"}) {
		t.Fatalf("install.nix = %#v, want [\"just\"]", nix)
	}
	mise, _ := install.Get("mise")
	python, _ := mise.(*jsonx.OrderedMap).Get("python")
	if python != "3.12" {
		t.Fatalf("install.mise.python = %#v, want string \"3.12\"", python)
	}
}

func TestLoadWorkspaceConfigReadableFormatAndIncludes(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, paths.WorkspaceConfigTOMLName), "format = 2\ninclude_if_found = ['parts/base.toml']\n\n[machine.resources]\ncpus = 8\n")
	write(t, filepath.Join(dir, "parts", "base.toml"), "include_if_found = ['nested.toml']\n[install]\nnix = ['just']\n")
	write(t, filepath.Join(dir, "parts", "nested.toml"), "[machine.resources]\nmemory = '16g'\n")
	write(t, filepath.Join(dir, paths.WorkspaceLocalConfigTOMLName), "[machine.resources]\ncpus = 10\n")

	got, src, err := LoadWorkspaceConfigWithSources(dir, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := got.Get("format"); exists {
		t.Fatal("format is reader metadata and must not appear in effective config")
	}
	install, _ := got.Get("install")
	nix, _ := install.(*jsonx.OrderedMap).Get("nix")
	if !reflect.DeepEqual(nix, []any{"just"}) {
		t.Fatalf("included install.nix = %#v", nix)
	}
	resources, _ := got.Get("machine")
	resources, _ = resources.(*jsonx.OrderedMap).Get("resources")
	cpus, _ := resources.(*jsonx.OrderedMap).Get("cpus")
	memory, _ := resources.(*jsonx.OrderedMap).Get("memory")
	cpusN, cpusIsInt := jsonx.AsInt(cpus)
	if !cpusIsInt || cpusN != 10 || memory != "16g" {
		t.Fatalf("machine.resources = %#v", resources)
	}
	if locs := src.Locations("config.install.nix[0]"); len(locs) != 1 || !strings.Contains(locs[0], "parts/base.toml:") {
		t.Fatalf("included TOML source location = %q", locs)
	}
}

func TestLoadWorkspaceConfigReadableIncludesOptionalAndCycle(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, paths.WorkspaceConfigTOMLName), "include_if_found = ['parts/one.toml', 'missing.toml']\n[install]\nnix = ['base']\n")
	write(t, filepath.Join(dir, "parts", "one.toml"), "include_if_found = ['two.toml']\n[install]\nnix = ['one']\n")
	write(t, filepath.Join(dir, "parts", "two.toml"), "include_if_found = ['one.toml']\n[install]\nnix = ['two']\n")

	got, err := LoadWorkspaceConfig(dir, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	install, _ := got.Get("install")
	nix, _ := install.(*jsonx.OrderedMap).Get("nix")
	if !reflect.DeepEqual(nix, []any{"base", "one", "two"}) {
		t.Fatalf("cycle/optional include result = %#v", nix)
	}
}

func TestLoadUserScopeReadsSortedConfigDFragments(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	userDir := filepath.Join(home, ".config", "yolo-jail")
	write(t, filepath.Join(userDir, paths.UserConfigTOMLName), "packs = ['base']\n")
	withoutFragments, err := UserScopeConfig(true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := withoutFragments.Get("packs"); !reflect.DeepEqual(got, []any{"base"}) {
		t.Fatalf("config without config.d = %#v", got)
	}
	fragments := filepath.Join(userDir, "config.d")
	if err := os.MkdirAll(fragments, 0o700); err != nil {
		t.Fatal(err)
	}
	fragment := filepath.Join(home, "outside.toml")
	write(t, fragment, "packs = ['linked']\n")
	if err := os.Symlink(fragment, filepath.Join(fragments, "20-extra.toml")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(fragments, "10-extra.toml"), "packs = ['first']\n")
	write(t, filepath.Join(fragments, "readme.txt"), "not a config")

	got, err := UserScopeConfig(true, nil)
	if err != nil {
		t.Fatal(err)
	}
	packs, _ := got.Get("packs")
	if !reflect.DeepEqual(packs, []any{"base", "first", "linked"}) {
		t.Fatalf("config.d merge order = %#v", packs)
	}
}

func TestLoadUserScopeRejectsReadableLegacyCollision(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	userDir := filepath.Join(home, ".config", "yolo-jail")
	write(t, filepath.Join(userDir, paths.UserConfigTOMLName), "packs = ['new']\n")
	write(t, filepath.Join(userDir, paths.UserConfigLegacyJSONCName), `{"packs":["old"]}`)
	if _, err := UserScopeConfig(true, nil); err == nil ||
		!strings.Contains(err.Error(), paths.UserConfigTOMLName) || !strings.Contains(err.Error(), paths.UserConfigLegacyJSONCName) {
		t.Fatalf("user format collision error = %v, want both sibling names", err)
	}
}

func TestReadableTOMLSourcePositionsHandleQuotedKeysAndArrayItems(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, paths.WorkspaceConfigTOMLName), "mounts = [{ host = '~/repo', at = '/ctx/repo' }]\n\n[providers.\"org/name.with.dot\"]\nregion = 'east'\n")
	_, src, err := LoadWorkspaceConfigWithSources(dir, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	mountLocs := src.Locations("config.mounts[0].host")
	providerLocs := src.Locations("config.providers.org/name.with.dot.region")
	if len(mountLocs) != 1 || !strings.HasSuffix(mountLocs[0], ":1:20") {
		t.Fatalf("array item source location = %q", mountLocs)
	}
	if len(providerLocs) != 1 || !strings.HasSuffix(providerLocs[0], ":4:10") {
		t.Fatalf("quoted dotted/slash key source location = %q", providerLocs)
	}
}

func TestLoadWorkspaceConfigReadableRejectsSiblingCollision(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, paths.WorkspaceConfigTOMLName), "[install]\nnix = ['just']\n")
	write(t, filepath.Join(dir, paths.WorkspaceConfigLegacyJSONCName), `{"packages":["just"]}`)
	if _, err := LoadWorkspaceConfig(dir, true, nil); err == nil ||
		!strings.Contains(err.Error(), paths.WorkspaceConfigTOMLName) || !strings.Contains(err.Error(), paths.WorkspaceConfigLegacyJSONCName) {
		t.Fatalf("collision error = %v, want both sibling names", err)
	}
}

func TestLoadWorkspaceConfigReadableFormatVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, paths.WorkspaceConfigTOMLName)
	write(t, path, "format = 3\n")
	if _, err := LoadWorkspaceConfig(dir, true, nil); err == nil || !strings.Contains(err.Error(), "format 3") {
		t.Fatalf("future format error = %v", err)
	}
	write(t, path, "format = 2\n[install]\nnix = ['just']\n")
	if _, err := LoadWorkspaceConfig(dir, true, nil); err != nil {
		t.Fatalf("format 2 refused: %v", err)
	}
	write(t, path, "format = 1\n")
	if _, err := LoadWorkspaceConfig(dir, true, nil); err == nil {
		t.Fatal("format 1 was accepted in the TOML filename")
	}
}
