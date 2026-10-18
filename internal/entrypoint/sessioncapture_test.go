package entrypoint

// sessioncapture_test.go pins capture from session homes (docs/design/per-session-profiles.md
// §4.5, OQ-SP2) through the real boot table, over the harness sessionhome_test.go builds: a
// setting a program saved in one session's home reaches the sessions entered after it, a
// half-written file is skipped, the jail's own boot captures what the previous container left and
// then removes it, and a stray is named.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/sessionhome"
)

// editSessionSettings is session A's program saving a theme and installing a package, in its home.
func editSessionSettings(t *testing.T, p string) {
	t.Helper()
	setJSONKey(t, p, "theme", "session-theme")
	pkgs, _ := jsonKey(t, p, "packages").([]any)
	setJSONKey(t, p, "packages", append(pkgs, "npm:session-pkg"))
}

func hasPackage(t *testing.T, p, want string) bool {
	t.Helper()
	pkgs, _ := jsonKey(t, p, "packages").([]any)
	for _, v := range pkgs {
		if v == want {
			return true
		}
	}
	return false
}

func TestASettingSavedInOneSessionReachesTheNext(t *testing.T) {
	j := newSessionHomeJail(t)
	j.cleanPass(t, "", "acme", false)
	j.cleanPass(t, sessionA, "beta", true)
	aSettings := filepath.Join(j.sessionHome(t, sessionA), "settings.json")
	editSessionSettings(t, aSettings)
	aBefore := readBytes(t, aSettings)
	sharedBaseline := filepath.Join(j.workspace, ".yolo", "prism", "pi-settings.last_render")
	sharedBaselineBefore := readBytes(t, sharedBaseline)

	j.cleanPass(t, sessionB, "acme", true)
	if !bytes.Equal(readBytes(t, sharedBaseline), sharedBaselineBefore) {
		t.Error("the capture moved the shared baseline: it diffs and records against session A's own")
	}
	aBaseline := filepath.Join(j.workspace, ".yolo", "prism", "sessions", sessionA, "pi-settings.last_render")
	if !bytes.Equal(readBytes(t, aBaseline), aBefore) {
		t.Error("session A's baseline is not its file as captured, so the edit would be captured again")
	}
	bSettings := filepath.Join(j.sessionHome(t, sessionB), "settings.json")
	if got := jsonKey(t, bSettings, "theme"); got != "session-theme" {
		t.Errorf("session B's theme = %v, want session A's saved one", got)
	}
	if !hasPackage(t, bSettings, "npm:session-pkg") {
		t.Errorf("session B's packages lack session A's install: %v", jsonKey(t, bSettings, "packages"))
	}
	if got := defaultProviderIn(t, bSettings); got != "acme" {
		t.Errorf("session B selects %q: a capture must not carry A's profile", got)
	}
	if !bytes.Equal(readBytes(t, aSettings), aBefore) {
		t.Error("session B's pass changed session A's settings.json: a running session keeps its own copy")
	}
	// Captured once: a third session's pass captures nothing new from A, and still has it.
	j.cleanPass(t, "cccccccccccccccccccccccccccccccc", "beta", true)
	if got := jsonKey(t, filepath.Join(j.sessionHome(t, "cccccccccccccccccccccccccccccccc"), "settings.json"), "theme"); got != "session-theme" {
		t.Errorf("a later session's theme = %v", got)
	}
}

func TestAHalfWrittenSessionFileIsSkippedNeverADeletion(t *testing.T) {
	j := newSessionHomeJail(t)
	j.cleanPass(t, "", "acme", false)
	setJSONKey(t, j.shared("settings.json"), "theme", "boot-theme")
	j.cleanPass(t, "", "acme", false)
	j.cleanPass(t, sessionA, "beta", true)
	aSettings := filepath.Join(j.sessionHome(t, sessionA), "settings.json")
	if err := os.WriteFile(aSettings, []byte(`{"theme": "half`), 0o644); err != nil {
		t.Fatal(err)
	}
	baseline := filepath.Join(j.workspace, ".yolo", "prism", "sessions", sessionA, "pi-settings.last_render")
	baselineBefore := readBytes(t, baseline)
	e, errw := j.pass(t, sessionB, "acme", true)
	mustBootClean(t, e, errw)
	if !bytes.Equal(readBytes(t, baseline), baselineBefore) {
		t.Error("a skipped capture moved session A's baseline, so its next whole edit would never be captured")
	}
	if got := jsonKey(t, filepath.Join(j.sessionHome(t, sessionB), "settings.json"), "theme"); got != "boot-theme" {
		t.Errorf("session B's theme = %v, want the captured boot-theme kept", got)
	}
	if string(readBytes(t, aSettings)) != `{"theme": "half` {
		t.Error("the pass touched session A's half-written file")
	}
}

