package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/configdoc"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func TestResolveEditTargetUsesSchemaDefaultsAndReaderRoles(t *testing.T) {
	home, workspace := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	user := filepath.Join(home, ".config", "yolo-jail", paths.UserConfigTOMLName)
	userFragment := filepath.Join(filepath.Dir(user), "config.d", "zz.toml")
	project := filepath.Join(workspace, paths.WorkspaceConfigLegacyJSONCName)
	local := filepath.Join(workspace, paths.WorkspaceLocalConfigTOMLName)
	writeTargetFixture(t, user, "[allow.git]\nalways = ['fetch']\n[machine.resources]\ncpus = 1\n")
	writeTargetFixture(t, userFragment, "[allow.git]\non_request = ['push']\n")
	writeTargetFixture(t, project, `{"machine":{"resources":{"cpus":2}}}`)
	writeTargetFixture(t, local, "[machine.resources]\ncpus = 3\n")

	cases := []struct {
		name   string
		key    []configdoc.Step
		choice EditTargetChoice
		want   string
		role   ExplicitEditScope
		format configdoc.Format
	}{
		{"User existing section origin", []configdoc.Step{{Key: "allow"}, {Key: "git"}, {Key: "always"}}, EditTargetChoice{Kind: EditTargetAutomatic}, user, ExplicitEditUser, configdoc.TOML},
		{"User config.d precedence", []configdoc.Step{{Key: "allow"}, {Key: "git"}, {Key: "on_request"}}, EditTargetChoice{Kind: EditTargetAutomatic}, userFragment, ExplicitEditUser, configdoc.TOML},
		{"Both defaults Project over User origin", []configdoc.Step{{Key: "machine"}, {Key: "resources"}, {Key: "cpus"}}, EditTargetChoice{Kind: EditTargetAutomatic}, project, ExplicitEditProject, configdoc.JSONC},
		{"explicit Local", []configdoc.Step{{Key: "machine"}, {Key: "resources"}, {Key: "cpus"}}, EditTargetChoice{Kind: EditTargetLocal}, local, ExplicitEditLocal, configdoc.TOML},
		{"explicit User", []configdoc.Step{{Key: "allow"}, {Key: "git"}, {Key: "always"}}, EditTargetChoice{Kind: EditTargetUser}, user, ExplicitEditUser, configdoc.TOML},
		{"named consumed User file", []configdoc.Step{{Key: "allow"}, {Key: "git"}, {Key: "on_request"}}, EditTargetChoice{Kind: EditTargetFile, File: userFragment}, userFragment, ExplicitEditUser, configdoc.TOML},
		{"explicit Project", []configdoc.Step{{Key: "machine"}, {Key: "resources"}, {Key: "cpus"}}, EditTargetChoice{Kind: EditTargetProject}, project, ExplicitEditProject, configdoc.JSONC},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveEditTarget(workspace, targetIntent(tc.key), tc.choice)
			if err != nil {
				t.Fatal(err)
			}
			physical, err := filepath.EvalSymlinks(tc.want)
			if err != nil {
				t.Fatal(err)
			}
			if got.LogicalPath != tc.want || got.PhysicalPath != physical || got.Role != tc.role || got.Format != tc.format {
				t.Fatalf("resolved target = (%q,%q,%v,%v), want (%q,%q,%v,%v)", got.LogicalPath, got.PhysicalPath, got.Role, got.Format, tc.want, physical, tc.role, tc.format)
			}
			if got.Target.scope != tc.role {
				t.Fatalf("bound target role = %v, want %v", got.Target.scope, tc.role)
			}
			pre := got.ExpectedBytes()
			pre[0] ^= 0xff
			actual, err := os.ReadFile(got.PhysicalPath)
			if err != nil || string(actual) == string(pre) {
				t.Fatalf("expected preimage is not detached: err=%v", err)
			}
		})
	}
}

func TestResolveEditTargetUsesLiteralDottedKeyAndRejectsUnconsumedFile(t *testing.T) {
	home, workspace := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	user := filepath.Join(home, ".config", "yolo-jail", paths.UserConfigTOMLName)
	project := filepath.Join(workspace, paths.WorkspaceConfigTOMLName)
	unconsumed := filepath.Join(workspace, "not-read.toml")
	unconsumedAlias := filepath.Join(workspace, "not-read-link.toml")
	writeTargetFixture(t, user, "[allow.git]\nalways = ['fetch']\n")
	writeTargetFixture(t, project, "[machine.resources]\ncpus = 2\n[providers.\"global.openai.test\"]\nregion = 'us-east-1'\n")
	writeTargetFixture(t, unconsumed, "[machine.resources]\ncpus = 4\n")
	if err := os.Symlink(project, unconsumedAlias); err != nil {
		t.Fatal(err)
	}
	key := []configdoc.Step{{Key: "providers"}, {Key: "global.openai.test"}, {Key: "region"}}
	got, err := ResolveEditTarget(workspace, targetIntent(key), EditTargetChoice{Kind: EditTargetAutomatic})
	if err != nil {
		t.Fatal(err)
	}
	if got.Role != ExplicitEditProject || got.LogicalPath != project {
		t.Fatalf("literal dotted key target = %#v", got)
	}
	if _, err := ResolveEditTarget(workspace, targetIntent([]configdoc.Step{{Key: "machine"}, {Key: "resources"}, {Key: "cpus"}}), EditTargetChoice{Kind: EditTargetFile, File: unconsumed}); err == nil {
		t.Fatal("unconsumed named config file was accepted")
	}
	if _, err := ResolveEditTarget(workspace, targetIntent([]configdoc.Step{{Key: "machine"}, {Key: "resources"}, {Key: "cpus"}}), EditTargetChoice{Kind: EditTargetFile, File: unconsumedAlias}); err == nil {
		t.Fatal("unconsumed symlink to a consumed file was accepted")
	}
}

