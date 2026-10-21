package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

func TestHostImportExclusionsAreKnownConfig(t *testing.T) {
	t.Setenv("YOLO_VERSION", "")
	cfg := decode(t, `{"host_import_exclusions":{"claude/settings":["/hooks"]}}`)
	errs, _ := ValidateConfig(cfg, t.TempDir(), nil)
	for _, err := range errs {
		if strings.Contains(err, "host_import_exclusions") {
			t.Fatalf("valid host_import_exclusions was rejected: %v", errs)
		}
	}
}

func TestHostImportExclusionsNormalizePointers(t *testing.T) {
	body := `{"host_import_exclusions":{"claude/settings":["/statusLine","/hooks","/hooks","/"]}}`
	cfg := decode(t, body)
	got, problems := checkHostImportExclusions(cfg)
	if len(problems) != 0 {
		t.Fatalf("problems = %v", problems)
	}
	want := []string{"/", "/hooks", "/statusLine"}
	if !equalStrings(got["claude/settings"], want) {
		t.Errorf("normalized pointers = %v, want %v", got["claude/settings"], want)
	}
	setHostImportExclusionsUserConfig(t, body)
	loaded, err := LoadHostImportExclusions()
	if err != nil || !equalStrings(loaded["claude/settings"], want) {
		t.Errorf("loaded normalized pointers = %v, err=%v; want %v", loaded["claude/settings"], err, want)
	}
}

func TestHostImportExclusionsRejectMalformedShapesAndPointers(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{"null policy", `{"host_import_exclusions":null}`, "expected an object"},
		{"list policy", `{"host_import_exclusions":[]}`, "expected an object"},
		{"list value", `{"host_import_exclusions":{"claude/settings":"/hooks"}}`, "expected a list"},
		{"non-string pointer", `{"host_import_exclusions":{"claude/settings":[4]}}`, "expected a JSON Pointer string"},
		{"pointer missing slash", `{"host_import_exclusions":{"claude/settings":["hooks"]}}`, "must be empty or start"},
		{"malformed escape", `{"host_import_exclusions":{"claude/settings":["/~2"]}}`, "RFC 6901"},
		{"root pointer", `{"host_import_exclusions":{"claude/settings":[""]}}`, "root pointer is not allowed"},
		{"malformed address", `{"host_import_exclusions":{"claude/settings/extra":["/hooks"]}}`, "spelled agent/name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, problems := checkHostImportExclusions(decode(t, tc.body))
			if !strings.Contains(strings.Join(problems, "\n"), tc.want) {
				t.Fatalf("problems = %v, want substring %q", problems, tc.want)
			}
		})
	}
}

func TestHostImportExclusionsValidateShippedTargetsEvenWhenUnselected(t *testing.T) {
	capable := "claude/settings"
	var incapable string
	for address, yes := range shippedHostImportSurfaces() {
		if !yes && incapable == "" {
			incapable = address
		}
	}
	if incapable == "" {
		t.Fatal("fixture: shipped packs must include a surface without an object host layer")
	}

	got, problems := checkHostImportExclusions(decode(t,
		`{"host_import_exclusions":{"`+capable+`":["/hooks"]}}`))
	if len(problems) != 0 || len(got[capable]) != 1 {
		t.Fatalf("known capable (possibly unselected) target: normalized=%v problems=%v", got, problems)
	}
	for _, tc := range []struct {
		address string
		want    string
	}{
		{"not-a-shipped-surface/name", "unknown shipped config surface"},
		{incapable, "cannot receive an object host layer"},
	} {
		_, problems := checkHostImportExclusions(decode(t,
			`{"host_import_exclusions":{"`+tc.address+`":["/hooks"]}}`))
		if !strings.Contains(strings.Join(problems, "\n"), tc.want) {
			t.Errorf("target %q: problems = %v, want %q", tc.address, problems, tc.want)
		}
	}
}

func TestHostImportExclusionsClassifiesCoreMiseSurfaceAsIncapable(t *testing.T) {
	capabilities := shippedHostImportSurfaces()
	capable, known := capabilities["mise/config"]
	if !known || capable {
		t.Fatalf("mise/config capability = %v, known=%v; want known and incapable", capable, known)
	}
	_, problems := checkHostImportExclusions(decode(t,
		`{"host_import_exclusions":{"mise/config":["/tools"]}}`))
	joined := strings.Join(problems, "\n")
	if !strings.Contains(joined, "cannot receive an object host layer") || strings.Contains(joined, "unknown shipped config surface") {
		t.Fatalf("mise/config problems = %v; want known-incapable error, not unknown-target error", problems)
	}
}

