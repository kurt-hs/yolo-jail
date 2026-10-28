package configedit

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/configdoc"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func TestApplyTargetedCreationDoesNotFollowLockSidecarSymlink(t *testing.T) {
	home, workspace, ownerDir := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	path := filepath.Join(workspace, paths.WorkspaceConfigTOMLName)
	sidecar := path + ".yolo-edit.lock"
	victim := filepath.Join(ownerDir, "owned-file")
	const original = "separately owned bytes\n"
	writeEditFixture(t, victim, original)
	if err := os.Symlink(victim, sidecar); err != nil {
		t.Fatal(err)
	}
	mutation := Mutation{Action: Add, Path: creationPath("machine", "resources", "cpus"), Value: 4}
	target, err := ResolveTarget(workspace, mutation, config.EditTargetChoice{Kind: config.EditTargetAutomatic})
	if err != nil {
		t.Fatal(err)
	}
	if !target.IsProspective() || target.PhysicalPath != path {
		t.Fatalf("target = %#v; want absent selected Project TOML %s", target, path)
	}
	if _, err := ApplyTargeted(workspace, target, mutation, ApplyOptions{}); err != nil {
		t.Fatalf("create Project TOML beside untrusted lock symlink: %v", err)
	}
	got, err := os.ReadFile(victim)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatalf("preexisting lock-sidecar symlink modified separately owned victim: got %q, want %q", got, original)
	}
	info, err := os.Lstat(sidecar)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("lock sidecar changed: mode=%v err=%v", info, err)
	}
	resolved, err := os.Readlink(sidecar)
	if err != nil || resolved != victim {
		t.Fatalf("lock sidecar target changed: got %q err=%v, want %q", resolved, err, victim)
	}
}

func TestApplyTargetedCreatesSelectedProjectTOMLFromAbsence(t *testing.T) {
	home, workspace := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")

	path := filepath.Join(workspace, paths.WorkspaceConfigTOMLName)
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("fixture project target is not absent: err=%v", err)
	}
	mutation := Mutation{Action: Add, Path: []configdoc.Step{{Key: "machine"}, {Key: "resources"}, {Key: "cpus"}}, Value: 4}
	choice := config.EditTargetChoice{Kind: config.EditTargetAutomatic}

	// An ordinary read of an absent project file stays an ordinary empty read.
	before, err := config.LoadReadableConfig(workspace, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := before.Get("machine"); ok {
		t.Fatalf("absent project file unexpectedly contributed machine: %#v", before)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("ordinary read created project target: err=%v", err)
	}

	target, err := ResolveTarget(workspace, mutation, choice)
	if err != nil {
		t.Fatalf("resolve absent selected Project TOML target: %v", err)
	}
	if target.LogicalPath != path || target.PhysicalPath != path || target.Role != config.ExplicitEditProject || target.Format != configdoc.TOML || !target.IsProspective() {
		t.Fatalf("prospective target = %#v; want selected Project TOML %s", target, path)
	}
	if target.ExpectedBytes() != nil {
		t.Fatalf("absent target has an existing-file preimage: %#v", target.ExpectedBytes())
	}

	result, err := ApplyTargeted(workspace, target, mutation, ApplyOptions{})
	if err != nil {
		t.Fatalf("create selected Project TOML target: %v", err)
	}
	if result.Path != path || result.Plan == nil {
		t.Fatalf("creation result = %#v", result)
	}
	created, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read created target: %v", err)
	}
	actual, err := config.LoadReadableConfig(workspace, true, nil)
	if err != nil {
		t.Fatalf("independently load published target: %v", err)
	}
	if got := readableCPUs(t, actual); got != 4 {
		t.Fatalf("actual Project reader cpus = %d (document %q), want 4", got, created)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("created target mode = %v, err=%v; want 0600", info, err)
	}
	entries, err := os.ReadDir(workspace)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), "yolo-create-") || strings.Contains(entry.Name(), "yolo-rollback-") {
			t.Fatalf("successful creation left temporary sibling %s", entry.Name())
		}
	}
	if _, err := os.Lstat(path + ".yolo-edit.lock"); !os.IsNotExist(err) {
		t.Fatalf("prospective creation left a PID sidecar: %v", err)
	}
}

