package run

// inheritscope.go writes the two GENERATED user-scope files a jail inherits, replacing the
// raw `:ro` bind of the human's real config.jsonc that used to serve as an inner jail's
// user scope (OQ-LP9, docs/reference/loophole-system.md#why-its-this-way).
//
// WHAT CHANGED, in one sentence: the inner user scope is now COMPOSED from the effective
// config and FILTERED per consumer (internal/config/inherit.go), rather than being whatever
// happened to be in the human's top config file.
//
// FOUR PROPERTIES THIS FILE IS RESPONSIBLE FOR, all four load-bearing:
//
//  1. SINGLE-FILE DELIVERY INTO A JAIL-OWNED DIRECTORY (R8). Each generated file is
//     mounted as its own `:ro` single file, and the DIRECTORY around it
//     (~/.config/yolo-jail) stays the jail's own writable home. That is what makes
//     writing BESIDE the file jail-local: an in-jail agent can drop a layer file next to
//     it and pass --user-layer, and nothing it writes can reach the host. Mounting the
//     DIRECTORY instead would take that away, which is why this never does.
//  2. THE NESTED FILE IS WRITTEN ONLY WHERE NESTING IS POSSIBLE (R2). Not gated by a
//     conditional inside a file that is always written — genuinely absent. On a backend
//     that cannot nest there is no file, so nothing is mounted that serves a capability
//     the setup lacks.
//  3. RECURSION IS BY COMPOSITION (R6). This runs from the EFFECTIVE config of whatever
//     jail is launching, which at depth 2 already includes what depth 1 inherited plus any
//     --user-layer it was passed. So jail A hands B one inherited file; it never stacks a
//     chain of ancestors' files, and there is no rule that changes with depth.
//  4. LAUNCH-FROZEN (R7). These are rendered once at assembly, so a host-side config edit
//     lands at the next launch rather than live. That is a REAL behaviour change from the
//     live bind, named in the file header, and it is the jail's normal contract — env, the
//     image and the relay wiring are all frozen at container start. `yolo config drift`
//     is the existing mechanism for noticing, and it now covers this file too.

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/configdoc"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// The in-jail names of the two generated files. Both live in the jail's own
// ~/.config/yolo-jail, beside where a --user-layer file would go.
//
// The PREFLIGHT file takes the name `config.jsonc` deliberately: it IS the inner user
// scope, so every existing in-jail reader (config.LoadConfig, LoadPacks, the loopholes
// commands, `yolo check`) finds it at paths.UserConfigPath() with no plumbing at all. The
// nested file gets its own name because nothing reads it as "the user config" — an inner
// launcher merges it explicitly.
const (
	inheritPreflightRel       = ".config/yolo-jail/config.toml"
	inheritNestedRel          = ".config/yolo-jail/inherited-launch.toml"
	inheritPreflightLegacyRel = ".config/yolo-jail/config.jsonc"
	inheritNestedLegacyRel    = ".config/yolo-jail/inherited-launch.jsonc"
)

// inheritScopeFile is one generated file to deliver: its rendered bytes and where it goes.
type inheritScopeFile struct {
	scope          config.InheritScope
	rel            string // path under /home/agent
	body           string
	format         configdoc.Format
	fallbackReason string
	keys           int
}

// canNest reports whether a jail on this backend can launch a jail of its own — the gate on
// whether the nested-launch file is written at all (R2).
//
// The answer is a property of the BACKEND, not of the current process: podman bakes a
// nested podman into the image (flake.nix corePackages: podman, fuse-overlayfs,
// slirp4netns) and the CLI has a whole podman-in-podman branch (--userns=host, forced
// --net=host, --cgroups=disabled). Apple Container has no nested container runtime inside
// its VM, and macos-user has no container at all — a Seatbelt-confined native process
// cannot start one, and the macos-user census (docs/reference/macos-user-nix-and-features.md)
// already states that nothing is bind-mounted there.
//
// Deliberately keyed on the runtime STRING rather than on a probe: this decides whether to
// write a file for the jail we are about to start, so it must answer "can the container we
// are creating nest?", which no probe of the current process can tell us.
func canNest(rt string) bool { return rt == "podman" } // parity: Honored — only Podman launches can nest; other backends omit the nested scope.

