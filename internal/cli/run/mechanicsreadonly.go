package run

import (
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/jailcontent"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

// mechanicsWorkspaceReadonlyFromArgv narrows the requested paths to the read-only workspace
// binds the assembler actually emitted and records whether the runtime enforces that mode.
func mechanicsWorkspaceReadonlyFromArgv(o *Options, facts *jailcontent.FactSheet, cfg *jsonx.OrderedMap, argv []string, rt string) {
	if facts == nil || rt == "macos-user" { // parity: NotApplicable — the native sandbox has no bind argv to inspect; it enforces workspace writes via Seatbelt
		return
	}
	facts.WorkspaceReadonly = nil
	reason := o.roBindsUnsupported(rt)
	for _, rel := range cfgStrList(cfg, "workspace_readonly") {
		dest := "/workspace/" + rel
		emitted := false
		for i := 0; i+1 < len(argv); i++ {
			if argv[i] == "-v" && strings.HasSuffix(argv[i+1], ":"+dest+":ro") {
				emitted = true
				break
			}
		}
		if emitted {
			facts.WorkspaceReadonly = append(facts.WorkspaceReadonly, jailcontent.WorkspaceReadonlyFact{
				Path: dest, Enforced: reason == "", Reason: reason,
			})
		}
	}
}
