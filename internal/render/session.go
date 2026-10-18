package render

// session.go is a SESSION RENDER at a jail target (docs/design/per-session-profiles.md): a
// session's boot pass renders the surfaces inside a pack's declared config directory into that
// session's own SESSION HOME (a term coined in the design, §4.1: a per-session copy of the
// directory, its rendered files the session's own and everything else linked to the shared
// copy). Two things move, and nothing else does:
//
//   - THE SURFACE PATH. A Redirect maps the shared directory onto the session home in
//     ExpandHome, the one place every surface path is expanded, so a reader and a writer of a
//     redirected surface cannot disagree about which file a render is about. SP-D2: the shared
//     copies stay the jail boot's alone.
//   - THE BASELINE SIDECARS of the redirected surfaces (last render, selection record,
//     provenance, insert record) go to the session's own sidecar dir (SP-D14). Prism's sidecars
//     are keyed by agent and surface name only, so a session render keeping the shared ones
//     would overwrite the shared copy's baseline and the next boot would capture a phantom
//     diff. The overlay and the list capture stay SHARED: they are the captured edits every
//     render folds in, and what a capture from a session home writes.

import (
	"path/filepath"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/agentcfg/manifest"
)

// Redirect sends every absolute path equal to or under From to the same relative place under
// To. Both are absolute and clean.
type Redirect struct {
	From string
	To   string
}

// apply returns p redirected, and whether it was.
func (r Redirect) apply(p string) (string, bool) {
	if p == r.From {
		return r.To, true
	}
	if rest, ok := strings.CutPrefix(p, r.From+string(filepath.Separator)); ok {
		return filepath.Join(r.To, rest), true
	}
	return p, false
}

// Session is one session pass's render at a jail target.
type Session struct {
	// Redirects are the declared directories and their session homes.
	Redirects []Redirect
	// SidecarDir is this session's baseline sidecar dir: <SidecarDir()>/sessions/<id>.
	SidecarDir string
	// Surfaces are the surfaces whose path a Redirect moves: the ones whose baseline sidecars
	// live in SidecarDir. A surface outside every declared directory keeps the shared ones.
	Surfaces map[manifest.SurfaceKey]bool
	// Skips are declared directories this session renders NOTHING into: the user's own value of
	// the relocation variable won (SP-D5), so the session reads its own directory, and the
	// shared copy stays the jail boot's alone (SP-D2).
	Skips []string
}

// SessionSkips reports whether the absolute path p lies in a directory this session renders
// nothing into (Session.Skips).
func (t Target) SessionSkips(p string) bool {
	if t.session == nil {
		return false
	}
	for _, dir := range t.session.Skips {
		if p == dir || strings.HasPrefix(p, dir+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// WithSession returns t rendering under s. nil returns t unchanged. Only a jail target renders
// a session: every other notch gets t back.
func (t Target) WithSession(s *Session) Target {
	if s == nil || t.kind != KindJail {
		return t
	}
	t.session = s
	return t
}

// redirect applies the session's redirects to an absolute path.
func (t Target) redirect(p string) string {
	if t.session == nil {
		return p
	}
	for _, r := range t.session.Redirects {
		if out, ok := r.apply(p); ok {
			return out
		}
	}
	return p
}

// sessionSidecar is the session's sidecar for one surface, or "" when the surface keeps the
// shared one (no session, or a surface no redirect moves).
func (t Target) sessionSidecar(agent, name, suffix string) string {
	if t.session == nil || t.session.SidecarDir == "" ||
		!t.session.Surfaces[manifest.SurfaceKey{Agent: agent, Name: name}] {
		return ""
	}
	return filepath.Join(t.session.SidecarDir, agent+"-"+name+suffix)
}
