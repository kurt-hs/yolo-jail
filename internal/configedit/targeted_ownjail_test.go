package configedit

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/configdoc"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func TestResolveAndApplyTargetedOwnJailUsesAuthoredProjectAndLocal(t *testing.T) {
	for _, tc := range []struct {
		name, userName, projectName, localName string
		format                                 configdoc.Format
		user, override, project, local         string
		path                                   []configdoc.Step
		valuePath                              []string
	}{
		{
			name: "JSONC", userName: paths.UserConfigLegacyJSONCName,
			projectName: paths.WorkspaceConfigLegacyJSONCName, localName: paths.WorkspaceLocalConfigLegacyJSONCName,
			format:   configdoc.JSONC,
			user:     `{"include_if_found":["host-overrides.jsonc"]}`,
			override: `{"mcp_servers":{"host_only":{"command":["host-tool"]}}}`,
			project:  `{"machine":{"resources":{"cpus":2}}} // project comment`, local: `{"machine":{"resources":{"cpus":3}}} // local comment`,
			path: []configdoc.Step{{Key: "machine"}, {Key: "resources"}, {Key: "cpus"}}, valuePath: []string{"machine", "resources", "cpus"},
		},
		{
			name: "TOML", userName: paths.UserConfigTOMLName,
			projectName: paths.WorkspaceConfigTOMLName, localName: paths.WorkspaceLocalConfigTOMLName,
			format:   configdoc.TOML,
			user:     "include_if_found = ['host-overrides.toml']\n",
			override: "[mcp_servers.host_only]\ncommand = ['host-tool']\n",
			project:  "[machine.resources]\ncpus = 2 # project comment\n", local: "[machine.resources]\ncpus = 3 # local comment\n",
			path: []configdoc.Step{{Key: "machine"}, {Key: "resources"}, {Key: "cpus"}}, valuePath: []string{"machine", "resources", "cpus"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, workspace := matrixCanonicalTempDir(t), matrixCanonicalTempDir(t)
			t.Setenv("HOME", home)
			t.Setenv("YOLO_VERSION", "")
			t.Setenv("YOLO_WORKSPACE", "")
			t.Setenv(config.UserLayerEnv, "")
			user := filepath.Join(home, ".config", "yolo-jail", tc.userName)
			userOverride := filepath.Join(filepath.Dir(user), strings.Replace(tc.userName, filepath.Ext(tc.userName), "", 1)+"-host-overrides"+filepath.Ext(tc.userName))
			// Keep the include spelling identical to the authored filename.
			if tc.format == configdoc.JSONC {
				userOverride = filepath.Join(filepath.Dir(user), "host-overrides.jsonc")
			} else {
				userOverride = filepath.Join(filepath.Dir(user), "host-overrides.toml")
			}
			project := filepath.Join(workspace, tc.projectName)
			local := filepath.Join(workspace, tc.localName)
			neighbor := filepath.Join(workspace, "neighbor.txt")
			writeEditFixture(t, user, tc.user)
			writeEditFixture(t, userOverride, tc.override)
			writeEditFixture(t, project, tc.project)
			writeEditFixture(t, local, tc.local)
			writeEditFixture(t, neighbor, "neighbor bytes\n")
			if err := os.Chmod(project, 0640); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(local, 0640); err != nil {
				t.Fatal(err)
			}

			assembled, sources, err := config.LoadReadableConfigWithSources(workspace, true, nil)
			if err != nil {
				t.Fatalf("host-side assembly: %v", err)
			}
			if err := config.WriteAssembledConfig(workspace, assembled); err != nil {
				t.Fatal(err)
			}
			if err := config.WriteAssembledOrigins(workspace, assembled, sources); err != nil {
				t.Fatal(err)
			}
			assembledBytes, err := config.SnapshotJSON(assembled)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(userOverride); err != nil {
				t.Fatal(err)
			}
			t.Setenv("YOLO_VERSION", "test-jail")
			t.Setenv("YOLO_WORKSPACE", workspace)

			ordinaryBefore, err := config.LoadReadableConfig(workspace, true, nil)
			if err != nil {
				t.Fatalf("ordinary assembled read before edit: %v", err)
			}
			ordinaryBeforeJSON, err := config.SnapshotJSON(ordinaryBefore)
			if err != nil || ordinaryBeforeJSON != assembledBytes {
				t.Fatalf("ordinary own-jail read did not return delivered snapshot before edit: equal=%v err=%v", ordinaryBeforeJSON == assembledBytes, err)
			}
			hostOnly, found := effectiveMap(t, ordinaryBefore, "mcp_servers").Get("host_only")
			if !found || hostOnly == nil {
				t.Fatalf("delivered host-only include value is absent before edit: %#v", ordinaryBefore)
			}

			projectBefore, err := os.ReadFile(project)
			if err != nil {
				t.Fatal(err)
			}
			localBefore, err := os.ReadFile(local)
			if err != nil {
				t.Fatal(err)
			}
			neighborBefore, err := os.ReadFile(neighbor)
			if err != nil {
				t.Fatal(err)
			}
			mutation := Mutation{Action: Set, Path: tc.path, Value: int64(4)}
			target, err := ResolveTarget(workspace, mutation, config.EditTargetChoice{Kind: config.EditTargetAutomatic})
			if err != nil {
				t.Fatalf("resolve authored own-jail Project target: %v", err)
			}
			if target.LogicalPath != project || target.Role != config.ExplicitEditProject || target.Format != tc.format {
				t.Fatalf("automatic target = (%s,%v,%v), want authored Project %s in %v", target.LogicalPath, target.Role, target.Format, project, tc.format)
			}
			result, err := ApplyTargeted(workspace, target, mutation, ApplyOptions{})
			if err != nil || result.Path != project || result.Plan == nil {
				t.Fatalf("ApplyTargeted Project = (%#v, %v)", result, err)
			}
			projectAfter, err := os.ReadFile(project)
			if err != nil {
				t.Fatal(err)
			}
			assertMatrixDocumentValue(t, projectAfter, tc.format, tc.valuePath, matrixInteger(t, tc.format, 4))
			if bytes.Equal(projectAfter, projectBefore) || !bytes.Contains(projectAfter, []byte("comment")) {
				t.Fatalf("authored Project bytes did not change with comment preserved: %q", projectAfter)
			}
			projectInfo, err := os.Stat(project)
			if err != nil || projectInfo.Mode().Perm() != 0640 {
				t.Fatalf("Project mode = %v err=%v, want 0640", projectInfo, err)
			}
			if got, err := os.ReadFile(local); err != nil || !bytes.Equal(got, localBefore) {
				t.Fatalf("Local changed during Project edit: %q err=%v", got, err)
			}

			projectInput, err := config.NewConfigReadInput(project, projectAfter)
			if err != nil {
				t.Fatal(err)
			}
			afterProjectEffective, err := config.LoadConfigWithReadInput(workspace, true, nil, projectInput)
			if err != nil {
				t.Fatalf("independent authored effective read after Project edit: %v", err)
			}
			afterProjectResources := effectiveMap(t, afterProjectEffective, tc.valuePath[:len(tc.valuePath)-1]...)
			winningProjectValue, found := afterProjectResources.Get(tc.valuePath[len(tc.valuePath)-1])
			if !found || fmt.Sprint(winningProjectValue) != "3" {
				t.Fatalf("Local did not mask authored Project edit: effective=%#v present=%v, want Local value 3", winningProjectValue, found)
			}

			explicitProjectMutation := Mutation{Action: Set, Path: tc.path, Value: int64(4)}
			explicitProjectTarget, err := ResolveTarget(workspace, explicitProjectMutation, config.EditTargetChoice{Kind: config.EditTargetProject})
			if err != nil || explicitProjectTarget.LogicalPath != project || explicitProjectTarget.Role != config.ExplicitEditProject {
				t.Fatalf("explicit Project target=(%#v,%v), want %s/Project", explicitProjectTarget, err, project)
			}
			if _, err := ApplyTargeted(workspace, explicitProjectTarget, explicitProjectMutation, ApplyOptions{}); err != nil {
				t.Fatalf("ApplyTargeted explicit Project: %v", err)
			}
			namedMutation := Mutation{Action: Set, Path: tc.path, Value: int64(6)}
			namedTarget, err := ResolveTarget(workspace, namedMutation, config.EditTargetChoice{Kind: config.EditTargetFile, File: project})
			if err != nil || namedTarget.LogicalPath != project || namedTarget.Role != config.ExplicitEditProject {
				t.Fatalf("named consumed Project target=(%#v,%v), want %s/Project", namedTarget, err, project)
			}
			if _, err := ApplyTargeted(workspace, namedTarget, namedMutation, ApplyOptions{}); err != nil {
				t.Fatalf("ApplyTargeted named Project: %v", err)
			}
			projectAfterNamed, err := os.ReadFile(project)
			if err != nil {
				t.Fatal(err)
			}
			assertMatrixDocumentValue(t, projectAfterNamed, tc.format, tc.valuePath, matrixInteger(t, tc.format, 6))

			localMutation := Mutation{Action: Set, Path: tc.path, Value: int64(5)}
			localTarget, err := ResolveTarget(workspace, localMutation, config.EditTargetChoice{Kind: config.EditTargetLocal})
			if err != nil {
				t.Fatalf("resolve authored own-jail Local target: %v", err)
			}
			if localTarget.LogicalPath != local || localTarget.Role != config.ExplicitEditLocal || localTarget.Format != tc.format {
				t.Fatalf("explicit Local target = (%s,%v,%v), want %s in %v", localTarget.LogicalPath, localTarget.Role, localTarget.Format, local, tc.format)
			}
			localResult, err := ApplyTargeted(workspace, localTarget, localMutation, ApplyOptions{})
			if err != nil || localResult.Path != local || localResult.Plan == nil {
				t.Fatalf("ApplyTargeted Local = (%#v, %v)", localResult, err)
			}
			localAfter, err := os.ReadFile(local)
			if err != nil {
				t.Fatal(err)
			}
			assertMatrixDocumentValue(t, localAfter, tc.format, tc.valuePath, matrixInteger(t, tc.format, 5))
			if tc.format == configdoc.TOML && !bytes.Contains(localAfter, []byte("# local comment")) {
				t.Fatalf("Local comment was not preserved: %q", localAfter)
			}
			localInfo, err := os.Stat(local)
			if err != nil || localInfo.Mode().Perm() != 0640 {
				t.Fatalf("Local mode = %v err=%v, want 0640", localInfo, err)
			}

			// Consume the authored graph independently of LoadReadableConfig's own-jail delivery shortcut.
			input, err := config.NewConfigReadInput(local, localAfter)
			if err != nil {
				t.Fatal(err)
			}
			authoredEffective, err := config.LoadConfigWithReadInput(workspace, true, nil, input)
			if err != nil {
				t.Fatalf("independent authored effective read: %v", err)
			}
			got := effectiveMap(t, authoredEffective, tc.valuePath[:len(tc.valuePath)-1]...)
			value, present := got.Get(tc.valuePath[len(tc.valuePath)-1])
			if !present || fmt.Sprint(value) != "5" {
				t.Fatalf("authored effective Local winner = %#v present=%v, want 5", value, present)
			}

			ordinaryAfter, err := config.LoadReadableConfig(workspace, true, nil)
			if err != nil {
				t.Fatalf("ordinary assembled read after edit: %v", err)
			}
			ordinaryAfterJSON, err := config.SnapshotJSON(ordinaryAfter)
			if err != nil || ordinaryAfterJSON != assembledBytes {
				t.Fatalf("ordinary own-jail read stopped returning delivered snapshot after edit: equal=%v err=%v", ordinaryAfterJSON == assembledBytes, err)
			}
			hostOnlyAfter, found := effectiveMap(t, ordinaryAfter, "mcp_servers").Get("host_only")
			if !found || hostOnlyAfter == nil {
				t.Fatalf("delivered host-only include value disappeared after edit: %#v", ordinaryAfter)
			}
			if got, err := os.ReadFile(neighbor); err != nil || !bytes.Equal(got, neighborBefore) {
				t.Fatalf("neighbor bytes changed: %q err=%v", got, err)
			}
			assertNoTargetArtifacts(t, project)
			assertNoTargetArtifacts(t, local)
		})
	}
}

