package entrypoint

// sessionhome.go is a session pass's SESSION HOMES (docs/design/per-session-profiles.md §4.3
// steps 4-5; "session home" is a term coined there: a per-session copy of the one directory a
// pack's program reads its config from, holding that session's render of the pack's surfaces in
// it and links to everything else). It closes the deadlock §2.3 traces: every entry used to
// re-render the shared ~/.pi/agent files a running session's subagents read, so a later attach on
// another profile changed what an earlier session's children ran on.
//
// The boot step prepare_session_homes (bootsteps.go) runs on a session's pass only, before
// configure_pack_surfaces. For every selected pack's `session_homes` declaration
// (packdecl.SessionHome) it builds the home with internal/sessionhome, points the render target
// at it (render.Session: the surface paths in the declared directory, and their baseline
// sidecars), and exports the declared variable into the session's environment, which execBash's
// shell and every process after it inherit (SP-D4). Core reads the declaration; it knows no agent.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/mschulkind-oss/yolo-jail/internal/agentcfg/manifest"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/render"
	"github.com/mschulkind-oss/yolo-jail/internal/sessionhome"
)

// sessionRenderLockName is the in-jail render lock beside the shared sidecars: a session pass
// holds it across its renders into its session homes, so two attaches render one at a time
// (the plan's task 2; task 4 extends it over capture).
const sessionRenderLockName = ".session-render.lock"

// sessionSidecarsDir is the directory under the shared sidecar store holding each session's own
// baseline sidecars, one directory per session id (SP-D14).
const sessionSidecarsDir = "sessions"

// prepareSessionHomesStep is the boot step: a session's pass with a valid YOLO_SESSION_ID gets a
// session home per declaration, and every other run (the jail's own boot, a raw exec with no id,
// a sealed build, which runs no agent) gets none and renders the shared copies as today.
func prepareSessionHomesStep(b *bootRun) {
	if !b.sessionPass || b.sealed {
		return
	}
	id := os.Getenv(SessionIDEnv)
	if !validSessionID(id) {
		return
	}
	// A load failure is reported once, at configure_pack_surfaces.
	packs, err := b.jailPacks()
	if err != nil {
		return
	}
	genStep(b.e, "prepare_session_homes", func() error { return prepareSessionHomes(b.e, packs, id) })
}

// sessionHomePlan is one declaration resolved at this home.
type sessionHomePlan struct {
	pack string
	env  string
	spec sessionhome.Spec
	// rmw are the in-directory surfaces rendered read-modify-write, seeded from the shared file
	// before the render (SP-D11).
	rmw []manifest.Surface
}

// prepareSessionHomes builds every declared session home for session id, then points e's render
// target at them and exports each declared variable. A home that cannot be made is the step's
// error, and the boot refuses the session on it (SP-D6): falling back to the shared directory
// would silently bring the deadlock back. Nothing is exported or redirected unless every home was
// made.
func prepareSessionHomes(e *Env, packs []*packload.Pack, id string) error {
	base := e.renderTarget()
	// Every selected pack's declared surfaces, read for their PATHS and modes alone: which of
	// them lie in a declared directory. Decoded here rather than planned, because this decides
	// no render (the one loop, surfaceloop.go, does); a malformed declaration is reported there.
	var surfaces []manifest.Surface
	for _, p := range packs {
		if raw := p.Decl.SurfaceContributions(); len(raw) > 0 {
			s, _ := manifest.DecodeSurfaces(raw)
			surfaces = append(surfaces, s...)
		}
	}
	var plans []sessionHomePlan
	// skips are the declared directories this pass renders nothing into: SP-D2 holds whether or
	// not the session got a home, so a preset variable (SP-D5) and a refused session (SP-D6) both
	// leave the shared copies to the jail's own boot.
	var skips []string
	for _, p := range packs {
		for _, decl := range p.Decl.SessionHomes() {
			shared := filepath.Join(e.Home, filepath.FromSlash(strings.TrimSuffix(decl.Dir, "/")))
			// SP-D5: the user's own value wins, in this session alone.
			if v := e.Vars[decl.Env]; v != "" || os.Getenv(decl.Env) != "" {
				e.warn(fmt.Sprintf("Note: pack %s: %s is already set in this session, so the session "+
					"reads the directory you set and gets no config directory of its own; another "+
					"session's profile can change what it reads, so unset the variable for yolo to "+
					"give each session its own", p.Name, decl.Env))
				skips = append(skips, shared)
				continue
			}
			home, err := sessionhome.Home(shared, id)
			if err != nil {
				return err
			}
			plan := sessionHomePlan{pack: p.Name, env: decl.Env, spec: sessionhome.Spec{
				Shared: shared, Home: home, Names: decl.Shared}}
			if len(decl.Prefer) > 0 {
				plan.spec.Prefer = map[string]string{}
				for name, target := range decl.Prefer {
					plan.spec.Prefer[name] = base.ExpandHome(target)
				}
			}
			seen := map[string]bool{}
			for _, s := range surfaces {
				rel, ok := strings.CutPrefix(base.SurfacePath(s), shared+string(filepath.Separator))
				if !ok {
					continue
				}
				top := strings.SplitN(rel, string(filepath.Separator), 2)[0]
				if !seen[top] {
					seen[top] = true
					plan.spec.Rendered = append(plan.spec.Rendered, top)
				}
				if s.ResolvedMode() == manifest.ModeRMW {
					plan.rmw = append(plan.rmw, s)
				}
			}
			plans = append(plans, plan)
		}
	}
	if len(plans) == 0 {
		if len(skips) > 0 {
			e.session = &render.Session{Skips: skips}
		}
		return nil
	}
	// A refused session renders nothing into any declared directory either: the boot refuses
	// after every step has run, and the render below must not reach the shared copies first.
	refuse := func(err error) error {
		for _, pl := range plans {
			skips = append(skips, pl.spec.Shared)
		}
		e.session = &render.Session{Skips: skips}
		return err
	}
	for _, pl := range plans {
		if err := sessionhome.Build(pl.spec); err != nil {
			return refuse(sessionHomeRefusal(pl, err))
		}
		if err := seedRMWSurfaces(base, pl); err != nil {
			return refuse(sessionHomeRefusal(pl, err))
		}
	}
	sess := &render.Session{Surfaces: map[manifest.SurfaceKey]bool{}, Skips: skips}
	if dir := base.SidecarDir(); dir != "" {
		sess.SidecarDir = filepath.Join(dir, sessionSidecarsDir, id)
		if err := os.MkdirAll(sess.SidecarDir, base.SidecarDirMode()); err != nil {
			return refuse(fmt.Errorf("cannot make this session's sidecar directory %s: %w\n\n"+
				"Without it the session's render would overwrite the shared baseline.\n\n"+
				"Check the workspace's .yolo directory, then start the session again:\n  ls -ld %s",
				sess.SidecarDir, err, filepath.Dir(sess.SidecarDir)))
		}
	}
	for _, pl := range plans {
		sess.Redirects = append(sess.Redirects, render.Redirect{From: pl.spec.Shared, To: pl.spec.Home})
		for _, s := range surfaces {
			if strings.HasPrefix(base.SurfacePath(s), pl.spec.Shared+string(filepath.Separator)) {
				sess.Surfaces[s.Key()] = true
			}
		}
	}
	if err := seedSessionBaselines(base, sess, surfaces); err != nil {
		return refuse(err)
	}
	e.session = sess
	for _, pl := range plans {
		setEnvBoth(e, pl.env, pl.spec.Home)
	}
	return nil
}