// inheritScopeFiles renders the generated user-scope files for a launch.
//
// effective is the already-composed config — the same value `yolo config dump` serializes
// (config.LoadConfig → SnapshotJSON), which is what makes both files renders of ONE
// computation rather than a second composer that could drift from the first.
//
// unknown collects config keys the census does not classify. They are DROPPED from every
// generated file and returned so the caller can say so: silently passing an unclassified
// key into a jail would defeat the census, and silently dropping it would hide a schema
// addition. (In a shipped binary the set is always empty — validate.go rejects an unknown
// top-level key long before assembly — so this is the seam that keeps a future key from
// slipping through, not a live path.)
//
// A scope whose filter kept NOTHING yields keys==0, and the caller then delivers no file at
// all. That matters twice over. It is honest — a user with no relevant config had no user
// scope before this change either, and a file saying `{}` under a header explaining what was
// filtered would invite the reader to hunt for a key they never set. And it keeps the
// promise the golden argv makes: a jail launched from a config with nothing to inherit emits
// byte-identical argv to the one it emitted before this feature existed.
//
// src is effective's provenance, which each file records in its header so a refusal in the jail
// names the host file that set a key (config.RenderFilteredWithOrigins); nil records nothing.
func inheritScopeFiles(effective *jsonx.OrderedMap, src *config.Sources, rt, launchedAt string) (files []inheritScopeFile, unknown []string, err error) {
	return inheritScopeFilesWithSelector(effective, src, rt, launchedAt, nil)
}

// inheritScopeFilesWithSelector lets a backend preserve its current destination family before
// the first stage/write. The selector may keep a compatible generated JSONC route, or refuse an
// ambiguous destination; it never edits the destination itself.
func inheritScopeFilesWithSelector(effective *jsonx.OrderedMap, src *config.Sources, rt, launchedAt string, selectLegacy func(config.InheritScope, configdoc.Format) (bool, error)) (files []inheritScopeFile, unknown []string, err error) {
	seen := map[string]bool{}
	addUnknown := func(keys []string) {
		for _, k := range keys {
			if !seen[k] {
				seen[k] = true
				unknown = append(unknown, k)
			}
		}
	}
	render := func(scope config.InheritScope) error {
		filtered, unk, ferr := config.FilterInheritErr(effective, scope)
		if ferr != nil {
			return ferr
		}
		addUnknown(unk)
		if filtered.Len() == 0 {
			return nil
		}
		body, format, fallbackReason, rerr := config.RenderFilteredForDelivery(filtered, src, scope, launchedAt)
		if rerr != nil {
			return rerr
		}
		if selectLegacy != nil {
			forceLegacy, selectErr := selectLegacy(scope, format)
			if selectErr != nil {
				return selectErr
			}
			if forceLegacy && format != configdoc.JSONC {
				body, rerr = config.RenderFilteredWithOrigins(filtered, src, scope, launchedAt)
				if rerr != nil {
					return rerr
				}
				format = configdoc.JSONC
				fallbackReason = "a regular legacy JSONC sibling already occupies the in-jail destination"
			}
		}
		rel := inheritScopeRel(scope, format)
		files = append(files, inheritScopeFile{scope: scope, rel: rel, body: body, format: format, fallbackReason: fallbackReason, keys: filtered.Len()})
		return nil
	}

	if err := render(config.InheritPreflight); err != nil {
		return nil, nil, err
	}
	// R2: absent, not empty, on a backend that cannot nest.
	if canNest(rt) {
		if err := render(config.InheritNested); err != nil {
			return nil, nil, err
		}
	}
	return files, unknown, nil
}

// inheritedScope renders this launch's generated user-scope files (inheritScopeFiles) and says
// what went wrong with the render, for every backend's delivery: the container path mounts each
// file (userConfigMountArgs) and the macos-user arm lays it into the home overlay
// (macosUserInheritedScope). A render that failed delivers nothing, and says so.
//
// stamped puts the launch time in each header (inheritStamp). A delivery reused across launches
// while its bytes are unchanged passes false, because a timestamp makes every launch's bytes new.
func (o *Options) inheritedScope(rt string, stamped bool) []inheritScopeFile {
	files, err := o.inheritedScopeWithSelector(rt, stamped, nil)
	if err == nil {
		return files
	}
	o.pr(o.Stderr).print("[yellow]Warning: could not render this jail's inherited user config (" +
		err.Error() + ") — in-jail `yolo check`, `pack` and `loopholes` will see no " +
		"user scope[/yellow]")
	return nil
}