func TestResolveEditTargetKeepsUserFragmentSymlinkPathAndPhysicalFile(t *testing.T) {
	home, workspace := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	userDir := filepath.Join(home, ".config", "yolo-jail")
	user := filepath.Join(userDir, paths.UserConfigTOMLName)
	fragmentDir := filepath.Join(userDir, "config.d")
	alias := filepath.Join(fragmentDir, "a-link.toml")
	real := filepath.Join(fragmentDir, "z-real.toml")
	writeTargetFixture(t, user, "[allow.git]\nalways = ['fetch']\n")
	writeTargetFixture(t, real, "[allow.git]\non_request = ['push']\n")
	if err := os.Symlink(real, alias); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveEditTarget(workspace, targetIntent([]configdoc.Step{{Key: "allow"}, {Key: "git"}, {Key: "on_request"}}), EditTargetChoice{Kind: EditTargetAutomatic})
	if err != nil {
		t.Fatal(err)
	}
	physical, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	if got.LogicalPath != alias || got.PhysicalPath != physical {
		t.Fatalf("symlink target = logical %s physical %s, want %s / %s", got.LogicalPath, got.PhysicalPath, alias, physical)
	}
}

func TestResolveEditTargetRefusesAssembledSnapshotAsWritableOrigin(t *testing.T) {
	home, workspace := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	t.Setenv("YOLO_WORKSPACE", workspace)
	user := filepath.Join(home, ".config", "yolo-jail", paths.UserConfigTOMLName)
	project := filepath.Join(workspace, paths.WorkspaceConfigTOMLName)
	writeTargetFixture(t, user, "[allow.git]\nalways = ['fetch']\n")
	writeTargetFixture(t, project, "[machine.resources]\ncpus = 2\n")
	assembled, sources, err := LoadReadableConfigWithSources(workspace, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(WorkspaceAssembledConfigPath(workspace)), 0700); err != nil {
		t.Fatal(err)
	}
	if err := WriteAssembledConfig(workspace, assembled); err != nil {
		t.Fatal(err)
	}
	if err := WriteAssembledOrigins(workspace, assembled, sources); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YOLO_VERSION", "test-jail")
	for _, tc := range []struct {
		intent EditMutationIntent
		choice EditTargetChoice
	}{
		{targetIntent([]configdoc.Step{{Key: "machine"}, {Key: "resources"}, {Key: "cpus"}}), EditTargetChoice{Kind: EditTargetAutomatic}},
		{targetIntent([]configdoc.Step{{Key: "allow"}, {Key: "git"}, {Key: "always"}}), EditTargetChoice{Kind: EditTargetAutomatic}},
		{targetIntent([]configdoc.Step{{Key: "machine"}, {Key: "resources"}, {Key: "cpus"}}), EditTargetChoice{Kind: EditTargetFile, File: project}},
	} {
		if _, err := ResolveEditTarget(workspace, tc.intent, tc.choice); err == nil || !strings.Contains(err.Error(), "not consumed") {
			t.Fatalf("assembled snapshot supplied writable origin: %v", err)
		}
	}
	for path, before := range map[string]string{user: "[allow.git]\nalways = ['fetch']\n", project: "[machine.resources]\ncpus = 2\n"} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != before {
			t.Fatalf("snapshot refusal changed %s: %q err=%v", path, got, err)
		}
	}
}

func TestResolveEditTargetRejectsConflictingAliasedReaderRoles(t *testing.T) {
	home, workspace := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	shared := filepath.Join(workspace, "shared.toml")
	writeTargetFixture(t, shared, "[machine.resources]\ncpus = 2\n")
	writeTargetFixture(t, filepath.Join(workspace, paths.WorkspaceConfigTOMLName), "include_if_found = ['shared.toml']\n")
	writeTargetFixture(t, filepath.Join(workspace, paths.WorkspaceLocalConfigTOMLName), "include_if_found = ['shared.toml']\n")
	_, err := ResolveEditTarget(workspace, targetIntent([]configdoc.Step{{Key: "machine"}, {Key: "resources"}, {Key: "cpus"}}), EditTargetChoice{Kind: EditTargetFile, File: shared})
	if err == nil || !strings.Contains(err.Error(), "conflicting reader roles") {
		t.Fatalf("aliased role conflict = %v", err)
	}
}

func TestResolveEditTargetRejectsUserOnlyBoundaryAndMissingDefault(t *testing.T) {
	home, workspace := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	if _, err := ResolveEditTarget(workspace, targetIntent([]configdoc.Step{{Key: "allow"}, {Key: "git"}, {Key: "always"}}), EditTargetChoice{Kind: EditTargetProject}); err == nil {
		t.Fatal("User-only key accepted a Project override")
	}
	if _, err := ResolveEditTarget(workspace, targetIntent([]configdoc.Step{{Key: "machine"}, {Key: "resources"}, {Key: "cpus"}}), EditTargetChoice{Kind: EditTargetAutomatic}); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("missing existing-file default = %v", err)
	}
}

func targetIntent(key []configdoc.Step) EditMutationIntent {
	return EditMutationIntent{Key: key, Kind: EditMutationSet}
}

func writeTargetFixture(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
}
