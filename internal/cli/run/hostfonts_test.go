package run

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/runtime"
)

// fontsArgv assembles a podman (or rt) argv with share_host_fonts set to share, HOME at home.
func fontsArgv(t *testing.T, rt string, share, macOS bool, hostDirs ...string) (argv []string, home, printed string) {
	t.Helper()
	return fontsArgvWith(t, rt, func(home string) {
		for _, d := range hostDirs {
			mkdirs(t, filepath.Join(home, d))
		}
	}, func(o *Options, _ *assembleInput) {
		o.ShareHostFontsConfig = func() bool { return share }
		o.IsMacOS, o.IsLinux = macOS, !macOS
	})
}

// fontsArgvWith is fontsArgv with the host home prepared by setup and the launch shaped by shape.
func fontsArgvWith(t *testing.T, rt string, setup func(home string), shape func(*Options, *assembleInput)) (argv []string, home, printed string) {
	t.Helper()
	home = evalTempDir(t)
	t.Setenv("HOME", home)
	setup(home)
	ws := t.TempDir()
	emptyLoopholeDirs(t)
	wsState := filepath.Join(ws, ".yolo", "home")
	if err := os.MkdirAll(wsState, 0o755); err != nil {
		t.Fatal(err)
	}
	o := goldenOptions(ws, home)
	var buf bytes.Buffer
	o.Stdout, o.Stderr = &buf, &buf
	sec := jsonx.NewOrderedMap()
	sec.Set("blocked_tools", []any{})
	in := &assembleInput{
		cfg: newConfig("security", sec), rt: rt, cname: "yolo-ws-abcd1234",
		agentsPath: filepath.Join(ws, "agents"), wsState: wsState, miseStore: "/mise-store",
		yoloVersion: "9.9.9-test", mountTargets: map[string]struct{}{},
	}
	shape(o, in)
	argv = o.assembleRunCmd(in)
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
		{source: filepath.Join(h, ".local/share/fonts"), dest: "/home/agent/.local/share/fonts/yolo-host"},
		{source: filepath.Join(h, ".fonts"), dest: "/home/agent/.local/share/fonts/yolo-host-dotfonts"},
	} {
		if !strings.Contains(joined, "-v "+b.source+":"+b.dest+":ro") {
			t.Errorf("argv lacks the read-only bind of %s:\n%s", b.source, joined)
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

// A SEALED BUILD JAIL gets no font bind, whatever the opt-in says (seal.go: a fork build gets no
// host folder).
func TestShareHostFontsBindsNothingIntoASealedBuild(t *testing.T) {
	argv, _, printed := fontsArgvWith(t, "podman", func(home string) {
		mkdirs(t, filepath.Join(home, ".fonts"))
	}, func(o *Options, in *assembleInput) {
		o.ShareHostFontsConfig = func() bool { return true }
		in.sealed = true
	})
	if strings.Contains(strings.Join(argv, " "), hostFontsDir) || strings.Contains(printed, "share_host_fonts") {
		t.Errorf("a sealed build got a font bind or line:\n%s\n%s", strings.Join(argv, " "), printed)
	}
}

// A FONT SOURCE THAT IS NOT A FONT FOLDER of yours is skipped with a line: a ~/.fonts link to
// ~/.ssh, and an XDG_DATA_HOME outside your home.
func TestShareHostFontsSkipsCredentialAndOutsideSources(t *testing.T) {
	outside := evalTempDir(t)
	mkdirs(t, filepath.Join(outside, "fonts"))
	argv, home, printed := fontsArgvWith(t, "podman", func(home string) {
		mkdirs(t, filepath.Join(home, ".ssh"))
		if err := os.Symlink(filepath.Join(home, ".ssh"), filepath.Join(home, ".fonts")); err != nil {
			t.Fatal(err)
		}
	}, func(o *Options, _ *assembleInput) {
		o.ShareHostFontsConfig = func() bool { return true }
		o.Getenv = func(k string) string {
			if k == "XDG_DATA_HOME" {
				return outside
			}
			return ""
		}
	})
	joined := strings.Join(argv, " ")
	if strings.Contains(joined, hostFontsDir) {
		t.Errorf("a credential or outside folder was bound:\n%s", joined)
	}
	for _, w := range []string{
		"not sharing " + filepath.Join(home, ".fonts") + ": it resolves to " + filepath.Join(home, ".ssh"),
		"not sharing " + filepath.Join(outside, "fonts") + ": it resolves to " + filepath.Join(outside, "fonts") + ", which is not a folder inside your home",
	} {
		if !strings.Contains(printed, w) {
			t.Errorf("the launch does not say %q:\n%s", w, printed)
		}
	}

	// An agent's home holds its login token: a ~/.fonts link into ~/.claude is skipped too.
	argv, home, printed = fontsArgvWith(t, "podman", func(home string) {
		mkdirs(t, filepath.Join(home, ".claude", "fonts"))
		if err := os.Symlink(filepath.Join(home, ".claude", "fonts"), filepath.Join(home, ".fonts")); err != nil {
			t.Fatal(err)
		}
	}, func(o *Options, _ *assembleInput) { o.ShareHostFontsConfig = func() bool { return true } })
	if strings.Contains(strings.Join(argv, " "), hostFontsDir) ||
		!strings.Contains(printed, "sits inside "+filepath.Join(home, ".claude")+", where credentials live") {
		t.Errorf("a font folder inside ~/.claude was not refused:\n%s\n%s", strings.Join(argv, " "), printed)
	}

	// A link to a folder of fonts elsewhere in the home is fine, and the bind uses the real path.
	argv, home, printed = fontsArgvWith(t, "podman", func(home string) {
		mkdirs(t, filepath.Join(home, "Dropbox", "fonts"))
		if err := os.Symlink(filepath.Join(home, "Dropbox", "fonts"), filepath.Join(home, ".fonts")); err != nil {
			t.Fatal(err)
		}
	}, func(o *Options, _ *assembleInput) { o.ShareHostFontsConfig = func() bool { return true } })
	if want := "-v " + filepath.Join(home, "Dropbox", "fonts") + ":" + hostFontsDir + "/yolo-host-dotfonts:ro"; !strings.Contains(strings.Join(argv, " "), want) {
		t.Errorf("a linked font folder inside the home is not bound by its real path (%s):\n%s\n%s", want, strings.Join(argv, " "), printed)
	}
}

// The Podman Machine skip names the exact commands that share the folder.
func TestShareHostFontsMachineSkipNamesTheCommands(t *testing.T) {
	o := goldenOptions(t.TempDir(), "/Users/you")
	o.machineShares = &machineSharesProbe{ok: true, shares: runtime.MachineShares{Machine: "dev",
		Shares: []runtime.MachineShare{{Source: "/Users", Target: "/Users"}}}}
	got := o.machineShareCommands("/Volumes/Fonts")
	for _, w := range []string{"podman machine stop dev && podman machine rm dev",
		"podman machine init", "-v /Users:/Users", "podman machine start"} {
		if !strings.Contains(got, w) {
			t.Errorf("machineShareCommands lacks %q: %s", w, got)
		}
	}
}
