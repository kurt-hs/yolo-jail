package render

import (
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/agentcfg/manifest"
)

// A session target sends every path equal to or under a redirect's From to the same place
// under its To, and keeps the BASELINE sidecars of the redirected surfaces in its own dir while
// the overlay and list capture stay on the shared store (SP-D2, SP-D14).
func TestASessionTargetRedirectsPathsAndBaselines(t *testing.T) {
	base := Jail("/home/agent", "/workspace", nil)
	key := manifest.SurfaceKey{Agent: "tool", Name: "settings"}
	s := base.WithSession(&Session{
		Redirects:  []Redirect{{From: "/home/agent/.tool/agent", To: "/home/agent/.tool/.yolo-sessions/ab/agent"}},
		SidecarDir: "/workspace/.yolo/prism/sessions/ab",
		Surfaces:   map[manifest.SurfaceKey]bool{key: true},
	})
	for in, want := range map[string]string{
		"~/.tool/agent/settings.json":    "/home/agent/.tool/.yolo-sessions/ab/agent/settings.json",
		"~/.tool/agent":                  "/home/agent/.tool/.yolo-sessions/ab/agent",
		"~/.tool/agentx/settings.json":   "/home/agent/.tool/agentx/settings.json",
		"~/.config/mcp/mcp.json":         "/home/agent/.config/mcp/mcp.json",
		"/home/agent/.tool/agent/a/b.js": "/home/agent/.tool/.yolo-sessions/ab/agent/a/b.js",
	} {
		if got := s.ExpandHome(in); got != want {
			t.Errorf("ExpandHome(%q) = %q, want %q", in, got, want)
		}
	}
	sess := "/workspace/.yolo/prism/sessions/ab/"
	shared := "/workspace/.yolo/prism/"
	for name, c := range map[string]struct{ got, want string }{
		"last render": {s.LastRenderPath("tool", "settings"), sess + "tool-settings.last_render"},
		"selection":   {s.SelectionPath("tool", "settings"), sess + "tool-settings.selection.json"},
		"provenance":  {s.ProvenancePath("tool", "settings"), sess + "tool-settings.provenance"},
		"insert":      {s.ListRecordPath("tool", "settings"), sess + "tool-settings" + ListRecordSuffix},
		"overlay":     {s.OverlayPath("tool", "settings"), shared + "tool-settings.overlay.json"},
		"capture":     {s.ListCapturePath("tool", "settings"), shared + "tool-settings" + ListCaptureSuffix},
		"other":       {s.LastRenderPath("tool", "models"), shared + "tool-models.last_render"},
	} {
		if c.got != c.want {
			t.Errorf("%s: %q, want %q", name, c.got, c.want)
		}
	}
	skip := base.WithSession(&Session{Skips: []string{"/home/agent/.tool/agent"}})
	if !skip.SessionSkips("/home/agent/.tool/agent/settings.json") || skip.SessionSkips("/home/agent/.tool/agentx") ||
		s.SessionSkips("/home/agent/.tool/agent/settings.json") {
		t.Error("SessionSkips does not answer for exactly the skipped directory")
	}
	// The base target is untouched, and a nil session is the base.
	if base.ExpandHome("~/.tool/agent/settings.json") != "/home/agent/.tool/agent/settings.json" {
		t.Error("WithSession changed its receiver")
	}
	if got := base.WithSession(nil).LastRenderPath("tool", "settings"); got != shared+"tool-settings.last_render" {
		t.Errorf("a nil session moved the baseline: %q", got)
	}
}
