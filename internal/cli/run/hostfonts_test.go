package run

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

// fontsArgv assembles a podman (or rt) argv with share_host_fonts set to share, HOME at home.
func fontsArgv(t *testing.T, rt string, share, macOS bool, hostDirs ...string) (argv []string, home, printed string) {
	t.Helper()
	home = evalTempDir(t)
	t.Setenv("HOME", home)
	for _, d := range hostDirs {
		mkdirs(t, filepath.Join(home, d))
	}
	ws := t.TempDir()
	emptyLoopholeDirs(t)
	wsState := filepath.Join(ws, ".yolo", "home")
	if err := os.MkdirAll(wsState, 0o755); err != nil {
		t.Fatal(err)
	}
	o := goldenOptions(ws, home)
	o.ShareHostFontsConfig = func() bool { return share }
	o.IsMacOS, o.IsLinux = macOS, !macOS
	var buf bytes.Buffer
	o.Stdout, o.Stderr = &buf, &buf
	sec := jsonx.NewOrderedMap()
	sec.Set("blocked_tools", []any{})
	argv = o.assembleRunCmd(&assembleInput{
		cfg: newConfig("security", sec), rt: rt, cname: "yolo-ws-abcd1234",
		agentsPath: filepath.Join(ws, "agents"), wsState: wsState, miseStore: "/mise-store",
		yoloVersion: "9.9.9-test", mountTargets: map[string]struct{}{},
	})
	return argv, home, buf.String()
}

func evalTempDir(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// THE CALL SITE: with the opt-in, a Linux host's two font folders are bound read-only under the
// jail's xdg fonts dir, each named by its disclosure line; without it, neither is.
func TestShareHostFontsBindsYourFontFoldersReadOnly(t *testing.T) {
	home := evalTempDir(t)
	o := goldenOptions(t.TempDir(), home)
	srcs := o.hostFontSources(home)
	if len(srcs) != 2 || srcs[0].source != filepath.Join(home, ".local/share/fonts") ||
		srcs[1].source != filepath.Join(home, ".fonts") {
		t.Fatalf("Linux sources = %+v", srcs)
	}

	argv, home, printed := fontsArgv(t, "podman", true, false)
	joined := strings.Join(argv, " ")
	// The folders did not exist: each skipped with its line, nothing bound.
	if strings.Contains(joined, hostFontsDir) {
		t.Errorf("a missing font folder was bound:\n%s", joined)
	}
	if !strings.Contains(printed, "share_host_fonts: "+filepath.Join(home, ".fonts")+" does not exist") {
		t.Errorf("a missing font folder was not reported:\n%s", printed)
	}

	// Now with the folders present.
	argv, h, printed := fontsArgv(t, "podman", true, false, ".local/share/fonts", ".fonts")
	joined = strings.Join(argv, " ")
	for _, b := range []hostFontBind{
		{filepath.Join(h, ".local/share/fonts"), "/home/agent/.local/share/fonts/yolo-host"},
		{filepath.Join(h, ".fonts"), "/home/agent/.local/share/fonts/yolo-host-dotfonts"},
	} {
		if !strings.Contains(joined, "-v "+b.source+":"+b.dest+":ro") {
			t.Errorf("argv lacks the read-only bind %s:\n%s", b.bindArg(), joined)
		}
		if !strings.Contains(printed, "Sharing your fonts read-only (share_host_fonts): "+b.source+" → "+b.dest) {
			t.Errorf("no disclosure line for %s:\n%s", b.source, printed)
		}
	}

	off, _, offPrinted := fontsArgv(t, "podman", false, false, ".fonts")
	if strings.Contains(strings.Join(off, " "), hostFontsDir) || strings.Contains(offPrinted, "share_host_fonts") {
		t.Errorf("without the opt-in the launch binds or mentions fonts")
	}
}

// A macOS host shares ~/Library/Fonts; an XDG_DATA_HOME moves the Linux folder.
func TestShareHostFontsSourcesPerHost(t *testing.T) {
	o := goldenOptions(t.TempDir(), "/h")
	o.IsMacOS = true
	if s := o.hostFontSources("/Users/you"); len(s) != 1 || s[0].source != "/Users/you/Library/Fonts" ||
		s[0].dest != hostFontsDir+"/yolo-host" {
		t.Errorf("macOS sources = %+v", s)
	}
	o.IsMacOS = false
	o.Getenv = func(k string) string {
		if k == "XDG_DATA_HOME" {
			return "/data"
		}
		return ""
	}
	if s := o.hostFontSources("/home/you"); s[0].source != "/data/fonts" {
		t.Errorf("XDG_DATA_HOME not honored: %+v", s)
	}
}
