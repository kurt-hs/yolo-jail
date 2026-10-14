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
type appleBuildPending struct {
	Reason  string `json:"reason"`
	Capture string `json:"capture"`
}

func (e *appleBuildPending) Error() string {
	return fmt.Sprintf("Apple Container build pending: %s — inspect `container ls`, stop the other jails, then run `yolo capture %s` or retry the launch; supported Podman can prepare it instead", e.Reason, e.Capture)
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
	if err := ctx.Err(); err != nil {
		return err
	}
	canBuild, reason := appleContainerBuildCapability(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if !canBuild {
		return &appleBuildPending{Reason: reason, Capture: capture}
	}
	return nil
}