func TestHostImportExclusionsEmptyPolicyDoesNotChangeOwnership(t *testing.T) {
	for _, body := range []string{
		`{}`,
		`{"host_import_exclusions":{}}`,
		`{"host_management":"own","host_import_exclusions":{"claude/settings":[]}}`,
	} {
		cfg := decode(t, body)
		policy, problems := checkHostImportExclusions(cfg)
		if len(problems) != 0 || lenHostImportExclusions(policy) != 0 {
			t.Errorf("%s: policy=%v problems=%v", body, policy, problems)
		}
	}
	setHostImportExclusionsUserConfig(t, `{"host_management":"own","host_import_exclusions":{"claude/settings":[]}}`)
	if policy, err := LoadHostImportExclusions(); err != nil || lenHostImportExclusions(policy) != 0 {
		t.Errorf("empty user policy under own = %v, err=%v; want unchanged empty policy", policy, err)
	}
}

func TestHostImportExclusionsRequireHostManagementNone(t *testing.T) {
	setHostImportExclusionsUserConfig(t, `{"host_management":"own","host_import_exclusions":{"claude/settings":["/hooks"]}}`)
	if _, err := LoadHostImportExclusions(); err == nil || !strings.Contains(err.Error(), "host-versus-jail targeting") {
		t.Fatalf("strict user loader error = %v, want ownership refusal and targeting remedy", err)
	}
	for _, mode := range []string{"own"} {
		cfg := decode(t, `{"host_management":"`+mode+`","host_import_exclusions":{"claude/settings":["/hooks"]}}`)
		errs, _ := ValidateConfig(cfg, t.TempDir(), nil)
		joined := strings.Join(errs, "\n")
		for _, want := range []string{"claude/settings", "/hooks", "host-versus-jail targeting", "remove the exclusions"} {
			if !strings.Contains(joined, want) {
				t.Errorf("host_management %q errors lack %q: %v", mode, want, errs)
			}
		}
	}
	for _, cfgText := range []string{
		`{"host_import_exclusions":{"claude/settings":["/hooks"]}}`,
		`{"host_management":"none","host_import_exclusions":{"claude/settings":["/hooks"]}}`,
	} {
		cfg := decode(t, cfgText)
		errs, _ := ValidateConfig(cfg, t.TempDir(), nil)
		if strings.Contains(strings.Join(errs, "\n"), "cannot be used with host_management: own") {
			t.Errorf("user scope %s was treated as owned: %v", cfgText, errs)
		}
	}
}

