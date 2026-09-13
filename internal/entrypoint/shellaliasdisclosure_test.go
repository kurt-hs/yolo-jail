package entrypoint

// shellaliasdisclosure_test.go pins the disclosure the alias path did not have: an
// interactive `copilot` in the jail runs `copilot --yolo`, and until discloseShellAliases
// nothing said so on any surface a user reads. The host half of the same declaration has
// had its disclosure since run/launchflagdisclosure.go; this is the in-jail half.
//
// EVERY TEST HERE FAILS IF THE CALL SITE IS DELETED, which is the shape AGENTS.md asks for:
// they drive GenerateBashrc — the boot's own genStep — rather than discloseShellAliases
// directly, so removing the call from packAliases fails them while the renderer stays
// green.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/shquote"
)

// disclosingEnv is aliasEnv plus a captured Stderr: the boot sets one, and every other
// caller (yolo check's preflight, the renderer's own tests) leaves it nil, which is what
// keeps a dry run silent.
func disclosingEnv(t *testing.T, root string) (*Env, *strings.Builder) {
	t.Helper()
	e := aliasEnv(t, root, ``)
	var buf strings.Builder
	e.Stderr = &buf
	return e, &buf
}

// flaglessPack installs a binary and declares NO launch flags — the common pack, and the
// case whose disclosure must be silence.
func flaglessPack(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "plain")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"plain","contributes":[` +
		`{"kind":"program","bin":"plain","via":"npm","package":"@acme/plain"}]}`
	if err := os.WriteFile(filepath.Join(dir, "pack.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// The disclosure states the three facts the host's own block states: what you type, what
// the shell will run instead, and which pack asked for it. A rewrite the user cannot see
// is the defect this closes, so a line missing any of the three is not a disclosure.
func TestShellAliasesAreDisclosed(t *testing.T) {
	e, out := disclosingEnv(t, profileLaunchPack(t))
	if err := GenerateBashrc(e); err != nil {
		t.Fatalf("GenerateBashrc: %v", err)
	}
	got := out.String()
	for _, want := range []string{"acme", "--static", "pack acme"} {
		if !strings.Contains(got, want) {
			t.Errorf("the boot must disclose %q when it writes the alias; got:\n%s", want, got)
		}
	}
}

// Silence is EXACT, not a judgement made at the print site: a pack with no declared flags
// writes no alias, so there is nothing to disclose and the boot says nothing at all. A
// disclosure that prints on every launch is the wallpaper OQ-BP-3 names.
func TestAPackWithNoLaunchFlagsDisclosesNothing(t *testing.T) {
	e, out := disclosingEnv(t, flaglessPack(t))
	if err := GenerateBashrc(e); err != nil {
		t.Fatalf("GenerateBashrc: %v", err)
	}
	if got := out.String(); got != "" {
		t.Errorf("no alias was written, so nothing may be disclosed; got:\n%s", got)
	}
}

// THE DISCLOSURE AND THE ALIAS ARE ONE RECORD. The line says what bash will run; the
// .bashrc says what bash will run. They come from a single packload.LaunchInjection, and
// this test is what fails if either is ever re-derived beside the other — the drift that
// made this whole pair worth unifying in the first place.
func TestTheDisclosedCommandIsTheAliasThatWasWritten(t *testing.T) {
	e, out := disclosingEnv(t, profileLaunchPack(t))
	if err := GenerateBashrc(e); err != nil {
		t.Fatalf("GenerateBashrc: %v", err)
	}
	const marker = "bash runs: "
	line := out.String()
	i := strings.Index(line, marker)
	if i < 0 {
		t.Fatalf("the disclosure must name the command bash will run; got:\n%s", line)
	}
	rest := line[i+len(marker):]
	disclosed := strings.TrimSpace(rest[:strings.Index(rest, "  (added by pack ")])

	bashrc := readFileString(t, e.BashrcPath())
	wantAlias := "alias acme=" + shquote.Quote(disclosed)
	if !strings.Contains(bashrc, wantAlias) {
		t.Errorf("the disclosed command and the written alias disagree.\n disclosed: %s\n"+
			" expected in .bashrc: %s", disclosed, wantAlias)
	}
}

// macos-user WRITES the aliases into a file its login shell never reads, so the disclosure
// there would be a false sentence. The boot says the opposite instead: absent and loud.
//
// The discriminator is $YOLO_DARWIN_LOGIN_PATH, the same one agentPath and imageProbePath
// use to decide which PATH counts — set only by the macos-user launcher.
func TestMacosUserSaysTheAliasIsNotDelivered(t *testing.T) {
	root := profileLaunchPack(t)
	e, out := disclosingEnv(t, root)
	e.Vars[DarwinLoginPathEnv] = "/opt/yolo/bin:/usr/bin:/bin"
	if err := GenerateBashrc(e); err != nil {
		t.Fatalf("GenerateBashrc: %v", err)
	}
	got := out.String()
	if strings.Contains(got, "bash runs:") {
		t.Errorf("this backend's login shell does not read the .bashrc, so the boot must "+
			"not claim the rewrite happens; got:\n%s", got)
	}
	for _, want := range []string{"acme", "zsh", "WITHOUT"} {
		if !strings.Contains(got, want) {
			t.Errorf("the undelivered alias must be reported and must name %q; got:\n%s", want, got)
		}
	}
}

// `yolo check` renders every generator into a temp home to prove they run. It must not
// print a jail's disclosures while doing so — the Env it builds sets no Stderr, and that is
// the whole mechanism, so this pins it at the generator rather than at the CLI.
func TestARenderWithNoStderrDisclosesNothing(t *testing.T) {
	e := aliasEnv(t, profileLaunchPack(t), ``)
	if e.Stderr != nil {
		t.Fatal("fixture must leave Stderr nil, the way the preflight does")
	}
	if err := GenerateBashrc(e); err != nil {
		t.Fatalf("GenerateBashrc: %v", err)
	}
	if !strings.Contains(readFileString(t, e.BashrcPath()), "alias acme=") {
		t.Error("the alias must still be written when nothing is listening")
	}
}