func TestTheJailsBootCapturesLeftoverHomesThenRemovesThem(t *testing.T) {
	j := newSessionHomeJail(t)
	j.cleanPass(t, "", "acme", false)
	j.cleanPass(t, sessionA, "beta", true)
	editSessionSettings(t, filepath.Join(j.sessionHome(t, sessionA), "settings.json"))

	// The next container's own boot: the main process of a hold jail.
	e, errw := j.pass(t, "", "acme", false, JailMainEnv, JailMainHold)
	mustBootClean(t, e, errw)
	if got := jsonKey(t, j.shared("settings.json"), "theme"); got != "session-theme" {
		t.Errorf("the shared theme = %v, want the leftover session's saved one", got)
	}
	if !hasPackage(t, j.shared("settings.json"), "npm:session-pkg") {
		t.Errorf("the shared packages lack the leftover session's install")
	}
	if _, err := os.Lstat(sessionhome.SessionsRoot(filepath.Join(j.home, ".pi", "agent"))); !os.IsNotExist(err) {
		t.Errorf("the leftover session homes were not removed: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(j.workspace, ".yolo", "prism", "sessions")); !os.IsNotExist(err) {
		t.Errorf("the leftover session sidecars were not removed: %v", err)
	}
	// A pass that is not the hold jail's boot removes nothing.
	j.cleanPass(t, sessionB, "acme", true)
	j.cleanPass(t, "", "acme", false)
	if _, err := os.Lstat(j.sessionHome(t, sessionB)); err != nil {
		t.Errorf("a pass that is not the jail's own boot removed a session home: %v", err)
	}
}

func TestAStrayIsNamedOnceAndKept(t *testing.T) {
	j := newSessionHomeJail(t)
	j.cleanPass(t, "", "acme", false)
	j.cleanPass(t, sessionA, "beta", true)
	stray := filepath.Join(j.sessionHome(t, sessionA), "new-state.json")
	if err := os.WriteFile(stray, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	e, errw := j.pass(t, sessionB, "acme", true)
	mustBootClean(t, e, errw)
	if n := strings.Count(errw.String(), "new-state.json"); n != 1 {
		t.Errorf("the stray is named %d times, want once:\n%s", n, errw.String())
	}
	// The jail's own boot keeps a home holding a stray, so nothing is lost (SP-D15).
	e, errw = j.pass(t, "", "acme", false, JailMainEnv, JailMainHold)
	mustBootClean(t, e, errw)
	if _, err := os.Stat(stray); err != nil {
		t.Errorf("the boot removed a session home holding a stray: %v", err)
	}
	if _, err := os.Lstat(j.sessionHome(t, sessionB)); !os.IsNotExist(err) {
		t.Errorf("the boot kept a session home holding no stray: %v", err)
	}
}

// M1 of the review: capturing ANOTHER session's home must take only that file's own edits, never
// the capturing pass's profile selection. A pass on no profile capturing a home a profile wrote
// used to read every key that profile's selection wrote as "deselected" and clear it from the
// shared overlay, deleting a default model the user had saved in another session (OQ-SP2).
func TestCapturingAProfiledHomeKeepsAUsersSavedSelection(t *testing.T) {
	j := newSessionHomeJail(t)
	j.cleanPass(t, "", "acme", false)
	j.cleanPass(t, sessionA, "acme", true)
	const d, f, g = "dddddddddddddddddddddddddddddddd", "ffffffffffffffffffffffffffffffff", "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	j.cleanPass(t, d, "", true)
	dSettings := filepath.Join(j.sessionHome(t, d), "settings.json")
	setJSONKey(t, dSettings, "defaultModel", "user-picked")
	setJSONKey(t, dSettings, "defaultProvider", "beta")
	j.cleanPass(t, f, "", true)
	j.cleanPass(t, g, "", true)
	gSettings := filepath.Join(j.sessionHome(t, g), "settings.json")
	if got := jsonKey(t, gSettings, "defaultModel"); got != "user-picked" {
		t.Errorf("a later unprofiled session's defaultModel = %v, want the one saved in session D", got)
	}
	if got := jsonKey(t, gSettings, "defaultProvider"); got != "beta" {
		t.Errorf("a later unprofiled session's defaultProvider = %v, want the one saved in session D", got)
	}
}

// The review's minor 5: the jail's own boot keeps a leftover home whose file it could not capture
// (pi mid-write when the container stopped), so the edit is read again at the next entry.
func TestTheJailsBootKeepsAHomeItCouldNotCapture(t *testing.T) {
	j := newSessionHomeJail(t)
	j.cleanPass(t, "", "acme", false)
	j.cleanPass(t, sessionA, "beta", true)
	aSettings := filepath.Join(j.sessionHome(t, sessionA), "settings.json")
	if err := os.WriteFile(aSettings, []byte(`{"theme": "half`), 0o644); err != nil {
		t.Fatal(err)
	}
	e, errw := j.pass(t, "", "acme", false, JailMainEnv, JailMainHold)
	mustBootClean(t, e, errw)
	if string(readBytes(t, aSettings)) != `{"theme": "half` {
		t.Error("the jail's boot removed or changed a session home whose file it could not capture")
	}
}

// The review's minor 3: a session whose render wrote its file but never replaced the seeded
// baseline is not captured, so its profile's values never reach the shared overlay as edits.
func TestAHomeWhoseRenderNeverFinishedIsNotCaptured(t *testing.T) {
	j := newSessionHomeJail(t)
	j.cleanPass(t, "", "acme", false)
	j.cleanPass(t, sessionA, "beta", true)
	baseline := filepath.Join(j.workspace, ".yolo", "prism", "sessions", sessionA, "pi-settings.last_render")
	if err := os.WriteFile(baseline, readBytes(t, filepath.Join(j.workspace, ".yolo", "prism", "pi-settings.last_render")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(baseline+seededBaselineSuffix, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	overlay := filepath.Join(j.workspace, ".yolo", "prism", "pi-settings.overlay.json")
	before := readBytes(t, overlay)
	// An unprofiled pass: its layers set none of beta's selection, so nothing filters it out.
	j.cleanPass(t, sessionB, "", true)
	if !bytes.Equal(readBytes(t, overlay), before) {
		t.Errorf("an unfinished session render was captured into the shared overlay:\n%s", readBytes(t, overlay))
	}
	if got := defaultProviderIn(t, filepath.Join(j.sessionHome(t, sessionB), "settings.json")); got == "beta" {
		t.Errorf("session B selects session A's beta: A's profile reached the shared overlay")
	}
}
