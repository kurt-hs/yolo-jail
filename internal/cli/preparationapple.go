package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// appleBuildPending is an admission refusal, not a build failure: no child ran,
// and the candidate must remain pending without a retry backoff.
type appleBuildPending struct{ reason, capture string }

func (e *appleBuildPending) Error() string {
	return fmt.Sprintf("Apple Container build pending: %s — inspect `container ls`, stop the other jails, then run `yolo capture %s` or retry the launch; supported Podman can prepare it instead", e.reason, e.capture)
}

func isAppleBuildPending(err error) bool {
	var pending *appleBuildPending
	return errors.As(err, &pending)
}

// applePreparationListing is a bounded read-only runtime query. WaitDelay also
// bounds pipes inherited by a misbehaving runtime subprocess.
var applePreparationListing = func(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "container", "ls")
	cmd.WaitDelay = 100 * time.Millisecond
	var out appleListingOutput
	cmd.Stdout, cmd.Stderr = &out, io.Discard
	err := cmd.Run()
	if out.overflow {
		return "", errors.New("container listing exceeded its output bound")
	}
	return out.String(), err
}

type appleListingOutput struct {
	strings.Builder
	overflow bool
}

func (w *appleListingOutput) Write(p []byte) (int, error) {
	n := len(p)
	if w.Len()+n > 1<<20 {
		w.overflow = true
		return n, nil
	}
	return w.Builder.Write(p)
}

// appleRequiredBuildAdmission is called only after the exact artifact lookup
// missed. A successful, recognizable empty running-container table is positive
// absence proof; query failure or unfamiliar output is never an empty answer.
func appleRequiredBuildAdmission(ctx context.Context, rt, capture string) error {
	if rt != "container" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := applePreparationListing(ctx)
	refuse := func(reason string) error { return &appleBuildPending{reason: reason, capture: capture} }
	if err != nil {
		return refuse("runtime liveness is unknown (" + oneLineErr(err) + ")")
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	header := strings.Fields(lines[0])
	state := -1
	for i, field := range header {
		if field == "STATE" {
			state = i
		}
	}
	if len(header) < 3 || header[0] != "ID" || header[1] != "IMAGE" || state < 2 {
		return refuse("runtime liveness is unknown (unrecognized container listing)")
	}
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) <= state || fields[state] != "running" {
			return refuse("runtime liveness is unknown (unrecognized container state)")
		}
		return refuse("a conflicting container is running (" + fields[0] + ")")
	}
	return nil
}
