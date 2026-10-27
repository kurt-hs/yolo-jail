package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/shquote"
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

func TestLoadReadableUserScopeConfigKeepsBaseWhenConfigDReadFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	userDir := filepath.Join(home, ".config", "yolo-jail")
	basePath := filepath.Join(userDir, paths.UserConfigTOMLName)
	write(t, basePath, "[install]\nnix = ['base']\n[env.set]\nRECOVERY_TEST = 'literal'\n")
	fragmentDir := filepath.Join(userDir, "config.d")
	write(t, fragmentDir, "not a directory")

	var warnings []string
	warn := func(msg string) { warnings = append(warnings, msg) }
	got, err := LoadReadableUserScopeConfig(false, warn)
	if err != nil {
		t.Fatal(err)
	}
	if got, exists := got.Get("install"); !exists {
		t.Fatalf("non-strict reader discarded valid base config: %#v", got)
	} else {
		install, ok := got.(*jsonx.OrderedMap)
		if !ok || install == nil {
			t.Fatalf("non-strict reader returned invalid map: %#v", got)
		}
	}
	install, _ := got.Get("install")
	actual, _ := install.(*jsonx.OrderedMap).Get("nix")
	if !reflect.DeepEqual(actual, []any{"base"}) {
		t.Errorf("install.nix = %#v, want preserved base value", actual)
	}
	env, _ := got.Get("env")
	if env == nil {
		t.Errorf("non-strict reader discarded valid env config: %#v", got)
	} else if set, _ := env.(*jsonx.OrderedMap).Get("set"); set == nil {
		t.Errorf("non-strict reader discarded valid env.set config: %#v", got)
	} else if value, _ := set.(*jsonx.OrderedMap).Get("RECOVERY_TEST"); value != "literal" {
		t.Errorf("env.set.RECOVERY_TEST = %#v, want preserved literal", value)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "Failed to parse") || !strings.Contains(warnings[0], fragmentDir) {
		t.Errorf("warnings = %q, want one existing parse-failure warning naming config.d", warnings)
	}

	got, sources, err := LoadReadableUserScopeConfigWithSources(false, warn)
	if err != nil {
		t.Fatal(err)
	}
	install, _ = got.Get("install")
	actual, _ = install.(*jsonx.OrderedMap).Get("nix")
	if !reflect.DeepEqual(actual, []any{"base"}) {
		t.Errorf("source reader install.nix = %#v, want preserved base value", actual)
	}
	env, _ = got.Get("env")
	set, _ := env.(*jsonx.OrderedMap).Get("set")
	if value, _ := set.(*jsonx.OrderedMap).Get("RECOVERY_TEST"); value != "literal" {
		t.Errorf("source reader env.set.RECOVERY_TEST = %#v, want preserved literal", value)
	}
	for _, check := range []struct {
		path string
		want string
	}{
		{path: "config.install.nix[0]", want: "~/.config/yolo-jail/" + paths.UserConfigTOMLName + ":2:"},
		{path: "config.env.set.RECOVERY_TEST", want: "~/.config/yolo-jail/" + paths.UserConfigTOMLName + ":4:"},
	} {
		locations := sources.Locations(check.path)
		if len(locations) != 1 || !strings.Contains(locations[0], check.want) {
			t.Errorf("%s source locations = %q, want base source %q", check.path, locations, check.want)
		}
	}
	if len(warnings) != 2 || !strings.Contains(warnings[1], "Failed to parse") || !strings.Contains(warnings[1], fragmentDir) {
		t.Errorf("source-reader warnings = %q, want one additional existing parse-failure warning", warnings)
	}

	strict, strictSources, err := LoadReadableUserScopeConfigWithSources(true, nil)
	if err == nil || strict != nil || strictSources != nil || !strings.Contains(err.Error(), "Failed to parse") || !strings.Contains(err.Error(), fragmentDir) {
		t.Errorf("strict source reader = (%#v, %#v, %v), want refusal naming config.d and no hydrated config", strict, strictSources, err)
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

func TestReadableWorkspaceConfigCollisionOffersSafeManualResolution(t *testing.T) {
	dir := t.TempDir()
	toml := filepath.Join(dir, paths.WorkspaceConfigTOMLName)
	legacy := filepath.Join(dir, paths.WorkspaceConfigLegacyJSONCName)
	write(t, toml, "[install]\nnix = ['new']\n")
	write(t, legacy, `{"packages":["legacy"]}`)
	before := snapshotReadableConfigDir(t, dir)

	if _, err := LoadReadableWorkspaceConfig(dir, true, nil); err == nil {
		t.Fatal("readable workspace loader accepted colliding files")
	} else {
		for _, want := range []string{paths.WorkspaceConfigTOMLName, paths.WorkspaceConfigLegacyJSONCName, "never merged", "backup path does not already exist", "then rerun the command", "mv ", paths.WorkspaceConfigLegacyJSONCName + ".backup", paths.WorkspaceConfigTOMLName + ".backup", "neither file is converted"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("collision error %q does not contain safe-resolution guidance %q", err, want)
			}
		}
		if strings.Contains(err.Error(), "yolo config migrate") {
			t.Errorf("collision error names nonexistent migration command: %v", err)
		}
	}
	if after := snapshotReadableConfigDir(t, dir); !reflect.DeepEqual(after, before) {
		t.Fatalf("collision refusal wrote config files: before=%v after=%v", before, after)
	}

	backup := toml + ".backup"
	if err := os.Rename(toml, backup); err != nil {
		t.Fatal(err)
	}
	legacyConfig, err := LoadWorkspaceConfig(dir, true, nil)
	if err != nil {
		t.Fatalf("legacy workspace reader stopped working after moving TOML aside: %v", err)
	}
	if packages, ok := legacyConfig.Get("packages"); !ok || !reflect.DeepEqual(packages, []any{"legacy"}) {
		t.Fatalf("legacy workspace config = %#v, want original packages", legacyConfig)
	}
	if got, err := os.ReadFile(backup); err != nil || string(got) != "[install]\nnix = ['new']\n" {
		t.Fatalf("preserved TOML backup = %q, %v", got, err)
	}
}

