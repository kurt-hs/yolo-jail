package crossaudit

import (
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/logstamp"
	"github.com/mschulkind-oss/yolo-jail/internal/svcendpoint"
)

// Install, which every host yolo process runs, gives the transport's diagnostics the one
// daemon-log stamp: svcendpoint is a stdlib-only leaf whose default logger carries none.
func TestInstallStampsTheTransportLogger(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	prevLogger, prevSink := svcendpoint.Logger, svcendpoint.CrossingSink()
	t.Cleanup(func() {
		svcendpoint.Logger = prevLogger
		svcendpoint.SetCrossingSink(prevSink)
	})
	Install()
	if _, ok := svcendpoint.Logger.Writer().(*logstamp.LineWriter); !ok || svcendpoint.Logger.Flags() != 0 {
		t.Fatalf("after Install the transport logs through %T with flags %d; want logstamp's writer",
			svcendpoint.Logger.Writer(), svcendpoint.Logger.Flags())
	}
}
