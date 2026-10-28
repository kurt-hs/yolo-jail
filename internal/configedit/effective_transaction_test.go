package configedit

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/configdoc"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func TestApplyEffectiveRejectsInvalidResourcesAcrossTransactionModes(t *testing.T) {
	cases := []struct {
		name, file, before, noopBefore, want string
		format                               configdoc.Format
		path                                 []configdoc.Step
		editValue, noopValue                 any
	}{
		{name: "TOML negative cpus", file: config.WorkspaceTOMLConfigName, before: "[machine.resources]\ncpus = -2\n", noopBefore: "[machine.resources]\ncpus = -1\n", editValue: -1, noopValue: -1, want: "cpus", format: configdoc.TOML, path: []configdoc.Step{{Key: "machine"}, {Key: "resources"}, {Key: "cpus"}}},
		{name: "TOML zero cpus", file: config.WorkspaceTOMLConfigName, before: "[machine.resources]\ncpus = -1\n", noopBefore: "[machine.resources]\ncpus = 0\n", editValue: 0, noopValue: 0, want: "cpus", format: configdoc.TOML, path: []configdoc.Step{{Key: "machine"}, {Key: "resources"}, {Key: "cpus"}}},
		{name: "TOML invalid memory", file: config.WorkspaceTOMLConfigName, before: "[machine.resources]\nmemory = 'bad'\n", noopBefore: "[machine.resources]\nmemory = 'invalid'\n", editValue: "invalid", noopValue: "invalid", want: "memory", format: configdoc.TOML, path: []configdoc.Step{{Key: "machine"}, {Key: "resources"}, {Key: "memory"}}},
		{name: "TOML invalid pids", file: config.WorkspaceTOMLConfigName, before: "[machine.resources]\npids_limit = 0\n", noopBefore: "[machine.resources]\npids_limit = -1\n", editValue: -1, noopValue: -1, want: "pids_limit", format: configdoc.TOML, path: []configdoc.Step{{Key: "machine"}, {Key: "resources"}, {Key: "pids_limit"}}},
		{name: "TOML invalid io", file: config.WorkspaceTOMLConfigName, before: "[machine.resources]\nio = 'bad'\n", noopBefore: "[machine.resources]\nio = 'invalid'\n", editValue: "invalid", noopValue: "invalid", want: "resources.io", format: configdoc.TOML, path: []configdoc.Step{{Key: "machine"}, {Key: "resources"}, {Key: "io"}}},
		{name: "JSONC negative cpus", file: config.WorkspaceConfigName, before: `{"resources":{"cpus":-2}}`, noopBefore: `{"resources":{"cpus":-1}}`, editValue: -1, noopValue: -1, want: "cpus", format: configdoc.JSONC, path: []configdoc.Step{{Key: "resources"}, {Key: "cpus"}}},
		{name: "JSONC zero cpus", file: config.WorkspaceConfigName, before: `{"resources":{"cpus":-1}}`, noopBefore: `{"resources":{"cpus":0}}`, editValue: 0, noopValue: 0, want: "cpus", format: configdoc.JSONC, path: []configdoc.Step{{Key: "resources"}, {Key: "cpus"}}},
		{name: "JSONC invalid memory", file: config.WorkspaceConfigName, before: `{"resources":{"memory":"bad"}}`, noopBefore: `{"resources":{"memory":"invalid"}}`, editValue: "invalid", noopValue: "invalid", want: "memory", format: configdoc.JSONC, path: []configdoc.Step{{Key: "resources"}, {Key: "memory"}}},
		{name: "JSONC invalid pids", file: config.WorkspaceConfigName, before: `{"resources":{"pids_limit":0}}`, noopBefore: `{"resources":{"pids_limit":-1}}`, editValue: -1, noopValue: -1, want: "pids_limit", format: configdoc.JSONC, path: []configdoc.Step{{Key: "resources"}, {Key: "pids_limit"}}},
		{name: "JSONC invalid io", file: config.WorkspaceConfigName, before: `{"resources":{"io":"bad"}}`, noopBefore: `{"resources":{"io":"invalid"}}`, editValue: "invalid", noopValue: "invalid", want: "resources.io", format: configdoc.JSONC, path: []configdoc.Step{{Key: "resources"}, {Key: "io"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, mode := range []string{"edit", "dry-run", "noop"} {
				t.Run(mode, func(t *testing.T) {
					home, workspace := t.TempDir(), t.TempDir()
					t.Setenv("HOME", home)
					path := filepath.Join(workspace, tc.file)
					before, value := tc.before, tc.editValue
					if mode == "noop" {
						before, value = tc.noopBefore, tc.noopValue
					}
					writeEditFixture(t, path, before)
					mutation := Mutation{Action: Set, Path: tc.path, Value: value}
					options := effectiveTestOptions(t, workspace, path, tc.format, config.ExplicitEditProject, ApplyOptions{DryRun: mode == "dry-run"})
					writes := spySystemWrites(t)
					_, err := ApplyEffective(workspace, path, tc.format, mutation, options)
					if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.want)) {
						t.Fatalf("invalid planned resource semantics accepted or wrong error: %v", err)
					}
					if *writes != 0 {
						t.Fatalf("semantic refusal made %d config writes", *writes)
					}
					got, readErr := os.ReadFile(path)
					if readErr != nil {
						t.Fatal(readErr)
					}
					if !bytes.Equal(got, []byte(before)) {
						t.Fatalf("refusal changed target bytes: got %q want %q", got, before)
					}
				})
			}
		})
	}
}