// seedSessionBaselines gives each redirected `stateful` surface a baseline in the session's own
// sidecar dir, copied from the shared one, before the session's first render of it. Without one
// that render is a FIRST MIGRATION (agentcfg.ComposeStateful): it starts from an empty overlay
// instead of the shared one, so the session's file would lack every captured edit, and the
// persisted overlay, which is shared (SP-D14), would be written back empty, discarding them for
// every later render. With one, and no file yet in the home, the render is steady state with
// nothing to capture: the shared overlay folds in whole. A session baseline already there is kept.
func seedSessionBaselines(base render.Target, sess *render.Session, surfaces []manifest.Surface) error {
	if sess.SidecarDir == "" {
		return nil
	}
	at := base.WithSession(sess)
	for _, s := range surfaces {
		if !sess.Surfaces[s.Key()] || s.ResolvedMode() != manifest.ModeStateful {
			continue
		}
		to := at.LastRenderPath(s.Agent, s.Name)
		if _, err := os.Lstat(to); err == nil {
			continue
		}
		data, err := os.ReadFile(base.LastRenderPath(s.Agent, s.Name))
		if err != nil {
			continue // no shared baseline: the shared copy was never rendered, so nothing was captured
		}
		if err := writeSidecar(at, to, string(data)); err != nil {
			return fmt.Errorf("cannot seed this session's baseline %s: %w\n\n"+
				"Without it the session's render would discard the shared captured edits.\n\n"+
				"Check the workspace's .yolo directory, then start the session again:\n  ls -ld %s",
				to, err, filepath.Dir(to))
		}
	}
	return nil
}

// seedRMWSurfaces copies the shared file of each read-modify-write surface into the session home
// before its render, since an rmw render reads the existing file as its base (SP-D11). A
// surface already in the home keeps its bytes.
func seedRMWSurfaces(base render.Target, pl sessionHomePlan) error {
	for _, s := range pl.rmw {
		from := base.SurfacePath(s)
		rel, _ := filepath.Rel(pl.spec.Shared, from)
		to := filepath.Join(pl.spec.Home, rel)
		if _, err := os.Lstat(to); err == nil {
			continue
		}
		data, err := os.ReadFile(from)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("reading %s to seed the session's copy: %w", from, err)
		}
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(to, data, 0o644); err != nil {
			return fmt.Errorf("seeding %s: %w", to, err)
		}
	}
	return nil
}

// sessionHomeRefusal is SP-D6's message: headline, the facts, then what to run.
func sessionHomeRefusal(pl sessionHomePlan, err error) error {
	root := sessionhome.SessionsRoot(pl.spec.Shared)
	return fmt.Errorf("cannot make this session's own config directory %s for pack %s: %v\n\n"+
		"Without it the session would read the shared %s, which another session's profile "+
		"rewrites, so the session does not start.\n\n"+
		"Free space or clear what is in the way under %s, then start the session again:\n"+
		"  ls -la %s\n  df -h %s",
		pl.spec.Home, pl.pack, err, pl.spec.Shared, root, root, filepath.Dir(root))
}

// holdSessionRenderLock takes the in-jail render lock for a session pass that renders into
// session homes, and returns its release. A pass with no session homes takes nothing, and a lock
// that cannot be opened is a warning, never a refusal: it orders two renders, and each still
// renders only its own session's files.
func holdSessionRenderLock(e *Env) func() {
	if e.session == nil {
		return func() {}
	}
	dir := e.renderTarget().SidecarDir()
	if dir == "" {
		return func() {}
	}
	f, err := os.OpenFile(filepath.Join(dir, sessionRenderLockName), os.O_CREATE|os.O_RDWR, 0o644)
	if err == nil {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
	}
	if err != nil {
		e.warn("warning: could not take the session render lock in " + dir + ": " + err.Error() +
			"; rendering this session's files without it")
		if f != nil {
			_ = f.Close()
		}
		return func() {}
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}
}
