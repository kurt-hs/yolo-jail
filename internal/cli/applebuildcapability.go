package cli

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/runtime"
)

const appleBuildLivenessTimeout = 5 * time.Second

// appleContainerBuildCapability is the actual runtime gate for a foreground build child. Apple
// Container cannot start that child beside an existing jail, so a failed or malformed listing is
// unknown, never evidence that the build is safe.
var appleContainerBuildCapability = appleContainerCanBuildNow

func appleContainerCanBuildNow() (bool, string) {
	bin, err := exec.LookPath("container")
	if err != nil {
		return false, fmt.Sprintf("Apple Container liveness is unknown (%v); `container ls` must show no running yolo jail before a build can start", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), appleBuildLivenessTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "ls").CombinedOutput()
	if ctx.Err() != nil {
		return false, "`container ls` timed out; Apple Container liveness is unknown, so no build child was started"
	}
	if err != nil {
		return false, fmt.Sprintf("`container ls` could not establish Apple Container liveness (%v); no build child was started", err)
	}
	return appleContainerBuildDecision(string(out))
}

func appleContainerBuildDecision(output string) (bool, string) {
	if !validAppleContainerLiveListing(output) {
		return false, "`container ls` returned an unrecognized listing; Apple Container liveness is unknown, so no build child was started"
	}
	live := runtime.ParseContainerLsLive(output)
	if len(live) != 0 {
		names := make([]string, 0, len(live))
		for name := range live {
			names = append(names, name)
		}
		sort.Strings(names)
		return false, "Apple Container has running yolo jail(s): " + strings.Join(names, ", ")
	}
	return true, ""
}

func validAppleContainerLiveListing(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		var id, state bool
		for _, field := range fields {
			id = id || strings.EqualFold(field, "ID")
			state = state || strings.EqualFold(field, "STATE")
		}
		if id && state {
			return true
		}
	}
	return false
}