func TestApplyTargetedOwnJailRollsBackLostAuthoredConsumptionWhenLocalMasksChange(t *testing.T) {
	home, workspace := matrixCanonicalTempDir(t), matrixCanonicalTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	base := filepath.Join(workspace, paths.WorkspaceConfigTOMLName)
	baseBefore := []byte("include_if_found = ['parts/edit.toml']\n")
	writeEditFixture(t, base, string(baseBefore))
	project := filepath.Join(workspace, "parts", "edit.toml")
	projectBefore := []byte("[machine.resources]\ncpus = 2 # retain project bytes\n")
	writeEditFixture(t, project, string(projectBefore))
	local := filepath.Join(workspace, paths.WorkspaceLocalConfigTOMLName)
	writeEditFixture(t, local, "[machine.resources]\ncpus = 4\n")
	writeEditFixture(t, filepath.Join(workspace, "neighbor.txt"), "neighbor stays\n")
	projectInfo, err := os.Stat(project)
	if err != nil {
		t.Fatal(err)
	}
	assembled, sources, err := config.LoadReadableConfigWithSources(workspace, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := config.WriteAssembledConfig(workspace, assembled); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteAssembledOrigins(workspace, assembled, sources); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YOLO_VERSION", "test-jail")
	t.Setenv("YOLO_WORKSPACE", workspace)
	mutation := Mutation{Action: Set, Path: matrixPath("machine", "resources", "cpus"), Value: int64(4)}
	target, err := ResolveTarget(workspace, mutation, config.EditTargetChoice{Kind: config.EditTargetFile, File: project})
	if err != nil || target.Role != config.ExplicitEditProject || target.LogicalPath != project {
		t.Fatalf("resolve consumed own-jail Project include=(%#v,%v)", target, err)
	}
	originalInput, err := config.NewConfigReadInput(project, projectBefore)
	if err != nil {
		t.Fatal(err)
	}
	beforeEffective, err := config.LoadConfigWithReadInput(workspace, true, nil, originalInput)
	if err != nil {
		t.Fatal(err)
	}
	beforeSnapshot, err := config.SnapshotJSON(beforeEffective)
	if err != nil {
		t.Fatal(err)
	}
	ops := systemFileOps
	writeCount := 0
	canonicalSame, lostConsumption := false, false
	ops.WriteAtomic = func(path string, data []byte, mode os.FileMode) error {
		writeCount++
		if err := writeAtomic(path, data, mode); err != nil {
			return err
		}
		if writeCount == 1 {
			if err := os.WriteFile(base, nil, 0600); err != nil {
				return err
			}
			plannedInput, err := config.NewConfigReadInput(project, data)
			if err != nil {
				return err
			}
			afterEffective, consumed, err := config.LoadConfigWithReadInputResult(workspace, true, nil, plannedInput)
			if err != nil {
				return err
			}
			afterSnapshot, err := config.SnapshotJSON(afterEffective)
			if err != nil {
				return err
			}
			canonicalSame = beforeSnapshot == afterSnapshot
			lostConsumption = !consumed
		}
		return nil
	}
	originalOps := systemFileOps
	systemFileOps = ops
	t.Cleanup(func() { systemFileOps = originalOps })
	_, err = ApplyTargeted(workspace, target, mutation, ApplyOptions{})
	if !canonicalSame || !lostConsumption {
		t.Fatalf("lost include did not retain equal effective Local winner while dropping consumption: same=%v lost=%v", canonicalSame, lostConsumption)
	}
	if err == nil || !strings.Contains(err.Error(), "not consumed by the standard config graph") || !strings.Contains(err.Error(), "original bytes restored") {
		t.Fatalf("lost authored consumption was not rejected with rollback: %v", err)
	}
	got, readErr := os.ReadFile(project)
	gotInfo, statErr := os.Stat(project)
	if readErr != nil || statErr != nil || !bytes.Equal(got, projectBefore) || gotInfo.Mode().Perm() != projectInfo.Mode().Perm() || writeCount != 2 {
		t.Fatalf("rollback did not restore exact Project bytes/mode: bytes=%q info=%v writes=%d errs=%v/%v", got, gotInfo, writeCount, readErr, statErr)
	}
	if err := os.WriteFile(base, baseBefore, 0600); err != nil {
		t.Fatal(err)
	}
	assertNoTargetArtifacts(t, project)
}
