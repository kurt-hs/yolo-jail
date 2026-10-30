package run

// ctxmounts.go is how a launch reads the config `mounts` key and a pack's `mount` grants:
// which ones this launch delivers, at which jail path, in which mode, and what it says
// about each (docs/reference/context-mounts.md#read-write-context-mounts).
//
// ONE READING, THREE CONSUMERS: the container argv (assembleRunCmd), the agent's briefing
// (prepare) and the read-write disclosure. The briefing listing a mount the argv skipped is
// backend-parity.md §6's defect — a jail told about a /ctx path that does not exist — so
// both call the same function rather than agreeing by construction.
//
// ⚠ READ-WRITE ELEMENTS COME FROM THE USER SCOPE (config.LoadRWMounts) AND FROM THE TOML
// WORKSPACE REQUESTS THE CONFIG-CHANGE GATE APPROVED (Options.workspaceMounts, RC-D24), never
// from the merged config this function is handed: the merge records no provenance, and the trust
// predicate of §2.2 is a statement about where an element was written. Nothing here re-reads the
// workspace files for a request: what is bound is what the gate read, judged and approved.
// Read-only elements keep coming from the merged view, but one the workspace config declares is
// bound at the destination the gate approved for it (oq-cx-symlink,
// docs/reference/context-mounts.md), so a link repointed after the approval is refused, not
// followed.

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/jailcontent"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/richtext"
)

// pendingWorkspaceMounts are the workspace mounts the gate's own read would ask about, before the
// gate runs: what the macos-user arm plans its links with, since it sites them first. The gate
// still judges and approves them, and the arm refuses unless it approved exactly this set. None
// when the read fails; the gate then refuses with the reason.
func (o *Options) pendingWorkspaceMounts() []config.WorkspaceMount {
	read, err := readWorkspaceForGateSeam(o.Workspace)
	if err != nil {
		return nil
	}
	return read.Mounts()
}

// sameWorkspaceMounts reports whether a and b are the same mounts resolved to the same paths.
func sameWorkspaceMounts(a, b []config.WorkspaceMount) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Mount.Spec != b[i].Mount.Spec || a[i].Resolved != b[i].Resolved {
			return false
		}
	}
	return true
}

// mountSourceDrift is the refusal for a launch whose approved workspace mount no longer resolves
// to the destination the gate approved (config.WorkspaceMount.Drift), nil when none moved.
func (o *Options) mountSourceDrift() error {
	for _, req := range o.workspaceMounts {
		if why := req.Drift(); why != "" {
			return errors.New(why)
		}
	}
	return nil
}

// configCtxMount is one config `mounts` element this launch delivers.
type configCtxMount struct {
	source string // resolved host path
	dest   string // jail path
	rw     bool
	// host is the host path as written ("~" unexpanded), for messages; optional is the
	// element's "optional" (docs/reference/context-mounts.md#optional-mounts).
	host     string
	optional bool
}

