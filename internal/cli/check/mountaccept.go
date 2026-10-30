package check

import (
	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/termsafe"
)

// acceptWorkspaceMounts is host `yolo check --accept-config-changes`' part of the gate for the
// mounts a workspace config declares (oq-cx-symlink in docs/reference/context-mounts.md; RC-D24),
// before anything is recorded:
//
//   - the gate's read must be the workspace config this check validated, or it would record a
//     config nothing checked;
//   - every refusal a launch's gate runs on its own read (config.CheckWorkspaceMounts) runs here
//     on the same read;
//   - when the record would change, each mount is shown at the destination its source resolves
//     to, a writable one loud and marked, as a launch's --accept-config-changes shows it.
//
// It returns the mounts, resolved once, for the record (config.RecordApprovalOfRead), and false
// when it refused, having said why.
func (o *Options) acceptWorkspaceMounts(r *reporter, workspace string, validated *jsonx.OrderedMap,
	read *config.WorkspaceRead, resolver config.LoopholeResolver) ([]config.WorkspaceMount, bool) {
	if validated == nil {
		validated = jsonx.NewOrderedMap()
	}
	a, err1 := jsonx.DumpsSnapshot(validated)
	b, err2 := jsonx.DumpsSnapshot(read.Config)
	if err1 != nil || err2 != nil || a != b {
		r.fail("The workspace config changed while yolo check was reading it, so nothing was recorded",
			"Run again: yolo check --accept-config-changes")
		return nil, false
	}
	mounts := read.Mounts()
	if refusals := config.CheckWorkspaceMounts(workspace, mounts, resolver); len(refusals) > 0 {
		for _, why := range refusals {
			r.fail(termsafe.VisibleLines(why), "Fix the entry as it says, then: yolo check --accept-config-changes")
		}
		return nil, false
	}
	if len(mounts) == 0 || !config.ConfigPartChangedOfRead(workspace, read, mounts) {
		return mounts, true
	}
	var rw, ro []string
	for _, m := range mounts {
		if m.Mount.RW {
			rw = append(rw, m.Line())
		} else {
			ro = append(ro, m.Line())
		}
	}
	r.line("  The workspace config's mounts, recorded as approved at these destinations:")
	for _, line := range config.WritableMountBlock(rw) {
		r.line("    " + r.style(line, ansiRed))
	}
	for _, line := range config.MountSourceBlock(ro) {
		r.line("    " + line)
	}
	return mounts, true
}
