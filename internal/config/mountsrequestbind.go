package config

// mountsrequestbind.go is what RC-D24's approval covers and what the launch binds
// (docs/reference/context-mounts.md#a-project-may-request-one-once-your-config-allows-it).
//
// THE APPROVAL IS OF THE RESOLVED PATH. A request names a host path the agent wrote, and a path
// can pass through a symbolic link: approve "<link>" and the agent repoints it at ~/.ssh before
// the next launch. So the approval record's config part carries every writable element's host
// path as it resolved when the gate read it (writableRecordKey), the prompt shows that path
// beside the written one, a retarget changes the record's bytes and asks again, and the launch
// binds the approved resolved path, refusing when the written one no longer resolves to it
// (WritableRequest.Drift).
//
// THE BIND IS OF THE GATE'S READ (WW-P2's rule for broker scope, applied to mounts). The run
// pipeline never re-reads the workspace files to learn what to mount: the gate's read is the
// one the human approved, and CheckWritableRequests runs every refusal on exactly that read
// before the question is asked.
//
// THE HOST NEVER FOLLOWS A LINK THE JAIL CAN WRITE (P5, the owner's governing rule;
// requestPathRefusal). The WRITTEN path is judged first, before any link is followed: inside the
// workspace, yolo's state directory, or another writable mount, it is refused, since the jail
// already writes what is there and any link there is the agent's. A written path outside them is
// then walked, and refused if a link it passes through sits in one of them. Re-checking at the
// bind catches a link moved after the gate; refusing a jail-writable link closes the window
// between that check and podman opening the path, which no re-check can.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/brokerscope"
	"github.com/mschulkind-oss/yolo-jail/internal/configdoc"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/termsafe"
)

// writableRecordKey is the approval record's entry for the writable elements' resolved host
// paths. It is not a config key (a space and parentheses keep it from being one), and it is
// present only when the config holds a writable element, so every other record's bytes are
// unchanged.
const writableRecordKey = "(writable host mounts, resolved)"

// WritableRequest is one writable `mounts` element as the config-change gate read it.
type WritableRequest struct {
	// Mount is the element as written.
	Mount ContextMount
	// Resolved is its host path with "~" expanded and every symbolic link resolved, when the gate
	// read it: what the human approves and what the launch binds.
	Resolved string
}

// resolveRequestHost is host as the gate resolves it: "~" expanded, every link in the longest
// existing prefix resolved (a source not there yet is judged by where it would be).
func resolveRequestHost(host string) string {
	return resolveExistingPrefix(expandUser(host))
}

// Dest is the jail path the request lands at.
func (r WritableRequest) Dest() string { return r.Mount.DestFor(r.Resolved) }

// Line is the request as the gate shows it: the resolved host path and the jail path, and the
// path as written when it differs. Every part is made terminal-safe; the agent wrote them.
func (r WritableRequest) Line() string {
	s := termsafe.Visible(r.Resolved) + " → " + termsafe.Visible(r.Dest())
	if filepath.Clean(expandUser(r.Mount.Host)) != r.Resolved {
		s += " (written as " + termsafe.Visible(r.Mount.Host) + ")"
	}
	return s
}

// recordEntry is the request's line in the approval record (writableRecordKey).
func (r WritableRequest) recordEntry() string {
	return WritableMountMarker + " " + r.Resolved + " -> " + r.Dest()
}

// Drift is "" when the request's host path still resolves to the path the gate approved, and
// otherwise the refusal: the launch binds nothing for a request whose target moved.
func (r WritableRequest) Drift() string {
	now := resolveRequestHost(r.Mount.Host)
	if now == r.Resolved {
		return ""
	}
	return fmt.Sprintf("The writable mount %s now resolves to %s, not to %s, which is the path "+
		"that was approved. Nothing was mounted for it. To use the new path, launch again: the "+
		"config-change prompt shows it so you can approve or decline it",
		termsafe.Visible(r.Mount.Host), termsafe.Visible(now), termsafe.Visible(r.Resolved))
}

