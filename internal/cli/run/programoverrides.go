package run

// programoverrides.go is the launch's half of a DEV OVERRIDE (docs/design/pack-buildable-programs.md
// §5, BG-D8/BG-D9) on the container backends: `programs.overrides.<bin>` in the USER config
// (config.ProgramOverrides, the one reader; a workspace config cannot set it) makes the jail run
// that program from a host checkout, in place of every rung of its pack's own delivery.
//
// ONE DECISION, FOUR CONSUMERS: programOverridePlansFor decides, per overridden bin a selected pack
// declares, where the checkout appears in the jail or why it is refused. The argv binds what it
// decided and hands the jail the decision (assembleRunCmd: one read-only bind per checkout outside
// the workspace, and entrypoint.ProgramOverridesEnv), the briefing lists the same binds
// (briefedCtxMounts), the build trigger leaves an overridden bin out (dropOverriddenPins: an
// override wins over a build, and a build that ran anyway could refuse a launch the user pointed
// elsewhere), and the disclosure names each one on every fresh launch and attach (BG-D10).
//
// `yolo host` applies the same checks (overrideCheckout) and runs the checkout from the host path
// itself, through its floor entry (HostProgramOverrides, hostfloor's override.go), with the same
// disclosure (HostProgramOverrideLines). ON MACOS-USER (the plan's Task 10) the same decision runs
// the checkout from its own host path: a checkout outside the workspace becomes a read-only context
// link the Seatbelt profile opens for read and exec (macosUserOverridePlace), and the decision
// reaches the darwin bootstrap through the launch env.

