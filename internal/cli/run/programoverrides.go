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
// macos-user and `yolo host` are later slices (the plan's Tasks 9 and 10). There, the launch says
// the override is not applied yet rather than ignoring it silently.

import (
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/setupcensus"
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
	if o.CapturesDir() == "" || o.Sealed {
		return nil
	}
	if rt == "macos-user" { // parity: Warned — macos-user's override arm (a context link with a read and exec grant) is the plan's Task 10; noteUnappliedProgramOverrides says so at launch
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
		dir, err := filepath.EvalSymlinks(ov.Path)
		switch {
		case err != nil:
			refuse(ov.Path + " does not exist or cannot be read")
		case !isDir(dir):
			refuse(ov.Path + " is not a directory")
		default:
			p.wire.Host = dir
			run := ov.Run
			if run == "" {
				run = d.run
			}
			if breach := paths.WorkspaceScopeBreach(dir); breach != nil {
				refuse(breach.WhatFor("the override path") + ", the credential boundary a jail may " +
					"never read")
				break
			}
			if run == "" {
				refuse("pack " + d.pack + "'s " + bin + " names no file to run from a checkout — set " +
					"programs.overrides." + bin + ".run to the file inside " + ov.Path)
				break
			}
			p.wire.Run = run
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

// noteUnappliedProgramOverrides says, on a backend whose override arm is not built yet, which
// selected programs the user's overrides name and that they still run their pack's own copy.
func (o *Options) noteUnappliedProgramOverrides(rt string, packs []*packload.Pack) {
	if rt != "macos-user" || o.CapturesDir() == "" || o.Sealed { // parity: Warned — this is the line that says it: macos-user's override arm is the plan's Task 10
		return
	}
	bins := OverriddenDeclaredPrograms(packs)
	if len(bins) == 0 {
		return
	}
	o.pr(o.Stderr).print(setupcensus.Warning(setupcensus.MacosUser, "programs.overrides").Line(strings.Join(bins, ", ")))
}

// OverriddenDeclaredPrograms is every bin the user's programs.overrides names that a selected pack
// declares, sorted: the overrides a notch that does not apply them yet must name (macos-user here,
// and `yolo host`'s HostUnappliedProgramOverridesLine).
func OverriddenDeclaredPrograms(packs []*packload.Pack) []string {
	declared := declaredPrograms(packs)
	var bins []string
	for bin := range config.ProgramOverrides(nil) {
		if _, ok := declared[bin]; ok {
			bins = append(bins, bin)
		}
	}
	sort.Strings(bins)
	return bins
}

// HostUnappliedProgramOverridesLine is `yolo host`'s line for the overrides it does not apply yet
// (the plan's Task 9), in the macos-user notice's words, "" when no selected pack declares an
// overridden bin. Plain text: the host prints it with its own prefix.
func HostUnappliedProgramOverridesLine(packs []*packload.Pack) string {
	bins := OverriddenDeclaredPrograms(packs)
	if len(bins) == 0 {
		return ""
	}
	return setupcensus.Notice{
		Says: "`programs.overrides` is not applied by `yolo host` yet",
		Then: "Each of these runs its pack's own copy; run a checkout from a podman jail.",
	}.Plain(strings.Join(bins, ", "))
}

// UndeclaredProgramOverrideLines is `yolo check`'s information about the user's overrides that name
// a bin no selected pack declares: they do nothing in this workspace, which is not a failure,
// because the user config spans every workspace (§5).
func UndeclaredProgramOverrideLines(packs []*packload.Pack) []string {
	declared := declaredPrograms(packs)
	var out []string
	for bin := range config.ProgramOverrides(nil) {
		if _, ok := declared[bin]; !ok {
			out = append(out, "programs.overrides."+bin+": no selected pack declares "+bin+
				", so it does nothing in this workspace")
		}
	}
	sort.Strings(out)
	return out
}
