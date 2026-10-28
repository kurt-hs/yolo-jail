package entrypoint

// sessioncapture.go is CAPTURE FROM SESSION HOMES (docs/design/per-session-profiles.md §4.5,
// ruled by OQ-SP2): a setting a program saves in its session's own config directory (a `pi
// install`, a theme, a default model) reaches every session entered after it. A session home
// (coined in that design: a per-session copy of a pack's declared config directory) holds a
// render the program then edits in place, so without this an edit would live and die with its
// session.
//
// The rules, each the design's:
//
//   - every render pass in a container captures each OTHER session home's rendered files into
//     the shared overlay, by the surface's own capture rule (agentcfg.ComposeStateful against
//     that session's file and that session's baseline), before it renders the surface, under
//     the in-jail render lock;
//   - a file that does not parse (the program mid-write) is skipped this pass, never treated as
//     a deletion; a home with no baseline for the surface is skipped too, since nothing then
//     tells the program's edit from yolo's own render;
//   - the session's baseline is then set to the file as it stands, so one edit is captured once,
//     and two sessions that changed one key resolve to the one captured last;
//   - a running session keeps its own copy: capture writes the shared overlay and that session's
//     baseline, never its file;
//   - the jail's own boot (the main process of a hold jail) captures every home the previous
//     container left, then removes each one, except a home holding a STRAY (coined in the
//     design: an entry neither a link yolo made nor a file it rendered), which it keeps so
//     nothing is lost (SP-D15);
//   - strays are named in one warning line per pass.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/agentcfg/codec"
	"github.com/mschulkind-oss/yolo-jail/internal/agentcfg/manifest"
	"github.com/mschulkind-oss/yolo-jail/internal/nativehome"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/render"
	"github.com/mschulkind-oss/yolo-jail/internal/sessionhome"
)

// sessionCaptureHome is one session home found on disk for a pass to capture from.
type sessionCaptureHome struct {
	decl   sessionDecl
	id     string
	home   string
	shared bool // native compatibility capture only; never retired
}

// findSessionHomes lists every session home of every declaration on disk, except own's (this
// pass's own, which it is about to render and has nothing yet to capture).
func findSessionHomes(decls []sessionDecl, own string) []sessionCaptureHome {
	var out []sessionCaptureHome
	for _, d := range decls {
		entries, err := os.ReadDir(d.root())
		if err != nil {
			continue
		}
		for _, ent := range entries {
			id := ent.Name()
			if !ent.IsDir() || id == own || !validSessionID(id) {
				continue
			}
			home, err := d.home(id)
			if err != nil {
				continue
			}
			if fi, err := os.Lstat(home); err == nil && fi.IsDir() {
				out = append(out, sessionCaptureHome{decl: d, id: id, home: home})
			}
		}
	}
	return out
}

// sessionCapturePass is one container pass's capture state: the homes it captures from, the
// render lock it holds across capture and render, and whether it is the jail's own boot, which
// removes the leftovers after its render.
type sessionCapturePass struct {
	decls   []sessionDecl
	release func()
	jail    bool
	native  bool
	claims  []func()
	failed  bool
}