func TestApplyEffectiveRefusesJailedUserTargetsBeforeEveryTransaction(t *testing.T) {
	for _, mode := range []string{"edit", "dry-run", "noop"} {
		t.Run("base TOML "+mode, func(t *testing.T) {
			home, workspace := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("YOLO_VERSION", "test-jail")
			path := filepath.Join(home, ".config", "yolo-jail", paths.UserConfigTOMLName)
			before := "[install]\nnix = ['just']\n"
			writeEditFixture(t, path, before)
			mutation := Mutation{Action: Append, Path: []configdoc.Step{{Key: "install"}, {Key: "nix"}}, Value: "cmake"}
			if mode == "noop" {
				mutation.Value = "just"
			}
			options := ApplyOptions{DryRun: mode == "dry-run"}
			options.Target = mustEditTarget(t, workspace, path, configdoc.TOML, config.ExplicitEditUser)
			writes := spySystemWrites(t)
			_, err := ApplyEffective(workspace, path, configdoc.TOML, mutation, options)
			assertJailedUserRefusal(t, err, path, *writes)
			got, readErr := os.ReadFile(path)
			if readErr != nil || string(got) != before {
				t.Fatalf("jailed refusal changed user config: bytes=%q err=%v", got, readErr)
			}
		})
		t.Run("base JSONC "+mode, func(t *testing.T) {
			home, workspace := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("YOLO_VERSION", "test-jail")
			path := filepath.Join(home, ".config", "yolo-jail", paths.UserConfigLegacyJSONCName)
			before := `{"packages":["just"]}`
			writeEditFixture(t, path, before)
			mutation := Mutation{Action: Append, Path: []configdoc.Step{{Key: "packages"}}, Value: "cmake"}
			if mode == "noop" {
				mutation.Value = "just"
			}
			options := ApplyOptions{DryRun: mode == "dry-run"}
			options.Target = mustEditTarget(t, workspace, path, configdoc.JSONC, config.ExplicitEditUser)
			writes := spySystemWrites(t)
			_, err := ApplyEffective(workspace, path, configdoc.JSONC, mutation, options)
			assertJailedUserRefusal(t, err, path, *writes)
			got, readErr := os.ReadFile(path)
			if readErr != nil || string(got) != before {
				t.Fatalf("jailed refusal changed user config: bytes=%q err=%v", got, readErr)
			}
		})
	}
}

