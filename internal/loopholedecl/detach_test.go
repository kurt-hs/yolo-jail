package loopholedecl_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/loopholedecl"
)

// detachManifest is a per-jail socket daemon whose host_daemon carries extra.
func detachManifest(extra map[string]any) map[string]any {
	hd := map[string]any{"cmd": []any{"d", "{socket}"}, "publishes": "socket"}
	for k, v := range extra {
		hd[k] = v
	}
	return map[string]any{"name": "detaching", "description": "x", "host_daemon": hd}
}

// TestDetachDecodesAndDefaultsOff: `host_daemon.detach` is how a daemon says the jail's end
// signals it and leaves it to finish (docs/design/host-daemon-ownership.md §1.3). Absent means
// no, which is the SIGTERM, grace, SIGKILL every daemon has always had.
func TestDetachDecodesAndDefaultsOff(t *testing.T) {
	for _, tc := range []struct {
		name  string
		extra map[string]any
		want  bool
	}{
		{"absent", nil, false},
		{"true", map[string]any{"detach": true}, true},
		{"false", map[string]any{"detach": false}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, err := decodeMap(t, "detaching", detachManifest(tc.extra))
			if err != nil {
				t.Fatalf("strict decode: %v", err)
			}
			if m.HostDaemon.Detach != tc.want {
				t.Errorf("Detach = %v, want %v", m.HostDaemon.Detach, tc.want)
			}
		})
	}
}

// TestDetachValueMustBeABoolean: a quoted "false" is Truthy, so a coerced value would leave a
// daemon running past its jail that nothing ever reaps. Refused at both strictnesses, like
// `launch_check`.
func TestDetachValueMustBeABoolean(t *testing.T) {
	for _, val := range []any{"false", "true", 1, ""} {
		manifest := detachManifest(map[string]any{"detach": val})
		_, err := decodeMap(t, "detaching", manifest)
		if err == nil || !strings.Contains(err.Error(), "host_daemon.detach") ||
			!strings.Contains(err.Error(), "boolean") {
			t.Errorf("detach %#v: err = %v, want a refusal naming the key and the type", val, err)
		}
		if _, _, terr := loopholedecl.DecodeTolerant(manifestBytes(t, manifest),
			filepath.Join("/loopholes", "detaching")); terr == nil {
			t.Errorf("detach %#v: the tolerant decode accepted a wrong type", val)
		}
	}
}
