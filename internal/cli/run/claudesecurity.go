package run

import (
	"github.com/mschulkind-oss/yolo-jail/internal/claudesecurity"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/macosuser"
)

func (o *Options) claudeSecurityCompatMode(rt string, cfg *jsonx.OrderedMap) string {
	if rt == "macos-user" && o.MacosUserMode == macosuser.ModeAccount && o.claudeCredentialView(rt, cfg) { // parity: NotApplicable — the account-mode adapter exists only on macos-user
		return claudesecurity.AccountMode
	}
	return ""
}

func setClaudeSecurityCompatEnv(env *jsonx.OrderedMap, mode string) {
	if mode == claudesecurity.AccountMode {
		env.Set(claudesecurity.EnvVar, mode)
	}
}
