package cli

// hostactperf.go is the collector the steps INSIDE a host-notch act's stages record into: the
// patched advances `host.apply_gate` and `host.resolve_target` wait on, their build jails, and the
// floor's install (docs/reference/perf-logging.md, "The host notch"). Those stages were single spans
// over minutes: a real `yolo host -- pi` spent 90.7 s in host.apply_gate and 119.3 s in
// host.resolve_target with roughly a minute of it in no span at all.

import (
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/perf"
)

// hostActPerf is the collector of the host-notch act this process is running — a `yolo host --`
// launch or a `yolo host apply` — and hostActFamily the family its step spans are named under
// ("host", "host_apply"). nil outside one and whenever nothing asked to record, which spans nothing.
//
// Package state, set and restored by startHostActPerf, for the reason hostApplyPerf gives: the steps
// it times sit under the patched advance (patchedadvance.go), the fork build (forkbuild.go) and the
// floor (hostfloor.go), each reached by every jail launch, `yolo capture` and the background advance
// too, and a collector threaded through their signatures would reach every caller for spans only
// the host notch records. Read from the parallel advance's goroutines, which start after it is set
// and end before it is restored.
var (
	hostActPerf   *perf.Log
	hostActFamily string
)

// startHostActPerf makes log the running host act's collector, its steps named under family, and
// returns the function that restores the one before it.
func startHostActPerf(log *perf.Log, family string) (restore func()) {
	prevLog, prevFamily := hostActPerf, hostActFamily
	hostActPerf, hostActFamily = log, family
	return func() { hostActPerf, hostActFamily = prevLog, prevFamily }
}

// hostActSpan starts the running host act's span <family>.<step>; a nil span when nothing records.
func hostActSpan(step string) *perf.Span {
	if hostActPerf == nil {
		return nil
	}
	return hostActPerf.Span(hostActFamily + "." + step)
}

// advanceSpan starts one step of f's advance: <family>.tree.<key>.<step> for a patched extension,
// <family>.fork.<key>.<step> for a patched or plain fork. The key is in the name because a host act
// advances its trees side by side (treepool.go), and two concurrent builds' spans must be told apart.
func advanceSpan(f packload.Fork, step string) *perf.Span {
	if hostActPerf == nil {
		return nil
	}
	kind := "fork"
	if f.IsTree() {
		kind = "tree"
	}
	return hostActSpan(kind + "." + f.Key() + "." + step)
}

// floorSpan is hostfloor.Floor.Span: the floor's own step spans, under the running host act's family.
func floorSpan(name string) func() {
	sp := hostActSpan(name)
	return sp.End
}