func TestReadableUserScopeCollisionOffersSafeManualResolution(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	userDir := filepath.Join(home, ".config", "yolo-jail")
	toml := filepath.Join(userDir, paths.UserConfigTOMLName)
	legacy := filepath.Join(userDir, paths.UserConfigLegacyJSONCName)
	write(t, toml, "packs = ['new']\n")
	write(t, legacy, `{"packs":["legacy"]}`)
	before := snapshotReadableConfigDir(t, userDir)

	if _, err := LoadReadableUserScopeConfig(true, nil); err == nil {
		t.Fatal("readable user loader accepted colliding files")
	} else {
		for _, want := range []string{paths.UserConfigTOMLName, paths.UserConfigLegacyJSONCName, "never merged", "backup path does not already exist", "then rerun the command", "mv ", paths.UserConfigLegacyJSONCName + ".backup", paths.UserConfigTOMLName + ".backup", "neither file is converted"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("collision error %q does not contain safe-resolution guidance %q", err, want)
			}
		}
		if strings.Contains(err.Error(), "yolo config migrate") {
			t.Errorf("collision error names nonexistent migration command: %v", err)
		}
	}
	if after := snapshotReadableConfigDir(t, userDir); !reflect.DeepEqual(after, before) {
		t.Fatalf("collision refusal wrote config files: before=%v after=%v", before, after)
	}

	backup := toml + ".backup"
	if err := os.Rename(toml, backup); err != nil {
		t.Fatal(err)
	}
	legacyConfig, err := UserScopeConfig(true, nil)
	if err != nil {
		t.Fatalf("legacy user reader stopped working after moving TOML aside: %v", err)
	}
	if packs, ok := legacyConfig.Get("packs"); !ok || !reflect.DeepEqual(packs, []any{"legacy"}) {
		t.Fatalf("legacy user config = %#v, want original packs", legacyConfig)
	}
	if got, err := os.ReadFile(backup); err != nil || string(got) != "packs = ['new']\n" {
		t.Fatalf("preserved TOML backup = %q, %v", got, err)
	}
}

func TestReadableWorkspaceConfigThreeSiblingCollisionRecovery(t *testing.T) {
	testReadableThreeSiblingCollisionRecovery(t, false)
}

func TestReadableUserScopeThreeSiblingCollisionRecovery(t *testing.T) {
	testReadableThreeSiblingCollisionRecovery(t, true)
}

