package run

// liveapply.go is `yolo config apply` and the attach-time gate
// (docs/design/live-config-apply.md §3, §6, §8): the launch's own config-change gate taken to a
// running jail, then each approved change the jail can take applied in place, and every other
// one reported with the step that will apply it.
//
// THE ORDER IS THE SEMANTICS (LA-P2, LA-D12): the approval is recorded first, by the launch's
// gate (runConfigGate, the body of checkConfigChanges); then each live-apply step runs on its
// own, with its own deadline, and a step that fails leaves the trust captured and the change
// pending. Nothing is rolled back across steps.
//
// SEAMS. The live-apply steps reach the broker's scope files (brokerscope) and the mount
// injector (ctxinjectdrive.go, ctxmountrecord.go) through the package-level variables below, so
// a test can drive every outcome without a running jail or a broker.

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/brokerscope"
	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/richtext"
	"github.com/mschulkind-oss/yolo-jail/internal/runtime"
	"github.com/mschulkind-oss/yolo-jail/internal/termsafe"
)

// liveScopeFile is one live broker's scope file of this jail: lane B's brokerscope.LiveFile,
// with its LiveScope capability (LA-D11) lifted out.
type liveScopeFile struct {
	Path      string
	File      brokerscope.File
	LiveScope bool
}

// liveCtxMount is one config mount the host's record says the jail has.
type liveCtxMount = ctxMountEntry

// ctxMountsTokenEnvName is the jail-env variable carrying the token its mount record is
// written with.
const ctxMountsTokenEnvName = ctxMountsTokenEnv

var (
	liveScopeFilesSeam = func(source, container, workspace string) ([]liveScopeFile, error) {
		files, err := brokerscope.LiveFiles(source, container, workspace)
		out := make([]liveScopeFile, 0, len(files))
		for _, f := range files {
			out = append(out, liveScopeFile{Path: f.Path, File: f.File, LiveScope: f.File.LiveScope})
		}
		return out, err
	}
	replaceScopeReposSeam  = brokerscope.ReplaceRepos
	ctxMountsLiveSeam      = (*Options).ctxMountsLive
	readCtxMountRecordSeam = readCtxMountRecord
	// The token is the one the jail's launch wrote; updateCtxMountRecord keeps it, so a
	// rewrite can never adopt a record for a jail it was not written for.
	writeCtxMountRecordSeam = func(cname, _ string, entries []liveCtxMount) error {
		return updateCtxMountRecord(cname, entries)
	}
	diffCtxMountsSeam  = diffCtxMounts
	injectCtxMountSeam = (*Options).injectCtxMount
	removeCtxMountSeam = (*Options).removeCtxMount
)

// applyStepDeadline and applyBudget are §8's defaults: each live step gets 10 s, and the
// whole apply 60 s after the y. A step past either is at next restart, with the reason.
var (
	applyStepDeadline = 10 * time.Second
	applyBudget       = 60 * time.Second
)

// configApplyUsage is `yolo config apply --help`.
const configApplyUsage = `Usage: yolo config apply [--record-only] [--accept-config-changes]

Approve this workspace's config changes on the host, and apply to its running jail
what the jail can take without a restart. It asks the question a fresh launch asks
(the same diff, the same repository scope block, the same y/N, the same approval
record), then says, per change, whether it was applied now, reaches your next yolo
session, or waits for the next restart (yolo stop, then yolo). It never restarts
the jail. With no jail running it records the approval, and the next yolo launches
with the changes. HOST ONLY: in a jail it refuses.

Flags:
  --record-only            Approve and record; touch no running jail.
  --accept-config-changes  Approve without a terminal, as on a launch.
  --help, -h               Show this help.

Exit: 0 nothing pending, 3 approved with a restart still owed, 1 declined,
refused or failed before the approval was recorded, 2 misuse.`

// ConfigApply is `yolo config apply`, registered in internal/cli's config verbs. args is
// everything after `apply`; color is the config command's one color decision.
func ConfigApply(args []string, stdout, stderr io.Writer, color bool) int {
	recordOnly, accept := false, false
	for _, a := range args {
		switch a {
		case "--help", "-h", "help":
			_, _ = io.WriteString(stdout, configApplyUsage+"\n")
			return 0
		case "--record-only":
			recordOnly = true
		case config.AcceptConfigChangesFlag:
			accept = true
		default:
			fmt.Fprintf(stderr, "yolo config apply: unknown argument %q\n\n%s\n", a, configApplyUsage)
			return 2
		}
	}
	opts := NewDefaultOptions()
	opts.Color = color
	opts.Stdout, opts.Stderr = stdout, stderr
	opts.AcceptConfigChanges = accept
	fillDefaults(&opts)
	return opts.configApply(recordOnly)
}

