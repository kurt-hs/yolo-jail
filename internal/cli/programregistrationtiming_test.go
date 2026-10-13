package cli

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/cli/run"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/perf"
)

func TestProgramRegistrationTimingAtActualSlot(t *testing.T) {
	for _, mode := range []string{"unchanged", "disabled", "held", "start failure", "filtered", "unsupported", "in jail"} {
		t.Run(mode, func(t *testing.T) {
			fx := newPatchedAdvanceFixture(t, "")
			fx.launch(t, "podman")
			before := fx.child
			fx.writeUserConfig(t, `,"agent_updates":{"forkpack":"next-launch"}`)
			if mode == "held" {
				fx.writeUserConfig(t, `,"agent_updates":false`)
			}
			stubSpawn(t)
			if mode == "start failure" {
				startBackgroundAdvance = func([]string, string) error { return errors.New("synthetic registration start failure") }
			}
			f := fx.fork(t)
			rt := "podman"
			if mode == "unsupported" {
				rt = "container"
			}
			if mode == "filtered" {
				f.Platforms = []string{"darwin-arm64"}
			}
			if mode == "in jail" {
				t.Setenv("YOLO_VERSION", "test")
			}
			var events []perf.Event
			log := perf.New(nil, func(e perf.Event) { events = append(events, e) })
			if mode == "disabled" {
				log = perf.Disabled()
			}
			req := run.ForkBuildRequest{Pins: []packload.ForkPin{{Fork: f}}, Platform: patchedTestPlatform, Runtime: rt, Interrupt: &run.ActInterrupt{}}
			d, _ := runBuildSlot(run.BuildSlotRequest{Forks: &req, Perf: log, MaxChecks: 1, MaxBuilds: 1}, io.Discard, false)
			want := mode == "unchanged" || mode == "start failure" || mode == "held"
			n := 0
			if want {
				n = 1
			}
			if phaseCount(events, "preparation.registration", perf.KindStart) != n || phaseCount(events, "preparation.registration", perf.KindEnd) != n {
				t.Fatalf("registration timing does not reflect actual eligible callback: %+v", events)
			}
			if mode == "unchanged" || mode == "disabled" {
				if d[f.Bin].Key == "" || fx.child != before {
					t.Fatalf("unchanged slot rebuilt/lost serving entry: %+v", d)
				}
				subs, err := readPreparationSubscriptions(preparationRoot())
				if err != nil || len(subs) != 1 {
					t.Fatalf("actual callback did not preserve program vector: %+v %v", subs, err)
				}
				for _, sub := range subs {
					if sub.Key != f.Key() {
						t.Fatalf("wrong subscription: %+v", sub)
					}
				}
			}
			if mode == "held" {
				subs, err := readPreparationSubscriptions(preparationRoot())
				if err != nil || len(subs) != 0 {
					t.Fatalf("held policy registered work: %+v %v", subs, err)
				}
				for _, e := range events {
					if e.Kind == perf.KindNote && e.Name == "preparation.registration" && e.Detail != "selected programs: returned without error" {
						t.Fatalf("no-op attempt claimed admission: %+v", e)
					}
				}
			}
			if mode == "start failure" {
				found := false
				for _, e := range events {
					if e.Kind == perf.KindNote && e.Name == "preparation.registration" && strings.Contains(e.Detail, "failed: synthetic registration start failure") {
						found = true
					}
				}
				if !found {
					t.Fatal("registration failure claimed no factual failed outcome")
				}
			}
		})
	}
}