// isWorkspaceRequest reports whether the element n was written by yolo-jail.toml or
// yolo-jail.local.toml itself, and by nothing else: a request is honored only from the readable
// project files (RC-D24, "don't need to backport that to old configs"). Not from a TOML file a
// JSONC workspace includes, which would backport it, and not from a fragment a TOML file
// includes, so the file a human reviews is the file that asks.
func isWorkspaceRequest(n *srcNode) bool {
	writers := n.writerList()
	if len(writers) == 0 {
		return false
	}
	for _, w := range writers {
		f := w.file
		if f == nil || f.format != configdoc.TOML || f.via != "" {
			return false
		}
		if f.role != editRoleProject && f.role != editRoleLocal {
			return false
		}
		if base := filepath.Base(f.path); base != WorkspaceTOMLConfigName && base != WorkspaceLocalTOMLConfigName {
			return false
		}
	}
	return true
}

// WritableRequests are the writable mounts this read's project files request
// (isWorkspaceRequest), each resolved now. It is the set the gate asks about and, once
// approved, the launch binds.
func (r *WorkspaceRead) WritableRequests() []WritableRequest {
	if r == nil || r.Config == nil {
		return nil
	}
	elements, indices := parseMountsIndexed(r.Config)
	node := r.sources.node().key(mountsKey)
	var out []WritableRequest
	for n, m := range elements {
		if m.RW && isWorkspaceRequest(node.elem(indices[n])) {
			out = append(out, WritableRequest{Mount: m, Resolved: resolveRequestHost(m.Host)})
		}
	}
	return out
}

// writableElements are every writable element of cfg, requested or not, each resolved through
// resolved (a host path the gate already resolved) or now: what the record and the prompt show.
// A writable element that is not a request is refused by validation, but if one reaches the
// gate it is still shown loud.
func writableElements(cfg *jsonx.OrderedMap, resolved map[string]string) []WritableRequest {
	if cfg == nil {
		return nil
	}
	var out []WritableRequest
	for _, m := range ParseMounts(cfg) {
		if !m.RW {
			continue
		}
		r, ok := resolved[m.Host]
		if !ok {
			r = resolveRequestHost(m.Host)
		}
		out = append(out, WritableRequest{Mount: m, Resolved: r})
	}
	return out
}

func resolvedHosts(reqs []WritableRequest) map[string]string {
	out := map[string]string{}
	for _, r := range reqs {
		out[r.Mount.Host] = r.Resolved
	}
	return out
}

func requestLines(reqs []WritableRequest) []string {
	var out []string
	for _, r := range reqs {
		out = append(out, r.Line())
	}
	return out
}

// withWritableRecord is part with the writable elements' resolved paths added under
// writableRecordKey; part itself is never changed.
func withWritableRecord(part, cfg *jsonx.OrderedMap, resolved map[string]string) *jsonx.OrderedMap {
	reqs := writableElements(cfg, resolved)
	if len(reqs) == 0 {
		return part
	}
	out := jsonx.NewOrderedMap()
	for _, k := range part.Keys() {
		v, _ := part.Get(k)
		out.Set(k, v)
	}
	entries := make([]any, 0, len(reqs))
	for _, r := range reqs {
		entries = append(entries, r.recordEntry())
	}
	out.Set(writableRecordKey, entries)
	return out
}

// CheckWritableRequests is every refusal a request meets, run on exactly what the gate read: the
// user's ceiling, the jail-writable path rule (requestPathRefusal), the refusal set
// (rwMountRefusal) on the resolved path, and the broker mount fence. Each is also a `yolo check`
// error, but the launch validated an earlier read; the gate's read is the one approved and bound.
// None in a jail for its own workspace, whose sources are host paths the host already judged.
func CheckWritableRequests(workspace string, reqs []WritableRequest, resolver LoopholeResolver) []string {
	if len(reqs) == 0 || (inJail() && jailOwnWorkspace(workspace)) {
		return nil
	}
	var out []string
	allowed := userAllowsWritableMountRequests(validationUserScope())
	fences := brokerMountFences(resolver)
	for i, r := range reqs {
		if !allowed {
			out = append(out, writableMountRequestRefusal(r.Mount))
			continue
		}
		if why := requestPathRefusal(r, workspace, otherRequests(reqs, i)); why != "" {
			out = append(out, why)
			continue
		}
		if why := rwMountRefusal(r.Resolved, workspace); why != "" {
			out = append(out, fmt.Sprintf("config.mounts: %s is refused: %s", termsafe.Visible(r.Mount.Spec), why))
		}
		if f, ok := fences.reach(r.Resolved); ok {
			out = append(out, workspaceFenceRefusal(r.Mount.Spec, f))
		}
	}
	return out
}

