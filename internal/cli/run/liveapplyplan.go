package run

// liveapplyplan.go is the pure half of `yolo config apply` (docs/design/live-config-apply.md
// §4): which changes a running jail can take, per key and per backend, and the plan, which is
// one change per difference between what the jail has and what the config now asks for. It
// runs nothing and reads nothing; liveapply.go gathers its inputs and carries it out.
//
// Terms (the design's, §1): the EFFECTIVE CONFIG is the workspace config a running jail has,
// recorded in .yolo/config-boot.json; a PENDING CHANGE is approved but not in it; a LIVE-APPLY
// STEP moves one change from pending to effective.

import (
	"sort"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

// applyOutcome is the one outcome every change gets (LA-D4, LA-P3).
type applyOutcome int

const (
	// outcomeAppliedNow: effective in every session, running processes included.
	outcomeAppliedNow applyOutcome = iota + 1
	// outcomeNextSession: effective for every later `yolo` entry; running processes keep
	// what they have.
	outcomeNextSession
	// outcomeNextRestart: approved and recorded, not applied; `yolo stop`, then `yolo`.
	outcomeNextRestart
	// outcomeNextLaunch: no jail is running, so the next `yolo` launches with it.
	outcomeNextLaunch
)

// String is the outcome as its line says it. Every line that waits names the next step
// (docs/reference/happy-path-principle.md); the command never offers to restart (OQ-LA5).
func (a applyOutcome) String() string {
	switch a {
	case outcomeAppliedNow:
		return "applied now"
	case outcomeNextSession:
		return "from your next yolo session"
	case outcomeNextRestart:
		return "at next restart (yolo stop, then yolo)"
	case outcomeNextLaunch:
		return "at your next yolo launch"
	}
	return "unknown"
}

// pending reports whether the outcome leaves the change owed to a restart or a launch: the
// command's exit 3, as `yolo config drift`'s 3.
func (a applyOutcome) pending() bool { return a == outcomeNextRestart || a == outcomeNextLaunch }

// nextSessionKeys reach the next session on every backend: an attach already recomposes the
// provider, profile and env channel from the current config (design §4, the A rows).
var nextSessionKeys = map[string]bool{
	"env_sources": true, "providers": true, "profiles": true, "profile": true,
	"model_facts": true, "adapters": true,
}

// briefingKeys reach the next session through the briefing staging an attach re-renders; on
// Apple Container the briefing is copied once, at boot, so they wait for a restart there.
var briefingKeys = map[string]bool{"agents_md_extra": true, "briefing_provenance": true}

// keyOutcome is the table's row for a workspace key that is neither `mounts` nor the
// repository scope. Every key not named is restart-only. On macos-user every key reaches the
// next session, because every invocation there is a fresh sandbox that runs the gate.
func keyOutcome(rt, key string) (applyOutcome, string) {
	switch {
	case rt == "macos-user": // parity: HonoredBy — every macos-user invocation is a fresh launch that runs the gate, so each key reaches the next session
		return outcomeNextSession, ""
	case nextSessionKeys[key]:
		return outcomeNextSession, ""
	case briefingKeys[key] && rt == "container": // parity: Warned — Apple Container copies its briefing once; the outcome line says it waits for a restart
		return outcomeNextRestart, "Apple Container copies its briefing once, when the jail starts"
	case briefingKeys[key]:
		return outcomeNextSession, ""
	}
	return outcomeNextRestart, ""
}

// mountsOutcome is the `mounts` row. The backend is decided BEFORE the jail's own answer
// (live, reason: the C lane's ctxMountsLive), because Apple Container shares are fixed when
// its VM boots whatever its contract tags say.
func mountsOutcome(rt string, live bool, reason string) (applyOutcome, string) {
	switch {
	case rt == "macos-user": // parity: HonoredBy — macos-user has no mounts; the next session re-stages its links
		return outcomeNextSession, ""
	case rt == "container": // parity: Warned — Apple Container shares are fixed at VM boot; the outcome line says so
		return outcomeNextRestart, "Apple Container shares are fixed when its VM boots"
	case live:
		return outcomeAppliedNow, ""
	}
	return outcomeNextRestart, reason
}

// scopeChangeOutcome is the `brokered` and repository-scope row, over the live broker files of
// this jail (the B lane's LiveFiles): applied now only when every one of them takes live scope
// (LA-D11). macos-user's sessions each run their own gate and broker, so with no live session
// the change reaches the next one.
func scopeChangeOutcome(rt string, files []liveScopeFile) (applyOutcome, string) {
	if rt == "container" { // parity: Warned — Apple Container starts no broker; the outcome line says so
		return outcomeNextRestart, "no broker runs in this jail"
	}
	if len(files) == 0 {
		if rt == "macos-user" { // parity: HonoredBy — a macos-user session runs its own gate and broker at its next start
			return outcomeNextSession, ""
		}
		return outcomeNextRestart, "no running broker of this jail was found"
	}
	for _, f := range files {
		if !f.LiveScope {
			if rt == "macos-user" { // parity: HonoredBy — a macos-user session runs its own gate and broker at its next start
				return outcomeNextSession, "a running session's broker started before live scope existed"
			}
			return outcomeNextRestart, "this jail's broker started before it could take a scope change live"
		}
	}
	return outcomeAppliedNow, ""
}

// changeKind is what a change's live-apply step does.
type changeKind int

const (
	changeKey         changeKind = iota // no step of its own: the outcome is the table's
	changeMountAdd                      // inject one context mount
	changeMountRemove                   // detach one context mount
	changeScope                         // replace one loophole's live scope files' repositories
)

// applyChange is one entry of the plan, and later of the result.
type applyChange struct {
	kind  changeKind
	sign  string // "+" added, "-" removed, "~" changed
	label string
	// key is the top-level workspace key folded into the effective-config record once every
	// change carrying it applied (LA-D8); "" folds nothing.
	key     string
	outcome applyOutcome
	reason  string
	// note is a fact the line always carries, such as a removal's open-file caveat.
	note string
	// disclosure is a launch disclosure the change carries (a read-write mount's line).
	disclosure  bool
	mountAdd    configCtxMount
	mountRemove liveCtxMount
	// scope is the loophole whose files a changeScope step rewrites, and to what.
	scope scopeDelta
}

// line is the change's outcome line, in plain text: `+ /ctx/datasets (read-only): applied now`.
func (c applyChange) line() string {
	s := c.sign + " " + c.label + ": " + c.outcome.String()
	if c.reason != "" {
		s += " — " + c.reason
	}
	if c.note != "" {
		s += " — " + c.note
	}
	return s
}

// renderApplyChanges is each change's line, in plan order.
func renderApplyChanges(plan []applyChange) []string {
	out := make([]string, len(plan))
	for i, c := range plan {
		out[i] = c.line()
	}
	return out
}

// scopeDelta is one brokered loophole's scope as the apply sees it: what the gate approves
// (desired), and the live broker files of this jail holding what they hold now.
type scopeDelta struct {
	label, source string
	desired       []string
	files         []liveScopeFile
	// recordChanged is the gate's own answer when no live file was found: the scope differs
	// from the approval record. With no broker to compare against, that is the change.
	recordChanged bool
}

// addedRemoved is, over every live file, what the gate approves that a file lacks (added) and
// what a file holds that the gate does not approve (removed), each repository once, compared
// without case as the scope is. Empty both ways means every file already holds the approval.
func (d scopeDelta) addedRemoved() (added, removed []string) {
	lower := func(l []string) map[string]bool {
		m := map[string]bool{}
		for _, r := range l {
			m[strings.ToLower(r)] = true
		}
		return m
	}
	want := lower(d.desired)
	seenAdd, seenRm := map[string]bool{}, map[string]bool{}
	for _, f := range d.files {
		have := lower(f.File.Repos)
		for _, r := range d.desired {
			if k := strings.ToLower(r); !have[k] && !seenAdd[k] {
				seenAdd[k] = true
				added = append(added, r)
			}
		}
		for _, r := range f.File.Repos {
			if k := strings.ToLower(r); !want[k] && !seenRm[k] {
				seenRm[k] = true
				removed = append(removed, r)
			}
		}
	}
	return added, removed
}

// applyPlanInput is everything the plan is computed from, gathered by liveapply.go.
type applyPlanInput struct {
	rt      string
	running bool
	// boot is the effective-config record, bootOK false when there is none; current is the
	// workspace config on disk.
	boot    *jsonx.OrderedMap
	bootOK  bool
	current *jsonx.OrderedMap
	// mountsLive and mountsReason are the jail's own answer (the C lane's ctxMountsLive);
	// mountRecordOK says the host's record of its mounts was readable, and the adds and
	// removes are that record against what the launch's own reading would bind now.
	mountsLive    bool
	mountsReason  string
	mountRecordOK bool
	mountAdds     []configCtxMount
	mountRemoves  []liveCtxMount
	scopes        []scopeDelta
}

// openFileNote is a removal's caveat (design §4, M5): a lazy detach leaves open files readable.
const openFileNote = "a file an agent already holds open stays readable until it is closed"

// buildApplyPlan is the plan: mount elements first, then each loophole's scope, then every
// other changed workspace key in name order. That is also the order the steps run in.
func buildApplyPlan(in applyPlanInput) []applyChange {
	var plan []applyChange
	keys := changedKeys(in.boot, in.current)
	if !in.bootOK {
		// No record of what the jail has: nothing can be compared or folded, so the whole
		// workspace config waits for the launch that writes one.
		keys = nil
		if in.current != nil && in.current.Len() > 0 {
			plan = append(plan, applyChange{kind: changeKey, sign: "~", label: "workspace config",
				outcome: outcomeNextRestart,
				reason:  "this jail has no record of the config it has (.yolo/config-boot.json)"})
		}
	}
	changed := map[string]bool{}
	for _, k := range keys {
		changed[k] = true
	}

	// MOUNTS: per element where the host's record says what the jail has, else the key.
	mo, mwhy := mountsOutcome(in.rt, in.mountsLive, in.mountsReason)
	elements := 0
	if in.mountRecordOK && in.bootOK {
		for _, m := range in.mountAdds {
			plan = append(plan, applyChange{kind: changeMountAdd, sign: "+", label: mountLabel(m.dest, m.rw),
				key: "mounts", outcome: mo, reason: mwhy, disclosure: m.rw, mountAdd: m})
			elements++
		}
		for _, m := range in.mountRemoves {
			plan = append(plan, applyChange{kind: changeMountRemove, sign: "-", label: mountLabel(m.Dest, m.RW),
				key: "mounts", outcome: mo, reason: mwhy, note: openFileNote, mountRemove: m})
			elements++
		}
	}
	if changed["mounts"] && elements == 0 {
		// The key moved but no element did (a source that does not exist, which a launch skips
		// too), or the record cannot say: one line for the key.
		o, why := mo, mwhy
		if in.mountRecordOK && mo == outcomeAppliedNow {
			why = "nothing for this jail to bind"
		}
		plan = append(plan, applyChange{kind: changeKey, sign: "~", label: "mounts", key: "mounts",
			outcome: o, reason: why})
	}

	// SCOPE: one change per loophole whose live files differ from what the gate approves.
	scopeLines := 0
	for _, d := range in.scopes {
		o, why := scopeChangeOutcome(in.rt, d.files)
		if len(d.files) == 0 {
			if !d.recordChanged {
				continue
			}
			plan = append(plan, applyChange{kind: changeKey, sign: "~", label: d.label + " repository scope",
				key: "brokered", outcome: o, reason: why})
			scopeLines++
			continue
		}
		added, removed := d.addedRemoved()
		if len(added) == 0 && len(removed) == 0 {
			continue
		}
		var parts []string
		for _, r := range added {
			parts = append(parts, "+"+r)
		}
		for _, r := range removed {
			parts = append(parts, "-"+r)
		}
		c := applyChange{kind: changeScope, sign: "~",
			label: d.label + " repository scope (" + strings.Join(parts, ", ") + ")",
			key:   "brokered", outcome: o, reason: why, scope: d}
		if len(removed) > 0 {
			c.note = "a removed repository is refused at its next request"
		}
		plan = append(plan, c)
		scopeLines++
	}
	if changed["brokered"] && scopeLines == 0 {
		o, why := outcomeNextRestart, ""
		if len(in.scopes) > 0 {
			// A broker is in play and its live scope already holds what the entry asks for.
			o = outcomeAppliedNow
			why = "the running broker's scope already matches"
		} else if in.rt == "macos-user" { // parity: HonoredBy — every macos-user invocation is a fresh launch that runs the gate
			o = outcomeNextSession
		}
		plan = append(plan, applyChange{kind: changeKey, sign: "~", label: "brokered", key: "brokered",
			outcome: o, reason: why})
	}

	for _, k := range keys {
		if k == "mounts" || k == "brokered" {
			continue
		}
		o, why := keyOutcome(in.rt, k)
		plan = append(plan, applyChange{kind: changeKey, sign: keySign(in.boot, in.current, k),
			label: strings.Join(changedPaths(in.boot, in.current, k), ", "), key: k, outcome: o, reason: why})
	}

	if !in.running {
		for i := range plan {
			plan[i].outcome, plan[i].reason = outcomeNextLaunch, ""
		}
	}
	return plan
}

// mountLabel is a mount element as its line names it: `/ctx/datasets (read-only)`.
func mountLabel(dest string, rw bool) string {
	if rw {
		return dest + " (read-write)"
	}
	return dest + " (read-only)"
}

// changedKeys are the top-level keys whose canonical values differ between boot and current,
// sorted.
func changedKeys(boot, current *jsonx.OrderedMap) []string {
	seen := map[string]bool{}
	var keys []string
	for _, m := range []*jsonx.OrderedMap{boot, current} {
		if m == nil {
			continue
		}
		for _, k := range m.Keys() {
			if !seen[k] {
				seen[k] = true
				if !sameValue(boot, current, k) {
					keys = append(keys, k)
				}
			}
		}
	}
	sort.Strings(keys)
	return keys
}

// sameValue compares one key of two maps on the canonical snapshot form, so cosmetic order
// is not a change (as drift compares).
func sameValue(a, b *jsonx.OrderedMap, k string) bool {
	av, aok := applyMapGet(a, k)
	bv, bok := applyMapGet(b, k)
	if aok != bok {
		return false
	}
	as, err1 := jsonx.DumpsSnapshot(av)
	bs, err2 := jsonx.DumpsSnapshot(bv)
	return err1 == nil && err2 == nil && as == bs
}

func applyMapGet(m *jsonx.OrderedMap, k string) (any, bool) {
	if m == nil {
		return nil, false
	}
	return m.Get(k)
}

// keySign is "+" for a key the jail does not have, "-" for one the config dropped, "~" else.
func keySign(boot, current *jsonx.OrderedMap, k string) string {
	_, inBoot := applyMapGet(boot, k)
	_, inCur := applyMapGet(current, k)
	switch {
	case !inBoot:
		return "+"
	case !inCur:
		return "-"
	}
	return "~"
}

// changedPaths names a changed key down to its changed sub-keys when both sides are objects
// (`network.ports`), so a line says which setting moved; the key alone otherwise.
func changedPaths(boot, current *jsonx.OrderedMap, k string) []string {
	bv, _ := applyMapGet(boot, k)
	cv, _ := applyMapGet(current, k)
	bm, bok := bv.(*jsonx.OrderedMap)
	cm, cok := cv.(*jsonx.OrderedMap)
	if !bok || !cok {
		return []string{k}
	}
	sub := changedKeys(bm, cm)
	if len(sub) == 0 {
		return []string{k}
	}
	out := make([]string, len(sub))
	for i, s := range sub {
		out[i] = k + "." + s
	}
	return out
}
