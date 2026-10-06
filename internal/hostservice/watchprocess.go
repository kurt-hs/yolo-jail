package hostservice

// watchprocess.go ends a JAIL-LESS LAUNCH'S DAEMON with the process it serves.
//
// Under HD-R1 (docs/design/host-daemon-ownership.md) a host-side daemon belongs to the launch
// that wanted it. A jail's daemon is ended by its keeper or its session's teardown; a launch with
// no jail (`yolo host -- pi`, `yolo openai-auth status`) is one process, which may stop its
// daemon itself or, at `yolo host`, exec the agent in its own place, after which nothing of yolo
// is left to. So the daemon is handed that process's pid and watches it: the exec keeps the pid,
// so the agent that replaced yolo is the process watched, and a launch killed outright ends its
// daemon too.
//
// Polling, as WatchStateDir polls, with signal 0: it delivers nothing, and ESRCH is the one
// answer that means the process is gone. EPERM is an answer too, that it exists and is someone
// else's, and keeps the daemon serving. A pid reused inside one interval would keep the daemon up
// until that process ends, which bounds the cost of the race to one more process's life.

import (
	"errors"
	"strconv"
	"syscall"
	"time"
)

// ProcessPollInterval is how often WatchProcess looks. A variable so a package's tests can
// shorten it; production never writes it.
var ProcessPollInterval = time.Second

// WatchProcess calls gone ONCE, from its own goroutine, when pid no longer exists, and returns
// without calling it when stop closes first. gone receives a one-line reason naming the pid, for
// the daemon's log.
func WatchProcess(pid int, stop <-chan struct{}, gone func(reason string)) {
	interval := ProcessPollInterval
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
			}
			if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
				gone("the launch it served (pid " + strconv.Itoa(pid) + ") has exited")
				return
			}
		}
	}()
}

// ProcessGoneExitLine is what a daemon logs as it shuts down for WatchProcess's reason.
func ProcessGoneExitLine(daemon, reason string) string {
	return daemon + ": " + reason + "; exiting, since it was that launch's own"
}