func otherRequests(reqs []WritableRequest, skip int) []WritableRequest {
	out := make([]WritableRequest, 0, len(reqs))
	for i, r := range reqs {
		if i != skip {
			out = append(out, r)
		}
	}
	return out
}

// writableTree is one host directory a jail launched for a workspace can write.
type writableTree struct{ path, what string }

type writableTrees []writableTree

// jailWritableTrees are the host directories a jail launched for workspace can write, each both
// as written and as resolved: the workspace (the jail's home overlay is inside it), yolo's state
// directory (the jails' shared home and the cache every jail mounts read-write are inside it),
// the user's own writable mounts, and others, the project's other requests.
func jailWritableTrees(workspace string, others []WritableRequest) writableTrees {
	trees := writableTrees{{workspaceOrCwd(workspace), "the workspace"},
		{paths.GlobalStorage(), "yolo's state directory, which holds the jails' shared home and cache"}}
	if user, err := LoadRWMounts(nil); err == nil {
		for _, m := range user {
			trees = append(trees, writableTree{expandUser(m.Host), "your writable mount " + m.Spec})
		}
	}
	for _, r := range others {
		trees = append(trees, writableTree{expandUser(r.Mount.Host), "the writable mount " + r.Mount.Spec})
	}
	var out writableTrees
	for _, t := range trees {
		lexical := filepath.Clean(t.path)
		if !filepath.IsAbs(lexical) {
			continue
		}
		out = append(out, writableTree{lexical, t.what})
		if real := resolveExistingPrefix(lexical); real != lexical {
			out = append(out, writableTree{real, t.what})
		}
	}
	return out
}

// containing is the first tree p is in or equal to.
func (trees writableTrees) containing(p string) (writableTree, bool) {
	for _, t := range trees {
		if pathWithin(p, t.path) {
			return t, true
		}
	}
	return writableTree{}, false
}

// requestPathRefusal is P5, the host never follows a link the jail can write, for one request:
// "" when it holds, else the refusal with its next step.
//
//  1. The WRITTEN path ("~" expanded, cleaned, no link followed) inside a jail-writable tree is
//     refused: the jail already sees what is there, and any link there is the agent's.
//  2. Otherwise the path is walked, and refused if a symbolic link it passes through sits inside
//     a jail-writable tree, where the agent could repoint it.
//
// The resolved path is then what the gate shows, records and binds (WritableRequest).
func requestPathRefusal(r WritableRequest, workspace string, others []WritableRequest) string {
	trees := jailWritableTrees(workspace, others)
	written := filepath.Clean(expandUser(r.Mount.Host))
	if t, in := trees.containing(written); in {
		return fmt.Sprintf("config.mounts: %s is inside %s (%s), which the jail can already write: "+
			"a writable mount there would only be a second name for it, and a link there is the "+
			"agent's to repoint. Write a directory outside it as `host`, or remove `write = true`",
			termsafe.Visible(r.Mount.Spec), t.what, termsafe.Visible(t.path))
	}
	link, target := linkInTrees(written, trees)
	if link == "" {
		return ""
	}
	return fmt.Sprintf("config.mounts: %s reaches its host directory through the symbolic link %s, "+
		"which sits in a directory the jail can write, so the agent could repoint it at any time and "+
		"the path you approve would be the agent's to change. Write the directory it points to, %s, "+
		"as `host` instead, or remove `write = true`",
		termsafe.Visible(r.Mount.Spec), termsafe.Visible(link), termsafe.Visible(target))
}

// linkInTrees walks p one component at a time and returns the first symbolic link whose
// containing directory is inside one of trees, with the path it resolves to; "" when there is
// none. A missing component ends the walk: nothing past it exists to be a link.
func linkInTrees(p string, trees writableTrees) (link, target string) {
	p = filepath.Clean(p)
	if !filepath.IsAbs(p) {
		return "", ""
	}
	cur := string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(p, cur), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		next := filepath.Join(cur, part)
		fi, err := os.Lstat(next)
		if err != nil {
			return "", ""
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			if _, in := trees.containing(resolveExistingPrefix(cur)); in {
				return next, resolveExistingPrefix(next)
			}
			resolvedNext, err := filepath.EvalSymlinks(next)
			if err != nil {
				return "", ""
			}
			next = resolvedNext
		}
		cur = next
	}
	return "", ""
}