func TestApplyEffectiveRefusesJailedUserIncludeAndSymlinkTargets(t *testing.T) {
	cases := []struct {
		name, base, targetRel, aliasRel, targetBody string
		format                                      configdoc.Format
		baseSymlink                                 bool
		mutationPath                                []configdoc.Step
	}{
		{name: "TOML include", base: "include_if_found = ['parts/extra.toml']\n", targetRel: "parts/extra.toml", aliasRel: "parts/extra.toml", targetBody: "[install]\nnix = ['just']\n", format: configdoc.TOML, mutationPath: []configdoc.Step{{Key: "install"}, {Key: "nix"}}},
		{name: "TOML config.d symlink", base: "[install]\nnix = ['base']\n", targetRel: "parts/extra.toml", aliasRel: "config.d/10-extra.toml", targetBody: "[install]\nnix = ['just']\n", format: configdoc.TOML, mutationPath: []configdoc.Step{{Key: "install"}, {Key: "nix"}}},
		{name: "JSONC include", base: `{"include_if_found":["parts/extra.jsonc"]}`, targetRel: "parts/extra.jsonc", aliasRel: "parts/extra.jsonc", targetBody: `{"packages":["just"]}`, format: configdoc.JSONC, mutationPath: []configdoc.Step{{Key: "packages"}}},
		{name: "JSONC base symlink", targetRel: "outside/base.jsonc", aliasRel: paths.UserConfigLegacyJSONCName, targetBody: `{"packages":["just"]}`, format: configdoc.JSONC, baseSymlink: true, mutationPath: []configdoc.Step{{Key: "packages"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home, workspace := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("YOLO_VERSION", "test-jail")
			userDir := filepath.Join(home, ".config", "yolo-jail")
			baseName := paths.UserConfigTOMLName
			if tc.format == configdoc.JSONC {
				baseName = paths.UserConfigLegacyJSONCName
			}
			physical := filepath.Join(userDir, tc.targetRel)
			writeEditFixture(t, physical, tc.targetBody)
			basePath := filepath.Join(userDir, baseName)
			if tc.baseSymlink {
				if err := os.MkdirAll(filepath.Dir(basePath), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(physical, basePath); err != nil {
					t.Fatal(err)
				}
			} else {
				writeEditFixture(t, basePath, tc.base)
			}
			alias := filepath.Join(userDir, tc.aliasRel)
			if alias != physical && alias != basePath {
				if err := os.MkdirAll(filepath.Dir(alias), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(physical, alias); err != nil {
					t.Fatal(err)
				}
			}
			physicalBefore, err := os.ReadFile(physical)
			if err != nil {
				t.Fatal(err)
			}
			physicalInfo, err := os.Stat(physical)
			if err != nil {
				t.Fatal(err)
			}
			baseBefore, err := os.ReadFile(basePath)
			if err != nil {
				t.Fatal(err)
			}
			linkTarget := ""
			if alias != physical {
				linkTarget, err = os.Readlink(alias)
				if err != nil {
					t.Fatal(err)
				}
			}
			options := ApplyOptions{DryRun: true, Target: mustEditTarget(t, workspace, physical, tc.format, config.ExplicitEditUser)}
			writes := spySystemWrites(t)
			mutation := Mutation{Action: Append, Path: tc.mutationPath, Value: "cmake"}
			_, err = ApplyEffective(workspace, alias, tc.format, mutation, options)
			assertJailedUserRefusal(t, err, physical, *writes)
			for _, mode := range []string{"edit", "noop"} {
				value := any([]any{"changed"})
				if mode == "noop" {
					value = []any{"just"}
				}
				mutation := Mutation{Action: Set, Path: tc.mutationPath, Value: value}
				_, err := ApplyEffective(workspace, alias, tc.format, mutation, ApplyOptions{Target: mustEditTarget(t, workspace, physical, tc.format, config.ExplicitEditUser)})
				assertJailedUserRefusal(t, err, physical, *writes)
			}
			physicalAfter, readErr := os.ReadFile(physical)
			physicalAfterInfo, statErr := os.Stat(physical)
			if readErr != nil || statErr != nil || !bytes.Equal(physicalAfter, physicalBefore) || physicalAfterInfo.Mode().Perm() != physicalInfo.Mode().Perm() {
				t.Fatalf("jailed refusal changed physical target: bytes=%q info=%v readErr=%v statErr=%v", physicalAfter, physicalAfterInfo, readErr, statErr)
			}
			baseAfter, readErr := os.ReadFile(basePath)
			if readErr != nil || !bytes.Equal(baseAfter, baseBefore) {
				t.Fatalf("jailed refusal changed base neighbor: bytes=%q err=%v", baseAfter, readErr)
			}
			if alias != physical {
				linkInfo, lstatErr := os.Lstat(alias)
				gotTarget, linkErr := os.Readlink(alias)
				if lstatErr != nil || linkInfo.Mode()&os.ModeSymlink == 0 || linkErr != nil || gotTarget != linkTarget {
					t.Fatalf("jailed refusal changed symlink: mode=%v target=%q lstat=%v readlink=%v", linkInfo, gotTarget, lstatErr, linkErr)
				}
			}
		})
	}
}

func TestApplyEffectiveRequiresMatchingTypedScopeAndWorkspace(t *testing.T) {
	t.Run("scope mismatch", func(t *testing.T) {
		home, workspace := t.TempDir(), t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("YOLO_VERSION", "")
		path := filepath.Join(home, ".config", "yolo-jail", paths.UserConfigTOMLName)
		writeEditFixture(t, path, "[install]\nnix = ['just']\n")
		options := ApplyOptions{Target: mustEditTarget(t, workspace, path, configdoc.TOML, config.ExplicitEditProject)}
		writes := spySystemWrites(t)
		_, err := ApplyEffective(workspace, path, configdoc.TOML, Mutation{Action: Append, Path: []configdoc.Step{{Key: "install"}, {Key: "nix"}}, Value: "cmake"}, options)
		if err == nil || !strings.Contains(err.Error(), "unauthorized config role user") || *writes != 0 {
			t.Fatalf("wrong-role target result err=%v writes=%d", err, *writes)
		}
	})
	t.Run("workspace mismatch", func(t *testing.T) {
		for _, mode := range []string{"edit", "dry-run", "noop"} {
			t.Run(mode, func(t *testing.T) {
				home, workspace := t.TempDir(), t.TempDir()
				t.Setenv("HOME", home)
				t.Setenv("YOLO_VERSION", "")
				path := filepath.Join(workspace, config.WorkspaceTOMLConfigName)
				before := "[machine.resources]\ncpus = 2\n"
				if mode == "noop" {
					before = "[machine.resources]\ncpus = 4\n"
				}
				writeEditFixture(t, path, before)
				wrongWorkspace := t.TempDir()
				options := ApplyOptions{DryRun: mode == "dry-run", Target: mustEditTarget(t, wrongWorkspace, path, configdoc.TOML, config.ExplicitEditProject)}
				writes := spySystemWrites(t)
				_, err := ApplyEffective(workspace, path, configdoc.TOML, Mutation{Action: Set, Path: []configdoc.Step{{Key: "machine"}, {Key: "resources"}, {Key: "cpus"}}, Value: 4}, options)
				if err == nil || !strings.Contains(err.Error(), "does not match config graph workspace") || *writes != 0 {
					t.Fatalf("wrong-workspace target result err=%v writes=%d", err, *writes)
				}
			})
		}
	})
	t.Run("missing binding", func(t *testing.T) {
		for _, mode := range []string{"edit", "dry-run", "noop"} {
			t.Run(mode, func(t *testing.T) {
				home, workspace := t.TempDir(), t.TempDir()
				t.Setenv("HOME", home)
				t.Setenv("YOLO_VERSION", "")
				path := filepath.Join(workspace, config.WorkspaceTOMLConfigName)
				before := "[machine.resources]\ncpus = 2\n"
				if mode == "noop" {
					before = "[machine.resources]\ncpus = 4\n"
				}
				writeEditFixture(t, path, before)
				writes := spySystemWrites(t)
				_, err := ApplyEffective(workspace, path, configdoc.TOML, Mutation{Action: Set, Path: []configdoc.Step{{Key: "machine"}, {Key: "resources"}, {Key: "cpus"}}, Value: 4}, ApplyOptions{DryRun: mode == "dry-run"})
				if err == nil || !strings.Contains(err.Error(), "explicit edit target is incomplete") || *writes != 0 {
					t.Fatalf("missing target binding result err=%v writes=%d", err, *writes)
				}
			})
		}
	})
}

func TestApplyEffectiveExposesPlannedValidationDiagnostics(t *testing.T) {
	home, workspace := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	userPath := filepath.Join(home, ".config", "yolo-jail", paths.UserConfigLegacyJSONCName)
	writeEditFixture(t, userPath, `{"packs":["github","aws-auth"],"allow":{"github":{"always":["read"]}}}`)
	path := filepath.Join(workspace, config.WorkspaceConfigName)
	writeEditFixture(t, path, `{"requests":{"github":{"create_pr":true}},"packages":["just"]}`)
	called := false
	options := effectiveTestOptions(t, workspace, path, configdoc.JSONC, config.ExplicitEditProject, ApplyOptions{
		DryRun: true,
		OnValidated: func(result config.ValidatedExplicitEditResult) {
			called = true
			if _, ok := result.SnapshotJSON(); !ok {
				t.Error("planned observer received an unvalidated result")
			}
			if len(result.ReadableWarnings()) == 0 {
				t.Error("planned callback discarded common readable warnings")
			}
			requests := result.ReadableRequests()
			if len(requests) != 1 || requests[0].Status != config.ReadableRequestSkipped {
				t.Errorf("planned callback discarded typed SKIPPED request: %#v", requests)
			}
			_ = result.Warnings()
		},
	})
	if _, err := ApplyEffective(workspace, path, configdoc.JSONC, Mutation{Action: Append, Path: []configdoc.Step{{Key: "packages"}}, Value: "cmake"}, options); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("planned validated diagnostics were not exposed to the caller")
	}
}

func TestApplyEffectiveRefusesJailedUserOnlyKeyAndAllowsJailedProjectLocal(t *testing.T) {
	for _, mode := range []string{"edit", "dry-run", "noop"} {
		t.Run("user-only "+mode, func(t *testing.T) {
			home, workspace := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("YOLO_VERSION", "test-jail")
			path := filepath.Join(home, ".config", "yolo-jail", paths.UserConfigTOMLName)
			before := "[allow.git]\nalways = ['fetch']\n"
			writeEditFixture(t, path, before)
			value := any([]any{"fetch", "push"})
			if mode == "noop" {
				value = []any{"fetch"}
			}
			options := ApplyOptions{DryRun: mode == "dry-run", Target: mustEditTarget(t, workspace, path, configdoc.TOML, config.ExplicitEditUser)}
			writes := spySystemWrites(t)
			_, err := ApplyEffective(workspace, path, configdoc.TOML, Mutation{Action: Set, Path: []configdoc.Step{{Key: "allow"}, {Key: "git"}, {Key: "always"}}, Value: value}, options)
			assertJailedUserRefusal(t, err, path, *writes)
			got, readErr := os.ReadFile(path)
			if readErr != nil || string(got) != before {
				t.Fatalf("jailed user-only refusal changed bytes: %q err=%v", got, readErr)
			}
		})
	}
	for _, scope := range []config.ExplicitEditScope{config.ExplicitEditProject, config.ExplicitEditLocal} {
		scopeName := "project"
		if scope == config.ExplicitEditLocal {
			scopeName = "local"
		}
		t.Run(scopeName+" remains editable in jail", func(t *testing.T) {
			home, workspace := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("YOLO_VERSION", "test-jail")
			name := config.WorkspaceTOMLConfigName
			if scope == config.ExplicitEditLocal {
				name = config.WorkspaceLocalTOMLConfigName
			}
			path := filepath.Join(workspace, name)
			before := "[machine.resources]\ncpus = 2\n"
			writeEditFixture(t, path, before)
			options := ApplyOptions{Target: mustEditTarget(t, workspace, path, configdoc.TOML, scope)}
			writes := spySystemWrites(t)
			if _, err := ApplyEffective(workspace, path, configdoc.TOML, Mutation{Action: Set, Path: []configdoc.Step{{Key: "machine"}, {Key: "resources"}, {Key: "cpus"}}, Value: 4}, options); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path)
			if err != nil || !strings.Contains(string(got), "cpus = 4") || *writes != 1 {
				t.Fatalf("jailed %s edit got %q err=%v writes=%d", scopeName, got, err, *writes)
			}
		})
	}
}

func TestApplyEffectiveValidatesPlannedRawUserAllowance(t *testing.T) {
	home, workspace := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	userPath := filepath.Join(home, ".config", "yolo-jail", paths.UserConfigLegacyJSONCName)
	before := `{"packs":["git"],"allow":{"git":{"always":["fetch"]}}}`
	writeEditFixture(t, userPath, before)
	options := effectiveTestOptions(t, workspace, userPath, configdoc.JSONC, config.ExplicitEditUser, ApplyOptions{})
	writes := spySystemWrites(t)
	_, err := ApplyEffective(workspace, userPath, configdoc.JSONC, Mutation{Action: Set, Path: []configdoc.Step{{Key: "allow"}, {Key: "git"}, {Key: "always"}}, Value: []any{"unknown-capability"}}, options)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "capability") || *writes != 0 {
		t.Fatalf("invalid planned raw user capability was not refused before writes: err=%v writes=%d", err, *writes)
	}
	got, readErr := os.ReadFile(userPath)
	if readErr != nil || string(got) != before {
		t.Fatalf("invalid planned user input changed original bytes: %q err=%v", got, readErr)
	}
}

