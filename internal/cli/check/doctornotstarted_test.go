package check

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/hostservice"
)

// TestCheckTellsTheSelfCheckWhenTheBackendDoesNotStartItsService is the call-site half of the
// Apple Container broker-log dead end (the self-check half is oauthbroker's
// TestSelfCheckOnABackendWithoutTheBrokerNamesAStepThatWorksThere). On Apple Container the
// launch does not start the Claude OAuth broker, so its self-check must not send the user to
// the broker's log; only `yolo check` knows which backend the next launch uses, and it decides
// admission with the launch's own predicate (run.HostServiceAdmittedOn). This drives the real
// loophole walk and the real doctor runner, and the doctor_cmd echoes what it was told.
func TestCheckTellsTheSelfCheckWhenTheBackendDoesNotStartItsService(t *testing.T) {
	for _, tc := range []struct {
		runtime, want string
	}{
		{"container", "told=[container]"},
		{"podman", "told=[]"},
		{"", "told=[]"},
	} {
		t.Run("runtime="+tc.runtime, func(t *testing.T) {
			moduleRoot := isolatedModuleDir(t)
			t.Setenv(hostservice.DoctorNotStartedOnEnv, "")
			selfCheckModule(t, moduleRoot, brokerLoopholeName, []string{"/bin/sh", "-c",
				`echo "FAIL: probe"; echo "  told=[$` + hostservice.DoctorNotStartedOnEnv + `]"; exit 1`})

			var buf bytes.Buffer
			r := newReporter(&buf, false)
			o := &Options{Workspace: t.TempDir(), Getenv: func(string) string { return "" }}
			fillDefaults(o)
			o.loopholeRuntime = tc.runtime
			o.checkLoopholes(r)
			out := buf.String()

			if !strings.Contains(out, "probe") {
				t.Fatalf("the broker's self-check never ran:\n%s", out)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("runtime %q: the self-check was not told %q:\n%s", tc.runtime, tc.want, out)
			}
			// The liveness rows promise "First `yolo run` will spawn it", which is false
			// where the launch never starts the broker.
			notStartedRow := strings.Contains(out, "daemon not started on container")
			if tc.runtime == "container" {
				if strings.Contains(out, "will spawn it") || !notStartedRow {
					t.Errorf("on Apple Container the liveness rows must say the launch does "+
						"not start the broker, not promise it will:\n%s", out)
				}
			} else if notStartedRow || !strings.Contains(out, "loophole "+brokerLoopholeName+": daemon ") {
				t.Errorf("runtime %q lost the broker liveness rows:\n%s", tc.runtime, out)
			}
		})
	}
}