// applyMode is how much of the sequence an apply may run.
type applyMode struct {
	// running: this workspace's jail is known to be running (or, on macos-user, sessions
	// may be). false with recordOnly "" is "known not running".
	running bool
	// recordOnly, when set, is why nothing is applied: --record-only, or a runtime that
	// could not be asked. The approval is still recorded.
	recordOnly string
	// unknown: the runtime could not say whether the jail runs, so nothing is folded.
	unknown bool
	// attach: an attach's gate, whose N starts the session anyway (LA-D14).
	attach bool
}

// configApply is the command's sequence (design §3.2): refuse in a jail, load and validate the
// config, resolve the runtime, hold the launch lock for the whole run, probe the jail
// tri-state, then the gate and the apply.
func (o *Options) configApply(recordOnly bool) int {
	// THE JAIL'S OWN WORKSPACE IS REFUSED, not every in-jail run (LA-D17): approving this jail's
	// config from inside it is the side being approved approving itself (OQ-S2). A workspace a
	// launch inside the jail opens is a nested jail, whose launch already runs its gate in here,
	// so its apply is no wider than that launch.
	if o.inJail() && config.IsJailOwnWorkspace(o.Workspace) {
		out := o.pr(o.Stderr)
		out.print("[bold red]yolo config apply runs on the host, not in this jail: recording an approval " +
			"must not be reachable from the side being approved.[/bold red]")
		out.print("[dim]Ask the user to run `yolo config apply` on the host, in this workspace. " +
			"It shows them the change and applies what the running jail can take.[/dim]")
		return 1
	}
	cfg, ok := o.loadAndValidateConfig()
	if !ok {
		return 1
	}
	rt, ok := o.resolveRuntime(cfg)
	if !ok {
		return 1
	}
	cname := runtime.FromWorkspace(o.Workspace)
	// THE LAUNCH LOCK, prompt included (LA-D3): a launch or attach that arrives meanwhile
	// waits with the existing line, and this apply never interleaves with one.
	o.holdLaunchLock(cname)
	defer o.releaseLaunchLock()

	mode := applyMode{running: true}
	var envLines []string
	if recordOnly {
		mode.recordOnly = "recorded only (--record-only)"
	}
	if rt != "macos-user" { // parity: HonoredBy — macos-user has no container to probe; its live sessions are found by their broker files
		cid, known, failure := o.probeJailForApply(cname, rt)
		switch {
		case !known:
			mode.running, mode.unknown = false, true
			mode.recordOnly = "could not ask " + rt + " whether this jail is running"
			out := o.pr(o.Stdout)
			out.printf("[yellow]Could not ask %s whether this workspace's jail (%s) is running: %s[/yellow]",
				rt, cname, richtext.Escape(termsafe.Visible(failure)))
			out.printf("[dim]Recording the approval only. Run `%s` to see why, then run "+
				"`yolo config apply` again to apply what the jail can take.[/dim]", runningListCommand(rt))
		case cid == "":
			mode.running = false
			o.pr(o.Stdout).print("[cyan]No jail is running for this workspace: recording the approval " +
				"only, and the next `yolo` here launches with these changes.[/cyan]")
		default:
			envLines, _ = o.inspectContainerEnv(rt, cname)
		}
	}
	return o.liveApply(cfg, rt, cname, envLines, mode)
}

// probeJailForApply is the launch's tri-state probe (probeRunningContainer), with the
// runtime's own words kept for the "could not ask" line.
func (o *Options) probeJailForApply(cname, rt string) (cid string, known bool, failure string) {
	var last ExecResult
	var lastArgv []string
	exec := o.Exec
	o.Exec = func(argv []string, dir string, env []string, timeout time.Duration) ExecResult {
		r := exec(argv, dir, env, timeout)
		last, lastArgv = r, argv
		return r
	}
	cid, known = o.probeRunningContainer(cname, rt, attachProbeTimeout)
	o.Exec = exec
	if !known {
		failure = runtimeProbeFailure(strings.Join(lastArgv, " "), last)
	}
	return cid, known, failure
}

