package broker

import "github.com/mschulkind-oss/yolo-jail/internal/loopholes"

// HostDaemonVerb is the management verb's registry name, in ONE place because several surfaces
// spell it: the command's own help, the refusals and next steps the verb prints, and the
// loophole runtime's warnings (internal/loopholes). DEFINED IN internal/loopholes, whose warnings
// need it and which cannot import this package; this name stays the one every other caller
// spells.
const HostDaemonVerb = loopholes.HostDaemonVerb

// CycleCommand is the spelling of the RETIRED cycle of a host-wide daemon,
// `yolo host-daemon restart <name>` (loopholes.HostDaemonCycleCommand). Since HD-R1 the verb's
// restart answers with the relaunch that restarts a jail's own copy (Restart), and nothing yolo
// prints names it; the tests that pin its absence spell it through here.
func CycleCommand(name string) string {
	return loopholes.HostDaemonCycleCommand(name)
}