func (o *Options) inheritedScopeWithSelector(rt string, stamped bool, selectLegacy func(config.InheritScope, configdoc.Format) (bool, error)) ([]inheritScopeFile, error) {
	out := o.pr(o.Stderr)
	stamp := ""
	if stamped {
		stamp = o.inheritStamp()
	}
	effective, src := o.effectiveConfigForInherit()
	files, unknown, err := inheritScopeFilesWithSelector(effective, src, rt, stamp, selectLegacy)
	if err != nil {
		return nil, err
	}
	if len(unknown) > 0 {
		// Named, not swallowed: an unclassified key reaching a jail unreviewed is the
		// failure the census exists to prevent, so its absence has to be visible.
		out.print("[yellow]Warning: config keys with no inherit classification were " +
			"dropped from the jail's user scope: " + joinComma(unknown) +
			" — classify them in internal/config/inherit.go[/yellow]")
	}
	for _, f := range files {
		if f.fallbackReason != "" {
			out.print("[yellow]Warning: inherited " + f.scope.String() +
				" scope uses generated JSONC because " + f.fallbackReason +
				". No user-owned sibling file was changed.[/yellow]")
		}
	}
	return files, nil
}

func selectInheritedJSONCRoute(wsState string, mapDestination func(string) (string, error), scope config.InheritScope, format configdoc.Format) (bool, error) {
	legacyRel, tomlRel := inheritScopeFamily(scope)
	legacyStorageRel, err := mapDestination(legacyRel)
	if err != nil {
		return false, err
	}
	tomlStorageRel, err := mapDestination(tomlRel)
	if err != nil {
		return false, err
	}
	legacyPath := filepath.Join(wsState, legacyStorageRel)
	tomlPath := filepath.Join(wsState, tomlStorageRel)
	_, legacyExists, err := lstatHomeFile(wsState, legacyStorageRel)
	if err != nil {
		return false, fmt.Errorf("cannot establish inherited %s user-config destination %s: %w. Restore this jail-home path as a real directory tree, preserving its data, then relaunch", scope, legacyPath, err)
	}
	_, tomlExists, err := lstatHomeFile(wsState, tomlStorageRel)
	if err != nil {
		return false, fmt.Errorf("cannot establish inherited %s user-config destination %s: %w. Restore this jail-home path as a real directory tree, preserving its data, then relaunch", scope, tomlPath, err)
	}
	if legacyExists && tomlExists {
		return false, fmt.Errorf("refusing inherited %s user config: both %s and %s exist in the jail's writable home; preserve both, move one to a backup outside this directory, then relaunch", scope, legacyPath, tomlPath)
	}
	if legacyExists {
		return true, nil
	}
	if tomlExists && format != configdoc.TOML {
		return false, fmt.Errorf("refusing inherited %s user config: its legacy JSONC payload needs %s, but the jail already has %s; preserve both, convert or back up one file, then relaunch", scope, legacyPath, tomlPath)
	}
	return false, nil
}

func inheritScopeFamily(scope config.InheritScope) (legacy, toml string) {
	if scope == config.InheritPreflight {
		return inheritPreflightLegacyRel, inheritPreflightRel
	}
	return inheritNestedLegacyRel, inheritNestedRel
}

