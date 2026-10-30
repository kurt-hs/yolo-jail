package config

import (
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

// TestWrittenDefaultsAreWhatTheReadersAnswerForAnAbsentKey fails when a reader stops answering
// the table's value for an absent key, which is what lets `yolo config migrate` drop a written
// default without a second list of defaults.
func TestWrittenDefaultsAreWhatTheReadersAnswerForAnAbsentKey(t *testing.T) {
	empty := jsonx.NewOrderedMap()
	answers := map[string]any{
		"perf_logging":        perfLoggingValue(empty),
		"briefing_provenance": BriefingProvenance(empty),
		"update_check":        updateCheckValue(empty),
	}
	for _, d := range WrittenDefaults() {
		if len(d.Path) == 1 {
			if got, ok := answers[d.Path[0]]; !ok || got != d.Value {
				t.Errorf("%s: reader answers %v, table says %v", d.Path[0], got, d.Value)
			}
		}
	}
	filled := jsonx.NewOrderedMap()
	ApplyWrittenDefaults(filled)
	network, _ := filled.Get("network")
	if mode, _ := network.(*jsonx.OrderedMap).Get("mode"); mode != DefaultNetworkMode {
		t.Errorf("network.mode filled as %v", mode)
	}
}