// beginSessionCapture opens a container pass's capture: it takes the render lock whenever a
// selected pack declares a session home, finds the homes to capture from, and names their strays
// in one line. sessionPass says the pass is a session's, whose own id is never captured from.
func beginSessionCapture(e *Env, packs []*packload.Pack, sessionPass bool) *sessionCapturePass {
	decls := declaredSessionHomes(e, packs)
	pass := &sessionCapturePass{decls: decls, release: func() {}}
	if len(decls) == 0 {
		return pass
	}
	pass.native = e.nativeSessionID != ""
	pass.jail = !sessionPass && e.Getenv(JailMainEnv) == JailMainHold
	if pass.native {
		var err error
		pass.release, err = takeSessionRenderLock(e)
		if err != nil {
			pass.failed = true
			genStep(e, "session_render_lock", func() error { return err })
			return pass
		}
	} else {
		pass.release = holdSessionRenderLock(e)
	}
	own := ""
	if sessionPass {
		own = os.Getenv(SessionIDEnv)
		if pass.native {
			own = e.nativeSessionID
		}
	}
	e.captureHomes = findSessionHomes(decls, own)
	if pass.native {
		// No extra-FD lease alone can prove detached children gone. The account-only
		// census is conservative: another session alive delays capture of all ended homes.
		idle, err := nativeAccountIdle()
		if err != nil || !idle {
			e.captureHomes = nil
		} else {
			claimed := map[string]bool{}
			var safe []sessionCaptureHome
			for _, h := range e.captureHomes {
				if _, seen := claimed[h.id]; !seen {
					release, gone := nativehome.ClaimGone(filepath.Join(h.decl.nativeRoot, h.id))
					claimed[h.id] = gone
					if gone {
						pass.claims = append(pass.claims, release)
					}
				}
				if claimed[h.id] {
					safe = append(safe, h)
				}
			}
			// Legacy shared state is captured first, so ended private-session edits
			// win later in ordinary capture order. Its bytes are never rendered or retired.
			var legacy []sessionCaptureHome
			for _, d := range decls {
				legacy = append(legacy, sessionCaptureHome{decl: d, home: d.shared, shared: true})
			}
			e.captureHomes = append(legacy, safe...)
		}
	}
	noteSessionHomeStrays(e, e.captureHomes)
	return pass
}

// end closes the pass: the jail's own boot removes the homes it captured from, then the lock
// goes.
func (p *sessionCapturePass) end(e *Env) {
	if p.jail || (p.native && genFailuresError(e) == nil) {
		var retired []sessionCaptureHome
		for _, h := range e.captureHomes {
			if !h.shared {
				retired = append(retired, h)
			}
		}
		retireSessionHomes(e, retired)
		if p.native {
			kept := map[string]bool{}
			for _, h := range retired {
				if _, err := os.Lstat(h.home); err == nil || !os.IsNotExist(err) {
					kept[h.id] = true
				}
			}
			done := map[string]bool{}
			for _, h := range retired {
				if !kept[h.id] && !done[h.id] {
					done[h.id] = true
					if err := nativehome.Retire(filepath.Join(h.decl.nativeRoot, h.id)); err != nil {
						e.warn("warning: native session bookkeeping at " + filepath.Join(h.decl.nativeRoot, h.id) + " was kept: " + err.Error() + "; inspect the directory and move unshared entries before retrying yolo")
					} else if sidecars := e.sharedRenderTarget().SidecarDir(); sidecars != "" {
						_ = os.RemoveAll(filepath.Join(sidecars, sessionSidecarsDir, h.id))
					}
				}
			}
		}
	}
	for _, release := range p.claims {
		release()
	}
	e.captureHomes = nil
	p.release()
}

// homeStrays is sessionhome.Strays for one found home, nil on any error.
func homeStrays(h sessionCaptureHome) []string {
	strays, err := sessionhome.Strays(h.decl.spec(h.home))
	if err != nil {
		return nil
	}
	return strays
}

// noteSessionHomeStrays names, once per pass and per declared directory, the strays yolo will KEEP
// when it retires the home, with the reason: a file that may be a renamed render, a name the shared
// directory already holds, or something nested inside a rendered directory. The ones it can move
// are reported when they move (retireSessionHomes), so no entry is named twice.
func noteSessionHomeStrays(e *Env, homes []sessionCaptureHome) {
	kept := map[string][]string{}
	for _, h := range homes {
		if h.shared {
			continue
		}
		spec := h.decl.spec(h.home)
		for _, name := range homeStrays(h) {
			_, why, err := sessionhome.Fate(spec, name)
			if err != nil {
				why = "it cannot be inspected: " + err.Error()
			}
			if why == "" {
				continue // yolo moves this one into the shared directory when the home is retired
			}
			kept[spec.Shared] = append(kept[spec.Shared], filepath.Join(h.home, name)+" ("+why+")")
		}
	}
	shared := make([]string, 0, len(kept))
	for dir := range kept {
		shared = append(shared, dir)
	}
	sort.Strings(shared)
	for _, dir := range shared {
		names := kept[dir]
		sort.Strings(names)
		e.warn(fmt.Sprintf("Warning: session homes hold %d entr%s yolo neither linked nor rendered and "+
			"cannot move into %s, so %s kept where it is: %s",
			len(names), plural(len(names), "y", "ies"), dir, plural(len(names), "it is", "they are"), strings.Join(names, ", ")))
	}
}

