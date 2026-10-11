package run

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	yruntime "github.com/mschulkind-oss/yolo-jail/internal/runtime"
	"github.com/mschulkind-oss/yolo-jail/internal/svcendpoint"
)

// crossingCapture records the crossings whose publication directory is dir's, so a test sees
// its own front's records and no other test's.
type crossingCapture struct {
	mu   sync.Mutex
	jail string
	seen []svcendpoint.Crossing
}

func captureCrossingsIn(t *testing.T, dir string) *crossingCapture {
	t.Helper()
	c := &crossingCapture{jail: filepath.Base(dir)}
	prev := svcendpoint.CrossingSink()
	svcendpoint.SetCrossingSink(func(x svcendpoint.Crossing) {
		c.mu.Lock()
		defer c.mu.Unlock()
		if x.Jail == c.jail {
			c.seen = append(c.seen, x)
		}
	})
	t.Cleanup(func() { svcendpoint.SetCrossingSink(prev) })
	return c
}

// first waits for the first crossing (one is recorded when its connection closes).
func (c *crossingCapture) first(t *testing.T) svcendpoint.Crossing {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		if len(c.seen) > 0 {
			x := c.seen[0]
			c.mu.Unlock()
			return x
		}
		c.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no crossing recorded for %s", c.jail)
	return svcendpoint.Crossing{}
}

// A jail launch names its container and the workspace code launches.log uses; a `yolo host`
// launch has no container and names the host notch's code.
func TestCrossingLabelNamesTheContainerAndTheWorkspaceCode(t *testing.T) {
	cname := "yolo-myproj-1a2b3c4d"
	code := paths.JailShortHash(cname)
	for _, rt := range []string{"podman", "container", "macos-user"} {
		if got := crossingLabelFor(cname, rt); got != (svcendpoint.CrossingLabel{Container: cname, Workspace: code}) {
			t.Errorf("%s: label = %+v", rt, got)
		}
	}
	if got := crossingLabelFor(cname, hostNotchRuntime); got != (svcendpoint.CrossingLabel{Workspace: "host:" + code}) {
		t.Errorf("host notch: label = %+v, want no container and host:%s", got, code)
	}
	// The host notch's code is the one its perf log heads a block with.
	if want := hostNotchLabel("/some/workspace"); crossingLabelFor(yruntime.FromWorkspace("/some/workspace"), hostNotchRuntime).Workspace != want {
		t.Errorf("the host notch's crossing code is not hostNotchLabel's %q", want)
	}
}
