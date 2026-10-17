package cli

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/macosuser"
)

// The verb a finishing macos-user launch hands its guest pack tree to must reach the reaper:
// without the dispatch, every hand-off would start a process that only says "unknown command".
func TestInternalDispatchesThePackTreeReaper(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stderr
	os.Stderr = w
	rc := runInternal([]string{macosuser.PackTreeReapVerb})
	os.Stderr = saved
	_ = w.Close()
	said, _ := io.ReadAll(r)
	if rc != 2 || !strings.Contains(string(said), "--root") || strings.Contains(string(said), "unknown command") {
		t.Errorf("yolo internal %s = %d, said %q; want the reaper's own usage", macosuser.PackTreeReapVerb, rc, said)
	}
}
