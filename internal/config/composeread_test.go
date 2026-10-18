package config

import (
	"path/filepath"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

// ComposeWithWorkspaceRead over the gate's own read is LoadConfig: the user scope under it and
// the workspace config over it, so a caller comparing the two learns only whether the workspace
// files changed between its reads.
func TestComposeWithWorkspaceReadIsLoadConfig(t *testing.T) {
	ws := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("YOLO_VERSION", "")
	mustWrite(t, filepath.Join(home, ".config", "yolo-jail", "config.jsonc"),
		`{ "packages": ["jq"], "network": { "mode": "bridge" } }`)
	mustWrite(t, filepath.Join(ws, "yolo-jail.jsonc"), `{ "packages": ["ripgrep"], "network": { "ports": ["8000:8000"] } }`)
	mustWrite(t, filepath.Join(ws, "yolo-jail.local.jsonc"), `{ "env": { "A": "1" } }`)

	loaded, err := LoadConfig(ws, true, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	read, err := ReadWorkspaceForGate(ws)
	if err != nil {
		t.Fatal(err)
	}
	composed, err := ComposeWithWorkspaceRead(ws, read.Config)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := jsonx.DumpsSnapshot(loaded)
	b, _ := jsonx.DumpsSnapshot(composed)
	if a != b {
		t.Errorf("composed over the gate's read:\n%s\nwant LoadConfig's:\n%s", b, a)
	}

	// A different workspace read composes to a different config.
	other := jsonx.NewOrderedMap()
	other.Set("packages", []any{"htop"})
	c, err := ComposeWithWorkspaceRead(ws, other)
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := jsonx.DumpsSnapshot(c); s == a {
		t.Error("a different workspace read composed to the same config")
	}
}
