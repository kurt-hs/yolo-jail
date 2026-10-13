package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestPreparationIdleTicksPerformZeroSourceChecksAndBuilds(t *testing.T) {
	pf := newPoolFixture(t)
	if initial, out := pf.launch(t, "podman", nil); initial[poolKeyA].Dir == "" || initial[poolKeyB].Dir == "" {
		t.Fatalf("initial build failed: %s", out)
	}
	pf.agentUpdates(t, `{"treepool":"next-launch"}`)
	stubPreparationWorkerStart(t)
	builds := pf.countBuilds()
	_, _ = pf.launch(t, "podman", nil)
	var output bytes.Buffer
	ctx, stop := context.WithCancel(context.Background())
	ticks := make(chan time.Time)
	done := make(chan error, 1)
	go func() { done <- runPreparationScheduler(ctx, ticks, &output) }()
	for i := 0; i < 4; i++ {
		ticks <- pf.now
	}
	stop()
	<-done
	if builds.Load() != 0 || strings.Contains(output.String(), "checking extension") {
		t.Fatalf("idle ticks performed source checks/builds: builds=%d output=%s", builds.Load(), output.String())
	}
}