// configCtxMounts returns the config `mounts` elements a container launch delivers, in
// order — read-only elements first, as the merged config lists them, then the user scope's
// read-write ones — and hands `note` one line per element it skips (nil drops them: the
// briefing's caller, whose launch prints the same lines from the assembler).
//
// An OPTIONAL element (docs/reference/context-mounts.md#optional-mounts) is skipped in each case
// below with config.OptionalMountSkipLine's one plain line instead, and additionally when a macOS
// Podman Machine does not share its source, which would otherwise refuse the launch.
//
// Two skips, both the rule the string form always had and neither a downgrade:
//
//   - a source that does not exist. yolo never creates a context source, and a missing
//     bind source would abort the container start;
//   - a READ-ONLY element on an Apple Container that would ignore `:ro`
//     (roBindsUnsupported). A read-write element is NOT gated by that floor: the floor
//     exists for `:ro`, and refusing a mount for a property it does not need is the wrong
//     error (§2.9). A read-only one is never handed out writable in its place.
func (o *Options) configCtxMounts(rt string, cfg *jsonx.OrderedMap, note func(string)) []configCtxMount {
	// A caller that drops the lines asks for no location either, which costs a second read of
	// the config files (missingMountSourceWarning).
	quiet := note == nil
	if quiet {
		note = func(string) {}
	}
	// oq-cx-symlink: a mount the WORKSPACE config declares is bound at the destination the
	// config-change gate approved (Options.workspaceMounts), never by resolving the written path
	// again here. One whose written path no longer resolves there is not bound; the assembler
	// refuses that launch (mountSourceDrift). Matched by spelling: the same element in the user
	// config is held to the approval too, which can only narrow it.
	approved := map[string]config.WorkspaceMount{}
	for _, wm := range o.workspaceMounts {
		approved[wm.Mount.Spec] = wm
	}
	bindApproved := func(wm config.WorkspaceMount) (config.ContextMount, bool) {
		if why := wm.Drift(); why != "" {
			note("[bold red]" + richtext.Escape(why) + "[/bold red]")
			return config.ContextMount{}, false
		}
		m := wm.Mount
		m.Host = wm.Resolved
		return m, true
	}
	elements := make([]config.ContextMount, 0)
	for _, m := range config.ParseMounts(cfg) {
		if m.RW {
			continue
		}
		if wm, ok := approved[m.Spec]; ok && !wm.Mount.RW {
			if bound, ok := bindApproved(wm); ok {
				elements = append(elements, bound)
			}
			continue
		}
		elements = append(elements, m)
	}
	rw, err := config.LoadRWMounts(nil)
	if err != nil {
		// Unreachable on a launch, which loaded the same file strict before getting here;
		// said rather than swallowed, because a read-write mount the user wrote that silently
		// does not appear is the failure this whole feature refuses.
		note("[yellow]Warning: read-write mounts: " + err.Error() + " — none mounted[/yellow]")
	}
	elements = append(elements, rw...)
	// RC-D24: the writable mounts the config-change gate approved, from its own read, bound at the
	// destination it resolved and showed.
	for _, wm := range o.workspaceMounts {
		if !wm.Mount.RW {
			continue
		}
		if bound, ok := bindApproved(wm); ok {
			elements = append(elements, bound)
		}
	}

	roUnsafe := o.roBindsUnsupported(rt)
	var out []configCtxMount
	for _, m := range elements {
		source := resolveExpand(m.Host)
		dest := m.DestFor(source)
		if !fileExists(source) {
			switch {
			case m.Optional:
				note(config.OptionalMountSkipLine(m.Host, dest, "source does not exist"))
			case !quiet:
				note("[yellow]Warning: " + o.missingMountSourceWarning(m, source) + "[/yellow]")
			}
			continue
		}
		if !m.RW && roUnsafe != "" {
			if m.Optional {
				note(config.OptionalMountSkipLine(m.Host, dest, "this Apple Container ignores read-only binds"))
			} else {
				note("[yellow]Skipping mount " + source + " → " + dest + ": " + roUnsafe + "[/yellow]")
			}
			continue
		}
		// AN OPTIONAL SOURCE THE PODMAN MACHINE DOES NOT SHARE is skipped here, so the argv never
		// names it and the launch's share pre-flight (unsharedBindSources) never refuses it. A
		// required one stays in the argv and is refused there, as before.
		if m.Optional && o.machineCannotSee(rt, source) {
			note(config.OptionalMountSkipLine(m.Host, dest, "the Podman Machine does not share it"))
			continue
		}
		out = append(out, configCtxMount{source: source, dest: dest, rw: m.RW, host: m.Host,
			optional: m.Optional})
	}
	return out
}

