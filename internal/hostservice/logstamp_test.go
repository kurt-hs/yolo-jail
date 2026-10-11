package hostservice

import (
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/logstamp"
)

// The host service log's lines — the per-request access line included — carry the daemon-log
// stamp (milliseconds, offset), not log.LstdFlags' second-resolution one.
func TestTheProductionLoggerStampsEveryLine(t *testing.T) {
	if _, ok := defaultLogger.Writer().(*logstamp.LineWriter); !ok || defaultLogger.Flags() != 0 {
		t.Fatalf("Logger writes through %T with flags %d; want logstamp's writer and no log flags",
			defaultLogger.Writer(), defaultLogger.Flags())
	}
}