func refuseExistingScopeDestination(wsState string, mapDestination func(string) (string, error), scope config.InheritScope, backend, overwriteRisk string) (bool, error) {
	legacyRel, tomlRel := inheritScopeFamily(scope)
	legacyStorageRel, err := mapDestination(legacyRel)
	if err != nil {
		return false, err
	}
	tomlStorageRel, err := mapDestination(tomlRel)
	if err != nil {
		return false, err
	}
	legacyPath := filepath.Join(wsState, legacyStorageRel)
	tomlPath := filepath.Join(wsState, tomlStorageRel)
	_, legacyExists, err := lstatHomeFile(wsState, legacyStorageRel)
	if err != nil {
		return false, fmt.Errorf("cannot establish %s inherited %s destination %s: %w. Restore this jail-home path as a real directory tree, preserving its data, then relaunch", backend, scope, legacyPath, err)
	}
	_, tomlExists, err := lstatHomeFile(wsState, tomlStorageRel)
	if err != nil {
		return false, fmt.Errorf("cannot establish %s inherited %s destination %s: %w. Restore this jail-home path as a real directory tree, preserving its data, then relaunch", backend, scope, tomlPath, err)
	}
	if legacyExists && tomlExists {
		return false, fmt.Errorf("refusing %s inherited %s scope: existing user files occupy both %s and %s; preserve them, choose one only after reviewing their contents, then relaunch", backend, scope, legacyPath, tomlPath)
	}
	if legacyExists || tomlExists {
		path := legacyPath
		if tomlExists {
			path = tomlPath
		}
		return false, fmt.Errorf("refusing %s inherited %s scope: %s could overwrite existing user file %s; preserve its contents and mode, move it aside only after review, then relaunch", backend, scope, overwriteRisk, path)
	}
	return false, nil
}

// in the assembly prefix. It rejects duplicate equal-specificity mounts and any backing outside
// wsState, rather than guessing from a generated staging filename.
func mappedHomePath(wsState string, argv []string, containerPath string) (string, error) {
	bestDepth := -1
	var sourceRoot, mountDest string
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] != "-v" {
			continue
		}
		fields := strings.SplitN(argv[i+1], ":", 3)
		if len(fields) < 2 {
			continue
		}
		source, dest := fields[0], filepath.Clean(fields[1])
		rel, err := filepath.Rel(dest, filepath.Clean(containerPath))
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		depth := strings.Count(strings.Trim(dest, string(filepath.Separator)), string(filepath.Separator)) + 1
		if depth < bestDepth {
			continue
		}
		if depth == bestDepth {
			return "", fmt.Errorf("ambiguous equal-specificity home binds for %s (%s and %s); preserve user files and resolve the mount overlap before relaunch", containerPath, mountDest, dest)
		}
		bestDepth, sourceRoot, mountDest = depth, source, dest
	}
	if bestDepth < 0 || sourceRoot == "" || !filepath.IsAbs(sourceRoot) {
		return "", fmt.Errorf("no proven home bind covers %s; preserve user files and retry after the destination mapping is restored", containerPath)
	}
	suffix, err := filepath.Rel(mountDest, filepath.Clean(containerPath))
	if err != nil || suffix == ".." || strings.HasPrefix(suffix, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid home-bind containment for %s", containerPath)
	}
	backing := filepath.Join(sourceRoot, suffix)
	rel, err := filepath.Rel(filepath.Clean(wsState), backing)
	if err != nil || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("the covering home bind for %s maps through %s outside this jail's state; preserve user files and resolve the mount before relaunch", containerPath, sourceRoot)
	}
	return rel, nil
}

// lstatHomeFile checks one actual destination beneath the already-existing jail home without
// following a link at any component. A missing component means no sibling exists; any linked or
// non-directory parent and any nonregular final object fail closed.
func lstatHomeFile(wsState, rel string) (os.FileInfo, bool, error) {
	state, err := os.Lstat(wsState)
	if os.IsNotExist(err) {
		parent, parentErr := os.Lstat(filepath.Dir(wsState))
		if parentErr == nil && (parent.Mode()&os.ModeSymlink != 0 || !parent.IsDir()) {
			return nil, false, fmt.Errorf("home state parent %s is not a real directory; restore it without following the link, preserve its data, then relaunch", filepath.Dir(wsState))
		}
		if parentErr != nil && !os.IsNotExist(parentErr) {
			return nil, false, parentErr
		}
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if state.Mode()&os.ModeSymlink != 0 || !state.IsDir() {
		return nil, false, fmt.Errorf("home state is not a real directory")
	}
	clean := filepath.Clean(rel)
	if clean == "." || clean == ".." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return nil, false, fmt.Errorf("invalid home-relative path %q", rel)
	}
	root, err := openStateRoot(wsState)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = root.Close() }()
	parts := strings.Split(clean, string(filepath.Separator))
	for i, part := range parts {
		info, err := root.Lstat(part)
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, err
		}
		full := filepath.Join(wsState, filepath.Join(parts[:i+1]...))
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, false, fmt.Errorf("%s is a symbolic link; the target was not followed. Replace the link with a regular config file or remove it after preserving its target data, then relaunch", full)
		}
		if i == len(parts)-1 {
			if !info.Mode().IsRegular() {
				return nil, false, fmt.Errorf("%s is not a regular file; replace it with a regular config file, preserving its contents, then relaunch", full)
			}
			return info, true, nil
		}
		if !info.IsDir() {
			return nil, false, fmt.Errorf("%s is not a directory", full)
		}
		next, err := root.OpenRoot(part)
		if err != nil {
			return nil, false, err
		}
		_ = root.Close()
		root = next
	}
	return nil, false, fmt.Errorf("invalid home-relative path %q", rel)
}

