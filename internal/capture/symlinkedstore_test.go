package capture

import (
	"os"
	"path/filepath"
	"testing"
)

// userLink makes link → target owned by someone other than root, as a link the invoking user placed
// is: under root (this jail, some CI) a link is root's, and root's links are trusted anyway.
func userLink(t *testing.T, target, link string) {
	t.Helper()
	must(t, os.MkdirAll(filepath.Dir(link), 0o755))
	must(t, os.Symlink(target, link))
	if os.Geteuid() == 0 {
		must(t, os.Lchown(link, 65534, 65534))
	}
}

// A USER MAY SYMLINK YOLO'S OWN STATE (BG-D22's trust root): the store directory, or the state
// directory above it, is a link the invoking user placed. The integration harness links the
// machine's captures/ into each test's home, and a user moves ~/.local/share/yolo-jail to a bigger
// disk the same way. Admission reaches its staging root as the OS does, links and all, and follows no
// link only below it. Red while the digest walked from / refusing every link root did not place:
// "captures is a symlink owned by uid 1001, not root" (Pack Installs run 39991091730).
func TestAdmitFollowsTheUsersOwnLinkToTheStore(t *testing.T) {
	for _, shape := range []string{"captures", "state-dir"} {
		t.Run(shape, func(t *testing.T) {
			base := t.TempDir()
			real := filepath.Join(base, "elsewhere")
			must(t, os.MkdirAll(filepath.Join(real, "captures"), 0o755))
			state := filepath.Join(base, "home", ".local", "share", "yolo-jail")
			if shape == "captures" {
				userLink(t, filepath.Join(real, "captures"), filepath.Join(state, "captures"))
			} else {
				userLink(t, real, state)
			}
			s := &Store{Dir: filepath.Join(state, "captures")}

			entry, err := s.Admit(stage(t, s, "run-1"))
			if err != nil {
				t.Fatalf("Admit through the user's own link: %v", err)
			}
			if _, err := s.Resolve(entry.Key); err != nil {
				t.Errorf("Resolve after Admit: %v", err)
			}

			work, err := s.Stage("run-2")
			must(t, err)
			out := filepath.Join(work, "out")
			must(t, os.MkdirAll(filepath.Join(out, "tree", "bin"), 0o755))
			must(t, os.WriteFile(filepath.Join(out, "tree", "bin", "x"), []byte("x"), 0o755))
			if _, err := s.AdmitEntry(out); err != nil {
				t.Errorf("AdmitEntry through the user's own link: %v", err)
			}
		})
	}
}

// BELOW THE STAGING ROOT NOTHING IS FOLLOWED (BG-D22): the capture jail's workspace is
// staging/<id>, so a link an installer leaves at <id>/out, naming a directory that holds the host's
// bytes, refuses the admission instead of digesting what it names.
func TestAdmitRefusesALinkPlacedInTheCaptureWorkspace(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	elsewhere := filepath.Join(t.TempDir(), "out")
	must(t, os.MkdirAll(filepath.Join(elsewhere, "tree"), 0o755))
	must(t, os.WriteFile(filepath.Join(elsewhere, "tree", "id_ed25519"), []byte("SECRET"), 0o600))
	work, err := s.Stage("run-1")
	must(t, err)
	userLink(t, elsewhere, filepath.Join(work, "out"))
	if e, err := s.AdmitEntry(filepath.Join(work, "out")); err == nil {
		t.Errorf("admitted %s through a link in the capture workspace", e.Key)
	}
}
