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

func TestLoadReadableWorkspaceConfigReadsReadableInstall(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, paths.WorkspaceConfigTOMLName), "[install]\nnix = ['just']\nmise = { python = '3.12' }\n")

	got, err := LoadReadableWorkspaceConfig(dir, true, nil)
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

func TestLoadReadableWorkspaceConfigFormatAndIncludes(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, paths.WorkspaceConfigTOMLName), "format = 2\ninclude_if_found = ['parts/base.toml']\n\n[machine.resources]\ncpus = 8\n")
	write(t, filepath.Join(dir, "parts", "base.toml"), "include_if_found = ['nested.toml']\n[install]\nnix = ['just']\n")
	write(t, filepath.Join(dir, "parts", "nested.toml"), "[machine.resources]\nmemory = '16g'\n")
	write(t, filepath.Join(dir, paths.WorkspaceLocalConfigTOMLName), "[machine.resources]\ncpus = 10\n")

	got, src, err := LoadReadableWorkspaceConfigWithSources(dir, true, nil)
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

func TestLoadReadableWorkspaceConfigIncludesOptionalAndCycle(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, paths.WorkspaceConfigTOMLName), "include_if_found = ['parts/one.toml', 'missing.toml']\n[install]\nnix = ['base']\n")
	write(t, filepath.Join(dir, "parts", "one.toml"), "include_if_found = ['two.toml']\n[install]\nnix = ['one']\n")
	write(t, filepath.Join(dir, "parts", "two.toml"), "include_if_found = ['one.toml']\n[install]\nnix = ['two']\n")

	got, err := LoadReadableWorkspaceConfig(dir, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	install, _ := got.Get("install")
	nix, _ := install.(*jsonx.OrderedMap).Get("nix")
	if !reflect.DeepEqual(nix, []any{"base", "one", "two"}) {
		t.Fatalf("cycle/optional include result = %#v", nix)
	}
}

func TestLoadReadableUserScopeConfigDFragmentsApplyResetSequentiallyWithSources(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	userDir := filepath.Join(home, ".config", "yolo-jail")
	basePath := filepath.Join(userDir, paths.UserConfigTOMLName)
	fragments := filepath.Join(userDir, "config.d")
	resetPath := filepath.Join(fragments, "00-reset.toml")
	lastPath := filepath.Join(fragments, "01-last.toml")
	write(t, basePath, "[install]\nnix = ['a']\n")
	write(t, resetPath, "[install]\nnix = []\n")
	write(t, lastPath, "[install]\nnix = ['b']\n")

	got, sources, err := LoadReadableUserScopeConfigWithSources(true, nil)
	if err != nil {
		t.Fatal(err)
	}
	install, _ := got.Get("install")
	nix, _ := install.(*jsonx.OrderedMap).Get("nix")
	if !reflect.DeepEqual(nix, []any{"b"}) {
		t.Fatalf("user config.d install.nix = %#v, want [\"b\"] after the fragment reset", nix)
	}
	locations := sources.Locations("config.install.nix")
	for _, path := range []string{basePath, resetPath, lastPath} {
		found := false
		for _, location := range locations {
			if strings.Contains(location, filepath.Base(path)+":") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("install.nix provenance %q omits writer %q", locations, path)
		}
	}
	itemLocations := sources.Locations("config.install.nix[0]")
	if len(itemLocations) != 1 || !strings.Contains(itemLocations[0], filepath.Base(lastPath)+":") {
		t.Errorf("surviving install.nix item provenance = %q, want only final fragment %q", itemLocations, lastPath)
	}
}