func testReadableThreeSiblingCollisionRecovery(t *testing.T, userScope bool) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "reader path with spaces and 'quotes")
	dir := root
	var names []string
	var read func() (*jsonx.OrderedMap, error)
	var toml string
	var expectedKey string
	var expectedValue any
	if userScope {
		dir = filepath.Join(root, ".config", "yolo-jail")
		t.Setenv("HOME", root)
		names = []string{paths.UserConfigTOMLName, paths.UserConfigLegacyJSONCName, paths.UserConfigLegacyJSONName}
		toml = filepath.Join(dir, names[0])
		expectedKey, expectedValue = "packs", []any{"new"}
		read = func() (*jsonx.OrderedMap, error) { return LoadReadableUserScopeConfig(true, nil) }
	} else {
		names = []string{paths.WorkspaceConfigTOMLName, paths.WorkspaceConfigLegacyJSONCName, paths.WorkspaceConfigLegacyJSONName}
		toml = filepath.Join(dir, names[0])
		expectedKey, expectedValue = "install", nil
		cwd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		workspace, err := filepath.Rel(cwd, dir)
		if err != nil {
			t.Fatal(err)
		}
		read = func() (*jsonx.OrderedMap, error) { return LoadReadableWorkspaceConfig(workspace, true, nil) }
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	contents := []string{"packs = ['new']\n", `{"packs":["old-jsonc"]}`, `{"packs":["old-json"]}`}
	if !userScope {
		contents = []string{"[install]\nnix = ['new']\n", `{"packages":["old-jsonc"]}`, `{"packages":["old-json"]}`}
	}
	files := make([]string, len(names))
	for i, name := range names {
		files[i] = filepath.Join(dir, name)
		write(t, files[i], contents[i])
	}
	before := snapshotReadableConfigDir(t, dir)
	if _, err := read(); err == nil {
		t.Fatal("readable reader accepted TOML, JSONC, and JSON siblings")
	} else {
		for _, file := range files {
			if !strings.Contains(err.Error(), file) {
				t.Errorf("collision error %q omits supplied full path %q", err, file)
			}
		}
		if strings.Contains(err.Error(), "yolo config migrate") {
			t.Errorf("collision error recommends nonexistent migration: %v", err)
		}
		if after := snapshotReadableConfigDir(t, dir); !reflect.DeepEqual(after, before) {
			t.Fatalf("collision refusal wrote config files: before=%v after=%v", before, after)
		}
		commands := readableCollisionRecoveryCommands(t, err, files)
		keepTOML, ok := commands[toml]
		if !ok {
			t.Fatalf("collision error has no keep-TOML recovery for %q: %v", toml, err)
		}
		for _, keep := range files {
			command, ok := commands[keep]
			if !ok {
				t.Errorf("collision error has no recovery for keeping %q active", keep)
				continue
			}
			for _, source := range files {
				if source == keep {
					continue
				}
				backup := source + ".backup"
				if !strings.Contains(command, shquote.Quote(source)) || !strings.Contains(command, shquote.Quote(backup)) {
					t.Errorf("recovery for keeping %q active omits full source/destination paths %q and %q: %s", keep, source, backup, command)
				}
			}
		}
		otherCWD := t.TempDir()
		jsoncBackup := files[1] + ".backup"
		write(t, jsoncBackup, "do not overwrite\n")
		runReadableRecoveryCommand(t, otherCWD, keepTOML, false)
		if got, err := os.ReadFile(jsoncBackup); err != nil || string(got) != "do not overwrite\n" {
			t.Fatalf("existing backup was changed: %q, %v", got, err)
		}
		if got, err := os.ReadFile(files[1]); err != nil || string(got) != contents[1] {
			t.Fatalf("source with existing backup was changed: %q, %v", got, err)
		}
		if err := os.Remove(jsoncBackup); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(dir, "missing backup target"), jsoncBackup); err != nil {
			t.Fatal(err)
		}
		runReadableRecoveryCommand(t, otherCWD, keepTOML, false)
		if target, err := os.Readlink(jsoncBackup); err != nil || target != filepath.Join(dir, "missing backup target") {
			t.Fatalf("dangling-symlink backup was changed: %q, %v", target, err)
		}
		if got, err := os.ReadFile(files[1]); err != nil || string(got) != contents[1] {
			t.Fatalf("source with dangling-symlink backup was changed: %q, %v", got, err)
		}
		if err := os.Remove(jsoncBackup); err != nil {
			t.Fatal(err)
		}
		runReadableRecoveryCommand(t, otherCWD, keepTOML, true)
		for i, source := range files[1:] {
			backup := source + ".backup"
			if got, err := os.ReadFile(backup); err != nil || string(got) != contents[i+1] {
				t.Fatalf("recovery did not preserve %q: got %q, %v", source, got, err)
			}
			if _, err := os.Lstat(source); !os.IsNotExist(err) {
				t.Errorf("recovery left competing sibling %q active (Lstat error %v)", source, err)
			}
		}
	}

	got, err := read()
	if err != nil {
		t.Fatalf("reader still reports a sibling collision after moving both legacy files aside: %v", err)
	}
	if userScope {
		if value, ok := got.Get(expectedKey); !ok || !reflect.DeepEqual(value, expectedValue) {
			t.Fatalf("readable user config %s = %#v, want %#v", expectedKey, value, expectedValue)
		}
	} else {
		install, ok := got.Get("install")
		if !ok || install == nil {
			t.Fatalf("readable workspace config lacks install after recovery: %#v", got)
		}
	}
}