// applyRun is one apply's inputs, kept from the plan to the steps.
type applyRun struct {
	cfg      *jsonx.OrderedMap
	rt       string
	cname    string
	envLines []string
	mode     applyMode
	current  *jsonx.OrderedMap
	have     []liveCtxMount
}

// liveApply is §3.2's steps 3 to 8, which an interactive attach runs too: the plan, the
// launch's gate with the plan printed above its question, the record (the gate's), each live
// step, the fold into the effective-config record, the result, and the exit.
func (o *Options) liveApply(cfg *jsonx.OrderedMap, rt, cname string, envLines []string, mode applyMode) int {
	run := &applyRun{cfg: cfg, rt: rt, cname: cname, envLines: envLines, mode: mode}
	var plan []applyChange
	hooks := &gateHooks{attach: mode.attach, plan: func(scope *config.ScopeCheck) []string {
		plan = o.applyPlan(run, scope)
		if len(plan) == 0 {
			return nil
		}
		return append([]string{"[bold]If you answer y, each change:[/bold]"}, o.applyBlock(plan, rt)...)
	}}
	if !o.runConfigGate(cfg, rt, hooks) {
		if mode.attach {
			return 0
		}
		return 1
	}
	if len(plan) == 0 {
		if !mode.attach {
			o.pr(o.Stdout).print("[green]Nothing to apply: the jail already has this workspace's config.[/green]")
		}
		return 0
	}
	result := o.runApplySteps(run, plan)
	out := o.pr(o.Stdout)
	out.print("[bold]Config apply result:[/bold]")
	for _, line := range o.applyBlock(result, rt) {
		out.print(line)
	}
	rc := 0
	for _, c := range result {
		if c.outcome.pending() {
			rc = 3
		}
	}
	if rc == 3 && mode.running && !mode.unknown {
		out.print("[dim]A restart ends every session of this jail, so yolo never does it for you: when " +
			"they are done, run `yolo stop`, then `yolo`. The approval is recorded, so the launch asks " +
			"nothing.[/dim]")
	}
	return rc
}

// applyPlan gathers the plan's inputs (applyPlanInput) and builds it. scope is the gate's own
// scope check; nothing here writes.
func (o *Options) applyPlan(run *applyRun, scope *config.ScopeCheck) []applyChange {
	current, err := config.LoadWorkspaceConfig(o.Workspace, false, func(string) {})
	if err != nil || current == nil {
		current = jsonx.NewOrderedMap()
	}
	run.current = current
	boot, bootOK, _ := config.ReadWorkspaceBootBaseline(o.Workspace)
	in := applyPlanInput{rt: run.rt, running: run.mode.running || run.mode.unknown, boot: boot,
		bootOK: bootOK, current: current}
	if run.rt == "podman" && run.mode.running { // parity: Warned — live mounts are podman only; elsewhere the mounts line says why it waits
		in.mountsLive, in.mountsReason = ctxMountsLiveSeam(o, run.rt, run.cname, run.envLines)
		if have, ok := readCtxMountRecordSeam(run.cname, run.envLines); ok {
			run.have = have
			in.mountRecordOK = true
			in.mountAdds, in.mountRemoves = diffCtxMountsSeam(have, o.configCtxMounts(run.rt, run.cfg, nil))
		}
	}
	if scope != nil {
		changedLabels := map[string]bool{}
		if r, err := config.ScopeChanges(o.Workspace, scope); err == nil {
			for _, l := range r.ScopeLabels {
				changedLabels[l] = true
			}
		}
		for _, s := range scope.Sources {
			d := scopeDelta{label: s.Label, source: s.Source, desired: s.Repos(),
				recordChanged: changedLabels[s.Label]}
			if run.mode.running && run.rt != "container" { // parity: Warned — Apple Container starts no broker; the scope line says so
				d.files, _ = liveScopeFilesSeam(s.Source, run.cname, o.Workspace)
			}
			in.scopes = append(in.scopes, d)
		}
	}
	plan := buildApplyPlan(in)
	if run.mode.recordOnly != "" {
		for i := range plan {
			if plan[i].outcome == outcomeAppliedNow {
				plan[i].outcome, plan[i].reason = outcomeNextRestart, run.mode.recordOnly
			}
		}
	}
	return plan
}