func TestLoadReadableConfigComposesEmptyListResetAcrossThreeLayersWithSources(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	userPath := filepath.Join(home, ".config", "yolo-jail", paths.UserConfigTOMLName)
	write(t, userPath, "[install]\nnix = ['a']\n")
	workspace := t.TempDir()
	projectPath := filepath.Join(workspace, paths.WorkspaceConfigTOMLName)
	localPath := filepath.Join(workspace, paths.WorkspaceLocalConfigTOMLName)
	write(t, projectPath, "[install]\nnix = []\n")
	write(t, localPath, "[install]\nnix = ['b']\n")

	got, sources, err := LoadReadableConfigWithSources(workspace, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	install, _ := got.Get("install")
	nix, _ := install.(*jsonx.OrderedMap).Get("nix")
	if !reflect.DeepEqual(nix, []any{"b"}) {
		t.Fatalf("three-layer install.nix = %#v, want [\"b\"] after the middle empty list clears the base", nix)
	}
	locations := sources.Locations("config.install.nix")
	for _, path := range []string{userPath, projectPath, localPath} {
		found := false
		for _, location := range locations {
			if strings.Contains(location, filepath.Base(path)+":") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("install.nix provenance %q omits writer %q", locations, path)
		}
	}
	itemLocations := sources.Locations("config.install.nix[0]")
	if len(itemLocations) != 1 || !strings.Contains(itemLocations[0], filepath.Base(localPath)+":") {
		t.Errorf("surviving install.nix item provenance = %q, want only local layer %q", itemLocations, localPath)
	}
}

func TestLoadReadableUserScopeReadsSortedConfigDFragments(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	userDir := filepath.Join(home, ".config", "yolo-jail")
	write(t, filepath.Join(userDir, paths.UserConfigTOMLName), "packs = ['base']\n")
	withoutFragments, err := LoadReadableUserScopeConfig(true, nil)
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

	got, err := LoadReadableUserScopeConfig(true, nil)
	if err != nil {
		t.Fatal(err)
	}
	packs, _ := got.Get("packs")
	if !reflect.DeepEqual(packs, []any{"base", "first", "linked"}) {
		t.Fatalf("config.d merge order = %#v", packs)
	}
}

func TestLoadReadableUserScopeRejectsLegacyCollision(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	userDir := filepath.Join(home, ".config", "yolo-jail")
	write(t, filepath.Join(userDir, paths.UserConfigTOMLName), "packs = ['new']\n")
	write(t, filepath.Join(userDir, paths.UserConfigLegacyJSONCName), `{"packs":["old"]}`)
	if _, err := LoadReadableUserScopeConfig(true, nil); err == nil ||
		!strings.Contains(err.Error(), paths.UserConfigTOMLName) || !strings.Contains(err.Error(), paths.UserConfigLegacyJSONCName) {
		t.Fatalf("user format collision error = %v, want both sibling names", err)
	}
}

func TestLoadReadableUserScopeAcceptsUserSideNewVocabulary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".config", "yolo-jail", paths.UserConfigTOMLName)
	write(t, path, `packs = ['claude', { git = 'https://example.invalid/matt-craft', ref = 'main' }]
[pack_sources.acme]
git = 'https://example.invalid/yolo-packs'
ref = 'main'
[active_provider]
default = 'bedrock'
[providers.zai]
api_key_env_name = 'ZAI_KEY'
[allow.aws-auth]
profile = 'my-sso-profile'
unnarrowed = true
`)
	if _, err := LoadReadableUserScopeConfig(true, nil); err != nil {
		t.Fatalf("valid user-side readable config refused: %v", err)
	}
}

func TestReadableTOMLSourcePositionsHandleQuotedKeysAndArrayItems(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, paths.WorkspaceConfigTOMLName), "mounts = [{ host = '~/repo', at = '/ctx/repo' }]\n\n[providers.\"org/name.with.dot\"]\nregion = 'east'\n")
	_, src, err := LoadReadableWorkspaceConfigWithSources(dir, true, nil)
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

func TestReadableTOMLRejectsLegacyVocabularyWithoutAliases(t *testing.T) {
	cases := []struct {
		name, document, want string
	}{
		{"packages", `packages = ['just']
`, "install.nix"},
		{"brokered", `[brokered.git]
always = ['fetch']
`, "allow.*"},
		{"agents", `agents = ['claude']
`, "packs"},
		{"agent_profiles", `[agent_profiles]
pi = 'bedrock'
`, "active_provider"},
		{"use_profiles", `use_profiles = 'bedrock'
`, "active_provider"},
		{"repo_path", `repo_path = '/src/yolo'
`, "retired"},
		{"macos_log", `macos_log = 'user'
`, "macos-log pack"},
		{"mcp_presets", `mcp_presets = ['chrome-devtools']
`, "mcp_servers"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, filepath.Join(dir, paths.WorkspaceConfigTOMLName), tc.document)
			if _, err := LoadReadableWorkspaceConfig(dir, true, nil); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("legacy TOML key error = %v, want guidance containing %q", err, tc.want)
			}
		})
	}
}