func inheritScopeRel(scope config.InheritScope, format configdoc.Format) string {
	if scope == config.InheritPreflight {
		if format == configdoc.TOML {
			return inheritPreflightRel
		}
		return inheritPreflightLegacyRel
	}
	if format == configdoc.TOML {
		return inheritNestedRel
	}
	return inheritNestedLegacyRel
}

// userConfigMountArgs delivers the generated inner user scope, returning the container
// argv for it.
//
// It REPLACED a raw `:ro` bind of paths.UserConfigPath(). The old comment said the mount
// was "for nested jails"; measured, its readers were broader (`yolo check`, `yolo loopholes
// list`, `yolo pack` all read user scope in-jail on every setup) and its content was
// narrower (only config.jsonc crossed, so `include_if_found` files stayed host-side). So it
// was neither the effective config nor a designed subset — it filtered by accident. Now it
// filters on purpose, per consumer.
//
// The user's ~/.config/yolo-jail/config.lua crossed here too, as the host's own file and
// unfiltered, because it was a Lua transform script rather than a config with keys to
// classify. The transform is gone (docs/reference/pack-system.md#oq-lt1) and so is that
// bind: nothing in the jail reads the file, so mounting it would be one more host path
// crossing for no reader.
func (o *Options) userConfigMountArgs(rt, wsState string) []string {
	args, err := o.userConfigMountArgsForHome(rt, wsState, nil)
	if err != nil {
		o.pr(o.Stderr).print("[yellow]Warning: could not render this jail's inherited user config (" + err.Error() + ")[/yellow]")
		return nil
	}
	return args
}

// userConfigMountArgsForAssembly checks the actual, already assembled home bind graph before
// any generated scope is staged. The most-specific covering bind decides where an existing
// sibling is stored; an unknown, ambiguous, linked, or nonregular destination refuses assembly.
func (o *Options) userConfigMountArgsForAssembly(in *assembleInput, assembled []string) ([]string, error) {
	if in == nil {
		return nil, fmt.Errorf("cannot establish the inherited user config destination without assemble input")
	}
	selectLegacy := func(scope config.InheritScope, format configdoc.Format) (bool, error) {
		mapDestination := func(rel string) (string, error) {
			return mappedHomePath(in.wsState, assembled, "/home/agent/"+rel)
		}
		if in.rt == "container" { // parity: Refused — Apple Container cannot cover an existing HOME file without overwriting it.
			return refuseExistingScopeDestination(in.wsState, mapDestination, scope,
				"Apple Container", "the whole HOME bind would overwrite it")
		}
		return selectInheritedJSONCRoute(in.wsState, mapDestination, scope, format)
	}
	return o.userConfigMountArgsForHome(in.rt, in.wsState, selectLegacy)
}

