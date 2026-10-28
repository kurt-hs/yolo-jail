package configedit

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/configdoc"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func TestApplyEffectiveRefusesInvalidExistingResourceSemanticsBeforeWrite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := t.TempDir()
	path := filepath.Join(workspace, config.WorkspaceTOMLConfigName)
	before := []byte("[install]\nnix = ['just']\n[machine.resources]\ncpus = -1\n")
	writeEditFixture(t, path, string(before))
	mutation := Mutation{Action: Append, Path: []configdoc.Step{{Key: "install"}, {Key: "nix"}}, Value: "cmake"}
	if _, err := ApplyEffective(workspace, path, configdoc.TOML, mutation, ApplyOptions{}); err == nil || !strings.Contains(err.Error(), "cpus") {
		t.Fatalf("effective edit accepted a semantically invalid resource graph: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, before) {
		t.Fatalf("semantic refusal changed target bytes: got %q, want %q", got, before)
	}
}

func TestApplyEffectiveEditsSymlinkedUserIncludeAgainstWholeLayerGraph(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := t.TempDir()
	userDir := filepath.Join(home, ".config", "yolo-jail")
	userConfig := filepath.Join(userDir, paths.UserConfigTOMLName)
	writeEditFixture(t, userConfig, "include_if_found = ['parts/alias.toml']\n[install]\nnix = ['user']\n")
	baseBefore, _ := os.ReadFile(userConfig)
	physical := filepath.Join(userDir, "parts", "real.toml")
	original := []byte("[install]\nnix = ['included'] # keep target comment\n")
	writeEditFixture(t, physical, string(original))
	alias := filepath.Join(userDir, "parts", "alias.toml")
	if err := os.Symlink(physical, alias); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(workspace, config.WorkspaceTOMLConfigName)
	writeEditFixture(t, project, "[install]\nnix = ['project'] # keep project comment\n")
	local := filepath.Join(workspace, config.WorkspaceLocalTOMLConfigName)
	localBefore := []byte("[install]\nnix = ['local']\n")
	writeEditFixture(t, local, string(localBefore))

	mutation := Mutation{Action: Append, Path: []configdoc.Step{{Key: "install"}, {Key: "nix"}}, Value: "planned"}
	plan, err := PlanEdit(original, configdoc.TOML, mutation)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyEffective(workspace, alias, configdoc.TOML, mutation, ApplyOptions{ExpectedBytes: original}); err != nil {
		t.Fatal(err)
	}
	gotBytes, err := os.ReadFile(physical)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotBytes, plan.Updated) {
		t.Fatalf("physical target bytes = %q, want splice %q", gotBytes, plan.Updated)
	}
	if _, err := os.Lstat(alias); err != nil {
		t.Fatalf("authored symlink was replaced: %v", err)
	}
	projectAfter, _ := os.ReadFile(project)
	if string(projectAfter) != "[install]\nnix = ['project'] # keep project comment\n" {
		t.Fatalf("unrelated project bytes changed: %q", projectAfter)
	}
	baseAfter, _ := os.ReadFile(userConfig)
	if !bytes.Equal(baseAfter, baseBefore) {
		t.Fatalf("unrelated user base bytes changed: %q", baseAfter)
	}
	localAfter, _ := os.ReadFile(local)
	if !bytes.Equal(localAfter, localBefore) {
		t.Fatalf("unrelated local bytes changed: %q", localAfter)
	}
	if !bytes.Contains(gotBytes, []byte("# keep target comment")) {
		t.Fatalf("target comment lost: %q", gotBytes)
	}

	effective, err := config.LoadConfig(workspace, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	install, ok := effective.Get("install")
	if !ok {
		t.Fatal("effective install missing")
	}
	installMap, ok := install.(*jsonx.OrderedMap)
	if !ok {
		t.Fatalf("effective install has type %T", install)
	}
	nixValue, ok := installMap.Get("nix")
	if !ok {
		t.Fatal("effective install.nix missing")
	}
	items, ok := nixValue.([]any)
	if !ok {
		t.Fatalf("effective install.nix has type %T", nixValue)
	}
	for _, want := range []string{"user", "included", "planned", "project", "local"} {
		found := false
		for _, item := range items {
			if item == want {
				found = true
			}
		}
		if !found {
			t.Errorf("effective install.nix %v does not include %q", items, want)
		}
	}
	isolated, err := configdoc.Parse(plan.Updated, configdoc.TOML)
	if err != nil {
		t.Fatal(err)
	}
	isolatedSnapshot, err := config.SnapshotJSON(isolated.Values)
	if err != nil {
		t.Fatal(err)
	}
	effectiveSnapshot, err := config.SnapshotJSON(effective)
	if err != nil {
		t.Fatal(err)
	}
	if effectiveSnapshot == isolatedSnapshot {
		t.Fatal("effective graph was reduced to the isolated target document")
	}
}