// OptionalMountSkips is `yolo check`'s prediction of the optional `mounts` elements a launch on
// rt would skip, as the launch's own skip lines, through the launch's own deciders: configCtxMounts
// on a container runtime, and macosCtxLinks' siting on macos-user.
//
// NARROWER THAN THE LAUNCH, never wider, so it can only under-report: it hands the macos-user
// siting no pack grants (a collision with one is not predicted), asks no macOS Podman Machine for
// its shares, and does not read Apple Container's version (so the `:ro` floor is not predicted).
// Those need probes a check does not run; the launch prints its own line for each.
func OptionalMountSkips(workspace string, cfg *jsonx.OrderedMap, rt string) []string {
	var lines []string
	note := func(line string) {
		if strings.HasPrefix(line, "skipped optional mount:") {
			lines = append(lines, line)
		}
	}
	o := &Options{Workspace: workspace, Getenv: func(string) string { return "" }}
	if rt == "macos-user" { // parity: HonoredBy — macos-user's skips come from its siting (macosCtxLinks), the container runtimes' from configCtxMounts
		o.macosCtxLinks(cfg, nil, note)
		return lines
	}
	o.configCtxMounts("podman", cfg, note) // the floor and the machine shares need probes; see above
	return lines
}

// missingMountSourceWarning is the launch's line for a `mounts` element whose source does not
// exist, and it names the next step (docs/reference/happy-path-principle.md): the element as
// written and the file, line and column that wrote it (config.MountElementWhere, read only on
// this path), then both fixes and the check that confirms either (CX-D24 in
// docs/reference/context-mounts.md). It used to name the resolved path alone, on every launch.
func (o *Options) missingMountSourceWarning(m config.ContextMount, source string) string {
	where := "the `mounts` element `" + m.Spec + "`"
	if _, locs := config.MountElementWhere(o.Workspace, m); len(locs) > 0 {
		where += " at " + locs[0]
	}
	return "mount path does not exist, skipping: " + source + " (" + where + "). Create " + source +
		" or remove that element, then run `yolo check`."
}

// bindArg is the `-v` value for one delivered mount: `:ro` for every read-only one, no
// mode at all for a read-write one. Never `:z`/`:Z` — the run path uses label=disable, and
// a relabel would rewrite SELinux labels on the host tree (§2.9).
func (m configCtxMount) bindArg() string {
	if m.rw {
		return m.source + ":" + m.dest
	}
	return m.source + ":" + m.dest + ":ro"
}

// rwMountDisclosure is the launch line every read-write mount gets, EVERY launch
// (docs/reference/context-mounts.md#the-launch-disclosure). It is a disclosure in OQ-RO3's sense: nothing
// suppresses it — not YOLO_NO_BANNER, which is the version line and nothing else — and it is
// teed to launch.log with everything else the launch prints.
//
// THE INJECTION SENTENCE is the point of it. A path-keyed writable directory lets the jail
// choose both the key and the bytes, which is why hostcas admits only a fixed list of
// caches yolo itself names (OQ-BF11); a user-declared read-write mount is path-keyed by definition, so
// it is admissible only because a trusted source named it — and the user is told, every
// launch, what that opens.
//
// rootful adds §2.5's ownership consequence (CX-D6: disclosed, not refused — the workspace
// bind already lands in-jail root's writes as host root on that host, and nothing refuses it).
func rwMountDisclosure(m configCtxMount, rootful bool) string {
	line := "[bold yellow]Read-write mount:[/bold yellow] " + m.source + " → " + m.dest +
		". The jail can WRITE there, and anything on this machine that later reads " +
		m.source + " reads what the jail wrote, including a symlink the jail planted."
	if rootful {
		line += " This podman is ROOTFUL, so a file the jail's root creates there is owned " +
			"by root on the host."
	}
	return line
}

// rootfulPodmanHost reports whether this launch's podman ANSWERED that it is rootful, on a
// real host. Positive facts only, hostLoopbackFacts.rootfulPodman's rule: a podman that never
// answered says nothing about ownership. A nested launch is excluded because it is forced
// onto `--userns host` and reports rootful for that reason alone; its writes land under the
// OUTER jail's mapping, not as host root.
func (o *Options) rootfulPodmanHost(rt string) bool {
	if rt != "podman" || o.IsMacOS || o.inContainer() || o.podmanFacts == nil || !o.podmanFacts.parsed { // parity: NotApplicable — a rootful podman's ownership sentence; no other backend has a host root to own the writes (CX-D6)
		return false
	}
	r := o.podmanFacts.info.Host.Security.Rootless
	return r != nil && !*r
}

