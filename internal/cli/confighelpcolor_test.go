package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/richtext"
)

var configHelpANSI = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestConfigHelpColorsWithoutChangingItsPlainBytes(t *testing.T) {
	baseline := configUsage + "\n"
	if subcommandUsage["config"].text != configUsage || richtext.Strip(configUsage) != configUsage {
		t.Fatal("registered config usage must remain plain text for raw consumers")
	}

	for _, args := range [][]string{
		{"config"},
		{"config", "--help"},
		{"config", "-h"},
		{"config", "help"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			home, cwd := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Chdir(cwd)
			standInTerminal(t)

			t.Setenv("NO_COLOR", "")
			var rc int
			out, errout := captureBoth(t, func() { rc = runConfig(args) })
			if rc != 0 {
				t.Fatalf("runConfig(%q) = %d, want 0", args, rc)
			}
			if errout != "" {
				t.Errorf("help disclosed or did work on stderr: %q", errout)
			}
			if !strings.Contains(out, "\x1b[1m") || !strings.Contains(out, "\x1b[36m") {
				t.Errorf("terminal help did not color both headings and command tokens:\n%s", out)
			}
			if plain := configHelpANSI.ReplaceAllString(out, ""); plain != baseline {
				t.Errorf("ANSI-stripped terminal output changed plain bytes: got %q, want %q", plain, baseline)
			}
			if got := entries(t, cwd); len(got) != 0 {
				t.Errorf("help created workspace files: %v", got)
			}
			if got := entries(t, filepath.Join(home, ".config")); len(got) != 0 {
				t.Errorf("help created user config state: %v", got)
			}

			t.Setenv("NO_COLOR", "1")
			out, errout = captureBoth(t, func() { rc = runConfig(args) })
			if rc != 0 || errout != "" {
				t.Errorf("NO_COLOR help returned %d, stderr %q", rc, errout)
			}
			if out != baseline {
				t.Errorf("NO_COLOR output = %q, want unchanged plain bytes %q", out, baseline)
			}

			t.Setenv("NO_COLOR", "")
			var piped bytes.Buffer
			var pipeErr bytes.Buffer
			if rc = configRunW(args[1:], &piped, &pipeErr); rc != 0 {
				t.Errorf("piped configRunW returned %d", rc)
			}
			if piped.String() != baseline || pipeErr.Len() != 0 {
				t.Errorf("piped output = %q, stderr = %q; want plain help and no disclosure", piped.String(), pipeErr.String())
			}
		})
	}
}

func TestConfigHelpStylesBothHelpFlagAliases(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())
	t.Setenv("NO_COLOR", "")
	standInTerminal(t)

	var rc int
	out, errout := captureBoth(t, func() { rc = runConfig([]string{"config", "--help"}) })
	if rc != 0 || errout != "" {
		t.Fatalf("runConfig(config --help) returned %d, stderr %q", rc, errout)
	}
	if !strings.Contains(out, "\x1b[36m--help, -h\x1b[0m") {
		t.Fatalf("combined --help, -h aliases were not styled as an exact span:\n%s", out)
	}
	if plain := configHelpANSI.ReplaceAllString(out, ""); plain != configUsage+"\n" {
		t.Fatalf("ANSI-stripped help changed plain bytes: got %q, want %q", plain, configUsage+"\n")
	}
}

