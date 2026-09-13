package config

import (
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

func TestResolveLSPInstalls(t *testing.T) {
	// Empty -> two empty strings.
	if npm, go_ := ResolveLSPInstalls(nil); npm != "" || go_ != "" {
		t.Errorf("empty => (%q, %q), want empty", npm, go_)
	}
	// python + typescript -> npm list only. The mcp-language-server bridge is GONE
	// with the gemini agent (A1): gemini was its only consumer, so a non-go LSP set
	// must now pull NOTHING from go.
	npm, go_ := ResolveLSPInstalls([]string{"python", "typescript"})
	if npm != "pyright\ntypescript-language-server\ntypescript" {
		t.Errorf("npm = %q", npm)
	}
	if go_ != "" {
		t.Errorf("go = %q, want empty (the gemini MCP bridge is removed)", go_)
	}
	// go server -> gopls, and only gopls.
	_, go2 := ResolveLSPInstalls([]string{"go"})
	if go2 != "golang.org/x/tools/gopls@latest" {
		t.Errorf("go server = %q", go2)
	}
	// Custom-only (unknown) name contributes nothing to either list.
	npm3, go3 := ResolveLSPInstalls([]string{"customlsp"})
	if npm3 != "" || go3 != "" {
		t.Errorf("custom-only => (%q, %q), want both empty", npm3, go3)
	}
}

// LSPInstalls is the entry BOTH backends call — the container's podman `-e` lines and the
// macos-user plan builder — so the config-reading half is pinned here rather than left to
// each caller's own accessor.
func TestLSPInstallsReadsTheConfigKey(t *testing.T) {
	if npm, go_ := LSPInstalls(nil); npm != "" || go_ != "" {
		t.Errorf("nil config => (%q, %q), want empty", npm, go_)
	}
	if npm, go_ := LSPInstalls(jsonx.NewOrderedMap()); npm != "" || go_ != "" {
		t.Errorf("no lsp_servers => (%q, %q), want empty", npm, go_)
	}

	servers := jsonx.NewOrderedMap()
	servers.Set("python", jsonx.NewOrderedMap())
	servers.Set("go", jsonx.NewOrderedMap())
	cfg := jsonx.NewOrderedMap()
	cfg.Set("lsp_servers", servers)

	npm, goPkgs := LSPInstalls(cfg)
	if npm != "pyright" {
		t.Errorf("npm = %q, want pyright", npm)
	}
	if goPkgs != "golang.org/x/tools/gopls@latest" {
		t.Errorf("go = %q, want gopls", goPkgs)
	}
	// A value of the wrong SHAPE must not panic or invent servers — validation lives in
	// validate.go and this resolver is called on already-merged config either way.
	bad := jsonx.NewOrderedMap()
	bad.Set("lsp_servers", "python")
	if npm, go_ := LSPInstalls(bad); npm != "" || go_ != "" {
		t.Errorf("non-map lsp_servers => (%q, %q), want empty", npm, go_)
	}
}
