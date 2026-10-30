package loopholes

import (
	"strconv"

	"github.com/mschulkind-oss/yolo-jail/internal/loopholedecl"
)

// HostDaemonVerb is the host-daemon management verb's registry name (`yolo host-daemon`).
// internal/broker names it as broker.HostDaemonVerb, which every caller outside this package
// spells; it is defined here because this package's own warnings need it and cannot import
// internal/broker, which imports this package.
const HostDaemonVerb = "host-daemon"

// HostDaemonCycleCommand is the spelling of the RETIRED cycle of a host-wide daemon,
// `yolo host-daemon restart <name>` — broker.CycleCommand's grammar. Since HD-R1
// (docs/design/host-daemon-ownership.md) no launch runs a host-wide daemon, and the verb answers
// a restart with the relaunch that restarts a jail's own. No line yolo prints names it any more:
// a launch's line beside an older yolo's copy and the attach line name that copy's optional stop
// instead. It stays so the tests that pin its absence spell it one way.
func HostDaemonCycleCommand(name string) string {
	return "yolo " + HostDaemonVerb + " restart " + name
}

// missingStateFileStep is the next step the missing-state-file warning names
// (docs/reference/happy-path-principle.md: every stop names its next step), decided by who
// writes the file, which is a fact of the declaration and never of the loophole's name:
//
//   - the mount sentinel is the launcher's own marker (mountsentinel.go), written just before
//     the argv; missing, it is because that write failed, and the launch said why above;
//   - a host daemon writes them when it starts, which is after the mounts (each jail's own,
//     since HD-R1), so the file is there for the next launch, and `yolo check` says why when it
//     never is;
//   - nothing else yolo runs writes a state file, so the manifest's own list is what to fix.
func missingStateFileStep(m *Loophole, rel string) string {
	switch {
	case rel == MountSentinelName:
		return "yolo writes this inert marker before every launch, so the warning above says why " +
			"it could not; fix that, then launch again"
	case m.HostDaemon != nil:
		return "its host daemon writes it once this launch starts it, after the mounts are " +
			"assembled; if this repeats on the next launch, run `yolo check` to see why " + m.Name +
			" does not"
	default:
		return "nothing yolo runs writes it: create it, or remove " + strconv.Quote(rel) +
			" from `state_files` in " + loopholedecl.ManifestPath(m.Path)
	}
}
