package entrypoint

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// countStagedWindows counts, per destination, the installs that built a replacement (the "staged"
// window of overlayInstallHook), through the real install.
func countStagedWindows(t *testing.T) map[string]int {
	t.Helper()
	n := map[string]int{}
	withOverlayInstallHook(t, func(dest, w string) {
		if w == "staged" {
			n[dest]++
		}
	})
	return n
}

func requireSameFile(t *testing.T, a, b, why string) {
	t.Helper()
	ai, err := os.Lstat(a)
	if err != nil {
		t.Fatal(err)
	}
	bi, err := os.Lstat(b)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(ai, bi) {
		t.Fatalf("%s: %s is not the same file as %s", why, a, b)
	}
}

// AN UNCHANGED RELAUNCH REPLACES NOTHING: the delivered files are hard links of the staged ones,
// so the second install of the same staged tree finds every destination already delivered and
// builds no replacement for any of them.
func TestOverlayInstallOfAnUnchangedStagedTreeReplacesNothing(t *testing.T) {
	f := newOverlayFixture(t)
	f.stage(t)
	staged := countStagedWindows(t)
	if err := f.install(t); err != nil {
		t.Fatal(err)
	}
	skill := filepath.Join(f.home, ".pi", "agent", "skills", "demo", "SKILL.md")
	brief := filepath.Join(f.home, ".pi", "agent", "AGENTS.md")
	requireSameFile(t, skill, filepath.Join(f.overlay, ".pi", "agent", "skills", "demo", "SKILL.md"),
		"a delivered skill file is a hard link of the staged one")
	requireSameFile(t, brief, filepath.Join(f.overlay, ".pi", "agent", "AGENTS.md"),
		"a delivered briefing is a hard link of the staged one")
	if staged[".pi/agent/skills"] != 1 || staged[".pi/agent/AGENTS.md"] != 1 {
		t.Fatalf("first install built %v replacements, want one per destination", staged)
	}

	if err := f.install(t); err != nil {
		t.Fatal(err)
	}
	if staged[".pi/agent/skills"] != 1 || staged[".pi/agent/AGENTS.md"] != 1 {
		t.Fatalf("an install of an unchanged staged tree built replacements: %v", staged)
	}
	requireFile(t, skill, "new skill")
	requireFile(t, brief, "new briefing")
}

// A RE-STAGED TREE IS DELIVERED AGAIN: new staged inodes are not the installed ones.
func TestOverlayInstallOfARestagedTreeReplacesEveryDestination(t *testing.T) {
	f := newOverlayFixture(t)
	f.stage(t)
	if err := f.install(t); err != nil {
		t.Fatal(err)
	}
	f.restage(t)
	staged := countStagedWindows(t)
	if err := f.install(t); err != nil {
		t.Fatal(err)
	}
	if staged[".pi/agent/skills"] != 1 || staged[".pi/agent/AGENTS.md"] != 1 {
		t.Fatalf("an install of a re-staged tree built %v replacements, want one per destination", staged)
	}
	requireSameFile(t, filepath.Join(f.home, ".pi", "agent", "skills", "demo", "SKILL.md"),
		filepath.Join(f.overlay, ".pi", "agent", "skills", "demo", "SKILL.md"), "the re-staged skill is delivered")
}

// A DESTINATION CHANGED BETWEEN SESSIONS IS REPLACED, however it changed — the check consults
// nothing the sandbox account could write, only names and inodes. Each case leaves the staged tree
// alone, so only the tampering can make the install rebuild.
func TestOverlayInstallReplacesADestinationChangedBetweenSessions(t *testing.T) {
	skillRel := filepath.Join(".pi", "agent", "skills", "demo", "SKILL.md")
	cases := map[string]func(t *testing.T, home string){
		"a file rewritten with the same bytes": func(t *testing.T, home string) {
			p := filepath.Join(home, skillRel)
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			writeTreeFile(t, p, "new skill")
		},
		"a file added": func(t *testing.T, home string) {
			writeTreeFile(t, filepath.Join(home, ".pi", "agent", "skills", "demo", "EXTRA.md"), "planted")
		},
		"a file removed": func(t *testing.T, home string) {
			if err := os.Remove(filepath.Join(home, skillRel)); err != nil {
				t.Fatal(err)
			}
		},
		"a file swapped for a link": func(t *testing.T, home string) {
			p := filepath.Join(home, skillRel)
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("/etc/hostname", p); err != nil {
				t.Fatal(err)
			}
		},
		"a briefing rewritten": func(t *testing.T, home string) {
			p := filepath.Join(home, ".pi", "agent", "AGENTS.md")
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			writeTreeFile(t, p, "the agent's own instructions")
		},
	}
	for name, tamper := range cases {
		t.Run(name, func(t *testing.T) {
			f := newOverlayFixture(t)
			f.stage(t)
			if err := f.install(t); err != nil {
				t.Fatal(err)
			}
			tamper(t, f.home)
			if err := f.install(t); err != nil {
				t.Fatal(err)
			}
			requireSameFile(t, filepath.Join(f.home, skillRel), filepath.Join(f.overlay, skillRel),
				"the skill is the staged file again")
			requireSameFile(t, filepath.Join(f.home, ".pi", "agent", "AGENTS.md"),
				filepath.Join(f.overlay, ".pi", "agent", "AGENTS.md"), "the briefing is the staged file again")
			requireAbsent(t, filepath.Join(f.home, ".pi", "agent", "skills", "demo", "EXTRA.md"),
				"a planted file survived the install")
		})
	}
}

// A FILE THE FILESYSTEM WILL NOT LINK IS COPIED — a workspace sidecar on another volume (EXDEV) —
// and such a destination is never taken for already delivered, since its inode is its own: it is
// replaced on every install, as every destination was before links.
func TestOverlayInstallCopiesWhatItCannotLink(t *testing.T) {
	orig := overlayLinkat
	overlayLinkat = func(int, string, int, string, int) error { return unix.EXDEV }
	t.Cleanup(func() { overlayLinkat = orig })

	f := newOverlayFixture(t)
	f.stage(t)
	staged := countStagedWindows(t)
	for i := 0; i < 2; i++ {
		if err := f.install(t); err != nil {
			t.Fatal(err)
		}
	}
	skill := filepath.Join(".pi", "agent", "skills", "demo", "SKILL.md")
	requireFile(t, filepath.Join(f.home, skill), "new skill")
	a, _ := os.Lstat(filepath.Join(f.home, skill))
	b, _ := os.Lstat(filepath.Join(f.overlay, skill))
	if os.SameFile(a, b) {
		t.Fatal("a refused link was delivered as a link")
	}
	if staged[".pi/agent/skills"] != 2 {
		t.Fatalf("a copied destination was taken for delivered: %d replacements over two installs, want 2",
			staged[".pi/agent/skills"])
	}
}