func TestApplyEffectiveRejectsUnsupportedTargetsBeforeDryRunAndNoop(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := t.TempDir()
	path := filepath.Join(workspace, "unreferenced.toml")
	before := []byte("packs = ['claude']\n")
	writeEditFixture(t, path, string(before))
	cases := []struct {
		name     string
		mutation Mutation
		options  ApplyOptions
	}{
		{name: "dry run", mutation: Mutation{Action: Append, Path: []configdoc.Step{{Key: "packs"}}, Value: "copilot"}, options: ApplyOptions{DryRun: true}},
		{name: "no-op", mutation: Mutation{Action: Append, Path: []configdoc.Step{{Key: "packs"}}, Value: "claude"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ApplyEffective(workspace, path, configdoc.TOML, tc.mutation, tc.options); err == nil || !strings.Contains(err.Error(), "not read by the standard config graph") {
				t.Fatalf("unconsumed target was not refused: %v", err)
			}
			got, _ := os.ReadFile(path)
			if !bytes.Equal(got, before) {
				t.Fatalf("unsupported-target check wrote bytes: %q", got)
			}
		})
	}
}

func TestApplyEffectiveUsesReaderScopeAndSchemaValidationBeforeWrite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := t.TempDir()
	userOnly := filepath.Join(workspace, config.WorkspaceTOMLConfigName)
	userOnlyBefore := []byte("[allow.git]\nalways = ['fetch']\n")
	writeEditFixture(t, userOnly, string(userOnlyBefore))
	_, err := ApplyEffective(workspace, userOnly, configdoc.TOML, Mutation{Action: Set, Path: []configdoc.Step{{Key: "allow"}, {Key: "git"}, {Key: "always"}}, Value: []any{"fetch", "push"}}, ApplyOptions{})
	if err == nil || !strings.Contains(err.Error(), "user-scope only") {
		t.Fatalf("workspace user-only mutation was not refused by standard reader: %v", err)
	}
	got, _ := os.ReadFile(userOnly)
	if !bytes.Equal(got, userOnlyBefore) {
		t.Fatalf("scope refusal changed bytes: %q", got)
	}

	invalidWorkspace := t.TempDir()
	invalid := filepath.Join(invalidWorkspace, config.WorkspaceLocalTOMLConfigName)
	invalidBefore := []byte("machine.ports = []\n")
	writeEditFixture(t, invalid, string(invalidBefore))
	_, err = ApplyEffective(invalidWorkspace, invalid, configdoc.TOML, Mutation{Action: Set, Path: []configdoc.Step{{Key: "machine"}, {Key: "ports"}}, Value: "not-a-list"}, ApplyOptions{DryRun: true})
	if err == nil || !strings.Contains(err.Error(), "expected a list") {
		t.Fatalf("invalid effective type was not refused: %v", err)
	}
	got, _ = os.ReadFile(invalid)
	if !bytes.Equal(got, invalidBefore) {
		t.Fatalf("type refusal changed bytes: %q", got)
	}
}

