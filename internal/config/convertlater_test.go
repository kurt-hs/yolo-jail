package config

// convertlater_test.go pins where `"convert_later": true` may be written (RC-D21,
// docs/design/config-migration.md §1 and §5): the old-format user file only. A workspace file
// refuses it as user-scope only, and a TOML file of either scope refuses it as having nothing to
// say there.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func convertLaterEnv(t *testing.T) (userDir, ws string) {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("YOLO_VERSION", "")
	t.Setenv("YOLO_USER_LAYER", "")
	userDir = filepath.Join(home, ".config", "yolo-jail")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ws, err = filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return userDir, ws
}

func writeConvertLaterFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestConvertLaterAcceptedInTheOldUserFile(t *testing.T) {
	userDir, ws := convertLaterEnv(t)
	writeConvertLaterFile(t, filepath.Join(userDir, "config.jsonc"), `{"convert_later": true}`)
	cfg, err := LoadConfig(ws, true, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if errs, _ := ValidateConfig(cfg, ws, nil); len(errs) != 0 {
		t.Fatalf("convert_later in the old user file was refused: %v", errs)
	}
}

func TestConvertLaterMustBeABoolean(t *testing.T) {
	userDir, ws := convertLaterEnv(t)
	writeConvertLaterFile(t, filepath.Join(userDir, "config.jsonc"), `{"convert_later": "yes"}`)
	cfg, err := LoadConfig(ws, true, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	errs, _ := ValidateConfig(cfg, ws, nil)
	if len(errs) != 1 || !strings.Contains(errs[0], "config.convert_later: expected true or false") {
		t.Fatalf("errors = %v, want one type refusal", errs)
	}
}

func TestConvertLaterRefusedInAWorkspaceFile(t *testing.T) {
	_, ws := convertLaterEnv(t)
	writeConvertLaterFile(t, filepath.Join(ws, "yolo-jail.jsonc"), `{"convert_later": true}`)
	cfg, err := LoadConfig(ws, true, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	errs, _ := ValidateConfig(cfg, ws, nil)
	if len(errs) != 1 || !strings.Contains(errs[0], "config.convert_later: user-scope only") ||
		!strings.Contains(errs[0], "config.toml") {
		t.Fatalf("errors = %v, want one user-scope refusal naming the user file", errs)
	}
}

func TestConvertLaterRefusedInTOML(t *testing.T) {
	for _, scope := range []string{"user", "project"} {
		t.Run(scope, func(t *testing.T) {
			userDir, ws := convertLaterEnv(t)
			path := filepath.Join(userDir, "config.toml")
			if scope == "project" {
				path = filepath.Join(ws, "yolo-jail.toml")
			}
			writeConvertLaterFile(t, path, "convert_later = true\n")
			_, err := LoadConfig(ws, true, func(string) {})
			if err == nil {
				t.Fatal("convert_later in a TOML file was read")
			}
			for _, want := range []string{":1:", "convert_later", "nothing to say there", "Remove this line"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("refusal lacks %q: %v", want, err)
				}
			}
		})
	}
}
