package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

type appleBuildDeferred struct{ reason string }

func (e appleBuildDeferred) Error() string { return e.reason }

// appleContainerBuildCapability is the actual runtime gate for a foreground build child. Apple
// Container cannot start that child beside an existing jail, so a failed or malformed listing is
// unknown, never evidence that the build is safe.
var appleContainerBuildCapability = appleContainerCanBuildNow

func appleContainerCanBuildNow(parent context.Context) (bool, string) {
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	out, err := applePreparationListing(ctx)
	if ctx.Err() != nil {
		return false, "`container ls` timed out; Apple Container liveness is unknown, so no build child was started"
	}
	if err != nil {
		return false, fmt.Sprintf("`container ls` could not establish Apple Container liveness (%v); no build child was started", err)
	}
	return appleContainerBuildDecision(string(out))
}

func appleContainerBuildDecision(output string) (bool, string) {
	live, valid := appleContainerLiveListing(output)
	if !valid {
		return false, "`container ls` returned an unrecognized listing; Apple Container liveness is unknown, so no build child was started"
	}
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

// appleContainerLiveListing validates and reads the same table: the header must be first and every
// data row must have exactly its column count. The container CLI's parser skips line one, so a
// header found later must never be used to authorize a build after that parser discards a live row.
func appleContainerLiveListing(output string) (map[string]struct{}, bool) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		return nil, false
	}
	header := strings.Fields(lines[0])
	wantHeader := []string{"ID", "IMAGE", "OS", "ARCH", "STATE"}
	if len(header) < len(wantHeader) {
		return nil, false
	}
	for i, field := range wantHeader {
		if !strings.EqualFold(header[i], field) {
			return nil, false
		}
	}
	stateColumn := len(wantHeader) - 1
	live := map[string]struct{}{}
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) != len(header) || strings.EqualFold(fields[0], "ID") ||
			!strings.EqualFold(fields[stateColumn], "running") {
			return nil, false
		}
		if strings.HasPrefix(fields[0], "yolo-") {
			live[fields[0]] = struct{}{}
		}
	}
	return live, true
}
