// Package testconfig makes isolated config fixtures without invoking config
// discovery or consulting the caller's real home directory.
package testconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/configdoc"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// Fixture identifies one config file and the symlink-resolved temporary root
// that owns it.
type Fixture struct{ Root, Path string }

// Workspace creates a new-format project fixture.
func Workspace(t testing.TB, text string) Fixture { return WorkspaceFormat(t, configdoc.TOML, text) }

// WorkspaceFormat creates a project fixture in the explicitly requested format.
func WorkspaceFormat(t testing.TB, format configdoc.Format, text string) Fixture {
	t.Helper()
	checkFormat(t, format)
	root := root(t)
	return write(t, root, filepath.Join(root, workspaceName(format)), text)
}

// User creates a new-format user fixture below a temporary, private home tree.
func User(t testing.TB, text string) Fixture { return UserFormat(t, configdoc.TOML, text) }

// UserFormat creates a user fixture without consulting or changing the real HOME.
func UserFormat(t testing.TB, format configdoc.Format, text string) Fixture {
	t.Helper()
	checkFormat(t, format)
	home := root(t)
	return write(t, home, filepath.Join(home, ".config", "yolo-jail", userName(format)), text)
}

// Manifest creates a new-format pack manifest fixture.
func Manifest(t testing.TB, text string) Fixture { return ManifestFormat(t, configdoc.TOML, text) }

// ManifestFormat creates a manifest fixture in the explicitly requested format.
func ManifestFormat(t testing.TB, format configdoc.Format, text string) Fixture {
	t.Helper()
	checkFormat(t, format)
	root := root(t)
	return write(t, root, filepath.Join(root, manifestName(format)), text)
}

func root(t testing.TB) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve fixture root: %v", err)
	}
	return root
}

func write(t testing.TB, root, path, text string) Fixture {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create fixture directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return Fixture{Root: root, Path: path}
}

func checkFormat(t testing.TB, format configdoc.Format) {
	t.Helper()
	if format != configdoc.JSONC && format != configdoc.TOML {
		t.Fatalf("unsupported fixture format %d", format)
	}
}

func workspaceName(format configdoc.Format) string {
	if format == configdoc.TOML {
		return paths.WorkspaceConfigTOMLName
	}
	return paths.WorkspaceConfigLegacyJSONCName
}
func userName(format configdoc.Format) string {
	if format == configdoc.TOML {
		return paths.UserConfigTOMLName
	}
	return paths.UserConfigLegacyJSONCName
}
func manifestName(format configdoc.Format) string {
	if format == configdoc.TOML {
		return paths.PackManifestTOMLName
	}
	return paths.PackManifestLegacyJSONCName
}
