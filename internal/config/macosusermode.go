package config

import (
	"fmt"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// macosusermode.go is the config half of the macos-user mode setting
// (docs/design/macos-user-model-plan.md, slice 2, MMP-D3): which account a
// macos-user jail's session runs as. "account", or no value, is the default;
// "self" is the opt-in run-as-you mode a launch runs since slice 6.

// macosUserModeKey is the top-level key naming the macos-user mode.
const macosUserModeKey = "macos_user_mode"

// MacosUserMode reports the value of `macos_user_mode` in the USER config, and
// whether the key is set there at all. It never consults the workspace config:
// the mode is one machine-wide choice (MMP-D3), and /workspace is agent-editable.
func MacosUserMode() (value string, set bool) {
	return macosUserModeValue(UserScopeConfigOrEmpty())
}

// macosUserModeValue is the reading, split from the real-home lookup so the
// validator and the tests exercise one implementation. A non-string value reads
// as unset; the validator is what reports it.
func macosUserModeValue(cfg *jsonx.OrderedMap) (string, bool) {
	v, present := cfg.Get(macosUserModeKey)
	if !present || v == nil {
		return "", false
	}
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	return s, true
}

// macosUserModeProblem reports why a value is not a usable `macos_user_mode`, or
// "" when its shape is fine. Exactly two values are accepted; the match is exact,
// so "Self" is a problem rather than a near miss.
func macosUserModeProblem(v any) string {
	s, ok := v.(string)
	if ok && (s == "self" || s == "account") {
		return ""
	}
	return fmt.Sprintf("expected \"self\" or \"account\" (got %s)", pyReprValue(v))
}

// validateMacosUserMode type-checks `macos_user_mode` and refuses the workspace
// spelling. The reader reads user scope only, so a workspace value would never
// be consulted; saying so beats a silent no-op.
func validateMacosUserMode(config *jsonx.OrderedMap, workspace string, errs *[]string, edit ...*editValidationContext) {
	v, present := config.Get(macosUserModeKey)
	if !present {
		return
	}
	if v != nil {
		if prob := macosUserModeProblem(v); prob != "" {
			add(errs, "config."+macosUserModeKey+": "+prob)
		}
	}
	wsCfg, err := loadValidationWorkspaceConfig(workspace, edit...)
	if err != nil || wsCfg == nil {
		return
	}
	if wsValue, atWorkspace := wsCfg.Get(macosUserModeKey); atWorkspace && wsValue != nil && WorkspaceUserScopeOnlyKey(macosUserModeKey) {
		add(errs, "config."+macosUserModeKey+": user-scope only — it chooses the account every "+
			"macos-user launch runs as, and it is read from "+paths.UserConfigDisplayPath()+" alone, so a "+
			"workspace value is never consulted. Move it there, or remove it.")
	}
}