func TestApplyEffectiveRestoresWhenIndependentPostWriteSemanticValidationFails(t *testing.T) {
	home, workspace := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	userPath := filepath.Join(home, ".config", "yolo-jail", paths.UserConfigLegacyJSONCName)
	writeEditFixture(t, userPath, `{"adapters":{"openai->anthropic":{"address":"https://example.invalid"}}}`)
	projectPath := filepath.Join(workspace, config.WorkspaceConfigName)
	projectBefore := []byte(`{}`)
	projectAfter := []byte(`{"adapters":{"openai->anthropic":{"address":"https://example.invalid"}}}`)
	writeEditFixture(t, projectPath, string(projectBefore))
	path := filepath.Join(workspace, config.WorkspaceLocalConfigName)
	before := []byte(`{"resources":{"cpus":2}}`)
	writeEditFixture(t, path, string(before))
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	options := effectiveTestOptions(t, workspace, path, configdoc.JSONC, config.ExplicitEditLocal, ApplyOptions{})
	ops := systemFileOps
	writes := 0
	canonicalSame := false
	ops.WriteAtomic = func(target string, data []byte, mode os.FileMode) error {
		writes++
		if err := writeAtomic(target, data, mode); err != nil {
			return err
		}
		if writes == 1 {
			if err := os.WriteFile(projectPath, projectAfter, 0600); err != nil {
				return err
			}
			withProject, err := config.LoadConfig(workspace, true, nil)
			if err != nil {
				return err
			}
			withProjectJSON, err := config.SnapshotJSON(withProject)
			if err != nil {
				return err
			}
			if err := os.WriteFile(projectPath, projectBefore, 0600); err != nil {
				return err
			}
			withoutProject, err := config.LoadConfig(workspace, true, nil)
			if err != nil {
				return err
			}
			withoutProjectJSON, err := config.SnapshotJSON(withoutProject)
			if err != nil {
				return err
			}
			canonicalSame = withProjectJSON == withoutProjectJSON
			return os.WriteFile(projectPath, projectAfter, 0600)
		}
		return nil
	}
	originalOps := systemFileOps
	systemFileOps = ops
	t.Cleanup(func() { systemFileOps = originalOps })
	_, err = ApplyEffective(workspace, path, configdoc.JSONC, Mutation{Action: Set, Path: []configdoc.Step{{Key: "resources"}, {Key: "cpus"}}, Value: 4}, options)
	if !canonicalSame {
		t.Fatal("postwrite invalid project-scope adapter did not preserve the effective snapshot")
	}
	if err == nil || !strings.Contains(err.Error(), "validate actual config") || !strings.Contains(err.Error(), "original bytes restored") {
		t.Fatalf("postwrite raw-scope semantic invalidation did not rollback: %v", err)
	}
	got, readErr := os.ReadFile(path)
	gotInfo, statErr := os.Stat(path)
	if readErr != nil || statErr != nil || !bytes.Equal(got, before) || gotInfo.Mode().Perm() != info.Mode().Perm() || writes != 2 {
		t.Fatalf("rollback failed exact bytes/mode: bytes=%q info=%v writes=%d readErr=%v statErr=%v", got, gotInfo, writes, readErr, statErr)
	}
}

