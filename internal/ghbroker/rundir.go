package ghbroker

import "github.com/mschulkind-oss/yolo-jail/internal/brokercore"

// rundir.go is where a broker, or `yolo check`'s self-check, lays out the directories its gh
// runs under (RunnerOptions.RunDir): broker/github/run/<pid>-<random>, 0700. Each holds a copy
// of the host's hosts.yml, which is the host login when gh keeps it in the file, so a run dir
// is collected by liveness, never by age (brokercore.SweepRunDirs).

// runRoot is the directory every run dir sits in.
func runRoot() string { return brokercore.RunRoot(Source) }

// newRunDir names this process's run dir. NewRunner creates it.
func newRunDir() string { return brokercore.NewRunDir(Source) }

// sweepRunDirs removes the run dirs whose owning process is known gone, and returns them.
func sweepRunDirs() (removed []string) { return brokercore.SweepRunDirs(Source) }