// keepSessionHome marks id's home as one the jail's own boot keeps (Env.keepHomes).
func (e *Env) keepSessionHome(id string) {
	if e.keepHomes == nil {
		e.keepHomes = map[string]bool{}
	}
	e.keepHomes[id] = true
}

// retireSessionHomes is the jail's own boot removing every session home the previous container
// left once it has captured them (SP-D7), with that session's baseline sidecars; a home holding
// a stray is kept, with its sidecars, so nothing is lost (SP-D15).
func retireSessionHomes(e *Env, homes []sessionCaptureHome) {
	sidecars := ""
	if dir := e.sharedRenderTarget().SidecarDir(); dir != "" {
		sidecars = filepath.Join(dir, sessionSidecarsDir)
	}
	kept := map[string]bool{}
	for _, h := range homes {
		if e.keepHomes[h.id] {
			kept[h.id] = true
			continue
		}
		spec := h.decl.spec(h.home)
		strays, strayErr := sessionhome.Strays(spec)
		if strayErr != nil {
			e.warn("warning: could not read the leftover session home " + h.home + ": " + strayErr.Error() + "; it was kept")
			kept[h.id] = true
			continue
		}
		if len(strays) > 0 {
			// What the program created in what it was told is its config directory belongs in the
			// shared one (design §3, SHR-D4), and moving it there is what stops the next session
			// creating it again. Whatever cannot go stays, and keeps the home with it — named once
			// by noteSessionHomeStrays, which ran before capture.
			outcomes, err := sessionhome.Repatriate(spec, strays)
			var moved, stayed []string
			for _, o := range outcomes {
				if o.To != "" {
					moved = append(moved, o.Name+" → "+o.To)
				} else {
					stayed = append(stayed, o.Name)
				}
			}
			if len(moved) > 0 {
				e.warn(fmt.Sprintf("moved %s from a session home into %s, where the program meant it; "+
					"later sessions link it", strings.Join(moved, ", "), spec.Shared))
			}
			if err != nil {
				e.warn("warning: could not move what a session created out of " + h.home + ": " + err.Error() + "; it was kept")
				kept[h.id] = true
				continue
			}
			if len(stayed) > 0 {
				kept[h.id] = true
				continue
			}
		}
		if err := os.RemoveAll(h.home); err != nil {
			e.warn("warning: could not remove the leftover session home " + h.home + ": " + err.Error())
			kept[h.id] = true
			continue
		}
		_ = os.Remove(filepath.Dir(h.home)) // the id directory, once no other declaration's home is in it
	}
	for _, h := range homes {
		if !kept[h.id] && sidecars != "" && h.decl.nativeRoot == "" {
			_ = os.RemoveAll(filepath.Join(sidecars, h.id))
		}
		_ = os.Remove(h.decl.root()) // only when empty
	}
	if sidecars != "" {
		_ = os.Remove(sidecars) // only when empty
	}
}

