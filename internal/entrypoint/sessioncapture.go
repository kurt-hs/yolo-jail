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
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/render"
	"github.com/mschulkind-oss/yolo-jail/internal/sessionhome"
)

// sessionCaptureHome is one session home found on disk for a pass to capture from.
type sessionCaptureHome struct {
	decl sessionDecl
	id   string
	home string
}

// findSessionHomes lists every session home of every declaration on disk, except own's (this
// pass's own, which it is about to render and has nothing yet to capture).
func findSessionHomes(decls []sessionDecl, own string) []sessionCaptureHome {
	var out []sessionCaptureHome
	for _, d := range decls {
		entries, err := os.ReadDir(sessionhome.SessionsRoot(d.shared))
		if err != nil {
			continue
		}
		for _, ent := range entries {
			id := ent.Name()
			if !ent.IsDir() || id == own || !validSessionID(id) {
				continue
			}
			home, err := sessionhome.Home(d.shared, id)
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
	pass.jail = !sessionPass && e.Getenv(JailMainEnv) == JailMainHold
	pass.release = holdSessionRenderLock(e)
	own := ""
	if sessionPass {
		own = os.Getenv(SessionIDEnv)
	}
	e.captureHomes = findSessionHomes(decls, own)
	noteSessionHomeStrays(e, e.captureHomes)
	return pass
}

// end closes the pass: the jail's own boot removes the homes it captured from, then the lock
// goes.
func (p *sessionCapturePass) end(e *Env) {
	if p.jail {
		retireSessionHomes(e, e.captureHomes)
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

// noteSessionHomeStrays is the one warning line per pass naming every stray, with the next step.
func noteSessionHomeStrays(e *Env, homes []sessionCaptureHome) {
	var found []string
	packs := map[string]bool{}
	for _, h := range homes {
		for _, name := range homeStrays(h) {
			found = append(found, filepath.Join(h.home, name))
			packs[h.decl.pack] = true
		}
	}
	if len(found) == 0 {
		return
	}
	var names []string
	for p := range packs {
		names = append(names, p)
	}
	sort.Strings(names)
	e.warn(fmt.Sprintf("Warning: session homes hold %d entr%s yolo neither linked nor rendered, kept "+
		"where they are: %s — pack %s's session_homes declaration should list each name as shared; "+
		"until it does, move what should be shared into the pack's config directory by hand",
		len(found), plural(len(found), "y", "ies"), strings.Join(found, ", "), strings.Join(names, ", ")))
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
		if len(homeStrays(h)) > 0 || e.keepHomes[h.id] {
			kept[h.id] = true
			continue
		}
		if err := os.RemoveAll(h.home); err != nil {
			e.warn("warning: could not remove the leftover session home " + h.home + ": " + err.Error())
			kept[h.id] = true
			continue
		}
		_ = os.Remove(filepath.Dir(h.home)) // the id directory, once no other declaration's home is in it
	}
	for _, h := range homes {
		if !kept[h.id] && sidecars != "" {
			_ = os.RemoveAll(filepath.Join(sidecars, h.id))
		}
		_ = os.Remove(sessionhome.SessionsRoot(h.decl.shared)) // only when empty
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
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
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
		at.captureOnly = true
		at.session = &render.Session{
			Redirects:  []render.Redirect{{From: h.decl.shared, To: h.home}},
			SidecarDir: filepath.Join(sidecars, sessionSidecarsDir, h.id),
			Surfaces:   map[manifest.SurfaceKey]bool{},
		}
		for _, s := range h.decl.surfaces {
			at.session.Surfaces[s.Key()] = true
		}
		t := at.renderTarget()
		baseline := t.LastRenderPath(surface.Agent, surface.Name)
		if _, err := os.Stat(baseline); err != nil {
			continue
		}
		if _, err := os.Lstat(baseline + seededBaselineSuffix); err == nil {
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
			e.warn("warning: capture from " + file + ": " + err.Error())
			continue
		}
		if r.out.ListCaptureJSON != nil {
			if err := writeSidecar(t, prismListCapturePath(&at, surface.Agent, surface.Name),
				string(r.out.ListCaptureJSON)+"\n"); err != nil {
				e.warn("warning: capture from " + file + ": " + err.Error())
			}
		}
		// Captured once: the session's baseline is now the file as it stands.
		if err := writeSidecar(t, baseline, string(render.StripGeneratedHeader(data))); err != nil {
			e.warn("warning: capture from " + file + ": " + err.Error())
		}
		e.note("capture: " + surface.Agent + "/" + surface.Name + " from the session home " + h.home)
	}
}
