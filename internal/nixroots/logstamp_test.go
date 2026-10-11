package nixroots

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/logstamp"
)

// The root watcher's lines carry the one daemon-log stamp (local, milliseconds, offset), so the
// supervisor that copies them into nix-roots.log does not stamp them a second time.
func TestTheWatcherLogLineCarriesTheDaemonLogStamp(t *testing.T) {
	var buf bytes.Buffer
	(&Watcher{Log: &buf}).logf("scan kept %d", 3)
	line := buf.String()
	if !logstamp.HasStamp([]byte(line)) || !strings.HasSuffix(line, " scan kept 3\n") {
		t.Fatalf("watcher line %q", line)
	}
}