// applyBlock renders the outcome block: one line per change, colored by outcome, each with
// the launch disclosure it carries.
func (o *Options) applyBlock(plan []applyChange, rt string) []string {
	var lines []string
	for _, c := range plan {
		color := "yellow"
		switch c.outcome {
		case outcomeAppliedNow:
			color = "green"
		case outcomeNextSession:
			color = "cyan"
		}
		lines = append(lines, "  ["+color+"]"+richtext.Escape(termsafe.Visible(c.line()))+"[/"+color+"]")
		if c.disclosure && c.kind == changeMountAdd {
			lines = append(lines, "    "+rwMountDisclosure(c.mountAdd, o.rootfulPodmanHost(rt)))
		}
	}
	return lines
}

// runApplySteps runs each applied-now change's step in plan order, under its deadline and the
// apply's budget, and folds each key whose every change took effect. It returns the plan with
// each change's outcome as it ended.
func (o *Options) runApplySteps(run *applyRun, plan []applyChange) []applyChange {
	res := append([]applyChange(nil), plan...)
	budgetEnd := time.Now().Add(applyBudget)
	stopped := false
	token := envLineValue(run.envLines, ctxMountsTokenEnvName)
	have := append([]liveCtxMount(nil), run.have...)
	for i := range res {
		c := &res[i]
		if c.outcome != outcomeAppliedNow || c.kind == changeKey {
			continue
		}
		if stopped {
			c.outcome, c.reason = outcomeNextLaunch, "jail stopped; applies at next launch"
			continue
		}
		left := time.Until(budgetEnd)
		if left <= 0 {
			c.outcome, c.reason = outcomeNextRestart, fmt.Sprintf("the apply's %s budget ran out", applyBudget)
			continue
		}
		deadline := min(applyStepDeadline, left)
		err := runWithDeadline(deadline, func() error { return o.applyStep(run, c) })
		if err != nil {
			if run.rt != "macos-user" { // parity: HonoredBy — macos-user's steps are scope files, not a container that can stop
				if cid, known := o.probeRunningContainer(run.cname, run.rt, attachProbeTimeout); known && cid == "" {
					stopped = true
					c.outcome, c.reason = outcomeNextLaunch, "jail stopped; applies at next launch"
					continue
				}
			}
			c.outcome, c.reason = outcomeNextRestart, err.Error()
			continue
		}
		switch c.kind {
		case changeMountAdd:
			have = append(have, liveCtxMount{Source: c.mountAdd.source, Dest: c.mountAdd.dest, RW: c.mountAdd.rw})
			o.noteMountRecord(run, token, have, c)
		case changeMountRemove:
			kept := have[:0:0]
			for _, h := range have {
				if h != c.mountRemove {
					kept = append(kept, h)
				}
			}
			have = kept
			o.noteMountRecord(run, token, have, c)
		}
	}
	o.foldApplied(run, res)
	return res
}

// noteMountRecord rewrites the host's record of the jail's mounts after one step, so the next
// apply's diff starts from what the jail has. A record that cannot be written is said on the
// change's line: the mount is in the jail either way.
func (o *Options) noteMountRecord(run *applyRun, token string, have []liveCtxMount, c *applyChange) {
	if err := writeCtxMountRecordSeam(run.cname, token, have); err != nil {
		note := "the host's record of this jail's mounts was not updated (" + err.Error() + ")"
		if c.note != "" {
			note = c.note + "; " + note
		}
		c.note = note
	}
}

// applyStep is one change's live-apply step.
func (o *Options) applyStep(run *applyRun, c *applyChange) error {
	switch c.kind {
	case changeMountAdd:
		return injectCtxMountSeam(o, run.rt, run.cname, c.mountAdd)
	case changeMountRemove:
		return removeCtxMountSeam(o, run.rt, run.cname, c.mountRemove)
	case changeScope:
		// THE SCOPE THE GATE APPROVED, its in-memory result (WW-P2), never a re-read.
		approved, ok := o.approvedScopes[c.scope.label]
		if !ok {
			return errors.New("no repository scope was approved for " + c.scope.label + " in this apply")
		}
		repos := make([]string, len(approved))
		for i, r := range approved {
			repos[i] = r.Repo
		}
		for _, f := range c.scope.files {
			if err := replaceScopeReposSeam(f.Path, f.File, repos); err != nil {
				return err
			}
		}
		return nil
	}
	return nil
}

