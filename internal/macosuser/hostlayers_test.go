package macosuser

import (
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// ⚠ THE macos-user HOST-LAYER CARVE-OUT IS RETIRED (DP-L1, 2026-09-13), and this file is
// the record of what it was and why it ended.
//
// WHAT IT SAID. A `readsHost` surface's bytes crossed on a /ctx mount and this backend has
// no mounts, so every host layer was missing here BY CONSTRUCTION. The jail's read fails
// CLOSED (OQ-CO10), which would have refused every launch that selects the claude or pi
// pack on a Mac with a ~/.claude/settings.json — the default configuration of the backend.
// So the plan declared `unsupported` unconditionally, on the ruling that severity belongs
// to the DISPOSITION: "this backend cannot" is a backend fact the launcher knows before it
// starts, not a delivery fault, and refusing a user for what yolo cannot do here is the
// shape the reachability witness's OQ-R3 already ruled against.
//
// WHY IT ENDED. The premise was about the MECHANISM, not the platform, and the mechanism
// changed: the bytes now cross by COPY into a root-owned tree under /var/yolo-jail and the
// bootstrap is told where (internal/cli/run/macosctxtree.go, StageCtxCommands,
// YOLO_CTX_ROOT). A delivered file is a file the jail can really open, so `unsupported`
// became the lie the carve-out existed to avoid telling.
//
// WHAT SURVIVES. The word, for the case it is still true of — a caller that staged no tree
// — and OQ-R3's rule with it, unchanged: a launch is not refused for a delivery nobody
// attempted. The pairing is now a FACT ABOUT THE LAUNCH rather than about the backend, and
// PlanInvariants fails a plan whose report and staged tree disagree in either direction.
//
// The behaviour is asserted in ctxtree_test.go, beside the staging it depends on:
// TestRunPlanReportsHostLayersDeliveredWhenATreeIsStaged and
// TestRunPlanStillReportsHostLayersUnsupportedWithNoTree.

// What is left HERE is the one property neither of those covers and that both depend on:
// whatever the plan emits is the REPORT SHAPE the jail parses, in both dispositions.
//
// It is worth its own test because the failure is silent and total. packload's parser reads
// an unparseable value as UNKNOWN rather than as an empty delivery, deliberately — a garbled
// variable must not refuse every host layer on the machine — so a hand-written token here
// would restore the fail-open behaviour OQ-CO10 ended, on this backend alone, with every
// other assertion in the tree still green.
func TestHostLayerReportIsTheShapeTheJailParses(t *testing.T) {
	for _, tc := range []struct {
		name     string
		hostCtx  HostContext
		delivery string
	}{
		{"a launch that staged host bytes", deliveredCtx(), packload.HostLayersSupported},
		{"a launch that staged none", HostContext{}, packload.HostLayersUnsupported},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := BuildRunPlan("/Users/Shared/yolo/proj", jsonx.NewOrderedMap(),
				[]string{"claude"}, []string{"/bin/zsh", "-l"}, "/usr/local/bin/yolo",
				hostStaged, "", tc.hostCtx, jsonx.NewOrderedMap(), nil, nil)

			wire, ok := argvEnvValue(plan.BootstrapArgv, packload.HostLayerEnvVar)
			if !ok {
				t.Fatalf("the bootstrap argv does not carry %s:\n%v\nWithout it the jail's "+
					"fail-closed host-layer read has no disposition to read at all",
					packload.HostLayerEnvVar, plan.BootstrapArgv)
			}
			report, parsed := packload.ParseHostLayerReport(wire)
			if !parsed {
				t.Fatalf("%s=%q is not the report shape the jail parses; it would be read "+
					"as UNKNOWN and silently restore the fail-open read on this backend",
					packload.HostLayerEnvVar, wire)
			}
			if report.Delivery != tc.delivery {
				t.Errorf("delivery = %q, want %q", report.Delivery, tc.delivery)
			}
			// A bare token would parse as nothing; the JSON field is what the jail keys on.
			if !strings.Contains(wire, `"delivery":"`+tc.delivery+`"`) {
				t.Errorf("wire %q does not carry the delivery field verbatim", wire)
			}
		})
	}
}