func TestReadableTOMLWorkspaceScopeRefusesUserLeversButAllowsModelFacts(t *testing.T) {
	cases := []struct {
		name, document, want string
	}{
		{"packs", `packs = ['claude']
`, "user-scope only"},
		{"provider routing", `[providers.zai]
api_key_env_name = 'ZAI_KEY'
`, "user-scope only"},
		{"mount write", `mounts = [{ host = '~/data', at = '/ctx/data', write = true }]
`, "user-scope only"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, filepath.Join(dir, paths.WorkspaceConfigTOMLName), tc.document)
			if _, err := LoadReadableWorkspaceConfig(dir, true, nil); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("scope refusal = %v, want %q", err, tc.want)
			}
		})
	}

	dir := t.TempDir()
	write(t, filepath.Join(dir, paths.WorkspaceConfigTOMLName), `[providers.zai.models."model.with.dot"]
name = 'Project model label'
[requests.github]
repos = ['acme/shared']
`)
	if _, err := LoadReadableWorkspaceConfig(dir, true, nil); err != nil {
		t.Fatalf("existing shared model fact/request was refused: %v", err)
	}
}

func TestReadableTOMLEnvFilesAnchorToDeclaringLayer(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, paths.WorkspaceConfigTOMLName), "include_if_found = ['parts/env.toml']\n[env]\nfiles = ['root.env']\n")
	write(t, filepath.Join(dir, "parts", "env.toml"), "[env]\nfiles = ['nested.env']\n")
	got, err := LoadReadableWorkspaceConfig(dir, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	env, _ := got.Get("env")
	files, _ := env.(*jsonx.OrderedMap).Get("files")
	want := []any{filepath.Join(dir, "root.env"), filepath.Join(dir, "parts", "nested.env")}
	if !reflect.DeepEqual(files, want) {
		t.Fatalf("anchored env.files = %#v, want %#v", files, want)
	}
}

func TestReadableTOMLEmptyListClearsPolicyKeyAndRetainsLayerSources(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, paths.WorkspaceConfigTOMLName), "[machine]\nports = [{ host = 8080, jail = 80 }]\n")
	write(t, filepath.Join(dir, paths.WorkspaceLocalConfigTOMLName), "[machine]\nports = []\n")
	got, sources, err := LoadReadableWorkspaceConfigWithSources(dir, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	machine, _ := got.Get("machine")
	ports, _ := machine.(*jsonx.OrderedMap).Get("ports")
	if gotPorts, ok := ports.([]any); !ok || len(gotPorts) != 0 {
		t.Fatalf("empty-list removal result = %#v", ports)
	}
	locations := sources.Locations("config.machine.ports")
	if len(locations) != 2 || !strings.Contains(strings.Join(locations, "\n"), paths.WorkspaceConfigTOMLName) || !strings.Contains(strings.Join(locations, "\n"), paths.WorkspaceLocalConfigTOMLName) {
		t.Fatalf("empty-list removal source writers = %q", locations)
	}
}

func TestReadableTOMLValidationUsesActualArrayItemPosition(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, paths.WorkspaceConfigTOMLName), `mounts = [
  { host = '~/one', at = '/ctx/one' },
  { host = 12, at = '/ctx/two' },
]
`)
	_, err := LoadReadableWorkspaceConfig(dir, true, nil)
	if err == nil || !strings.Contains(err.Error(), ":3:") || !strings.Contains(err.Error(), "config.mounts[1].host") {
		t.Fatalf("second array item type error = %v, want line 3 and indexed config path", err)
	}
}

func TestReadableTOMLMCPServerTableAndOffRemoval(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, paths.WorkspaceConfigTOMLName), `[mcp_servers.local]
command = ['node']
args = ['server.js']
env = { API_KEY = 'value' }
requires_env = ['API_KEY']
provides = 'web_search'
`)
	if _, err := LoadReadableWorkspaceConfig(dir, true, nil); err != nil {
		t.Fatalf("MCP server table refused: %v", err)
	}
	write(t, filepath.Join(dir, paths.WorkspaceConfigTOMLName), `mcp_servers.local = 'off'
`)
	if _, err := LoadReadableWorkspaceConfig(dir, true, nil); err != nil {
		t.Fatalf("MCP server removal value refused: %v", err)
	}
	write(t, filepath.Join(dir, paths.WorkspaceConfigTOMLName), `mcp_servers.local = 'disabled'
`)
	if _, err := LoadReadableWorkspaceConfig(dir, true, nil); err == nil || !strings.Contains(err.Error(), "expected a table") {
		t.Fatalf("non-removal MCP string error = %v", err)
	}
}