func readableCollisionRecoveryCommands(t *testing.T, err error, files []string) map[string]string {
	t.Helper()
	commands := make(map[string]string, len(files))
	for _, keep := range files {
		marker := "To keep " + keep + " active: `"
		start := strings.Index(err.Error(), marker)
		if start < 0 {
			continue
		}
		start += len(marker)
		end := strings.Index(err.Error()[start:], "`")
		if end < 0 {
			t.Fatalf("unterminated recovery command for %q in %v", keep, err)
		}
		commands[keep] = err.Error()[start : start+end]
	}
	return commands
}

func runReadableRecoveryCommand(t *testing.T, cwd, command string, wantSuccess bool) {
	t.Helper()
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = cwd
	output, err := cmd.CombinedOutput()
	if wantSuccess && err != nil {
		t.Fatalf("recovery command failed from different CWD: %v\n%s\ncommand: %s", err, output, command)
	}
	if !wantSuccess && err == nil {
		t.Fatalf("recovery command unexpectedly succeeded with occupied backup: %s", command)
	}
}

func TestReadableTOMLUnsupportedFormatOffersSafeManualRewrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "supplied-workspace")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Clean(dir) == filepath.Clean(cwd) {
		t.Fatalf("test workspace unexpectedly equals CWD: %s", dir)
	}
	path := filepath.Join(dir, paths.WorkspaceConfigTOMLName)
	document := "format = 1\npackages = ['legacy']\n"
	write(t, path, document)
	before := snapshotReadableConfigDir(t, dir)

	_, err = LoadReadableWorkspaceConfig(dir, true, nil)
	if err == nil {
		t.Fatal("format-1 TOML was accepted")
	}
	backupCommand := shquote.Join([]string{"cp", "-p", path, path + ".backup"})
	moveAsideCommand := shquote.Join([]string{"mv", path, path + ".disabled"})
	for _, want := range []string{path, "format 1", "format 2", "backup path does not already exist", backupCommand, "manually rewrite", "TOML", "schema", "before using it", "changing or removing the format marker alone is not a conversion", "original legacy JSONC/JSON filename", moveAsideCommand, "never rename legacy bytes to `.toml`"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("unsupported-format error %q does not contain safe recovery guidance %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "yolo config migrate") || strings.Contains(err.Error(), "set format = 2") {
		t.Errorf("unsupported-format error recommends a nonexistent or marker-only conversion: %v", err)
	}
	if after := snapshotReadableConfigDir(t, dir); !reflect.DeepEqual(after, before) {
		t.Fatalf("unsupported-format refusal wrote config files: before=%v after=%v", before, after)
	}

	write(t, path, "[install]\nnix = ['just']\n")
	if _, err := LoadReadableWorkspaceConfig(dir, true, nil); err != nil {
		t.Fatalf("format-2 TOML without a marker was refused: %v", err)
	}
	write(t, path, "format = 2\n[install]\nnix = ['just']\n")
	if _, err := LoadReadableWorkspaceConfig(dir, true, nil); err != nil {
		t.Fatalf("explicit format-2 TOML was refused: %v", err)
	}
	for _, format := range []int{0, 1, 3} {
		t.Run(fmt.Sprintf("format-%d-refused", format), func(t *testing.T) {
			write(t, path, fmt.Sprintf("format = %d\n", format))
			_, err := LoadReadableWorkspaceConfig(dir, true, nil)
			if err == nil {
				t.Fatalf("format %d TOML was accepted", format)
			}
			if format > 2 && (!strings.Contains(err.Error(), "Upgrade with `yolo update`") || !strings.Contains(err.Error(), "just install")) {
				t.Fatalf("future format error = %v, want existing upgrade remedy", err)
			}
		})
	}
}

func snapshotReadableConfigDir(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := make(map[string][]byte, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatalf("unexpected subdirectory %s in config snapshot", entry.Name())
		}
		contents, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		snapshot[entry.Name()] = contents
	}
	return snapshot
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
