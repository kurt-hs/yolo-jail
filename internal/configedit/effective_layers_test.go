package configedit

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/configdoc"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func TestApplyEffectiveValidatesReadableRemovalAndLiteralValues(t *testing.T) {
	t.Run("empty list clears inherited ports", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		workspace := t.TempDir()
		userPath := filepath.Join(home, ".config", "yolo-jail", paths.UserConfigTOMLName)
		writeEditFixture(t, userPath, "[[machine.ports]]\nhost = 8000\njail = 8000\n")
		path := filepath.Join(workspace, config.WorkspaceLocalTOMLConfigName)
		writeEditFixture(t, path, "machine.ports = [{ host = 9001, jail = 9001 }]\n")
		mutation := Mutation{Action: Set, Path: []configdoc.Step{{Key: "machine"}, {Key: "ports"}}, Value: []any{}}
		if _, err := ApplyEffective(workspace, path, configdoc.TOML, mutation, ApplyOptions{}); err != nil {
			t.Fatal(err)
		}
		effective, err := config.LoadConfig(workspace, true, nil)
		if err != nil {
			t.Fatal(err)
		}
		machine := effectiveMap(t, effective, "machine")
		if ports, _ := machine.Get("ports"); !reflect.DeepEqual(ports, []any{}) {
			t.Fatalf("effective ports after [] = %#v, want empty list", ports)
		}
	})

	t.Run("off disables table only", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		workspace := t.TempDir()
		userPath := filepath.Join(home, ".config", "yolo-jail", paths.UserConfigTOMLName)
		writeEditFixture(t, userPath, "[mcp_servers.demo]\ncommand = ['demo']\n")
		path := filepath.Join(workspace, config.WorkspaceTOMLConfigName)
		writeEditFixture(t, path, "[mcp_servers.demo]\ncommand = ['workspace']\n")
		mutation := Mutation{Action: Set, Path: []configdoc.Step{{Key: "mcp_servers"}, {Key: "demo"}}, Value: "off"}
		if _, err := ApplyEffective(workspace, path, configdoc.TOML, mutation, ApplyOptions{}); err != nil {
			t.Fatal(err)
		}
		effective, err := config.LoadConfig(workspace, true, nil)
		if err != nil {
			t.Fatal(err)
		}
		servers := effectiveMap(t, effective, "mcp_servers")
		if value, ok := servers.Get("demo"); !ok || value != "off" {
			t.Fatalf("table-only off effective value = %#v, present=%v", value, ok)
		}
	})

	t.Run("literal env off remains a string and unset is explicit", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		workspace := t.TempDir()
		userPath := filepath.Join(home, ".config", "yolo-jail", paths.UserConfigTOMLName)
		writeEditFixture(t, userPath, "[env]\nunset = []\n[env.set]\nLITERAL = 'before'\n")
		setMutation := Mutation{Action: Set, Path: []configdoc.Step{{Key: "env"}, {Key: "set"}, {Key: "LITERAL"}}, Value: "off"}
		if _, err := ApplyEffective(workspace, userPath, configdoc.TOML, setMutation, ApplyOptions{}); err != nil {
			t.Fatal(err)
		}
		appendMutation := Mutation{Action: Append, Path: []configdoc.Step{{Key: "env"}, {Key: "unset"}}, Value: "UNWANTED"}
		if _, err := ApplyEffective(workspace, userPath, configdoc.TOML, appendMutation, ApplyOptions{}); err != nil {
			t.Fatal(err)
		}
		effective, err := config.LoadConfig(workspace, true, nil)
		if err != nil {
			t.Fatal(err)
		}
		env := effectiveMap(t, effective, "env")
		set := effectiveMap(t, env, "set")
		if value, _ := set.Get("LITERAL"); value != "off" {
			t.Fatalf("literal env value = %#v, want string off", value)
		}
		if value, _ := env.Get("unset"); !reflect.DeepEqual(value, []any{"UNWANTED"}) {
			t.Fatalf("explicit env.unset = %#v", value)
		}
	})

	t.Run("native provider selection is explicit", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		workspace := t.TempDir()
		userPath := filepath.Join(home, ".config", "yolo-jail", paths.UserConfigTOMLName)
		writeEditFixture(t, userPath, "[active_provider]\ndefault = 'codex'\n")
		mutation := Mutation{Action: Set, Path: []configdoc.Step{{Key: "active_provider"}, {Key: "default"}}, Value: "native"}
		if _, err := ApplyEffective(workspace, userPath, configdoc.TOML, mutation, ApplyOptions{}); err != nil {
			t.Fatal(err)
		}
		effective, err := config.LoadConfig(workspace, true, nil)
		if err != nil {
			t.Fatal(err)
		}
		provider := effectiveMap(t, effective, "active_provider")
		if value, _ := provider.Get("default"); value != "native" {
			t.Fatalf("active_provider.default = %#v, want native", value)
		}
	})
}

