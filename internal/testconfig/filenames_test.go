package testconfig

import (
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

func TestReadableFilenameConstantsDoNotReplaceLegacyNames(t *testing.T) {
	if config.WorkspaceConfigName != paths.WorkspaceConfigLegacyJSONCName || config.WorkspaceLocalConfigName != paths.WorkspaceLocalConfigLegacyJSONCName {
		t.Fatal("existing config filename constants no longer name the legacy reader")
	}
	if config.WorkspaceTOMLConfigName != paths.WorkspaceConfigTOMLName || config.WorkspaceLocalTOMLConfigName != paths.WorkspaceLocalConfigTOMLName {
		t.Fatal("workspace TOML aliases diverged from paths constants")
	}
	if paths.UserConfigTOMLName != "config.toml" || paths.UserConfigLegacyJSONCName != "config.jsonc" || paths.UserConfigLegacyJSONName != "config.json" {
		t.Fatal("user config filename constants changed")
	}
	if paths.WorkspaceConfigLegacyJSONName != "yolo-jail.json" || paths.WorkspaceLocalConfigLegacyJSONName != "yolo-jail.local.json" {
		t.Fatal("workspace JSON fallback filename constants changed")
	}
	if paths.PackManifestTOMLName != "pack.toml" || paths.PackManifestLegacyJSONCName != "pack.jsonc" || paths.PackManifestLegacyJSONName != "pack.json" {
		t.Fatal("manifest filename constants changed")
	}
}