// captureFromSessionHomes captures surface's file in every found session home into the shared
// overlay, before the pass renders surface itself (the file header's rules). l and contribs are
// the layers this pass renders the surface over: the capture is narrowed by the same layers the
// render that follows folds, which is what keeps a provider key or a selection this pass decides
// out of the overlay.
func captureFromSessionHomes(e *Env, surface manifest.Surface, l surfaceLayers, contribs *surfaceContribs) {
	if len(e.captureHomes) == 0 || surface.ResolvedMode() != manifest.ModeStateful {
		return
	}
	shared := e.sharedRenderTarget()
	path := shared.SurfacePath(shared.Prepare(surface))
	sidecars := shared.SidecarDir()
	if sidecars == "" {
		return
	}
	for _, h := range e.captureHomes {
		rel, ok := strings.CutPrefix(path, h.decl.shared+string(filepath.Separator))
		if !ok {
			continue
		}
		file := filepath.Join(h.home, rel)
		fi, err := os.Lstat(file)
		if err != nil || !fi.Mode().IsRegular() {
			if (err != nil && !os.IsNotExist(err)) || (err == nil && !fi.Mode().IsRegular()) {
				e.keepSessionHome(h.id)
			}
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			e.keepSessionHome(h.id)
			continue
		}
		if c, ok := codec.LookupCodec(surface.Codec); ok {
			if _, derr := c.Decode(render.StripGeneratedHeader(data)); derr != nil {
				e.keepSessionHome(h.id)
				e.note(fmt.Sprintf("capture: skipped %s this pass: it does not parse (%v), so it is "+
					"read again at the next entry, never as a deletion", file, derr))
				continue
			}
		}
		at := *e
		at.captureHomes = nil
		at.captureOnly = !h.shared
		at.session = &render.Session{
			Redirects:  []render.Redirect{{From: h.decl.shared, To: h.home}},
			SidecarDir: filepath.Join(sidecars, sessionSidecarsDir, h.id),
			Surfaces:   map[manifest.SurfaceKey]bool{},
		}
		for _, s := range h.decl.surfaces {
			at.session.Surfaces[s.Key()] = true
		}
		if h.shared {
			at.session = nil
		}
		t := at.renderTarget()
		baseline := t.LastRenderPath(surface.Agent, surface.Name)
		if _, err := os.Stat(baseline); err != nil && !h.shared {
			e.keepSessionHome(h.id)
			continue
		}
		if _, err := os.Lstat(baseline + seededBaselineSuffix); err == nil {
			e.keepSessionHome(h.id)
			continue // the session's render never finished: its file is not the user's
		}
		r, err := composeStatefulSurface(&at, surface, l.hostBytes, l.computed, l.inFull, contribs)
		if err != nil {
			e.keepSessionHome(h.id)
			e.warn(fmt.Sprintf("warning: capture from %s skipped: %v", file, err))
			continue
		}
		if err := writeSidecar(t, prismOverlayPath(&at, surface.Agent, surface.Name),
			string(r.out.OverlayJSON)+"\n"); err != nil {
			e.keepSessionHome(h.id)
			e.warn("warning: capture from " + file + ": " + err.Error() + "; repair the sidecar path and retry yolo")
			continue
		}
		if r.out.ListCaptureJSON != nil {
			if err := writeSidecar(t, prismListCapturePath(&at, surface.Agent, surface.Name),
				string(r.out.ListCaptureJSON)+"\n"); err != nil {
				e.keepSessionHome(h.id)
				e.warn("warning: capture from " + file + ": " + err.Error() + "; repair the sidecar path and retry yolo")
				continue
			}
		}
		// Captured once: the session's baseline is now the file as it stands.
		if err := writeSidecar(t, baseline, string(render.StripGeneratedHeader(data))); err != nil {
			e.keepSessionHome(h.id)
			e.warn("warning: capture from " + file + ": " + err.Error() + "; repair the sidecar path and retry yolo")
		}
		e.note("capture: " + surface.Agent + "/" + surface.Name + " from the session home " + h.home)
	}
}

// nativeAccountIdle is the account-only liveness witness; synthetic tests inject outcomes.
var nativeAccountIdle = nativehome.AccountIdle