// runWithDeadline runs fn and gives up waiting at d: a step past its deadline is reported as
// such and left to finish or fail on its own (lane C's injector has its own deadline inside).
func runWithDeadline(d time.Duration, fn func() error) error {
	done := make(chan error, 1)
	go func() { done <- fn() }()
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case err := <-done:
		return err
	case <-t.C:
		return fmt.Errorf("did not finish within %s", d)
	}
}

// foldApplied folds into the effective-config record every key whose every change took effect
// (LA-D8): applied now, or reaching the next session. Container backends only, and only for a
// jail known running; macos-user writes none (each session carries its own digest).
func (o *Options) foldApplied(run *applyRun, res []applyChange) {
	if run.rt == "macos-user" || !run.mode.running || run.mode.unknown || run.current == nil { // parity: HonoredBy — macos-user writes no record: each session carries its own baseline digest
		return
	}
	ok := map[string]bool{}
	var order []string
	for _, c := range res {
		if c.key == "" {
			continue
		}
		if _, seen := ok[c.key]; !seen {
			ok[c.key] = true
			order = append(order, c.key)
		}
		if c.outcome != outcomeAppliedNow && c.outcome != outcomeNextSession {
			ok[c.key] = false
		}
	}
	for _, k := range order {
		if !ok[k] {
			continue
		}
		if err := config.FoldBootBaselineKey(o.Workspace, k, run.current); err != nil {
			o.pr(o.Stdout).printf("[dim]Warning: could not record %s as applied in %s: %s; `yolo config "+
				"drift` will still list it.[/dim]", k, config.WorkspaceConfigBootPath(o.Workspace),
				richtext.Escape(err.Error()))
		}
	}
}

// attachPendingNotice is the one line a non-interactive attach prints when the workspace config
// moved past what the jail has or what was approved (LA-D9).
const attachPendingNotice = "This workspace's config has changes this jail does not have; nothing was " +
	"asked or applied for this session. Run `yolo config apply` on the host to approve and apply " +
	"them, or `yolo config drift` to see what differs."

// applyAtAttach is the attach-time gate (design §3.3), called by attachExisting with the launch
// lock held, after the session is counted and before the briefings are refreshed. A human is
// there when stdin is a terminal, the launch gate's own predicate (LA-D13), or the attach
// carried the launch's --accept-config-changes: then it is the command's sequence, and an N
// starts the session anyway. Otherwise it asks nothing and prints one notice when something is
// pending. Nothing here can fail the attach (LA-D14).
func (o *Options) applyAtAttach(cname, rt string, cfg *jsonx.OrderedMap, envLines []string) {
	if rt == "macos-user" { // parity: NotApplicable — macos-user has no attach: every invocation is a fresh launch, which runs the gate
		return
	}
	if o.IsTTYStdin() || o.AcceptConfigChanges {
		o.liveApply(cfg, rt, cname, envLines, applyMode{running: true, attach: true})
		return
	}
	if o.configChangesPending(rt, cfg) {
		o.pr(o.Stdout).print("[yellow]" + attachPendingNotice + "[/yellow]")
	}
}

// configChangesPending is the non-interactive attach's cheap check, with no side effect: the
// effective-config record drifted from the workspace config, or the approval record differs
// from it, config part or scope part.
func (o *Options) configChangesPending(rt string, cfg *jsonx.OrderedMap) bool {
	if _, drifted, _, err := config.WorkspaceConfigDrift(o.Workspace); err == nil && drifted {
		return true
	}
	read, err := config.ReadWorkspaceForGate(o.Workspace)
	if err != nil {
		return false
	}
	if changed, err := config.ConfigPartChanged(o.Workspace, read.Config); err == nil && changed {
		return true
	}
	lps := o.brokeredToStart(rt, cfg)
	if len(lps) == 0 {
		return false
	}
	brokers := make([]config.ScopeBroker, 0, len(lps))
	for _, lp := range lps {
		brokers = append(brokers, config.ScopeBroker{Source: lp.Brokered.Source, Label: lp.Name,
			RemoteHost: lp.Brokered.RemoteHost})
	}
	r, err := config.ScopeChanges(o.Workspace, config.NewScopeCheck(o.Workspace, brokers, read))
	return err == nil && r.ScopeChanged
}