func TestReadableListSchemaDeclaresElementShape(t *testing.T) {
	for _, rule := range ReadableSchemaKeys() {
		if rule.Type != SchemaTypeList {
			continue
		}
		itemPath := strings.Split(rule.Path, ".")
		itemPath = append(itemPath, "*")
		if _, ok := readableSchemaRule(itemPath); !ok {
			t.Errorf("readable list %q has no element schema", rule.Path)
		}
	}
}

func TestLoadReadableConfigComposesNewUserAndWorkspaceLayers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	write(t, filepath.Join(home, ".config", "yolo-jail", paths.UserConfigTOMLName), "packs = ['claude']\n")
	workspace := t.TempDir()
	write(t, filepath.Join(workspace, paths.WorkspaceConfigTOMLName), "[install]\nnix = ['just']\n")
	got, err := LoadReadableConfig(workspace, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if packs, _ := got.Get("packs"); !reflect.DeepEqual(packs, []any{"claude"}) {
		t.Fatalf("composed packs = %#v", packs)
	}
	if _, exists := got.Get("install"); !exists {
		t.Fatalf("composed readable workspace layer missing: %#v", got)
	}
}

func TestLegacyJSONCConfigStillUsesLegacyVocabulary(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, paths.WorkspaceConfigLegacyJSONCName), `{"packages": ["just"]}`)
	got, err := LoadWorkspaceConfig(dir, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, present := got.Get("packages"); !present {
		t.Fatal("legacy JSONC reader stopped returning the legacy package key")
	}
	if _, present := got.Get("install"); present {
		t.Fatal("legacy JSONC input was projected into readable vocabulary")
	}
}

func TestRuntimeReadersDoNotActivateTOMLBeforeHandoff(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, paths.WorkspaceConfigTOMLName), "[install]\nnix = ['just']\n")
	got, err := LoadWorkspaceConfig(dir, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := got.Get("install"); exists {
		t.Fatalf("runtime reader activated readable install config: %#v", got)
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	write(t, filepath.Join(home, ".config", "yolo-jail", paths.UserConfigTOMLName), "packs = ['claude']\n")
	user, err := UserScopeConfig(true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := user.Get("packs"); exists {
		t.Fatalf("runtime user reader activated readable packs: %#v", user)
	}
	composed, err := LoadConfig(dir, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := composed.Get("install"); exists {
		t.Fatalf("runtime composed reader activated readable config: %#v", composed)
	}
}

func TestLoadWorkspaceConfigReadableRejectsSiblingCollision(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, paths.WorkspaceConfigTOMLName), "[install]\nnix = ['just']\n")
	write(t, filepath.Join(dir, paths.WorkspaceConfigLegacyJSONCName), `{"packages":["just"]}`)
	if _, err := LoadReadableWorkspaceConfig(dir, true, nil); err == nil ||
		!strings.Contains(err.Error(), paths.WorkspaceConfigTOMLName) || !strings.Contains(err.Error(), paths.WorkspaceConfigLegacyJSONCName) {
		t.Fatalf("collision error = %v, want both sibling names", err)
	}
}

func TestLoadReadableWorkspaceConfigFormatVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, paths.WorkspaceConfigTOMLName)
	write(t, path, "format = 3\n")
	if _, err := LoadReadableWorkspaceConfig(dir, true, nil); err == nil || !strings.Contains(err.Error(), "format 3") {
		t.Fatalf("future format error = %v", err)
	}
	write(t, path, "format = 2\n[install]\nnix = ['just']\n")
	if _, err := LoadReadableWorkspaceConfig(dir, true, nil); err != nil {
		t.Fatalf("format 2 refused: %v", err)
	}
	write(t, path, "format = 1\n")
	if _, err := LoadReadableWorkspaceConfig(dir, true, nil); err == nil {
		t.Fatal("format 1 was accepted in the TOML filename")
	}
}
