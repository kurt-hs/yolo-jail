package cli

// hostlaunchcontext.go is the host notch's half of the launch log's archaeology records
// (internal/cli/run's launchcontext.go): what `yolo host --` was typed as, and, once it has
// composed, where its agent's profile came from, what changed in its config and packs since the
// previous host launch from this directory, and the captured edits a host apply re-applies. Every
// record goes to the launch's block in host-launch.log alone, never to the terminal
// (report-tiers.md's launch stream: no quiet mode, so detail goes to the file).

import (
	"os"

	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/render"
)

// hostInvocationArgv is the argv the invocation record names: the process's, as typed. A var so
// a test can hand one in.
var hostInvocationArgv = func() []string { return os.Args[1:] }

// recordInvocation writes the invocation record into the launch's block.
func (t *hostLaunchTrace) recordInvocation() {
	t.log.RecordInvocation(hostInvocationArgv())
}

// recordComposition writes the context records for a launch that composed. typed is the -p this
// launch was given for its agent ("" for none), as the composition read it.
func (t *hostLaunchTrace) recordComposition(launch *hostComposition, typed string) {
	if t == nil || t.log == nil || launch == nil || launch.err != nil {
		return
	}
	fold := hostProfileFold(launch.cfg, launch.packs, launch.agent, typed)
	profile := run.ProfileRecord(fold, config.ReceiversOf(launch.packs).Bins,
		config.ConfigProfileSelection(launch.cfg), func(string, bool) string { return "-p " + typed })
	t.log.RecordContext(run.HostLaunchContext{
		Workspace:    launch.workspace,
		Profile:      profile,
		Config:       launch.cfg,
		Packs:        launch.packs,
		CaptureStore: render.Host(paths.Home(), nil, hostOwnership()).SidecarDir(),
	})
}