func (o *Options) userConfigMountArgsForHome(rt, wsState string, selectLegacy func(config.InheritScope, configdoc.Format) (bool, error)) ([]string, error) {
	var args []string
	out := o.pr(o.Stderr)
	files, err := o.inheritedScopeWithSelector(rt, true, selectLegacy)
	if err != nil {
		return nil, err
	}
	// The GENERATED files. Written into wsState (the jail's own per-workspace state dir,
	// which is where every other composed file goes — the gitconfig, the user-env script)
	// and mounted from there, one bind per file.
	for _, f := range files {
		// Both writes are BENEATH wsState (writeFileBeneath, wsstatebeneath.go): the jail can
		// leave a link at either destination, or above the Apple Container one, for a plain
		// write to follow onto a host file, and podman would then bind the link's target.
		stagedName := "inherit-" + f.scope.String() + filepath.Ext(f.rel)
		staged := filepath.Join(wsState, stagedName)
		if err := writeFileBeneath(wsState, stagedName, []byte(f.body), 0o644); err != nil {
			out.print("[yellow]Warning: could not stage the " + f.scope.String() +
				" user config at " + staged + " (" + err.Error() + ") — this jail gets no " +
				f.scope.String() + " user scope[/yellow]")
			continue
		}
		if rt == "container" { // parity: Honored — Apple Container delivers through its whole-HOME bind.
			// Apple Container mounts the whole wsState at /home/agent and cannot
			// bind a single file, so the content is copied to its destination inside
			// that tree instead.
			_ = writeFileBeneath(wsState, filepath.FromSlash(f.rel), []byte(f.body), 0o644)
			continue
		}
		// SINGLE-FILE bind (R8): the enclosing ~/.config/yolo-jail stays the jail's
		// own writable dir, so a --user-layer file written beside this one is
		// jail-local and cannot reach the host.
		args = append(args, "-v", staged+":/home/agent/"+f.rel+":ro")
	}
	return args, nil
}

// macosUserInheritedScope is the macos-user arm's delivery of the generated user scope: the
// files inheritedScope renders, as files of core's for the home overlay
// (buildMacosHomeOverlayWith). The overlay is copied into the sandbox account home by the
// bootstrap, through the ~/.config layout link into the workspace's sidecar, and the Seatbelt
// profile denies the agent writes to it — so the file is read-only as a container's single-file
// `:ro` bind is, and the directory around it (where a --user-layer file goes, and the
// conventional local pack) stays the agent's own (R8). canNest is false for this backend, so
// the nested-launch file is never among them (R2).
//
// AND THE ONE IT DID NOT RENDER, removed (removeStaleInheritedScope), on a launch only. A
// container's bind leaves nothing behind when it is not made; this backend's copy stays in the
// sidecar, so a user config that lost its last inheritable key would otherwise keep serving the
// previous launch's user scope to every in-sandbox reader. A plan render removes nothing.
func (o *Options) macosUserInheritedScope(rt string) ([]overlayFile, error) {
	var core []overlayFile
	wsState := paths.WorkspaceHomeState(o.Workspace)
	selectLegacy := func(scope config.InheritScope, format configdoc.Format) (bool, error) {
		return refuseExistingScopeDestination(wsState, func(rel string) (string, error) {
			// DeriveDarwinHomeLayout lays ~/.config as the `config` subtree of this sidecar.
			// The installer follows exactly this link into the workspace's sidecar.
			return filepath.FromSlash(strings.TrimPrefix(rel, ".")), nil
		}, scope, "macos-user", "the home-overlay installer cannot prove this file is its prior delivery")
	}
	files, err := o.inheritedScopeWithSelector(rt, false, selectLegacy)
	if err != nil {
		return nil, err
	}
	// UNSTAMPED: the home overlay is content-keyed and an unchanged relaunch reuses it whole
	// (buildMacosHomeOverlayTreeIDs). A launch time in this header moved the overlay's digest on
	// every launch, so every relaunch rebuilt, re-staged and re-installed all of it.
	for _, f := range files {
		core = append(core, overlayFile{Rel: f.rel, Body: []byte(f.body)})
	}
	if !o.DryRun {
		// Remove only known generated copies before applying the new overlay. In particular,
		// a legacy config.jsonc beside a new config.toml would trigger the sibling refusal.
		removeStaleInheritedScope(wsState)
	}
	return core, nil
}

