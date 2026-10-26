//go:build linux

package run

// noteCgroupDelegateUnavailable and noteCgroupDelegateFailed are how the in-process
// delegate reports NOT starting. Two functions rather than one with a severity
// argument, because the distinction is the whole content:
//
//   - UNAVAILABLE is "yolo could not ask" — this kernel has no cgroup v2, so there is
//     nothing to install and nothing to fix here. The register is [dim] and the
//     sentence says so outright, for the reason loophole-system.md gives the
//     unsupported-platform message: a reader not told that nothing is missing spends
//     the afternoon proving it.
//   - FAILED is "yolo asked and it did not work" — a bind or a chmod that returned an
//     error, naming the path and the error. That is an actionable fault on a machine
//     that CAN run the delegate, so it is a [yellow] warning.
//
// Collapsing the two would flatten exactly the distinction AGENTS.md preserves for the
// loopback witness (OQ-R3): a host yolo could not ask is never reported as a failure it
// could have avoided. Both are unconditional — the delegate only reaches here when the
// user switched its loophole ON, so neither line can appear on a launch that did not
// ask for the capability.
//
// THEY LIVE IN A LINUX FILE BECAUSE THEY DESCRIBE LINUX FACTS. Every decline they
// word is one cgddaemon_linux.go takes, and off Linux there is no such decline to report —
// the platform axis answers instead (cgddaemon_other.go). Putting them in the untagged
// loopholesruntime.go, where they started, made them unused on darwin and the
// `GOOS=darwin staticcheck` arm of `just lint-ci` rejected the tree (U1000).
//
// AND NOT IN cgddaemon_linux.go ITSELF: both print on the launch stream (o.Stderr, as every
// launch line does, launchlog.go), once, as the delegate fails to start and before any session
// has the terminal, while that file holds the serve loop, which must never write to the terminal
// (TestCgroupDelegateServeLoopAuditsEveryPath bans the spelling there).
func (o *Options) noteCgroupDelegateUnavailable(reason string) {
	o.pr(o.Stderr).print("[dim]The cgroup-delegate loophole is enabled but the delegate " +
		"cannot run on this machine: " + reason + ". Nothing is missing — yolo-cglimit " +
		"will be unavailable in the jail.[/dim]")
}

func (o *Options) noteCgroupDelegateFailed(reason string) {
	o.pr(o.Stderr).print("[yellow]Warning: the cgroup-delegate loophole is enabled but " +
		"its delegate " + reason + " — yolo-cglimit will not work in this jail.[/yellow]")
}