// Composition verbs now print the same detailed help in the surface namespace; retained config
// runtime verbs keep the config help. Both dispatch paths share the original colorizer.
func TestConfigAndSurfaceVerbHelpIsColoredLikeTheTopLevel(t *testing.T) {
	for _, tc := range []struct{ namespace, verb, usage string }{
		{"surface", "render", surfaceUsage}, {"surface", "ls", surfaceUsage},
		{"surface", "diff", surfaceUsage}, {"surface", "reset", surfaceUsage},
		{"surface", "capture", surfaceUsage}, {"surface", "promote", surfaceUsage},
		{"config", "drift", configUsage}, {"config", "dump", configUsage},
	} {
		t.Run(tc.namespace+"/"+tc.verb, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Chdir(t.TempDir())
			standInTerminal(t)
			t.Setenv("NO_COLOR", "")
			var rc int
			out, _ := captureBoth(t, func() {
				if tc.namespace == "surface" {
					rc = runSurface([]string{"surface", tc.verb, "--help"})
				} else {
					rc = runConfig([]string{"config", tc.verb, "--help"})
				}
			})
			if rc != 0 {
				t.Fatalf("%s %s --help = %d", tc.namespace, tc.verb, rc)
			}
			if !strings.Contains(out, "\x1b[1m") || !strings.Contains(out, "\x1b[36m") {
				t.Errorf("%s %s --help was not colored on a terminal:\n%s", tc.namespace, tc.verb, out)
			}
			if plain := configHelpANSI.ReplaceAllString(out, ""); plain != tc.usage+"\n" {
				t.Errorf("%s %s --help changed plain bytes: %q", tc.namespace, tc.verb, plain)
			}
			t.Setenv("NO_COLOR", "1")
			if out, _ = captureBoth(t, func() {
				if tc.namespace == "surface" {
					rc = runSurface([]string{"surface", tc.verb, "--help"})
				} else {
					rc = runConfig([]string{"config", tc.verb, "--help"})
				}
			}); out != tc.usage+"\n" {
				t.Errorf("%s %s --help under NO_COLOR = %q, want plain usage", tc.namespace, tc.verb, out)
			}
		})
	}
}

// A wrapped prose line that starts with a flag is not an entry and stays unstyled.
func TestConfigHelpLeavesWrappedProseUnstyled(t *testing.T) {
	var out bytes.Buffer
	writeConfigUsage(&out, true)
	for _, line := range strings.Split(out.String(), "\n") {
		plain := configHelpANSI.ReplaceAllString(line, "")
		if strings.HasPrefix(plain, "   ") && strings.Contains(line, "\x1b[36m") {
			t.Errorf("a continuation line was styled as an entry: %q", line)
		}
	}
}

// The three verbs that take the caller's color decision print their --help with it, not with a
// decision re-derived from out. A *bytes.Buffer is not a terminal, so re-deriving says "plain".
func TestConfigVerbsPrintHelpInTheColorTheyWereGiven(t *testing.T) {
	verbs := map[string]func(out io.Writer, color bool) int{
		"ls": func(out io.Writer, color bool) int {
			return configLs(configTarget{}, []string{"--help"}, out, io.Discard, color)
		},
		"render": func(out io.Writer, color bool) int {
			return configRender(configTarget{}, []string{"--help"}, out, io.Discard, color)
		},
		"drift": func(out io.Writer, color bool) int {
			return configDrift([]string{"--help"}, out, io.Discard, color)
		},
	}
	for name, run := range verbs {
		t.Run(name, func(t *testing.T) {
			t.Setenv("NO_COLOR", "")
			os.Unsetenv("NO_COLOR")

			var colored bytes.Buffer
			if rc := run(&colored, true); rc != 0 {
				t.Fatalf("%s --help with color = %d, want 0", name, rc)
			}
			if !strings.Contains(colored.String(), "\x1b[") {
				t.Errorf("%s --help given color=true printed no ANSI escape:\n%s", name, colored.String())
			}

			var plain bytes.Buffer
			if rc := run(&plain, false); rc != 0 {
				t.Fatalf("%s --help without color = %d, want 0", name, rc)
			}
			if strings.Contains(plain.String(), "\x1b[") {
				t.Errorf("%s --help given color=false printed an ANSI escape:\n%s", name, plain.String())
			}
		})
	}
}

func entries(t *testing.T, dir string) []string {
	t.Helper()
	got, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(got))
	for i := range got {
		names[i] = got[i].Name()
	}
	return names
}