func TestApplyEffectiveRejectsStaleExpectedBytes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := t.TempDir()
	path := filepath.Join(workspace, config.WorkspaceTOMLConfigName)
	before := []byte("packs = []\n")
	writeEditFixture(t, path, string(before))
	_, err := ApplyEffective(workspace, path, configdoc.TOML, Mutation{Action: Append, Path: []configdoc.Step{{Key: "packs"}}, Value: "claude"}, ApplyOptions{ExpectedBytes: []byte("packs = ['stale']\n")})
	if err == nil || !strings.Contains(err.Error(), "changed since it was planned") {
		t.Fatalf("stale expected bytes accepted: %v", err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, before) {
		t.Fatalf("stale expectation changed bytes: %q", got)
	}
}

func TestApplyEffectiveRestoresExactTargetAfterEffectiveMismatch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := t.TempDir()
	userPath := filepath.Join(home, ".config", "yolo-jail", paths.UserConfigTOMLName)
	writeEditFixture(t, userPath, "[install]\nnix = ['before']\n")
	path := filepath.Join(workspace, config.WorkspaceTOMLConfigName)
	before := []byte("machine.ports = [] # preserve exact bytes\n")
	writeEditFixture(t, path, string(before))
	ops := systemFileOps
	writes := 0
	ops.WriteAtomic = func(target string, data []byte, mode os.FileMode) error {
		writes++
		if err := writeAtomic(target, data, mode); err != nil {
			return err
		}
		if writes == 1 {
			return os.WriteFile(userPath, []byte("[install]\nnix = ['after']\n"), 0600)
		}
		return nil
	}
	mutation := Mutation{Action: Append, Path: []configdoc.Step{{Key: "machine"}, {Key: "ports"}}, Value: map[string]any{"host": 8000, "jail": 8000}}
	_, err := applyEffectiveWithOps(workspace, path, configdoc.TOML, mutation, ApplyOptions{}, ops)
	if err == nil || !strings.Contains(err.Error(), "effective config differs from the planned graph") || !strings.Contains(err.Error(), "original bytes restored") {
		t.Fatalf("effective mismatch did not fail with rollback: %v", err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, before) {
		t.Fatalf("effective mismatch failed to restore exact target bytes: got %q want %q", got, before)
	}
}

func TestApplyEffectiveSerializesIndependentProcessesFromLockedPreimages(t *testing.T) {
	if workspace := os.Getenv("CONFIGEDIT_EFFECTIVE_HELPER_WORKSPACE"); workspace != "" {
		path := os.Getenv("CONFIGEDIT_EFFECTIVE_HELPER_PATH")
		value := os.Getenv("CONFIGEDIT_EFFECTIVE_HELPER_VALUE")
		port := 8001
		if value == "second" {
			port = 8002
		}
		mutation := Mutation{Action: Append, Path: []configdoc.Step{{Key: "machine"}, {Key: "ports"}}, Value: map[string]any{"host": port, "jail": port}}
		if _, err := ApplyEffective(workspace, path, configdoc.TOML, mutation, ApplyOptions{}); err != nil {
			t.Fatal(err)
		}
		return
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := t.TempDir()
	path := filepath.Join(workspace, config.WorkspaceTOMLConfigName)
	writeEditFixture(t, path, "machine.ports = []\n")
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, value := range []string{"first", "second"} {
		value := value
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exec.Command(os.Args[0], "-test.run=^TestApplyEffectiveSerializesIndependentProcessesFromLockedPreimages$")
			cmd.Env = append(os.Environ(), "CONFIGEDIT_EFFECTIVE_HELPER_WORKSPACE="+workspace, "CONFIGEDIT_EFFECTIVE_HELPER_PATH="+path, "CONFIGEDIT_EFFECTIVE_HELPER_VALUE="+value)
			if output, err := cmd.CombinedOutput(); err != nil {
				errs <- fmt.Errorf("helper %s: %s: %w", value, output, err)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	effective, err := config.LoadConfig(workspace, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	portsValue, _ := effectiveMap(t, effective, "machine").Get("ports")
	ports, ok := portsValue.([]any)
	if !ok || len(ports) != 2 {
		t.Fatalf("serialized effective edits lost an update: %#v", portsValue)
	}
}

func writeEditFixture(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}
