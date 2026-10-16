package cli

import (
	"fmt"
	"io"

	"github.com/mschulkind-oss/yolo-jail/internal/hostfloor"
)

// refreshRefusalExit is the launch's exit status for a pre-launch refresh the floor refused
// (hostfloor.RefreshResult.Refused, PD-NF1 of docs/design/program-delivery.md): one that reached its
// upstream and then failed or timed out, with the user's YOLO_USE_INSTALLED not covering the program.
// The floor has already printed the refusal block, its repair and its hatch; this adds the one line
// that says the launch stops, and returns 1. It returns 0 for every other outcome, which the caller
// handles as before.
func refreshRefusalExit(res hostfloor.RefreshResult, bin string, errw io.Writer) int {
	if !res.Refused {
		return 0
	}
	fmt.Fprintf(errw, "yolo host: refusing to launch %s: its pre-launch refresh failed\n", bin)
	return 1
}
