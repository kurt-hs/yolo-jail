package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/mschulkind-oss/yolo-jail/internal/logcap"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// runInternalLogAppend is used only by generated in-jail writers. The path is a workspace
// root plus a fixed .yolo basename, never an arbitrary host-side file path.
func runInternalLogAppend(args []string, input io.Reader) int {
	tee := len(args) == 3 && args[2] == "--tee"
	reset := len(args) == 3 && args[2] == "--reset"
	if (len(args) != 2 && !tee && !reset) || args[0] == "" || (args[1] != "receipts.jsonl" && args[1] != "startup.log") || ((tee || reset) && args[1] != "startup.log") {
		fmt.Fprintln(os.Stderr, "usage: yolo internal log-append <workspace> receipts.jsonl|startup.log [--tee|--reset (startup only)]")
		return 2
	}
	// On the macos-user backend this runs as the sandbox account, whose ~/.config links into
	// the workspace's own sidecar (<ws>/.yolo/home/config). Judged by the process HOME, that
	// link put "yolo's user config directory" inside the workspace and refused every
	// provisioning log (macos-user.yml run 38733308150). Judge by the account's boundary
	// instead, the rule `yolo internal darwin-bootstrap` already applies to the same layout.
	if darwinBootstrapIdentityRefusal() == nil {
		defer paths.GuardScopeUnder(darwinBootstrapBoundaryHome())()
	}
	open := func(name string, flag int, perm os.FileMode) (*os.File, error) {
		return paths.OpenWorkspaceStateFile(args[0], name, flag, perm)
	}
	if tee {
		// Read chunks, not lines: installers may stream progress without a newline.
		// A failed log must not hide the remaining setup output from the console.
		buf := make([]byte, 32<<10)
		status := 0
		for {
			n, err := input.Read(buf)
			if n > 0 {
				if _, werr := os.Stdout.Write(buf[:n]); werr != nil {
					status = 1
				}
				if werr := logcap.AppendBeneath(open, args[1], buf[:n]); werr != nil {
					if status == 0 {
						fmt.Fprintln(os.Stderr, werr)
					}
					status = 1
				}
			}
			if err != nil {
				if err != io.EOF {
					fmt.Fprintln(os.Stderr, err)
					status = 1
				}
				return status
			}
		}
	}
	data, err := io.ReadAll(input)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	appendData := logcap.AppendBeneath
	if reset {
		appendData = logcap.ResetBeneath
	}
	if err := appendData(open, args[1], data); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