func TestApplyEffectiveEditsSymlinkedUserConfigDFragment(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := t.TempDir()
	userDir := filepath.Join(home, ".config", "yolo-jail")
	writeEditFixture(t, filepath.Join(userDir, paths.UserConfigTOMLName), "[install]\nnix = ['base']\n")
	physical := filepath.Join(userDir, "parts", "extra.toml")
	writeEditFixture(t, physical, "[machine.resources]\ncpus = 2\n")
	alias := filepath.Join(userDir, "config.d", "10-extra.toml")
	if err := os.MkdirAll(filepath.Dir(alias), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(physical, alias); err != nil {
		t.Fatal(err)
	}
	mutation := Mutation{Action: Set, Path: []configdoc.Step{{Key: "machine"}, {Key: "resources"}, {Key: "cpus"}}, Value: 4}
	if _, err := ApplyEffective(workspace, alias, configdoc.TOML, mutation, ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	effective, err := config.LoadConfig(workspace, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	resources := effectiveMap(t, effective, "machine", "resources")
	cpus, _ := resources.Get("cpus")
	cpuCount, valid := jsonx.AsInt(cpus)
	if !valid || cpuCount != 4 {
		t.Fatalf("effective cpus = %#v, want 4", cpus)
	}
	if _, err := os.Lstat(alias); err != nil {
		t.Fatalf("config.d symlink was replaced: %v", err)
	}
}

func TestApplyEffectiveEditsProjectIncludeAndLocalTargets(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := t.TempDir()
	userPath := filepath.Join(home, ".config", "yolo-jail", paths.UserConfigTOMLName)
	writeEditFixture(t, userPath, "[install]\nnix = ['user']\n")
	writeEditFixture(t, filepath.Join(workspace, config.WorkspaceTOMLConfigName), "include_if_found = ['parts/project.toml']\n[install]\nnix = ['project']\n")
	projectInclude := filepath.Join(workspace, "parts", "project.toml")
	writeEditFixture(t, projectInclude, "[machine.resources]\nmemory = '16g'\n")
	localPath := filepath.Join(workspace, config.WorkspaceLocalTOMLConfigName)
	writeEditFixture(t, localPath, "[machine.resources]\ncpus = 2\n")

	projectMutation := Mutation{Action: Set, Path: []configdoc.Step{{Key: "machine"}, {Key: "resources"}, {Key: "memory"}}, Value: "24g"}
	if _, err := ApplyEffective(workspace, projectInclude, configdoc.TOML, projectMutation, ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	localMutation := Mutation{Action: Set, Path: []configdoc.Step{{Key: "machine"}, {Key: "resources"}, {Key: "cpus"}}, Value: 4}
	if _, err := ApplyEffective(workspace, localPath, configdoc.TOML, localMutation, ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	effective, err := config.LoadConfig(workspace, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	resources := effectiveMap(t, effective, "machine", "resources")
	if memory, _ := resources.Get("memory"); memory != "24g" {
		t.Fatalf("project include memory = %#v", memory)
	}
	cpus, _ := resources.Get("cpus")
	cpuCount, validCPUCount := jsonx.AsInt(cpus)
	if !validCPUCount || cpuCount != 4 {
		t.Fatalf("local cpus = %#v", cpus)
	}
	install := effectiveMap(t, effective, "install")
	if nix, _ := install.Get("nix"); !reflect.DeepEqual(nix, []any{"user", "project"}) {
		t.Fatalf("user/project list union = %#v", nix)
	}
}

func TestApplyEffectiveRejectsUnknownKeyInDryRun(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := t.TempDir()
	path := filepath.Join(workspace, config.WorkspaceTOMLConfigName)
	before := []byte("machine.ports = []\n")
	writeEditFixture(t, path, string(before))
	_, err := ApplyEffective(workspace, path, configdoc.TOML, Mutation{Action: Add, Path: []configdoc.Step{{Key: "unrecognized"}}, Value: true}, ApplyOptions{DryRun: true})
	if err == nil || !strings.Contains(err.Error(), "unknown TOML config key") {
		t.Fatalf("unknown key accepted by effective validation: %v", err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, before) {
		t.Fatalf("unknown-key refusal changed bytes: %q", got)
	}
}

func effectiveMap(t *testing.T, value any, keys ...string) *jsonx.OrderedMap {
	t.Helper()
	current := value
	for _, key := range keys {
		m, ok := current.(*jsonx.OrderedMap)
		if !ok {
			t.Fatalf("%s parent has type %T", key, current)
		}
		var found bool
		current, found = m.Get(key)
		if !found {
			t.Fatalf("missing key %q in %#v", key, m)
		}
	}
	m, ok := current.(*jsonx.OrderedMap)
	if !ok {
		t.Fatalf("value at %v has type %T", keys, current)
	}
	return m
}