// ApproveWorkspaceRead is the launch's config-change gate over its own read (ApproveConfigAndScope
// with the read's writable requests): it resolves each request ONCE, records those paths in the
// approval record, shows them, and returns them as what was approved, so the launch binds the
// paths the human saw. approved is nil unless ok.
func ApproveWorkspaceRead(workspace string, read *WorkspaceRead, scope *ScopeCheck, isTTY, acceptNonInteractive bool,
	prompter ChangePrompter) (ok bool, scopes map[string][]ScopeRepo, approved []WritableRequest, err error) {
	reqs := read.WritableRequests()
	var cfg *jsonx.OrderedMap
	if read != nil {
		cfg = read.Config
	}
	ok, scopes, err = approveConfigAndScope(workspace, cfg, resolvedHosts(reqs), scope, isTTY, acceptNonInteractive, prompter)
	if !ok || err != nil {
		return ok, scopes, nil, err
	}
	return ok, scopes, reqs, nil
}

// ApprovedWritableRequests are the workspace's requests as the gate would read them now, when the
// approval record already holds them exactly (resolved paths included), and none otherwise. It is
// what a path that asks nothing (an attach, its briefing, the pending-change check) may treat as
// approved: a request the record does not hold is not.
func ApprovedWritableRequests(workspace string) []WritableRequest {
	read, err := ReadWorkspaceForGate(workspace)
	if err != nil {
		return nil
	}
	reqs := read.WritableRequests()
	if len(reqs) == 0 {
		return nil
	}
	current, err := SnapshotJSON(approvalConfigPartWith(read.Config, resolvedHosts(reqs)))
	if err != nil {
		return nil
	}
	old, err := os.ReadFile(ApprovalSnapshotPath(workspace))
	if err != nil || pyRstrip(string(old)) != current {
		return nil
	}
	return reqs
}

// mountFence is one path the broker mount fence keeps out of a workspace mount.
type mountFence struct{ path, owner, what string }

// mountFences are the broker mount fence's paths for one resolver.
type mountFences []mountFence

// brokerMountFences are the fenced paths with a selected pack shipping a brokered loophole: yolo's
// broker directory and each such loophole's credential paths. None without a resolver, in a jail,
// or with no brokered loophole.
func brokerMountFences(resolver LoopholeResolver) mountFences {
	if resolver == nil || inJail() {
		return nil
	}
	known, _ := resolver.Known()
	var names []string
	for name, info := range known {
		if info.Brokered != nil {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	sortStrings(names)
	home := os.Getenv("HOME")
	fences := mountFences{{paths.BrokerDir(), strings.Join(names, ", "),
		"yolo's broker directory: the brokers' store, each launch's repository scope and the " +
			"audit log of every workspace's brokered calls"}}
	for _, name := range names {
		for _, p := range brokerscope.FencedPaths(known[name].Brokered.CredentialPaths, home, os.Getenv)[1:] {
			fences = append(fences, mountFence{p, name, "the host credential the " + name +
				" loophole keeps out of the jail"})
		}
	}
	return fences
}

// reach is the fence source reaches, if any.
func (fs mountFences) reach(source string) (mountFence, bool) {
	if len(fs) == 0 {
		return mountFence{}, false
	}
	fencedPaths := make([]string, len(fs))
	for i, f := range fs {
		fencedPaths[i] = f.path
	}
	reached := brokerscope.Reaches(source, fencedPaths)
	if reached == "" {
		return mountFence{}, false
	}
	for _, f := range fs {
		if f.path == reached {
			return f, true
		}
	}
	return mountFence{}, false
}

// workspaceFenceRefusal is the fence's refusal for a workspace entry spec reaching f.
func workspaceFenceRefusal(spec string, f mountFence) string {
	return fmt.Sprintf("config.mounts: %q reaches %s, %s (%s). A workspace config may not "+
		"mount it: the agent can edit that file, and the mount would carry it into the next "+
		"launch. Remove the entry; the same entry in %s is allowed and disclosed",
		spec, f.path, f.what, f.owner, paths.UserConfigPath())
}
