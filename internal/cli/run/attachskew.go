package run

import "fmt"

// attachskew.go says out loud that an attached jail is running an OLDER yolo than
// the host launcher driving it.
//
// WHY THIS BECAME NECESSARY. Before flake-bundle generations, a `just install`
// deleted a running jail's binaries out from under it, so an old jail did not
// keep running — it broke, loudly and permanently (internal/flakebundle). Now it
// keeps working, which is the right outcome and also the one that can be silently
// wrong: the container's yolo-entrypoint is frozen at the generation it launched
// with, while the host `yolo` emitting its argv has moved on. That is exactly the
// host<->jail skew AGENTS.md warns about ("leaves the machine skewed by default,
// and it stays that way silently"), and version.SourceSkew does not cover it —
// that gate compares the BINARY to the TREE before a build, and here there is no
// build and no tree, just two halves of a contract that were shipped weeks apart.
//
// ONE DIM LINE, and never a refusal. The overwhelmingly common case is a
// maintainer who installs several times a day and whose jails are hours older
// than the launcher; refusing that, or shouting about it, would make the honest
// outcome (the jail still works) feel like a failure. What the line has to carry
// is the comparison and the remedy, because the launch banner already prints the
// baked version and it is the DIFFERENCE that is actionable.

// warnIfJailIsOlderThanTheLauncher prints the skew notice, or nothing.
//
// Silent unless it can name both halves and they actually differ: an absent baked
// version means a container older than the variable, and an "unknown" host
// version means an unstamped binary. Neither is evidence of skew, and a warning
// that fires on "I cannot tell" is one a user learns to scroll past.
func (o *Options) warnIfJailIsOlderThanTheLauncher(rt, cname, baked string) {
	host := o.yoloVersion("")
	if baked == "" || host == "" || host == "unknown" || baked == host {
		return
	}
	o.pr(o.Stderr).print(fmt.Sprintf(
		"[yellow]⚠ This jail runs yolo %s; this launcher is %s.[/yellow] "+
			"[dim]A jail keeps the binaries it launched with, so fixes and contract "+
			"changes since then are not in it. %s "+
			"(finishing its running sessions), then relaunch, to pick them up.[/dim]",
		baked, host, stopRemedy(rt, cname)))
}

// restartAction is the runnable command and the stable launcher wording for a restart.
type restartAction struct {
	command string
	remedy  string
}

func restartActionFor(rt, cname string) restartAction {
	if rt == "macos-user" { // parity: HonoredBy — end the native session and start a fresh one; there is no jail container to stop
		return restartAction{
			command: "yolo",
			remedy:  "end this session, then run `yolo` from this workspace",
		}
	}
	return restartAction{command: "yolo stop", remedy: "'yolo stop' from this workspace"}
}

// stopCommand is the runnable command generated guidance embeds. A native macos-user session
// has no jail to stop: end it, then start a new session with `yolo`.
func stopCommand(rt, cname string) string { return restartActionFor(rt, cname).command }

// stopRemedy retains the launcher-facing wording; generated guidance uses stopCommand rather
// than treating this descriptive phrase as runnable shell text.
func stopRemedy(rt, cname string) string { return restartActionFor(rt, cname).remedy }

// keyedStopRemedy is for native keeper refusals and reports: unlike an ordinary
// macos-user session restart, `yolo stop` signals every session of this workspace key.
func keyedStopRemedy(rt, cname string) string {
	if rt == "macos-user" { // parity: HonoredBy — `yolo stop` signals this workspace's native sessions and keeper; no container exists to stop
		return "'yolo stop' from this workspace"
	}
	return stopRemedy(rt, cname)
}

// mechanicsRestartRemedy describes restarting the current context without composing
// a jail/container action with a native session restart.
func mechanicsRestartRemedy(rt, cname string) string {
	if rt == "macos-user" { // parity: HonoredBy — end the current native session before starting fresh; there is no container to stop
		return "end this session, then run `yolo` from this workspace again"
	}
	return "restart this jail with " + stopRemedy(rt, cname) + ", then run `yolo` again"
}