func TestApplyEffectiveRestoresWhenPostwriteTargetIsNoLongerConsumed(t *testing.T) {
	home, workspace := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	base := filepath.Join(workspace, config.WorkspaceTOMLConfigName)
	baseBefore := []byte("include_if_found = ['parts/edit.toml']\n")
	writeEditFixture(t, base, string(baseBefore))
	path := filepath.Join(workspace, "parts", "edit.toml")
	before := []byte("[machine.resources]\ncpus = 2\n")
	writeEditFixture(t, path, string(before))
	writeEditFixture(t, filepath.Join(workspace, config.WorkspaceLocalTOMLConfigName), "[machine.resources]\ncpus = 4\n")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	options := effectiveTestOptions(t, workspace, path, configdoc.TOML, config.ExplicitEditProject, ApplyOptions{})
	ops := systemFileOps
	writes := 0
	canonicalSame := false
	ops.WriteAtomic = func(target string, data []byte, mode os.FileMode) error {
		writes++
		if err := writeAtomic(target, data, mode); err != nil {
			return err
		}
		if writes == 1 {
			if err := os.WriteFile(base, nil, 0600); err != nil {
				return err
			}
			withoutInclude, err := config.LoadConfig(workspace, true, nil)
			if err != nil {
				return err
			}
			withoutJSON, err := config.SnapshotJSON(withoutInclude)
			if err != nil {
				return err
			}
			if err := os.WriteFile(base, baseBefore, 0600); err != nil {
				return err
			}
			withInclude, err := config.LoadConfig(workspace, true, nil)
			if err != nil {
				return err
			}
			withJSON, err := config.SnapshotJSON(withInclude)
			if err != nil {
				return err
			}
			canonicalSame = withoutJSON == withJSON
			return os.WriteFile(base, nil, 0600)
		}
		return nil
	}
	originalOps := systemFileOps
	systemFileOps = ops
	t.Cleanup(func() { systemFileOps = originalOps })
	_, err = ApplyEffective(workspace, path, configdoc.TOML, Mutation{Action: Set, Path: []configdoc.Step{{Key: "machine"}, {Key: "resources"}, {Key: "cpus"}}, Value: 4}, options)
	if !canonicalSame {
		t.Fatal("removing the edited include changed the canonical graph despite local cpus override")
	}
	if err == nil || !strings.Contains(err.Error(), "not consumed by the standard config graph") || !strings.Contains(err.Error(), "original bytes restored") {
		t.Fatalf("postwrite consumption loss did not rollback: %v", err)
	}
	got, readErr := os.ReadFile(path)
	gotInfo, statErr := os.Stat(path)
	if readErr != nil || statErr != nil || !bytes.Equal(got, before) || gotInfo.Mode().Perm() != info.Mode().Perm() || writes != 2 {
		t.Fatalf("postwrite consumption rollback failed exact bytes/mode: bytes=%q info=%v writes=%d readErr=%v statErr=%v", got, gotInfo, writes, readErr, statErr)
	}
	if err := os.WriteFile(base, baseBefore, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestApplyEffectiveRejectsTransactionFormatMismatchBeforeWrite(t *testing.T) {
	home, workspace := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	path := filepath.Join(workspace, config.WorkspaceConfigName)
	writeEditFixture(t, path, "")
	options := ApplyOptions{Target: mustEditTarget(t, workspace, path, configdoc.JSONC, config.ExplicitEditProject)}
	writes := spySystemWrites(t)
	_, err := ApplyEffective(workspace, path, configdoc.TOML, Mutation{Action: Add, Path: []configdoc.Step{{Key: "packs"}}, Value: []any{}}, options)
	if err == nil || *writes != 0 {
		t.Fatalf("transaction format mismatch was not refused before writes: err=%v writes=%d", err, *writes)
	}
}

func TestApplyEffectiveRejectsTargetFormatMismatchBeforeWrite(t *testing.T) {
	home, workspace := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	path := filepath.Join(workspace, config.WorkspaceTOMLConfigName)
	writeEditFixture(t, path, "[machine.resources]\ncpus = 2\n")
	options := ApplyOptions{Target: mustEditTarget(t, workspace, path, configdoc.JSONC, config.ExplicitEditProject)}
	writes := spySystemWrites(t)
	_, err := ApplyEffective(workspace, path, configdoc.TOML, Mutation{Action: Set, Path: []configdoc.Step{{Key: "machine"}, {Key: "resources"}, {Key: "cpus"}}, Value: 4}, options)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "format") || *writes != 0 {
		t.Fatalf("target format mismatch was not refused before writes: err=%v writes=%d", err, *writes)
	}
}

func TestApplyEffectiveValidatesInactiveProviderTwinAndPlannedUserProfiles(t *testing.T) {
	t.Run("inactive provider twin", func(t *testing.T) {
		home, workspace := t.TempDir(), t.TempDir()
		t.Setenv("HOME", home)
		path := filepath.Join(home, ".config", "yolo-jail", paths.UserConfigLegacyJSONCName)
		writeEditFixture(t, path, `{"packs":["pi"]}`)
		invalid := map[string]any{"broken": map[string]any{"models": map[string]any{"child": map[string]any{"twin_of": "missing"}}}}
		options := effectiveTestOptions(t, workspace, path, configdoc.JSONC, config.ExplicitEditUser, ApplyOptions{})
		writes := spySystemWrites(t)
		_, err := ApplyEffective(workspace, path, configdoc.JSONC, Mutation{Action: Add, Path: []configdoc.Step{{Key: "providers"}}, Value: invalid}, options)
		if err == nil || !strings.Contains(err.Error(), "twin_of names a missing model row") || *writes != 0 {
			t.Fatalf("inactive provider twin was not rejected before write: err=%v writes=%d", err, *writes)
		}
	})
	t.Run("planned raw user profile shape", func(t *testing.T) {
		home, workspace := t.TempDir(), t.TempDir()
		t.Setenv("HOME", home)
		path := filepath.Join(home, ".config", "yolo-jail", paths.UserConfigLegacyJSONCName)
		before := `{"packs":["git"]}`
		writeEditFixture(t, path, before)
		options := effectiveTestOptions(t, workspace, path, configdoc.JSONC, config.ExplicitEditUser, ApplyOptions{})
		writes := spySystemWrites(t)
		_, err := ApplyEffective(workspace, path, configdoc.JSONC, Mutation{Action: Add, Path: []configdoc.Step{{Key: "profiles"}}, Value: []any{}}, options)
		if err == nil || !strings.Contains(err.Error(), "profiles: expected an object") || *writes != 0 {
			t.Fatalf("planned raw-user profile error was not rejected before write: err=%v writes=%d", err, *writes)
		}
		got, readErr := os.ReadFile(path)
		if readErr != nil || string(got) != before {
			t.Fatalf("profile refusal changed user file: %q err=%v", got, readErr)
		}
	})
}

func TestApplyEffectiveUsesPlannedLegacyBrokeredUserCeiling(t *testing.T) {
	home, workspace := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	userPath := filepath.Join(home, ".config", "yolo-jail", paths.UserConfigLegacyJSONCName)
	before := `{"brokered":{"git":{"grant_kinds":["branch"]}}}`
	writeEditFixture(t, userPath, before)
	projectPath := filepath.Join(workspace, config.WorkspaceConfigName)
	writeEditFixture(t, projectPath, `{"brokered":{"git":{"grants":[{"kind":"branch","repo":"github.com/example/repo","branch":"main"}]}}}`)
	options := effectiveTestOptions(t, workspace, userPath, configdoc.JSONC, config.ExplicitEditUser, ApplyOptions{})
	writes := spySystemWrites(t)
	_, err := ApplyEffective(workspace, userPath, configdoc.JSONC, Mutation{Action: Set, Path: []configdoc.Step{{Key: "brokered"}, {Key: "git"}, {Key: "grant_kinds"}}, Value: []any{}}, options)
	if err == nil || *writes != 0 {
		t.Fatalf("planned removal of legacy user grant ceiling accepted: err=%v writes=%d", err, *writes)
	}
	got, readErr := os.ReadFile(userPath)
	if readErr != nil || string(got) != before {
		t.Fatalf("legacy grant ceiling refusal changed original user bytes: %q err=%v", got, readErr)
	}
}

func TestApplyEffectiveRequiresExactProjectLocalAndUnambiguousPhysicalRole(t *testing.T) {
	t.Setenv("YOLO_VERSION", "")
	t.Run("project target mislabeled local", func(t *testing.T) {
		home, workspace := t.TempDir(), t.TempDir()
		t.Setenv("HOME", home)
		path := filepath.Join(workspace, config.WorkspaceTOMLConfigName)
		writeEditFixture(t, path, "[machine.resources]\ncpus = 2\n")
		options := ApplyOptions{Target: mustEditTarget(t, workspace, path, configdoc.TOML, config.ExplicitEditLocal)}
		writes := spySystemWrites(t)
		_, err := ApplyEffective(workspace, path, configdoc.TOML, Mutation{Action: Set, Path: []configdoc.Step{{Key: "machine"}, {Key: "resources"}, {Key: "cpus"}}, Value: 4}, options)
		if err == nil || !strings.Contains(err.Error(), "unauthorized config role project") || *writes != 0 {
			t.Fatalf("project/local mismatch accepted: err=%v writes=%d", err, *writes)
		}
	})
	t.Run("local target mislabeled project", func(t *testing.T) {
		home, workspace := t.TempDir(), t.TempDir()
		t.Setenv("HOME", home)
		path := filepath.Join(workspace, config.WorkspaceLocalTOMLConfigName)
		writeEditFixture(t, path, "[machine.resources]\ncpus = 2\n")
		options := ApplyOptions{Target: mustEditTarget(t, workspace, path, configdoc.TOML, config.ExplicitEditProject)}
		writes := spySystemWrites(t)
		_, err := ApplyEffective(workspace, path, configdoc.TOML, Mutation{Action: Set, Path: []configdoc.Step{{Key: "machine"}, {Key: "resources"}, {Key: "cpus"}}, Value: 4}, options)
		if err == nil || !strings.Contains(err.Error(), "unauthorized config role local") || *writes != 0 {
			t.Fatalf("local/project mismatch accepted: err=%v writes=%d", err, *writes)
		}
	})
	t.Run("one physical target read as project and local", func(t *testing.T) {
		home, workspace := t.TempDir(), t.TempDir()
		t.Setenv("HOME", home)
		physical := filepath.Join(t.TempDir(), "shared.toml")
		writeEditFixture(t, physical, "[machine.resources]\ncpus = 2\n")
		projectPath := filepath.Join(workspace, config.WorkspaceTOMLConfigName)
		writeEditFixture(t, projectPath, "include_if_found = ['parts/shared.toml']\n")
		projectInclude := filepath.Join(workspace, "parts", "shared.toml")
		localPath := filepath.Join(workspace, config.WorkspaceLocalTOMLConfigName)
		for _, alias := range []string{projectInclude, localPath} {
			if err := os.MkdirAll(filepath.Dir(alias), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(physical, alias); err != nil {
				t.Fatal(err)
			}
		}
		options := ApplyOptions{Target: mustEditTarget(t, workspace, physical, configdoc.TOML, config.ExplicitEditProject)}
		writes := spySystemWrites(t)
		_, err := ApplyEffective(workspace, physical, configdoc.TOML, Mutation{Action: Set, Path: []configdoc.Step{{Key: "machine"}, {Key: "resources"}, {Key: "cpus"}}, Value: 4}, options)
		if err == nil || !strings.Contains(err.Error(), "conflicting or unauthorized config role local") || *writes != 0 {
			t.Fatalf("cross-role project/local target was not refused: err=%v writes=%d", err, *writes)
		}
	})
	t.Run("one physical target read as user and project", func(t *testing.T) {
		home, workspace := t.TempDir(), t.TempDir()
		t.Setenv("HOME", home)
		physical := filepath.Join(t.TempDir(), "shared.toml")
		writeEditFixture(t, physical, "[machine.resources]\ncpus = 2\n")
		userPath := filepath.Join(home, ".config", "yolo-jail", paths.UserConfigTOMLName)
		projectPath := filepath.Join(workspace, config.WorkspaceTOMLConfigName)
		for _, alias := range []string{userPath, projectPath} {
			if err := os.MkdirAll(filepath.Dir(alias), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(physical, alias); err != nil {
				t.Fatal(err)
			}
		}
		options := ApplyOptions{Target: mustEditTarget(t, workspace, physical, configdoc.TOML, config.ExplicitEditProject)}
		writes := spySystemWrites(t)
		_, err := ApplyEffective(workspace, physical, configdoc.TOML, Mutation{Action: Set, Path: []configdoc.Step{{Key: "machine"}, {Key: "resources"}, {Key: "cpus"}}, Value: 4}, options)
		if err == nil || !strings.Contains(err.Error(), "conflicting or unauthorized config role") || *writes != 0 {
			t.Fatalf("cross-role physical target was not refused: err=%v writes=%d", err, *writes)
		}
	})
}

func TestApplyEffectiveRefusesJailedUserOnlyIncludeAndConfigDSymlink(t *testing.T) {
	cases := []struct {
		name, base, body, targetRel, aliasRel string
		format                                configdoc.Format
		baseName                              string
	}{
		{name: "JSONC include", base: `{"include_if_found":["allow.jsonc"]}`, body: `{"allow":{"git":{"always":["fetch"]}}}`, targetRel: "allow.jsonc", aliasRel: "allow.jsonc", format: configdoc.JSONC, baseName: paths.UserConfigLegacyJSONCName},
		{name: "JSONC explicit config.d path via TOML include", base: "include_if_found = ['config.d/10-allow.jsonc']\n", body: `{"allow":{"git":{"always":["fetch"]}}}`, targetRel: "config.d/10-allow.jsonc", aliasRel: "config.d/10-allow.jsonc", format: configdoc.JSONC, baseName: paths.UserConfigTOMLName},
		{name: "TOML config.d symlink", base: "[install]\nnix = ['base']\n", body: "[allow.git]\nalways = ['fetch']\n", targetRel: "parts/allow.toml", aliasRel: "config.d/10-allow.toml", format: configdoc.TOML, baseName: paths.UserConfigTOMLName},
	}
	for _, tc := range cases {
		for _, mode := range []string{"edit", "dry-run", "noop"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				home, workspace := t.TempDir(), t.TempDir()
				t.Setenv("HOME", home)
				t.Setenv("YOLO_VERSION", "test-jail")
				userDir := filepath.Join(home, ".config", "yolo-jail")
				basePath := filepath.Join(userDir, tc.baseName)
				writeEditFixture(t, basePath, tc.base)
				physical := filepath.Join(userDir, tc.targetRel)
				writeEditFixture(t, physical, tc.body)
				alias := filepath.Join(userDir, tc.aliasRel)
				if alias != physical {
					if err := os.MkdirAll(filepath.Dir(alias), 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(physical, alias); err != nil {
						t.Fatal(err)
					}
				}
				mutation := Mutation{Action: Set, Path: []configdoc.Step{{Key: "allow"}, {Key: "git"}, {Key: "always"}}, Value: []any{"fetch", "push"}}
				if mode == "noop" {
					mutation.Value = []any{"fetch"}
				}
				options := ApplyOptions{DryRun: mode == "dry-run", Target: mustEditTarget(t, workspace, physical, tc.format, config.ExplicitEditUser)}
				writes := spySystemWrites(t)
				_, err := ApplyEffective(workspace, alias, tc.format, mutation, options)
				assertJailedUserRefusal(t, err, physical, *writes)
				got, readErr := os.ReadFile(physical)
				if readErr != nil || string(got) != tc.body {
					t.Fatalf("user-only refusal changed target bytes: %q err=%v", got, readErr)
				}
				if alias != physical {
					info, lstatErr := os.Lstat(alias)
					link, linkErr := os.Readlink(alias)
					if lstatErr != nil || info.Mode()&os.ModeSymlink == 0 || linkErr != nil || link != physical {
						t.Fatalf("refusal changed symlink mode/target: info=%v link=%q errors=%v/%v", info, link, lstatErr, linkErr)
					}
				}
			})
		}
	}
}

func TestApplyEffectiveRefusesJailedJSONCUserOnlyBaseInAllModes(t *testing.T) {
	for _, mode := range []string{"edit", "dry-run", "noop"} {
		t.Run(mode, func(t *testing.T) {
			home, workspace := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("YOLO_VERSION", "test-jail")
			path := filepath.Join(home, ".config", "yolo-jail", paths.UserConfigLegacyJSONCName)
			before := `{"allow":{"git":{"always":["fetch"]}}}`
			writeEditFixture(t, path, before)
			value := any([]any{"fetch", "push"})
			if mode == "noop" {
				value = []any{"fetch"}
			}
			options := ApplyOptions{DryRun: mode == "dry-run", Target: mustEditTarget(t, workspace, path, configdoc.JSONC, config.ExplicitEditUser)}
			writes := spySystemWrites(t)
			_, err := ApplyEffective(workspace, path, configdoc.JSONC, Mutation{Action: Set, Path: []configdoc.Step{{Key: "allow"}, {Key: "git"}, {Key: "always"}}, Value: value}, options)
			assertJailedUserRefusal(t, err, path, *writes)
			got, readErr := os.ReadFile(path)
			if readErr != nil || string(got) != before {
				t.Fatalf("JSONC user-only refusal changed bytes: %q err=%v", got, readErr)
			}
		})
	}
}

func mustEditTarget(t *testing.T, workspace, path string, format configdoc.Format, scope config.ExplicitEditScope) config.ExplicitEditTarget {
	t.Helper()
	physicalWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	physical, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	target, err := config.NewExplicitEditTarget(physicalWorkspace, physical, scope, format)
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func spySystemWrites(t *testing.T) *int {
	t.Helper()
	original := systemFileOps
	writes := new(int)
	systemFileOps.WriteAtomic = func(path string, data []byte, mode os.FileMode) error {
		*writes++
		return original.WriteAtomic(path, data, mode)
	}
	t.Cleanup(func() { systemFileOps = original })
	return writes
}

func assertJailedUserRefusal(t *testing.T, err error, path string, writes int) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), "only allowed on the host") || !strings.Contains(err.Error(), path) {
		t.Fatalf("jailed user edit lacked host next action and actual path: %v", err)
	}
	if writes != 0 {
		t.Fatalf("jailed user edit wrote config %d times", writes)
	}
	for _, pattern := range []string{filepath.Join(filepath.Dir(path), "*.backup*"), filepath.Join(filepath.Dir(path), "*.pre-migrate*")} {
		if backups, _ := filepath.Glob(pattern); len(backups) != 0 {
			t.Fatalf("jailed user refusal left backups: %v", backups)
		}
	}
}