func TestApplyTargetedCreatesProjectByDefaultDespiteLocalWinner(t *testing.T) {
	home, workspace := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	user := filepath.Join(home, ".config", "yolo-jail", paths.UserConfigTOMLName)
	local := filepath.Join(workspace, paths.WorkspaceLocalConfigTOMLName)
	project := filepath.Join(workspace, paths.WorkspaceConfigTOMLName)
	writeEditFixture(t, user, "[machine.resources]\ncpus = 1\n")
	writeEditFixture(t, local, "[machine.resources]\ncpus = 3\n")
	mutation := Mutation{Action: Add, Path: creationPath("machine", "resources", "cpus"), Value: 4}
	target, err := ResolveTarget(workspace, mutation, config.EditTargetChoice{Kind: config.EditTargetAutomatic})
	if err != nil {
		t.Fatal(err)
	}
	if target.PhysicalPath != project || target.Role != config.ExplicitEditProject || !target.IsProspective() {
		t.Fatalf("Both-scope default with a Local winner selected %#v, want absent Project %s", target, project)
	}
	if _, err := ApplyTargeted(workspace, target, mutation, ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	got, err := config.LoadReadableConfig(workspace, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cpus := readableCPUs(t, got); cpus != 3 {
		t.Fatalf("new Project file replaced existing Local winner: cpus=%d, want 3", cpus)
	}
}

func TestApplyTargetedCreatesExplicitLocalTOML(t *testing.T) {
	home, workspace := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	project := filepath.Join(workspace, paths.WorkspaceConfigTOMLName)
	local := filepath.Join(workspace, paths.WorkspaceLocalConfigTOMLName)
	writeEditFixture(t, project, "[machine.resources]\ncpus = 2\n")
	mutation := Mutation{Action: Add, Path: creationPath("machine", "resources", "cpus"), Value: 5}
	target, err := ResolveTarget(workspace, mutation, config.EditTargetChoice{Kind: config.EditTargetLocal})
	if err != nil {
		t.Fatal(err)
	}
	if target.PhysicalPath != local || target.Role != config.ExplicitEditLocal || !target.IsProspective() {
		t.Fatalf("explicit Local target = %#v, want %s", target, local)
	}
	if _, err := ApplyTargeted(workspace, target, mutation, ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	got, err := config.LoadReadableConfig(workspace, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cpus := readableCPUs(t, got); cpus != 5 {
		t.Fatalf("Local TOML winner = %d, want 5", cpus)
	}
	projectAfter, _ := os.ReadFile(project)
	if string(projectAfter) != "[machine.resources]\ncpus = 2\n" {
		t.Fatalf("Project sibling changed: %q", projectAfter)
	}
}

func TestApplyTargetedProspectiveDryNoopAndInvalidHaveZeroArtifacts(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mutation Mutation
		dry      bool
		wantErr  bool
	}{
		{name: "dry-run", mutation: Mutation{Action: Add, Path: creationPath("machine", "resources", "cpus"), Value: 4}, dry: true},
		{name: "absent-remove-noop", mutation: Mutation{Action: Remove, Path: creationPath("machine", "resources", "cpus")}},
		{name: "invalid-value", mutation: Mutation{Action: Add, Path: creationPath("machine", "resources", "cpus"), Value: "four"}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, workspace := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("YOLO_VERSION", "")
			before := creationInventory(t, workspace)
			target, err := ResolveTarget(workspace, tc.mutation, config.EditTargetChoice{Kind: config.EditTargetAutomatic})
			if err != nil {
				t.Fatal(err)
			}
			_, err = ApplyTargeted(workspace, target, tc.mutation, ApplyOptions{DryRun: tc.dry})
			if (err != nil) != tc.wantErr {
				t.Fatalf("ApplyTargeted error = %v, wantErr=%v", err, tc.wantErr)
			}
			after := creationInventory(t, workspace)
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("dry/noop/invalid changed workspace entries: before=%v after=%v", before, after)
			}
			if _, err := os.Lstat(filepath.Join(workspace, paths.WorkspaceConfigTOMLName)); !os.IsNotExist(err) {
				t.Fatalf("dry/noop/invalid created target: err=%v", err)
			}
		})
	}
}

func TestApplyTargetedProspectiveRefusesStaleTargetsAndParents(t *testing.T) {
	t.Run("target appears after resolution", func(t *testing.T) {
		home, workspace := t.TempDir(), t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("YOLO_VERSION", "")
		path := filepath.Join(workspace, paths.WorkspaceConfigTOMLName)
		mutation := Mutation{Action: Add, Path: creationPath("machine", "resources", "cpus"), Value: 4}
		target, err := ResolveTarget(workspace, mutation, config.EditTargetChoice{Kind: config.EditTargetAutomatic})
		if err != nil {
			t.Fatal(err)
		}
		competitor := "[machine.resources]\ncpus = 8\n"
		writeEditFixture(t, path, competitor)
		if _, err := ApplyTargeted(workspace, target, mutation, ApplyOptions{}); err == nil {
			t.Fatal("stale target appeared after resolution was overwritten")
		}
		if got, _ := os.ReadFile(path); string(got) != competitor {
			t.Fatalf("stale target changed: %q", got)
		}
	})
	t.Run("parent retargeted", func(t *testing.T) {
		home, workspace := t.TempDir(), t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("YOLO_VERSION", "")
		moved := workspace + "-moved"
		mutation := Mutation{Action: Add, Path: creationPath("machine", "resources", "cpus"), Value: 4}
		target, err := ResolveTarget(workspace, mutation, config.EditTargetChoice{Kind: config.EditTargetAutomatic})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(workspace, moved); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(workspace, 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := ApplyTargeted(workspace, target, mutation, ApplyOptions{}); err == nil {
			t.Fatal("retargeted parent accepted the old absence receipt")
		}
		if _, err := os.Lstat(filepath.Join(workspace, paths.WorkspaceConfigTOMLName)); !os.IsNotExist(err) {
			t.Fatalf("new parent received the stale candidate: %v", err)
		}
		if entries, err := os.ReadDir(moved); err != nil || len(entries) != 0 {
			t.Fatalf("original parent changed: entries=%v err=%v", entries, err)
		}
	})
	t.Run("legacy sibling is selected, not shadowed by creation", func(t *testing.T) {
		home, workspace := t.TempDir(), t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("YOLO_VERSION", "")
		legacy := filepath.Join(workspace, paths.WorkspaceConfigLegacyJSONCName)
		writeEditFixture(t, legacy, `{"machine":{"resources":{"cpus":2}}}`)
		mutation := Mutation{Action: Set, Path: creationPath("machine", "resources", "cpus"), Value: 4}
		target, err := ResolveTarget(workspace, mutation, config.EditTargetChoice{Kind: config.EditTargetAutomatic})
		if err != nil {
			t.Fatal(err)
		}
		if target.PhysicalPath != legacy || target.IsProspective() {
			t.Fatalf("legacy selected file was shadowed by missing TOML: %#v", target)
		}
		if _, err := os.Lstat(filepath.Join(workspace, paths.WorkspaceConfigTOMLName)); !os.IsNotExist(err) {
			t.Fatalf("TOML target appeared beside legacy file: %v", err)
		}
	})
}

func TestApplyTargetedProspectiveNoReplaceAndRollbackProtectExternalEntries(t *testing.T) {
	for _, tc := range []string{"locked-target-race", "link-race", "postwrite-replacement", "postwrite-replacement-link"} {
		t.Run(tc, func(t *testing.T) {
			home, workspace := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("YOLO_VERSION", "")
			path := filepath.Join(workspace, paths.WorkspaceConfigTOMLName)
			mutation := Mutation{Action: Add, Path: creationPath("machine", "resources", "cpus"), Value: 4}
			target, err := ResolveTarget(workspace, mutation, config.EditTargetChoice{Kind: config.EditTargetAutomatic})
			if err != nil {
				t.Fatal(err)
			}
			competitor := "[machine.resources]\ncpus = 9\n"
			victim := filepath.Join(t.TempDir(), "victim")
			options := ApplyOptions{}
			switch tc {
			case "locked-target-race":
				options.onProspectiveLocked = func() { writeEditFixture(t, path, competitor) }
			case "link-race":
				options.onProspectiveBeforePublish = func() { writeEditFixture(t, path, competitor) }
			case "postwrite-replacement":
				options.onProspectivePublished = func() {
					external := filepath.Join(workspace, "external-replacement")
					writeEditFixture(t, external, competitor)
					if err := os.Rename(external, path); err != nil {
						t.Fatal(err)
					}
				}
			case "postwrite-replacement-link":
				writeEditFixture(t, victim, competitor)
				options.onProspectivePublished = func() {
					external := filepath.Join(workspace, "external-replacement")
					if err := os.Symlink(victim, external); err != nil {
						t.Fatal(err)
					}
					if err := os.Rename(external, path); err != nil {
						t.Fatal(err)
					}
				}
			}
			_, err = ApplyTargeted(workspace, target, mutation, options)
			if err == nil {
				t.Fatal("racing external file was not rejected")
			}
			if tc == "postwrite-replacement-link" {
				info, statErr := os.Lstat(path)
				resolved, linkErr := os.Readlink(path)
				if statErr != nil || info.Mode()&os.ModeSymlink == 0 || linkErr != nil || resolved != victim {
					t.Fatalf("external replacement symlink was not preserved: info=%v target=%q statErr=%v linkErr=%v err=%v", info, resolved, statErr, linkErr, err)
				}
				got, readErr := os.ReadFile(victim)
				if readErr != nil || string(got) != competitor {
					t.Fatalf("external symlink victim changed: got=%q err=%v (apply error %v)", got, readErr, err)
				}
			} else {
				got, readErr := os.ReadFile(path)
				if readErr != nil || string(got) != competitor {
					t.Fatalf("external replacement was not preserved: got=%q readErr=%v err=%v", got, readErr, err)
				}
			}
			if (tc == "postwrite-replacement" || tc == "postwrite-replacement-link") && !strings.Contains(err.Error(), "external replacement") {
				t.Fatalf("replacement rollback did not disclose preserved external entry: %v", err)
			}
			for _, entry := range mustCreationEntries(t, workspace) {
				if strings.Contains(entry, "yolo-create-") || strings.Contains(entry, "yolo-rollback-") || entry == "external-replacement" {
					t.Fatalf("race left unexpected entry %q; err=%v", entry, err)
				}
			}
		})
	}
}

func TestApplyTargetedProspectiveAnchorsEnvFilesAtSelectedBasefile(t *testing.T) {
	home, workspace := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	mutation := Mutation{Action: Add, Path: creationPath("env", "files"), Value: []any{"secrets.env"}}
	target, err := ResolveTarget(workspace, mutation, config.EditTargetChoice{Kind: config.EditTargetAutomatic})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyTargeted(workspace, target, mutation, ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	got, err := config.LoadReadableConfig(workspace, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	envValue, _ := got.Get("env")
	envMap, ok := envValue.(*jsonx.OrderedMap)
	if !ok {
		t.Fatalf("env = %#v", envValue)
	}
	filesValue, _ := envMap.Get("files")
	files, ok := filesValue.([]any)
	if !ok || len(files) != 1 || files[0] != filepath.Join(workspace, "secrets.env") {
		t.Fatalf("anchored env.files = %#v, want [%q]", filesValue, filepath.Join(workspace, "secrets.env"))
	}
}

func TestProspectiveCreationLockHolder(t *testing.T) {
	parentPath := os.Getenv("YOLO_TEST_CONFIGEDIT_CREATION_PARENT")
	if parentPath == "" {
		return
	}
	parent, err := os.Open(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	if err := lockParentDirectory(parent); err != nil {
		t.Fatal(err)
	}
	defer unlockParentDirectory(parent)
	ready, release := os.Getenv("YOLO_TEST_CONFIGEDIT_CREATION_READY"), os.Getenv("YOLO_TEST_CONFIGEDIT_CREATION_RELEASE")
	writeEditFixture(t, ready, "ready\n")
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(release); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for independent process release")
}

func TestApplyTargetedProspectiveWaitsForIndependentProcess(t *testing.T) {
	home, workspace := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	path := filepath.Join(workspace, paths.WorkspaceConfigTOMLName)
	mutation := Mutation{Action: Add, Path: creationPath("machine", "resources", "cpus"), Value: 4}
	target, err := ResolveTarget(workspace, mutation, config.EditTargetChoice{Kind: config.EditTargetAutomatic})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	ready, release := filepath.Join(dir, "ready"), filepath.Join(dir, "release")
	command := exec.Command(os.Args[0], "-test.run=^TestProspectiveCreationLockHolder$")
	command.Env = append(os.Environ(), "YOLO_TEST_CONFIGEDIT_CREATION_PARENT="+workspace,
		"YOLO_TEST_CONFIGEDIT_CREATION_READY="+ready, "YOLO_TEST_CONFIGEDIT_CREATION_RELEASE="+release)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = os.WriteFile(release, []byte("release\n"), 0o600)
		_ = command.Wait()
	}()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(ready); err != nil {
		t.Fatalf("independent lock holder did not become ready: %v", err)
	}
	done := make(chan error, 1)
	go func() { _, err := ApplyTargeted(workspace, target, mutation, ApplyOptions{}); done <- err }()
	select {
	case err := <-done:
		t.Fatalf("creation bypassed independent process lock: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("target published before process lock was released: %v", err)
	}
	if err := os.WriteFile(release, []byte("release\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err != nil {
		t.Fatalf("independent lock holder failed: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("creation after independent lock release: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("creation did not publish after lock release: %v", err)
	}
}

func TestApplyTargetedExistingEmptyTOMLIsNotProspectiveAbsence(t *testing.T) {
	home, workspace := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	path := filepath.Join(workspace, paths.WorkspaceConfigTOMLName)
	writeEditFixture(t, path, "")
	mutation := Mutation{Action: Add, Path: creationPath("machine", "resources", "cpus"), Value: 4}
	target, err := ResolveTarget(workspace, mutation, config.EditTargetChoice{Kind: config.EditTargetAutomatic})
	if err != nil {
		t.Fatal(err)
	}
	if target.IsProspective() || target.ExpectedBytes() == nil || len(target.ExpectedBytes()) != 0 {
		t.Fatalf("existing empty TOML was conflated with absence: prospective=%t bytes=%#v", target.IsProspective(), target.ExpectedBytes())
	}
	if _, err := ApplyTargeted(workspace, target, mutation, ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	got, err := config.LoadReadableConfig(workspace, true, nil)
	if err != nil || readableCPUs(t, got) != 4 {
		t.Fatalf("existing empty target edit failed: err=%v", err)
	}
}

func readableCPUs(t *testing.T, cfg *jsonx.OrderedMap) int64 {
	t.Helper()
	machineValue, _ := cfg.Get("machine")
	machine, ok := machineValue.(*jsonx.OrderedMap)
	if !ok {
		t.Fatalf("machine = %#v", machineValue)
	}
	resourcesValue, _ := machine.Get("resources")
	resources, ok := resourcesValue.(*jsonx.OrderedMap)
	if !ok {
		t.Fatalf("resources = %#v", resourcesValue)
	}
	cpus, _ := resources.Get("cpus")
	value, ok := jsonx.AsInt(cpus)
	if !ok {
		t.Fatalf("cpus = %#v", cpus)
	}
	return value
}

func creationPath(keys ...string) []configdoc.Step {
	path := make([]configdoc.Step, len(keys))
	for i, key := range keys {
		path[i] = configdoc.Step{Key: key}
	}
	return path
}

func creationInventory(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return entryNames(entries)
}

func mustCreationEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return entryNames(entries)
}

func entryNames(entries []os.DirEntry) []string {
	out := make([]string, len(entries))
	for i, entry := range entries {
		out[i] = entry.Name()
	}
	return out
}

func TestApplyTargetedProspectiveInvalidScopeRefusesBeforeArtifacts(t *testing.T) {
	home, workspace := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	before := creationInventory(t, workspace)
	mutation := Mutation{Action: Add, Path: creationPath("active_provider", "default"), Value: "bedrock"}
	if _, err := ResolveTarget(workspace, mutation, config.EditTargetChoice{Kind: config.EditTargetProject}); err == nil {
		t.Fatal("User-only selection was accepted in Project scope")
	}
	if after := creationInventory(t, workspace); !reflect.DeepEqual(after, before) {
		t.Fatalf("invalid scope created artifacts: before=%v after=%v", before, after)
	}
	if _, err := os.Lstat(filepath.Join(workspace, paths.WorkspaceConfigTOMLName)); !os.IsNotExist(err) {
		t.Fatalf("invalid scope created target: %v", err)
	}
}

func TestApplyTargetedProspectiveMissingParentAndDanglingLeafRefuse(t *testing.T) {
	t.Run("missing parent", func(t *testing.T) {
		home, parent := t.TempDir(), t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("YOLO_VERSION", "")
		workspace := filepath.Join(parent, "absent")
		mutation := Mutation{Action: Add, Path: creationPath("machine", "resources", "cpus"), Value: 4}
		if _, err := ResolveTarget(workspace, mutation, config.EditTargetChoice{Kind: config.EditTargetAutomatic}); err == nil {
			t.Fatal("missing parent chain acquired creation authority")
		}
		if entries, err := os.ReadDir(parent); err != nil || len(entries) != 0 {
			t.Fatalf("missing-parent refusal created entries: %v err=%v", entries, err)
		}
	})
	t.Run("dangling final symlink", func(t *testing.T) {
		home, workspace := t.TempDir(), t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("YOLO_VERSION", "")
		path := filepath.Join(workspace, paths.WorkspaceConfigTOMLName)
		if err := os.Symlink(filepath.Join(workspace, "missing-destination"), path); err != nil {
			t.Fatal(err)
		}
		mutation := Mutation{Action: Add, Path: creationPath("machine", "resources", "cpus"), Value: 4}
		if _, err := ResolveTarget(workspace, mutation, config.EditTargetChoice{Kind: config.EditTargetAutomatic}); err == nil {
			t.Fatal("dangling TOML target link was treated as absence")
		}
		if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("dangling target link changed: info=%v err=%v", info, err)
		}
	})
}
