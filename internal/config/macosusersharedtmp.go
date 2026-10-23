package config

import (
	"fmt"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// macosusersharedtmp.go is the config half of the shared-/tmp hatch
// (docs/design/macos-user-model-plan.md, MMP-D18, OQ-MM8 ruled 2026-10-23): the
// deny-by-default profile closes the shared /tmp, and `"macos_user_shared_tmp": true`
// in the USER config opens it again, read and write, for a user whose tools need it.

// macosUserSharedTmpKey is the top-level key of the hatch.
const macosUserSharedTmpKey = "macos_user_shared_tmp"

// MacosUserSharedTmp reports whether the user config sets `macos_user_shared_tmp` to
// true. It never consults the workspace config: the workspace is agent-editable, and
// the hatch widens what the agent can read.
func MacosUserSharedTmp() bool {
	return macosUserSharedTmpValue(UserScopeConfigOrEmpty())
}

// macosUserSharedTmpValue is the reading; anything but a JSON true reads false, and the
// validator is what reports a value of the wrong type.
func macosUserSharedTmpValue(cfg *jsonx.OrderedMap) bool {
	v, _ := cfg.Get(macosUserSharedTmpKey)
	b, ok := v.(bool)
	return ok && b
}

// validateMacosUserSharedTmp type-checks the key and refuses the workspace spelling,
// which the reader would never consult.
func validateMacosUserSharedTmp(config *jsonx.OrderedMap, workspace string, errs *[]string) {
	v, present := config.Get(macosUserSharedTmpKey)
	if !present {
		return
	}
	if _, ok := v.(bool); !ok && v != nil {
		add(errs, fmt.Sprintf("config.%s: expected true or false (got %s)", macosUserSharedTmpKey, pyReprValue(v)))
	}
	wsCfg, err := LoadWorkspaceConfig(workspace, false, func(string) {})
	if err != nil || wsCfg == nil {
		return
	}
	if wsValue, atWorkspace := wsCfg.Get(macosUserSharedTmpKey); atWorkspace && wsValue != nil {
		add(errs, "config."+macosUserSharedTmpKey+": user-scope only — it opens the shared /tmp to "+
			"every run-as-you macos-user launch, and it is read from "+paths.UserConfigPath()+
			" alone, so a workspace value is never consulted. Move it there, or remove it.")
	}
}