import (
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/hostfloor"
	"github.com/mschulkind-oss/yolo-jail/internal/macosuser"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// programOverridePlan is one applied (or refused) override this launch.
type programOverridePlan struct {
	bin  string
	pack string // the selected pack declaring bin
	// wire is what the jail is handed (Dir, Run, Host, Build, Refused).
	wire entrypoint.ProgramOverride
	// bindSource is the resolved host directory bound read-only at wire.Dir, "" when the checkout
	// is inside the workspace (already mounted, live) or the override is refused.
	bindSource string
}

// overrideUserConfigKey is the key and file every override line names.
func overrideUserConfigKey(bin string) string {
	return "programs.overrides." + bin + " in ~/.config/yolo-jail/config.jsonc"
}

// programOverridePlans is programOverridePlansFor over the user's own overrides. Nothing applies in
// a capture or build jail (CapturesDir ""), whose launch runs a pack's install or build and nothing
// the user develops, nor under the seal.
func (o *Options) programOverridePlans(rt string, packs []*packload.Pack) []programOverridePlan {
	if o.CapturesDir == nil || o.CapturesDir() == "" || o.Sealed {
		return nil
	}
	return o.programOverridePlansFor(rt, packs, config.ProgramOverrides(nil))
}

// programOverridePlansFor decides each override that names a bin a selected pack declares, in bin
// order. An override naming no selected bin does nothing (the user config spans every workspace).
//
// The checks, each a refusal of that one program naming the path and the key (§4's table):
//   - the path resolves (symlinks first: the darwin /var → /private/var class) to an existing
//     directory;
//   - it is not at the credential boundary (paths.WorkspaceScopeBreach: the home, the user config
//     directory, yolo's state directory, or anything containing one);
//   - there is a file to run: the entry's `run`, else the program's own (only a via "pack"
//     program has one);
//   - outside the workspace, the backend honors a read-only bind (an Apple Container below its
//     floor would hand the checkout out writable, which a read-only grant never becomes).
//
// Whether the file exists is the jail's to check, at each invocation: the checkout is live.
func (o *Options) programOverridePlansFor(rt string, packs []*packload.Pack,
	overrides map[string]config.ProgramOverride) []programOverridePlan {
	if len(overrides) == 0 {
		return nil
	}
	declared := declaredPrograms(packs)
	bins := make([]string, 0, len(overrides))
	for bin := range overrides {
		if _, ok := declared[bin]; ok {
			bins = append(bins, bin)
		}
	}
	sort.Strings(bins)
	ws := resolvePath(o.Workspace)
	var out []programOverridePlan
	for _, bin := range bins {
		ov, d := overrides[bin], declared[bin]
		p := programOverridePlan{bin: bin, pack: d.pack}
		p.wire.Build = d.build
		refuse := func(why string) {
			p.wire = entrypoint.ProgramOverride{Host: ov.Path, Build: d.build,
				Refused: why + " (" + overrideUserConfigKey(bin) + ")"}
		}
		dir, run, why := overrideCheckout(bin, ov, d, "a jail may never read")
		switch {
		case why != "":
			refuse(why)
		default:
			p.wire.Host = dir
			p.wire.Run = run
			if rt == "macos-user" { // parity: HonoredBy — macos-user runs the checkout at its own host path, a context link plus the profile's read and exec grant (macosUserOverridePlace)
				if why := o.macosUserOverridePlace(&p, dir, ws); why != "" {
					refuse(why)
				}
				break
			}
			if isUnderOrEqual(dir, ws) {
				rel, _ := filepath.Rel(ws, dir)
				p.wire.Dir = path.Join(jailWorkspace, filepath.ToSlash(rel))
				break
			}
			if why := o.roBindsUnsupported(rt); why != "" {
				refuse(ov.Path + " is outside the workspace and would be bound into the jail, but " + why)
				break
			}
			p.wire.Dir = path.Join(entrypoint.ProgramOverrideCtxDir, bin)
			p.bindSource = dir
		}
		out = append(out, p)
	}
	return out
}

// overrideCheckout is the checks every notch makes of one override on the host (§5, BG-D9), in one
// place so a jail and `yolo host` refuse alike: the path resolves, symlinks first (the darwin /var →
// /private/var class), to an existing directory; it is not at the credential boundary
// (paths.WorkspaceScopeBreach); and there is a file to run, the entry's `run` else the program's own.
// It returns the resolved directory and the run file, or why the override is refused, without the
// key (the caller appends it). boundary ends the credential-boundary sentence in the notch's words.
func overrideCheckout(bin string, ov config.ProgramOverride, d declaredProgram, boundary string) (dir, run, refused string) {
	dir, err := filepath.EvalSymlinks(ov.Path)
	switch {
	case err != nil:
		return "", "", ov.Path + " does not exist or cannot be read"
	case !isDir(dir):
		return "", "", ov.Path + " is not a directory"
	}
	run = ov.Run
	if run == "" {
		run = d.run
	}
	if breach := paths.WorkspaceScopeBreach(dir); breach != nil {
		return dir, "", breach.WhatFor("the override path") + ", the credential boundary " + boundary
	}
	if run == "" {
		return dir, "", "pack " + d.pack + "'s " + bin + " names no file to run from a checkout — set " +
			"programs.overrides." + bin + ".run to the file inside " + ov.Path
	}
	return dir, run, ""
}

// HostProgramOverrides is `yolo host`'s decision for the user's overrides (§5's delivery table: the
// host path itself), one per overridden bin a selected pack declares: the checks a jail launch makes
// (overrideCheckout), resolved for the floor, which writes bin/<bin> to exec the file (hostfloor's
// override.go). An override naming no selected bin does nothing, as in a jail.
func HostProgramOverrides(packs []*packload.Pack, overrides map[string]config.ProgramOverride) map[string]hostfloor.Override {
	return hostOverrides(declaredPrograms(packs), overrides)
}

// HostFloorOverrides is HostProgramOverrides over the floor's own programs (the one candidate set a
// floor is built from, first declaration of a bin winning, as declaredPrograms takes them).
func HostFloorOverrides(progs []hostfloor.Program, overrides map[string]config.ProgramOverride) map[string]hostfloor.Override {
	declared := map[string]declaredProgram{}
	for _, p := range progs {
		d := declaredProgram{pack: p.Pack}
		if p.Install.IsPackTree() {
			d.run, d.build = p.Install.PackRun, p.Install.Build
		}
		declared[p.Bin()] = d
	}
	return hostOverrides(declared, overrides)
}

// hostOverrides is the host's decision for each override naming a declared bin.
func hostOverrides(declared map[string]declaredProgram, overrides map[string]config.ProgramOverride) map[string]hostfloor.Override {
	if len(overrides) == 0 {
		return nil
	}
	out := map[string]hostfloor.Override{}
	for bin, ov := range overrides {
		d, ok := declared[bin]
		if !ok {
			continue
		}
		dir, run, why := overrideCheckout(bin, ov, d, "yolo never runs a program from")
		o := hostfloor.Override{Host: ov.Path, Dir: dir, Run: run, Build: d.build}
		if why != "" {
			o = hostfloor.Override{Host: ov.Path, Build: d.build, Refused: why + " (" + overrideUserConfigKey(bin) + ")"}
		}
		out[bin] = o
	}
	return out
}

// HostProgramOverrideLines is `yolo host`'s disclosure of the overrides it applies (BG-D10), one line
// per overridden bin a selected pack declares, in the jail's words: which file runs, or why the
// override is refused and that yolo host will not run the program until it is fixed or removed. Plain
// text: the host prints it with its own prefix, on every `yolo host --` and `yolo host env`.
func HostProgramOverrideLines(packs []*packload.Pack, overrides map[string]config.ProgramOverride) []string {
	resolved := HostProgramOverrides(packs, overrides)
	bins := make([]string, 0, len(resolved))
	for bin := range resolved {
		bins = append(bins, bin)
	}
	sort.Strings(bins)
	var out []string
	for _, bin := range bins {
		o := resolved[bin]
		if o.Refused != "" {
			out = append(out, bin+": dev override refused — "+o.Refused+"; yolo host will not run "+bin+
				" until it is fixed or removed")
			continue
		}
		out = append(out, bin+": dev override — runs "+o.File()+" (programs.overrides in "+
			"~/.config/yolo-jail/config.jsonc)")
	}
	return out
}

// declaredProgram is what an override reads off the program it replaces.
type declaredProgram struct {
	pack, run, build string
}

// declaredPrograms is every bin a selected pack declares as a program, first pack winning, as the
// jail's launcher generation takes them.
func declaredPrograms(packs []*packload.Pack) map[string]declaredProgram {
	out := map[string]declaredProgram{}
	for _, p := range packs {
		if p == nil {
			continue
		}
		installs, _ := p.HonoredInstalls()
		for _, inst := range installs {
			if inst.Bin == "" {
				continue
			}
			if _, ok := out[inst.Bin]; ok {
				continue
			}
			d := declaredProgram{pack: p.Name}
			if inst.IsPackTree() {
				d.run, d.build = inst.PackRun, inst.Build
			}
			out[inst.Bin] = d
		}
	}
	return out
}

// programOverridesWire is the jail's decision table, nil for none.
func programOverridesWire(plans []programOverridePlan) map[string]entrypoint.ProgramOverride {
	if len(plans) == 0 {
		return nil
	}
	out := make(map[string]entrypoint.ProgramOverride, len(plans))
	for _, p := range plans {
		out[p.bin] = p.wire
	}
	return out
}

// programOverrideBindArgs are the read-only binds of the checkouts outside the workspace.
func programOverrideBindArgs(plans []programOverridePlan) []string {
	var args []string
	for _, p := range plans {
		if p.bindSource != "" {
			args = append(args, "-v", p.bindSource+":"+p.wire.Dir+":ro")
		}
	}
	return args
}

// programOverrideLines is the disclosure, one line per override (BG-D10): which bytes run, or why
// the override was refused. Printed on every fresh launch (assembleRunCmd, where the binds are
// emitted) and every attach (noteAttachProgramOverrides, from the running jail's own decision).
func programOverrideLines(d map[string]entrypoint.ProgramOverride) []string {
	bins := make([]string, 0, len(d))
	for bin := range d {
		bins = append(bins, bin)
	}
	sort.Strings(bins)
	var out []string
	for _, bin := range bins {
		ov := d[bin]
		if ov.Refused != "" {
			out = append(out, "[yellow]"+bin+": dev override refused — "+ov.Refused+
				"; the jail will not run "+bin+" until it is fixed or removed[/yellow]")
			continue
		}
		out = append(out, bin+": dev override — runs "+ov.RunPath()+" (programs.overrides in "+
			"~/.config/yolo-jail/config.jsonc)")
	}
	return out
}

// dropOverriddenPins leaves out of the build trigger every fork or pack-tree program an override
// replaces: an override wins over every rung (§4), so its pack's build is not this launch's to run.
func dropOverriddenPins(pins []packload.ForkPin, plans []programOverridePlan) []packload.ForkPin {
	if len(plans) == 0 {
		return pins
	}
	over := map[string]bool{}
	for _, p := range plans {
		over[p.bin] = true
	}
	out := pins[:0:0]
	for _, p := range pins {
		if !over[p.Fork.Bin] {
			out = append(out, p)
		}
	}
	return out
}

// noteAttachProgramOverrides prints the running jail's own override lines on an attach, from the
// decision its fresh launch froze into its environment: a config edited since takes effect at the
// next fresh launch, so the line says what the jail runs, not what the file now says.
func (o *Options) noteAttachProgramOverrides(envLines []string) {
	raw, ok := envLineLookup(envLines, entrypoint.ProgramOverridesEnv)
	if !ok || raw == "" {
		return
	}
	d, err := entrypoint.ParseProgramOverrides(raw)
	if err != nil {
		return
	}
	out := o.pr(o.Stdout)
	for _, line := range programOverrideLines(d) {
		out.print(line)
	}
}

// macosUserOverridePlace is the macos-user arm's placement of one override whose path resolved to
// dir (BG-D9): the program runs from the host path itself, since the sandbox has no mount namespace
// and the workspace is at its own path. A checkout inside the workspace is already readable and
// needs nothing more. Any other is delivered as a read-only context link at
// /ctx/program-overrides/<bin>, which the Seatbelt profile opens for read and exec at dir
// (macosuser's program-override-read-allow) and the DAC preflight asks the sandbox's user to reach
// — sited first by the context mounts' own rules (macosuser.SiteContextLinks), so a checkout the
// sandbox cannot reach (inside a home in the account mode, where the sandbox account reaches
// nothing) or must not (yolo's state, every home, a place the sandbox writes) refuses this one
// program, with the rule's reason, rather than the launch. It returns that reason, "" when placed.
func (o *Options) macosUserOverridePlace(p *programOverridePlan, dir, ws string) string {
	p.wire.Dir = dir
	if isUnderOrEqual(dir, ws) {
		return ""
	}
	l := macosUserOverrideLink(p.bin, dir)
	if r := macosuser.SiteContextLinks(o.macosSiting(), ws, []macosuser.ContextLink{l}, nil); len(r) > 0 {
		return dir + " cannot be run from on macos-user: " + strings.TrimPrefix(r[0].Reason, "its ") +
			"; a checkout under " + macosuser.SharedRootDefault() + " works in either macos_user_mode"
	}
	p.bindSource = dir
	return ""
}

// macosUserOverrideLink is the context link a macos-user override outside the workspace is
// delivered as: read-only, at the jail path the container backends bind it at.
func macosUserOverrideLink(bin, dir string) macosuser.ContextLink {
	return macosuser.ContextLink{Dest: path.Join(entrypoint.ProgramOverrideCtxDir, bin), Source: dir,
		Named: dir, Dir: true, Override: bin}
}

// macosUserOverrideLinks are the context links this macos-user launch's applied overrides need: one
// per checkout outside the workspace, handed to the backend with the context mounts.
func macosUserOverrideLinks(plans []programOverridePlan) []macosuser.ContextLink {
	var out []macosuser.ContextLink
	for _, p := range plans {
		if p.bindSource != "" && p.wire.Refused == "" {
			out = append(out, macosUserOverrideLink(p.bin, p.bindSource))
		}
	}
	return out
}

// noteProgramOverrides prints the disclosure (programOverrideLines) on an arm that has no
// assembleRunCmd to print it beside the binds: macos-user, where every invocation is a fresh launch.
func (o *Options) noteProgramOverrides(plans []programOverridePlan) {
	out := o.pr(o.Stdout)
	for _, line := range programOverrideLines(programOverridesWire(plans)) {
		out.print(line)
	}
}

// UndeclaredProgramOverrideLines is `yolo check`'s information about the user's overrides that name
// a bin no selected pack declares: they do nothing in this workspace, which is not a failure,
// because the user config spans every workspace (§5). A prebuilt opt-in (BG-D18) is reported the same
// way, and also where the program it names declares no `prebuilt` at all, which it cannot opt into;
// one missing only for this launch's platform is the launch's refusal (packload.PackTreeResolve).
func UndeclaredProgramOverrideLines(packs []*packload.Pack) []string {
	declared := declaredPrograms(packs)
	var out []string
	for bin := range config.ProgramOverrides(nil) {
		if _, ok := declared[bin]; !ok {
			out = append(out, "programs.overrides."+bin+": no selected pack declares "+bin+
				", so it does nothing in this workspace")
		}
	}
	prebuilts := map[string]packload.Fork{}
	for _, f := range packload.PackTreePrograms(packs) {
		if _, seen := prebuilts[f.Bin]; !seen {
			prebuilts[f.Bin] = f
		}
	}
	for bin := range config.ProgramPrebuiltOptIns(nil) {
		f, packTree := prebuilts[bin]
		d, isDeclared := declared[bin]
		switch {
		case !isDeclared:
			out = append(out, "programs.overrides."+bin+".prebuilt: no selected pack declares "+bin+
				", so it does nothing in this workspace")
		case !packTree || len(f.Prebuilts) == 0:
			out = append(out, "programs.overrides."+bin+".prebuilt: pack "+d.pack+" declares no "+
				"prebuilt artifact for "+bin+", so it does nothing — remove the setting")
		}
	}
	sort.Strings(out)
	return out
}
