package entrypoint

// sessionhome_test.go pins a session pass's session homes (docs/design/per-session-profiles.md
// §4.3-§4.6, SP-D2..D6) through the REAL boot table: prepare_session_homes and
// configure_pack_surfaces, picked by name out of bootSteps() and run by runSteps over the real
// shipped pi pack with a fixture home. Deleting the step from the table, or the session target's
// redirect, fails these.
//
// The relocation variable is read off the pack's own declaration, never spelled here: core does
// not name an agent (SP-P4).

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"github.com/mschulkind-oss/yolo-jail/internal/sessionhome"
)

const (
	sessionA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	sessionB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// twoProviders are two providers pi does not ship, so each profile's selection is a row yolo writes.
const twoProviders = `{
 "acme":{"endpoints":{"openai":{"base_url":"https://acme.example/v1","wire_api":"openai-chat-completions"}},
   "api_key_env_name":"ACME_API_KEY","models":{"default":"acme-large"}},
 "beta":{"endpoints":{"openai":{"base_url":"https://beta.example/v1","wire_api":"openai-chat-completions"}},
   "api_key_env_name":"BETA_API_KEY","models":{"default":"beta-large"}}}`

// sessionHomeJail is one jail home and workspace that several passes run over, each a fresh Env
// as each is a fresh process in a real jail.
type sessionHomeJail struct {
	home, workspace string
	packs           []*packload.Pack
	// env is the pi pack's declared relocation variable.
	env string
}

func newSessionHomeJail(t *testing.T) *sessionHomeJail {
	t.Helper()
	j := &sessionHomeJail{home: t.TempDir(), workspace: t.TempDir()}
	root := t.TempDir()
	withCtxRoot(t, root, "pi")
	for _, name := range []string{"pi", "zai"} {
		p, err := embeddedPack(name)
		if err != nil {
			t.Fatal(err)
		}
		j.packs = append(j.packs, p)
	}
	homes := j.packs[0].Decl.SessionHomes()
	if len(homes) != 1 {
		t.Fatalf("the pi pack declares %d session homes", len(homes))
	}
	j.env = homes[0].Env
	// Restore whatever the process had, and start every pass with the variable unset.
	t.Setenv(j.env, "")
	t.Setenv(SessionIDEnv, "")
	// A pre-existing entry the program made in its shared dir.
	if err := os.MkdirAll(filepath.Join(j.home, ".pi", "agent", "extensions"), 0o755); err != nil {
		t.Fatal(err)
	}
	return j
}

// pass runs one boot pass on profile (a provider name). id "" leaves YOLO_SESSION_ID unset.
func (j *sessionHomeJail) pass(t *testing.T, id, profile string, sessionPass bool, vars ...string) (*Env, *bytes.Buffer) {
	t.Helper()
	_ = os.Unsetenv(j.env)
	if id == "" {
		_ = os.Unsetenv(SessionIDEnv)
	} else {
		_ = os.Setenv(SessionIDEnv, id)
	}
	use := `{"pi":"` + profile + `"}`
	if profile == "" {
		use = `{}` // no profile selected for pi
	}
	errw := &bytes.Buffer{}
	e := &Env{Home: j.home, Workspace: j.workspace, Stderr: errw, Vars: map[string]string{
		"YOLO_PROVIDERS": composedFor(t, twoProviders, ""), "YOLO_USE_PROFILES": use,
		"YOLO_PROFILES": nameAsProviderTable(t, use)}}
	for i := 0; i+1 < len(vars); i += 2 {
		e.Vars[vars[i]] = vars[i+1]
	}
	runSteps(&bootRun{e: e, target: bootContainer, sessionPass: sessionPass, packsLoaded: true, packs: j.packs},
		bootStepsNamed(t, "prepare_session_homes", "configure_pack_surfaces"))
	return e, errw
}

// bootStepsNamed is the real table's steps of these names, in table order.
func bootStepsNamed(t *testing.T, names ...string) []bootStep {
	t.Helper()
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	var out []bootStep
	for _, s := range bootSteps() {
		if want[s.name] {
			out = append(out, s)
			delete(want, s.name)
		}
	}
	if len(want) != 0 {
		t.Fatalf("the boot table has no step %v", want)
	}
	return out
}

func (j *sessionHomeJail) shared(rel string) string {
	return filepath.Join(j.home, ".pi", "agent", rel)
}

func (j *sessionHomeJail) sessionHome(t *testing.T, id string) string {
	t.Helper()
	h, err := sessionhome.Home(filepath.Join(j.home, ".pi", "agent"), id)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func readBytes(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func defaultProviderIn(t *testing.T, p string) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(readBytes(t, p), &m); err != nil {
		t.Fatalf("%s: %v", p, err)
	}
	s, _ := m["defaultProvider"].(string)
	return s
}

// cleanPass is pass, fatal on any failed step.
func (j *sessionHomeJail) cleanPass(t *testing.T, id, profile string, sessionPass bool) {
	t.Helper()
	e, errw := j.pass(t, id, profile, sessionPass)
	mustBootClean(t, e, errw)
}

func mustBootClean(t *testing.T, e *Env, errw *bytes.Buffer) {
	t.Helper()
	if fails := e.GenFailures(); len(fails) != 0 {
		t.Fatalf("the pass failed: %v\n%s", fails, errw.String())
	}
}

// 1, 3: a session pass renders the declared directory's surfaces into its own home, links the
// rest, and exports the declared variable into e.Vars and the process environment.
func TestASessionPassRendersItsOwnSessionHome(t *testing.T) {
	j := newSessionHomeJail(t)
	j.cleanPass(t, "", "acme", false)
	e, errw := j.pass(t, sessionA, "beta", true)
	mustBootClean(t, e, errw)

	home := j.sessionHome(t, sessionA)
	for _, name := range []string{"settings.json", "models.json", "yolo-model-lists.json",
		"yolo-openai-codex-models.json", "mcp.json"} {
		fi, err := os.Lstat(filepath.Join(home, name))
		if err != nil || !fi.Mode().IsRegular() {
			t.Errorf("%s in the session home is not a regular file: %v", name, err)
		}
	}
	for _, name := range []string{"auth.json", "sessions", "extensions", "models-store.json"} {
		target, err := os.Readlink(filepath.Join(home, name))
		if err != nil || target != j.shared(name) {
			t.Errorf("%s -> %q (%v), want a link to %s", name, target, err, j.shared(name))
		}
	}
	// The dangling models-store.json link's first write creates the shared file.
	if _, err := os.Stat(j.shared("models-store.json")); !os.IsNotExist(err) {
		t.Fatalf("models-store.json exists before any write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "models-store.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(j.shared("models-store.json")); err != nil {
		t.Errorf("a write in the session home did not create the shared file: %v", err)
	}
	if got := defaultProviderIn(t, filepath.Join(home, "settings.json")); got != "beta" {
		t.Errorf("the session's settings.json selects %q, want its own profile's beta", got)
	}
	if got := defaultProviderIn(t, j.shared("settings.json")); got != "acme" {
		t.Errorf("the shared settings.json selects %q, want the boot's acme", got)
	}
	if e.Vars[j.env] != home || os.Getenv(j.env) != home {
		t.Errorf("%s = %q (Vars) / %q (process), want %s", j.env, e.Vars[j.env], os.Getenv(j.env), home)
	}
}

// 2: THE DEADLOCK'S UNIT FORM. Session A on beta, then session B on acme: B's pass leaves A's home
// byte-identical and the shared settings.json byte-identical, because a session pass never
// writes the shared copy (SP-D2).
func TestALaterSessionChangesNothingAnEarlierOneReads(t *testing.T) {
	j := newSessionHomeJail(t)
	j.cleanPass(t, "", "acme", false)
	sharedBefore := readBytes(t, j.shared("settings.json"))
	j.cleanPass(t, sessionA, "beta", true)
	homeA := j.sessionHome(t, sessionA)
	aBefore := map[string][]byte{}
	for _, name := range []string{"settings.json", "models.json", "yolo-model-lists.json", "mcp.json"} {
		aBefore[name] = readBytes(t, filepath.Join(homeA, name))
	}
	if !bytes.Equal(readBytes(t, j.shared("settings.json")), sharedBefore) {
		t.Fatal("session A's pass rewrote the shared settings.json")
	}

	j.cleanPass(t, sessionB, "acme", true)
	for name, before := range aBefore {
		if !bytes.Equal(readBytes(t, filepath.Join(homeA, name)), before) {
			t.Errorf("session B's pass changed session A's %s", name)
		}
	}
	if !bytes.Equal(readBytes(t, j.shared("settings.json")), sharedBefore) {
		t.Error("session B's pass rewrote the shared settings.json")
	}
	if got := defaultProviderIn(t, filepath.Join(j.sessionHome(t, sessionB), "settings.json")); got != "acme" {
		t.Errorf("session B's settings.json selects %q, want acme", got)
	}
	// Each session keeps its own baseline sidecars; the shared baseline is the boot's.
	sess := filepath.Join(j.workspace, ".yolo", "prism", "sessions", sessionA, "pi-settings.last_render")
	if _, err := os.Stat(sess); err != nil {
		t.Errorf("session A's baseline sidecar is missing: %v", err)
	}
}

// 4: SP-D5. A session whose environment already sets the variable gets no home, and one line
// names the variable.
func TestAPresetVariableWins(t *testing.T) {
	j := newSessionHomeJail(t)
	j.cleanPass(t, "", "acme", false)
	mine := t.TempDir()
	e, errw := j.pass(t, sessionA, "beta", true, j.env, mine)
	mustBootClean(t, e, errw)
	if _, err := os.Lstat(j.sessionHome(t, sessionA)); !os.IsNotExist(err) {
		t.Errorf("a session home was made though %s was set: %v", j.env, err)
	}
	if e.Vars[j.env] != mine {
		t.Errorf("%s = %q, want the user's %q", j.env, e.Vars[j.env], mine)
	}
	if n := strings.Count(errw.String(), j.env); n != 1 {
		t.Errorf("the pass named %s %d times, want once:\n%s", j.env, n, errw.String())
	}
	if got := defaultProviderIn(t, j.shared("settings.json")); got != "acme" {
		t.Errorf("the shared settings.json selects %q: a pass with no home still never writes it", got)
	}
}

// 5: SP-D6. A home that cannot be made fails the step, so the boot refuses, naming the path.
func TestAHomeThatCannotBeMadeRefusesTheSession(t *testing.T) {
	j := newSessionHomeJail(t)
	j.cleanPass(t, "", "acme", false)
	sharedBefore := readBytes(t, j.shared("settings.json"))
	sessions := sessionhome.SessionsRoot(filepath.Join(j.home, ".pi", "agent"))
	if err := os.WriteFile(sessions, []byte("in the way"), 0o644); err != nil {
		t.Fatal(err)
	}
	e, errw := j.pass(t, sessionA, "beta", true)
	err := genFailuresError(e)
	if err == nil {
		t.Fatalf("the boot did not refuse:\n%s", errw.String())
	}
	if !strings.Contains(err.Error(), "prepare_session_homes") || !strings.Contains(errw.String(), sessions) {
		t.Errorf("the refusal does not name the step and the path %s:\n%v\n%s", sessions, err, errw.String())
	}
	if os.Getenv(j.env) != "" {
		t.Errorf("a refused session exported %s=%s", j.env, os.Getenv(j.env))
	}
	if !bytes.Equal(readBytes(t, j.shared("settings.json")), sharedBefore) {
		t.Error("a refused session's render rewrote the shared settings.json before the refusal")
	}
}

// 6: agents links to ~/.agents when it exists, else to the shared agents/.
func TestAgentsLinksToTheHomesAgentsWhenThere(t *testing.T) {
	j := newSessionHomeJail(t)
	j.cleanPass(t, "", "acme", false)
	j.cleanPass(t, sessionA, "acme", true)
	if got, _ := os.Readlink(filepath.Join(j.sessionHome(t, sessionA), "agents")); got != j.shared("agents") {
		t.Errorf("without ~/.agents, agents -> %q, want %s", got, j.shared("agents"))
	}
	dot := filepath.Join(j.home, ".agents")
	if err := os.MkdirAll(dot, 0o755); err != nil {
		t.Fatal(err)
	}
	j.cleanPass(t, sessionB, "acme", true)
	if got, _ := os.Readlink(filepath.Join(j.sessionHome(t, sessionB), "agents")); got != dot {
		t.Errorf("with ~/.agents, agents -> %q, want %s", got, dot)
	}
}

// 7: no session id, or the jail's own boot, makes no home and renders the shared copy as today.
func TestNoSessionIDMakesNoHome(t *testing.T) {
	j := newSessionHomeJail(t)
	j.cleanPass(t, sessionA, "acme", false) // the jail's own boot, even with an id
	j.cleanPass(t, "", "beta", true)        // a session pass with no id
	if _, err := os.Lstat(sessionhome.SessionsRoot(filepath.Join(j.home, ".pi", "agent"))); !os.IsNotExist(err) {
		t.Errorf("a session home was made: %v", err)
	}
	if got := defaultProviderIn(t, j.shared("settings.json")); got != "beta" {
		t.Errorf("the shared settings.json selects %q, want the last pass's beta", got)
	}
	if os.Getenv(j.env) != "" {
		t.Errorf("%s was exported with no session home", j.env)
	}
	j.cleanPass(t, "not-hex", "beta", true)
	if _, err := os.Lstat(sessionhome.SessionsRoot(filepath.Join(j.home, ".pi", "agent"))); !os.IsNotExist(err) {
		t.Errorf("an invalid session id made a session home: %v", err)
	}
}

// The prelaunch refresh's watched files resolve in the session home: with the declared variable
// set, the shipped pi launcher reads `$VAR/settings.json` for its worth-running test and its
// content key, not the shared copy the jail boot wrote (per-session-profiles.md §5).
func TestTheLaunchersRefreshReadsTheSessionsOwnSettings(t *testing.T) {
	pi, err := embeddedPack("pi")
	if err != nil {
		t.Fatal(err)
	}
	env := pi.Decl.SessionHomes()[0].Env
	for _, tc := range []struct {
		name, shared, session string
		refresh               bool
	}{
		{"the session's settings name a raw package", `{"packages":["~/.pi/agent/yolo-ext/x"]}`, `{"packages":["npm:a"]}`, true},
		{"only the shared settings do", `{"packages":["npm:a"]}`, `{"packages":["~/.pi/agent/yolo-ext/x"]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			launcher, home, log := shippedPiLauncher(t)
			writePiSettings(t, home, tc.shared)
			session := filepath.Join(home, ".pi", ".yolo-sessions", sessionA, "agent")
			if err := os.MkdirAll(session, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(session, "settings.json"), []byte(tc.session), 0o644); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(launcher, "--help")
			cmd.Dir = home
			cmd.Env = []string{"HOME=" + home, "PATH=" + os.Getenv("PATH"), "TMPDIR=" + t.TempDir(), env + "=" + session}
			cmd.Stdin = strings.NewReader("")
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("the shipped pi launcher failed: %v\n%s", err, out)
			}
			if got := countLine(logLines(t, log), "REFRESH") == 1; got != tc.refresh {
				t.Errorf("refreshed = %v, want %v\n%s", got, tc.refresh, out)
			}
		})
	}
}

// A session home's render is the full file, the shared overlay's captured edits included (§4.4),
// and a session pass leaves that overlay as it found it: a session home's first render has no
// baseline of its own, and an unseeded one would compose as a first migration, ignoring the
// shared overlay and then persisting an empty one over it.
func TestASessionHomeCarriesTheSharedCapturedEdits(t *testing.T) {
	j := newSessionHomeJail(t)
	j.cleanPass(t, "", "acme", false)
	setJSONKey(t, j.shared("settings.json"), "theme", "captured-theme")
	j.cleanPass(t, "", "acme", false) // the boot captures the edit into the shared overlay
	overlay := filepath.Join(j.workspace, ".yolo", "prism", "pi-settings.overlay.json")
	if !strings.Contains(string(readBytes(t, overlay)), "captured-theme") {
		t.Fatalf("the fixture's boot did not capture the edit: %s", readBytes(t, overlay))
	}
	j.cleanPass(t, sessionA, "beta", true)
	if got := jsonKey(t, filepath.Join(j.sessionHome(t, sessionA), "settings.json"), "theme"); got != "captured-theme" {
		t.Errorf("the session's settings.json theme = %v, want the captured edit", got)
	}
	if !strings.Contains(string(readBytes(t, overlay)), "captured-theme") {
		t.Errorf("the session pass dropped the shared overlay's captured edit: %s", readBytes(t, overlay))
	}
}

// setJSONKey sets one top-level key of a JSON file, the shape of an in-program settings change.
func setJSONKey(t *testing.T, p, key string, value any) {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(readBytes(t, p), &m); err != nil {
		t.Fatal(err)
	}
	m[key] = value
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, out, 0o644); err != nil {
		t.Fatal(err)
	}
}

func jsonKey(t *testing.T, p, key string) any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(readBytes(t, p), &m); err != nil {
		t.Fatalf("%s: %v", p, err)
	}
	return m[key]
}
