package check

// workspaceconfigname_test.go pins that `yolo check` names the workspace config files the loader
// READ (config.ResolveWorkspaceConfigPath: `yolo-jail.jsonc`, or `yolo-jail.json` where only
// that exists). It parsed both `.json` files and then reported no workspace config at all, and
// its next step for a config finding sent the user to create a `yolo-jail.jsonc`, which would
// shadow the `yolo-jail.json` holding their config.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
)

// The parse line names the `.json` files a workspace keeps its config in, as it names the
// `.jsonc` ones.
func TestCheckNamesTheJSONWorkspaceConfigsItParsed(t *testing.T) {
	var out bytes.Buffer
	o := baseOptions(t, &out)
	o.PathExists = func(p string) bool {
		_, err := os.Stat(p)
		return err == nil
	}
	ws := filepath.Join(o.Workspace, "yolo-jail.json")
	local := filepath.Join(o.Workspace, "yolo-jail.local.json")
	for _, p := range []string{ws, local} {
		if err := os.WriteFile(p, []byte(`{"network": {"mode": "bridge"}}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	Check(o)
	got := out.String()
	if want := "Parsed workspace config: " + ws + " + " + local; !strings.Contains(got, want) {
		t.Errorf("check does not name the .json workspace configs it parsed (want %q):\n%s", want, got)
	}
	if strings.Contains(got, "No workspace config found") {
		t.Errorf("check parsed the workspace's yolo-jail.json and reported no workspace config:\n%s", got)
	}
}

// In a jail, a refusal about a key the host's config set names that host file and the relaunch,
// and `yolo check`'s next step under it says the same: edit the host file, then relaunch — not
// the jail's read-only copy of the user config.
func TestCheckSendsALaunchCopyRefusalToTheHostFile(t *testing.T) {
	// The host half: the user scope a launch generates for a jail, with its record.
	hostHome := t.TempDir()
	t.Setenv("HOME", hostHome)
	t.Setenv("YOLO_VERSION", "")
	t.Setenv(config.UserLayerEnv, "")
	hostDir := filepath.Join(hostHome, ".config", "yolo-jail")
	if err := os.MkdirAll(hostDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hostDir, "config.jsonc"),
		[]byte(`{"providers": {"kilo": {"models": {"default": {"id": "m", "wireId": "w"}}}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	effective, src, err := config.LoadConfigWithoutWorkspaceFileWithSources(t.TempDir(), false, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	filtered, _ := config.FilterInherit(effective, config.InheritPreflight)
	body, err := config.RenderFilteredWithOrigins(filtered, src, config.InheritPreflight, "")
	if err != nil {
		t.Fatal(err)
	}

	// The jail half: that file is the user config `yolo check` reads.
	var out bytes.Buffer
	opts := baseOptions(t, &out)
	t.Setenv("HOME", t.TempDir())
	jailDir := filepath.Join(os.Getenv("HOME"), ".config", "yolo-jail")
	if err := os.MkdirAll(jailDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jailDir, "config.jsonc"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YOLO_VERSION", "9.9.9-test")
	Check(opts)
	got := stripANSI(out.String())
	if !strings.Contains(got, "(set in ~/.config/yolo-jail/config.jsonc on the host;") {
		t.Fatalf("the refusal does not name the host file:\n%s", got)
	}
	if !strings.Contains(got, "Edit the host file this names, then relaunch the jail") ||
		strings.Contains(got, "Fix it in ") {
		t.Errorf("the next step does not send the user to the host file and a relaunch:\n%s", got)
	}
}

// The next step for an unlocated config finding names the workspace config the loader reads.
func TestConfigNoteNamesTheWorkspaceConfigTheLoaderReads(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "yolo-jail.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	note := configNote("config.definitely_not_a_key: unknown key", ws)
	if want := "Fix it in " + filepath.Join(ws, "yolo-jail.json") + " or "; !strings.HasPrefix(note, want) {
		t.Errorf("the next step does not name the workspace's yolo-jail.json (want prefix %q): %q",
			want, note)
	}
}