// removeStaleInheritedScope removes only the generated user-scope files a previous macos-user
// launch installed in sidecar (the workspace's <ws>/.yolo/home, which the sandbox's ~/.config
// links into at `config`). Anything else at either format path is an agent's own and is kept.
//
// BENEATH THE SIDECAR'S ROOT, never by path (openStateRoot): the sidecar is written by the agent,
// so a link it planted above the file would otherwise take the removal to whatever it names.
// Best-effort: a stale file left behind costs a stale user scope, never a launch.
//
// ⚠ RESIDUAL, stated rather than fixed: a launch that DOES render the file replaces an agent's
// own config.jsonc at the path, where podman's bind only hides it until the next launch without
// one. The agent's place for its own user-scope config is a --user-layer file beside it.
func removeStaleInheritedScope(sidecar string) {
	// No sidecar, no files: and openStateRoot would create the directories it opens.
	if _, err := os.Lstat(sidecar); err != nil {
		return
	}
	r, err := openStateRoot(sidecar)
	if err != nil {
		return
	}
	defer r.Close()
	for _, candidate := range []struct {
		rel    string
		marker string
	}{
		{inheritPreflightLegacyRel, inheritHeaderFirstLine()},
		{inheritPreflightRel, inheritTOMLHeaderFirstLine()},
	} {
		rel := filepath.FromSlash(strings.TrimPrefix(candidate.rel, "."))
		if fi, err := r.Lstat(rel); err != nil || !fi.Mode().IsRegular() {
			continue
		}
		f, err := r.Open(rel)
		if err != nil {
			continue
		}
		first, _ := bufio.NewReader(f).ReadString('\n')
		_ = f.Close()
		if strings.TrimRight(first, "\n") == candidate.marker {
			_ = r.Remove(rel)
		}
	}
}

// inheritHeaderFirstLine is the first line of a generated legacy JSONC user-scope file.
// header's own renderer so the two cannot disagree.
func inheritHeaderFirstLine() string {
	line, _, _ := strings.Cut(config.InheritHeader(config.InheritPreflight, ""), "\n")
	return line
}

func inheritTOMLHeaderFirstLine() string {
	line, _, _ := strings.Cut(config.InheritTOMLHeader(config.InheritPreflight, ""), "\n")
	return line
}

// effectiveConfigForInherit returns the config the generated files are rendered FROM.
//
// It re-reads through config's own composer rather than taking the run pipeline's already-loaded
// map as a parameter, for one reason: this is the computation `yolo config dump` renders, less
// one layer (below), and going through the same function is what keeps them from diverging. It
// is also where a --user-layer (config.UserLayerPath) is folded in, so the recursion property
// (R6) comes for free — at depth 2 the effective config already contains what depth 1
// inherited plus any layer it was passed.
//
// A load failure yields nil, which FilterInherit renders as an empty file rather than
// failing the launch: the run pipeline has ALREADY loaded and validated this config
// (loadAndValidateConfig, strict) and refused the launch on error, so a failure here means
// the file changed underneath a launch in progress — and an empty inner scope degrades to
// "this jail knows of no user config", which is what a user with no config has.
//
// LESS THE PER-WORKSPACE FILE (config.LoadConfigWithoutWorkspaceFile), the one difference from
// `yolo config dump`'s computation, and deliberate: that file's switches are for THIS workspace
// alone, keyed by a host path no jail has, and the inherited files are the jail's user scope,
// which applies to every workspace a launch inside the jail opens. Inherited, a brokered
// loophole switched on here would also reach the inner user scope, which refuses it
// (docs/design/boundary-broker.md BB-D56).
//
// The provenance comes with it (config.LoadConfigWithoutWorkspaceFileWithSources), so each file
// records which host file set each key and a refusal in the jail names that file.
func (o *Options) effectiveConfigForInherit() (*jsonx.OrderedMap, *config.Sources) {
	cfg, src, err := config.LoadConfigWithoutWorkspaceFileWithSources(o.Workspace, false, func(string) {})
	if err != nil {
		return nil, nil
	}
	return cfg, src
}

// inheritStamp is the human-readable launch time for the generated files' headers. It uses
// the injected clock so a golden test can pin the bytes.
func (o *Options) inheritStamp() string {
	now := o.Now
	if now == nil {
		return ""
	}
	return now().UTC().Format("2006-01-02T15:04:05Z")
}