// packCtxMount is one pack `mount` grant this launch delivers.
type packCtxMount struct {
	pack   string
	from   string // home-relative, as declared
	source string
	dest   string
	file   bool
}

// packCtxMounts returns the pack `mount` grants a container launch delivers, handing
// `note` one line per grant it skips: an absent source (the pack's content simply is not
// there, and a missing bind source would abort the container start) or an Apple Container
// that would ignore `:ro`. The sole decider for both hostMountArgs and the briefing.
func (o *Options) packCtxMounts(rt string, packs []*packload.Pack, note func(string)) []packCtxMount {
	if note == nil {
		note = func(string) {}
	}
	roUnsafe := o.roBindsUnsupported(rt)
	var out []packCtxMount
	for _, p := range packs {
		if p == nil {
			continue
		}
		granted, _ := p.HonoredMounts()
		for _, mt := range granted {
			src := filepath.Join(homeDir(), filepath.FromSlash(mt.From))
			dest := packload.MountCtxPath(mt)
			dir, file := isDir(src), isFile(src)
			switch {
			case !dir && !file:
				note("[yellow]Warning: pack " + p.Name + " mount source " +
					"does not exist, skipping: ~/" + mt.From + "[/yellow]")
				continue
			case roUnsafe != "":
				note("[yellow]Skipping pack " + p.Name + " mount ~/" +
					mt.From + " → " + dest + ": " + roUnsafe + "[/yellow]")
				continue
			}
			out = append(out, packCtxMount{pack: p.Name, from: mt.From, source: src, dest: dest, file: file})
		}
	}
	return out
}

// briefedCtxMounts is what the agent's briefing lists under "Additional Context Mounts":
// every config element and pack grant this launch binds, each with its mode and, for a
// grant, its pack (§3.8 — before this, an agent learned a pack mount's path only from the
// pack's own prose), at the path the agent opens under ctxDir, the context dir.
//
// macos-user lists what ITS decider delivers (macosCtxLinks: a link in the context dir per
// mount, or a copy of a pack's single-file grant, marked Copied so the agent is told a host edit
// arrives at the next launch), and nothing at all when that decider refuses one, since such a
// launch refuses before any agent reads a briefing. One decider per backend for the briefing and
// the delivery, so the agent is never told about a path that is not there.
func (o *Options) briefedCtxMounts(rt, ctxDir string, cfg *jsonx.OrderedMap, packs []*packload.Pack) []jailcontent.ContextMount {
	if rt == "macos-user" { // parity: HonoredBy — macos-user delivers a context mount as a root-owned link plus Seatbelt rules, or a pack's single-file grant as a copy (macosCtxLinks), and the briefing lists exactly what that decider delivers
		links, copies, refused := o.macosCtxLinks(cfg, packs, nil)
		if len(refused) > 0 {
			return nil
		}
		var out []jailcontent.ContextMount
		for _, l := range links {
			out = append(out, jailcontent.ContextMount{Path: ctxDir + "/" + l.Rel(),
				Host: l.Source, ReadWrite: l.RW, Pack: l.Pack, Optional: l.Optional})
		}
		for _, c := range copies {
			out = append(out, jailcontent.ContextMount{Path: ctxDir + "/" + c.Rel(),
				Host: c.Source, Pack: c.Pack, Copied: true})
		}
		return out
	}
	var out []jailcontent.ContextMount
	for _, m := range o.configCtxMounts(rt, cfg, nil) {
		out = append(out, jailcontent.ContextMount{Path: m.dest, Host: m.source, ReadWrite: m.rw,
			Optional: m.optional})
	}
	for _, m := range o.packCtxMounts(rt, packs, nil) {
		out = append(out, jailcontent.ContextMount{Path: m.dest, Host: m.source, Pack: m.pack})
	}
	return out
}
