package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/configdoc"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func explicitEditTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func TestReadExplicitEditConfigBindsRoleAndLeavesSemanticValidationUnclaimed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("YOLO_VERSION", "")
	for _, tc := range []struct {
		name   string
		file   string
		format configdoc.Format
		body   string
	}{
		{name: "canonical TOML", file: WorkspaceTOMLConfigName, format: configdoc.TOML, body: "[machine.resources]\ncpus = -1\n"},
		{name: "legacy JSONC", file: WorkspaceConfigName, format: configdoc.JSONC, body: `{"resources":{"cpus":-1}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workspace := explicitEditTempDir(t)
			physical := filepath.Join(workspace, tc.file)
			write(t, physical, tc.body)
			input, err := NewConfigReadInput(physical, []byte(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			target, err := NewExplicitEditTarget(workspace, physical, ExplicitEditProject, tc.format)
			if err != nil {
				t.Fatal(err)
			}
			result, err := ReadExplicitEditConfig(workspace, target, input)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(result.SnapshotJSON(), "-1") {
				t.Fatalf("planned negative CPU disappeared from the unvalidated read result: %s", result.SnapshotJSON())
			}
		})
	}
}

func TestReadExplicitEditConfigRejectsRoleAndFormatMismatches(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("YOLO_VERSION", "")
	workspace := explicitEditTempDir(t)
	physical := filepath.Join(workspace, WorkspaceTOMLConfigName)
	body := []byte("[machine.resources]\ncpus = 2\n")
	write(t, physical, string(body))
	input, err := NewConfigReadInput(physical, body)
	if err != nil {
		t.Fatal(err)
	}
	wrongRole, err := NewExplicitEditTarget(workspace, physical, ExplicitEditLocal, configdoc.TOML)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReadExplicitEditConfig(workspace, wrongRole, input); err == nil || !strings.Contains(err.Error(), "unauthorized config role project") {
		t.Fatalf("wrong scope accepted or wrong error: %v", err)
	}
	wrongFormat, err := NewExplicitEditTarget(workspace, physical, ExplicitEditProject, configdoc.JSONC)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReadExplicitEditConfig(workspace, wrongFormat, input); err == nil || !strings.Contains(err.Error(), "different config format") {
		t.Fatalf("wrong format accepted or wrong error: %v", err)
	}
	if _, err := ReadExplicitEditConfig(t.TempDir(), wrongRole, input); err == nil || !strings.Contains(err.Error(), "does not match config graph workspace") {
		t.Fatalf("mismatched graph workspace accepted or wrong error: %v", err)
	}
}

func TestReadExplicitEditConfigRejectsPhysicalTargetEncounteredAcrossProjectAndLocalRoles(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("YOLO_VERSION", "")
	workspace := explicitEditTempDir(t)
	physical := filepath.Join(workspace, WorkspaceTOMLConfigName)
	body := []byte("[machine.resources]\ncpus = 2\n")
	write(t, physical, string(body))
	if err := os.Symlink(physical, filepath.Join(workspace, WorkspaceLocalTOMLConfigName)); err != nil {
		t.Fatal(err)
	}
	input, err := NewConfigReadInput(physical, body)
	if err != nil {
		t.Fatal(err)
	}
	target, err := NewExplicitEditTarget(workspace, physical, ExplicitEditProject, configdoc.TOML)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReadExplicitEditConfig(workspace, target, input); err == nil || !strings.Contains(err.Error(), "conflicting or unauthorized config role local") {
		t.Fatalf("shared target accepted across project/local roles or wrong error: %v", err)
	}
}

func TestReadExplicitEditConfigRefusesUserScopeInsideJail(t *testing.T) {
	home := explicitEditTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "explicit-edit-test")
	workspace := explicitEditTempDir(t)
	physical := filepath.Join(home, ".config", "yolo-jail", paths.UserConfigTOMLName)
	if err := os.MkdirAll(filepath.Dir(physical), 0o700); err != nil {
		t.Fatal(err)
	}
	body := []byte("[machine.resources]\ncpus = 2\n")
	write(t, physical, string(body))
	input, err := NewConfigReadInput(physical, body)
	if err != nil {
		t.Fatal(err)
	}
	target, err := NewExplicitEditTarget(workspace, physical, ExplicitEditUser, configdoc.TOML)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReadExplicitEditConfig(workspace, target, input); err == nil || !strings.Contains(err.Error(), "only allowed on the host") || !strings.Contains(err.Error(), physical) {
		t.Fatalf("jailed user edit accepted or error lacks target/next action: %v", err)
	}
}