func TestHostImportExclusionsRejectWorkspaceDeclarationAndOverride(t *testing.T) {
	for _, tc := range []struct {
		name string
		user string
		ws   string
	}{
		{"workspace only", `{}`, `{"host_import_exclusions":{"claude/settings":["/hooks"]}}`},
		{"workspace replaces", `{"host_import_exclusions":{"claude/settings":["/hooks"]}}`, `{"host_import_exclusions":{"claude/settings":["/other"]}}`},
		{"workspace extends", `{"host_import_exclusions":{"claude/settings":["/hooks"]}}`, `{"host_import_exclusions":{"claude/settings":["/other"],"codex/config":[]}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setHostImportExclusionsUserConfig(t, tc.user)
			ws := t.TempDir()
			write(t, filepath.Join(ws, WorkspaceConfigName), tc.ws)
			merged := MergeConfig(decode(t, tc.user), decode(t, tc.ws))
			errs, _ := ValidateConfig(merged, ws, nil)
			if !strings.Contains(strings.Join(errs, "\n"), "user-scope only") {
				t.Fatalf("workspace policy errors = %v, want user-scope-only refusal", errs)
			}
		})
	}
}

func TestLoadHostImportExclusionsReadsUserIncludesAndInheritedSnapshot(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(home, ".config", "yolo-jail")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv(UserLayerEnv, "")
	t.Setenv("YOLO_VERSION", "")
	if err := os.WriteFile(filepath.Join(configDir, "config.jsonc"),
		[]byte(`{"include_if_found":["machine.jsonc"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "machine.jsonc"),
		[]byte(`{"host_import_exclusions":{"claude/settings":["/from-include"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	policy, err := LoadHostImportExclusions()
	if err != nil || !equalStrings(policy["claude/settings"], []string{"/from-include"}) {
		t.Fatalf("included user policy = %v, err=%v", policy, err)
	}

	if err := os.WriteFile(filepath.Join(configDir, "inherited-launch.jsonc"),
		[]byte(`{"host_import_exclusions":{"claude/settings":["/from-inherited-user-snapshot"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YOLO_VERSION", "9.9.9-test")
	policy, err = LoadHostImportExclusions()
	if err != nil || !equalStrings(policy["claude/settings"], []string{"/from-include", "/from-inherited-user-snapshot"}) {
		t.Fatalf("local include and inherited user snapshot policy = %v, err=%v", policy, err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.jsonc"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	policy, err = LoadHostImportExclusions()
	if err != nil || !equalStrings(policy["claude/settings"], []string{"/from-inherited-user-snapshot"}) {
		t.Fatalf("inherited user policy = %v, err=%v", policy, err)
	}
}

func TestLoadHostImportExclusionsNeverReadsWorkspace(t *testing.T) {
	setHostImportExclusionsUserConfig(t, `{}`)
	workspace := t.TempDir()
	write(t, filepath.Join(workspace, WorkspaceConfigName), `{"host_import_exclusions":{"claude/settings":["/workspace"]}}`)
	t.Setenv("YOLO_WORKSPACE", workspace)
	policy, err := LoadHostImportExclusions()
	if err != nil || len(policy) != 0 {
		t.Fatalf("absent user policy = %v, err=%v", policy, err)
	}
}

func TestHostImportExclusionsRejectWorkspaceLocalDeclaration(t *testing.T) {
	setHostImportExclusionsUserConfig(t, `{}`)
	ws := t.TempDir()
	write(t, filepath.Join(ws, WorkspaceLocalConfigName), `{"host_import_exclusions":{"claude/settings":[]}}`)
	merged := decode(t, `{"host_import_exclusions":{"claude/settings":[]}}`)
	errs, _ := ValidateConfig(merged, ws, nil)
	if !strings.Contains(strings.Join(errs, "\n"), "user-scope only") {
		t.Fatalf("workspace-local policy errors = %v, want user-scope-only refusal", errs)
	}
}

func TestFilterInheritUsesOnlyUserHostImportExclusions(t *testing.T) {
	setHostImportExclusionsUserConfig(t, `{"host_import_exclusions":{"claude/settings":["/hooks"]}}`)
	effective := jsonx.NewOrderedMap()
	effective.Set("host_import_exclusions", mapOf(t, "claude/settings", []any{"/workspace-only"}))
	for _, scope := range []InheritScope{InheritPreflight, InheritNested} {
		got, unknown, err := FilterInheritErr(effective, scope)
		if err != nil || len(unknown) != 0 {
			t.Fatalf("%s: filtered unknown=%v err=%v", scope, unknown, err)
		}
		value, present := got.Get("host_import_exclusions")
		if !present {
			t.Fatalf("%s: user policy missing from inherited snapshot", scope)
		}
		policy, problems := checkHostImportExclusions(mapOf(t, hostImportExclusionsKey, value))
		if len(problems) != 0 || !equalStrings(policy["claude/settings"], []string{"/hooks"}) {
			t.Errorf("%s: inherited policy=%v problems=%v; workspace value leaked", scope, policy, problems)
		}
	}

	setHostImportExclusionsUserConfig(t, `{}`)
	workspaceOnly := jsonx.NewOrderedMap()
	workspaceOnly.Set("host_import_exclusions", mapOf(t, "claude/settings", []any{"/workspace-only"}))
	for _, scope := range []InheritScope{InheritPreflight, InheritNested} {
		got, unknown, err := FilterInheritErr(workspaceOnly, scope)
		if err != nil || len(unknown) != 0 {
			t.Fatalf("%s workspace-only policy: unknown=%v err=%v", scope, unknown, err)
		}
		if _, present := got.Get("host_import_exclusions"); present {
			t.Errorf("%s inherited a workspace policy", scope)
		}
	}
}

func setHostImportExclusionsUserConfig(t *testing.T, body string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	t.Setenv(UserLayerEnv, "")
	path := filepath.Join(home, ".config", "yolo-jail", "config.jsonc")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
