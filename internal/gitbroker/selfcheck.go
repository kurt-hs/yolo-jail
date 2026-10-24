package gitbroker

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/brokercore"
)

// selfCheckTimeout bounds each version probe the self-check runs.
const selfCheckTimeout = 10 * time.Second

// SelfCheck is the loophole's `doctor_cmd` (`yolo internal daemon git-broker --self-check`,
// git-broker-plan.md task 7): it reports the host ssh and git the broker would run, found on
// the PATH it inherits and checked by the placement rule as a broker checks them. It fails only
// when there is no ssh it would run, since without one the broker reaches no remote; a host
// with no git still serves SSH remotes, so that is reported without failing.
func SelfCheck(w io.Writer) int {
	// `yolo check` runs this from the workspace, as a launch spawns the daemon, so the cwd is
	// checked as the workspace.
	cwd, _ := os.Getwd()
	pathEnv := os.Getenv("PATH")
	refuse := func(program string) func(string) string { return brokercore.PlacementRefusal(program, cwd) }

	ssh, err := lookPath("ssh", pathEnv)
	if err != nil {
		fmt.Fprintln(w, Name+": no ssh on the host's PATH, so the broker reaches no remote; install OpenSSH on the host")
		return 1
	}
	if why := refuse("ssh")(ssh); why != "" {
		fmt.Fprintln(w, Name+": "+why)
		return 1
	}
	fmt.Fprintln(w, Name+": host ssh "+ssh+" ("+probeVersion(ssh, "-V")+")")

	git, err := lookPath("git", pathEnv)
	switch {
	case err != nil:
		fmt.Fprintln(w, Name+": no git on the host's PATH; SSH remotes still work, and HTTPS remotes need git's credential helper: install git on the host")
	case refuse("git")(git) != "":
		fmt.Fprintln(w, Name+": "+refuse("git")(git))
	default:
		fmt.Fprintln(w, Name+": host git "+git+" ("+probeVersion(git, "--version")+")")
	}
	return 0
}

// probeVersion runs path with arg in an env of PATH and HOME alone, as the broker's own runs
// start from nothing, and returns the first line it printed, or what went wrong.
func probeVersion(path, arg string) string {
	ctx, cancel := context.WithTimeout(context.Background(), selfCheckTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, arg)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME")}
	out, err := cmd.CombinedOutput()
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	switch {
	case line != "":
		return line
	case err != nil:
		return "its version did not print: " + err.Error()
	default:
		return "no version printed"
	}
}
