package run

// programoverrides_test.go pins the launch's half of a DEV OVERRIDE (docs/design/pack-buildable-programs.md
// §5) on the container backends: where a checkout appears in the jail, every refusal the host makes
// (each naming the path and the key), and, at Run's own entry point, that the argv binds and hands
// over what was decided, the launch names it, and the overridden program's build is not run.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/hostfloor/floortest"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/macosuser"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// THE MACOS-USER ARM (the plan's Task 10), at Run's own entry point: an override of a selected
// pack's program whose checkout is outside the workspace reaches the backend as a read-only context
// link marked as that program's override, the decision rides the launch env the darwin bootstrap is
// handed (YOLO_PROGRAM_OVERRIDES, which runplan's buildBootstrapEnv relays), and the launch names
// the file that runs. Delete the arm's append to ctx.Links, its launchEnv.Set or its disclosure, and
// this fails; so does putting back the early return that applied none on macos-user.
func TestMacosUserRunsAnOverrideFromItsCheckout(t *testing.T) {
	home := packHome(t)
	pack := t.TempDir()
	if err := os.WriteFile(filepath.Join(pack, "pack.json"), []byte(`{"contributes":[`+
		`{"kind":"program","bin":"x","via":"pack","from":"tools/x","run":"x"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	dev := floortest.ResolvedTemp(t)
	writeConfigFile(t, filepath.Join(home, ".config", "yolo-jail", "config.jsonc"),
		`{"packs":[{"source":"file://`+pack+`","name":"fx"}],"programs":{"overrides":{"x":{"path":"`+dev+`"}}}}`)
	ws := t.TempDir()
	var stdout, stderr bytes.Buffer
	o := dispatchOptions(t, ws, "macos-user", &stdout, &stderr, nil)
	o.DryRun = true
	o.macosCtxSiting = sitingWritable(t, "")
	var ctx macosuser.HostContext
	var env *jsonx.OrderedMap
	o.MacosUserRun = func(_ *jsonx.OrderedMap, _ string, _, _ []string, _, _ string, _ macosuser.HomeOverlay,
		hostCtx macosuser.HostContext, _ bool, packEnv *jsonx.OrderedMap, _ []packload.BlockedTool, _ macosuser.JailDaemons) int {
		ctx, env = hostCtx, packEnv
		return 0
	}
	if rc := Run(*o); rc != 0 {
		t.Fatalf("Run() = %d\nstdout:\n%s\nstderr:\n%s", rc, stdout.String(), stderr.String())
	}
	want := macosuser.ContextLink{Dest: "/ctx/program-overrides/x", Source: dev, Named: dev, Dir: true, Override: "x"}
	found := false
	for _, l := range ctx.Links {
		found = found || l == want
	}
	if !found {
		t.Errorf("the backend was handed no override link %+v; links: %+v", want, ctx.Links)
	}
	raw, _ := env.Get(entrypoint.ProgramOverridesEnv)
	s, _ := raw.(string)
	d, err := entrypoint.ParseProgramOverrides(s)
	if err != nil || d["x"].Dir != dev || d["x"].Run != "x" || d["x"].Refused != "" {
		t.Errorf("the launch env's %s = %q (%v), want x at %s running x", entrypoint.ProgramOverridesEnv, s, err, dev)
	}
	if want := "x: dev override — runs " + filepath.Join(dev, "x"); !strings.Contains(stdout.String(), want) {
		t.Errorf("the launch does not name the file the override runs (%q):\n%s", want, stdout.String())
	}
	if strings.Contains(stderr.String(), "not applied") {
		t.Errorf("the launch still says the override is not applied:\n%s", stderr.String())
	}
}

// macosUserOverridePlans is programOverridePlansFor on the macos-user arm, with siting s.
func macosUserOverridePlans(t *testing.T, ws string, s *macosuser.ContextSiting,
	ov map[string]config.ProgramOverride) map[string]programOverridePlan {
	t.Helper()
	o := &Options{Workspace: ws, Getenv: func(string) string { return "" }, macosCtxSiting: s}
	out := map[string]programOverridePlan{}
	for _, p := range o.programOverridePlansFor("macos-user", overridePacks(t), ov) {
		out[p.bin] = p
	}
	return out
}

// On macos-user a checkout runs from its own host path: inside the workspace with nothing more, and
// outside it as a context link (bindSource), which macosUserOverrideLinks hands the backend. A
// checkout the siting refuses — here one inside a real home, which the account mode's sandbox
// account cannot reach — refuses that one program, naming the reason and the key, and is never
// linked; the run-as-you mode, which reaches your home as you do, runs it.
func TestMacosUserOverridePlacesTheCheckoutAtItsHostPath(t *testing.T) {
	ws := floortest.ResolvedTemp(t)
	inWS := filepath.Join(ws, "dev", "board")
	users := floortest.ResolvedTemp(t)
	inHome := filepath.Join(users, "alice", "code", "board")
	for _, d := range []string{inWS, inHome} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	account := sitingWritable(t, "")
	account.UsersRoot, account.UsersRootAliases = users, nil

	got := macosUserOverridePlans(t, ws, account, map[string]config.ProgramOverride{"board": {Path: inWS}})["board"]
	if got.wire.Dir != inWS || got.wire.Refused != "" || got.bindSource != "" {
		t.Errorf("a checkout inside the workspace: %+v, want it run at %s with no link", got, inWS)
	}

	got = macosUserOverridePlans(t, ws, account, map[string]config.ProgramOverride{"board": {Path: inHome}})["board"]
	if got.wire.Refused == "" || !strings.Contains(got.wire.Refused, "inside the home folder") ||
		!strings.Contains(got.wire.Refused, "programs.overrides.board") || got.bindSource != "" {
		t.Errorf("a checkout in a home under the account mode: %+v, want that program refused, naming why", got)
	}
	if links := macosUserOverrideLinks([]programOverridePlan{got}); len(links) != 0 {
		t.Errorf("a refused override was linked: %+v", links)
	}

	self := macosuser.DarwinContextSitingFor(macosuser.ModeSelf, filepath.Join(users, "alice"))
	self.UsersRoot, self.UsersRootAliases, self.WritableRoots = users, nil, nil
	got = macosUserOverridePlans(t, ws, &self, map[string]config.ProgramOverride{"board": {Path: inHome}})["board"]
	if got.wire.Dir != inHome || got.wire.Refused != "" || got.bindSource != inHome {
		t.Errorf("a checkout in your home under the run-as-you mode: %+v, want it linked and run at %s", got, inHome)
	}
	links := macosUserOverrideLinks([]programOverridePlan{got})
	if len(links) != 1 || links[0].Override != "board" || links[0].RW || links[0].Dest != "/ctx/program-overrides/board" {
		t.Errorf("the override's link: %+v, want one read-only link at /ctx/program-overrides/board", links)
	}
}

func overridePacks(t *testing.T) []*packload.Pack {
	t.Helper()
	return []*packload.Pack{inlinePack(t, "fx", `{"contributes":[`+
		`{"kind":"program","bin":"board","via":"pack","from":"tools/board","build":"make","run":"dist/board.js"},`+
		`{"kind":"program","bin":"pi","via":"npm","package":"@scope/pi"}]}`)}
}

func overridePlans(t *testing.T, ws string, ov map[string]config.ProgramOverride) map[string]programOverridePlan {
	t.Helper()
	o := &Options{Workspace: ws, Getenv: func(string) string { return "" }}
	out := map[string]programOverridePlan{}
	for _, p := range o.programOverridePlansFor("podman", overridePacks(t), ov) {
		out[p.bin] = p
	}
	return out
}

func writeConfigFile(t *testing.T, path, body string) {
	t.Helper()
	mkdirs(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mkdirs(t *testing.T, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAnOverrideInsideTheWorkspaceRunsFromTheWorkspacesOwnPath(t *testing.T) {
	packHome(t)
	ws := floortest.ResolvedTemp(t)
	mkdirs(t, filepath.Join(ws, "dev", "board"))
	p := overridePlans(t, ws, map[string]config.ProgramOverride{"board": {Path: filepath.Join(ws, "dev", "board")}})["board"]
	if p.wire.Refused != "" || p.wire.Dir != "/workspace/dev/board" || p.wire.Run != "dist/board.js" || p.bindSource != "" {
		t.Errorf("plan = %+v, want /workspace/dev/board running the pack's own run, with no bind", p)
	}
	if p.wire.Build != "make" {
		t.Errorf("the plan lost the pack's build line, which the jail's refusal names: %+v", p.wire)
	}
}

func TestAnOverrideOutsideTheWorkspaceIsBoundReadOnly(t *testing.T) {
	packHome(t)
	ws, dev := floortest.ResolvedTemp(t), floortest.ResolvedTemp(t)
	p := overridePlans(t, ws, map[string]config.ProgramOverride{"board": {Path: dev, Run: "bin/b"}})["board"]
	if p.wire.Dir != entrypoint.ProgramOverrideCtxDir+"/board" || p.bindSource != dev || p.wire.Run != "bin/b" {
		t.Fatalf("plan = %+v, want a read-only bind of %s at /ctx/program-overrides/board", p, dev)
	}
	args := strings.Join(programOverrideBindArgs([]programOverridePlan{p}), " ")
	if args != "-v "+dev+":/ctx/program-overrides/board:ro" {
		t.Errorf("bind args = %q", args)
	}
}

// TestASymlinkedOverrideIsResolvedBeforeTheChecks is the darwin symlink class: a link into the
// workspace runs from the workspace, and a link to the home is refused at the boundary.
func TestASymlinkedOverrideIsResolvedBeforeTheChecks(t *testing.T) {
	home := packHome(t)
	ws := floortest.ResolvedTemp(t)
	mkdirs(t, filepath.Join(ws, "dev", "board"))
	links := t.TempDir()
	toWs, toHome := filepath.Join(links, "ws-board"), filepath.Join(links, "home")
	if err := os.Symlink(filepath.Join(ws, "dev", "board"), toWs); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(home, toHome); err != nil {
		t.Fatal(err)
	}
	if p := overridePlans(t, ws, map[string]config.ProgramOverride{"board": {Path: toWs}})["board"]; p.wire.Dir != "/workspace/dev/board" {
		t.Errorf("a link into the workspace was not resolved first: %+v", p)
	}
	if p := overridePlans(t, ws, map[string]config.ProgramOverride{"board": {Path: toHome}})["board"]; p.wire.Refused == "" || p.bindSource != "" {
		t.Errorf("a link to the home was not refused: %+v", p)
	}
}

func TestOverrideRefusalsNameThePathAndTheKey(t *testing.T) {
	home := packHome(t)
	ws, dev := floortest.ResolvedTemp(t), floortest.ResolvedTemp(t)
	file := filepath.Join(dev, "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		bin  string
		ov   config.ProgramOverride
		want []string
	}{
		"missing":     {"board", config.ProgramOverride{Path: filepath.Join(dev, "nope")}, []string{filepath.Join(dev, "nope"), "does not exist"}},
		"not a dir":   {"board", config.ProgramOverride{Path: file}, []string{file, "not a directory"}},
		"the home":    {"board", config.ProgramOverride{Path: home}, []string{"IS your home directory", "credential boundary"}},
		"npm, no run": {"pi", config.ProgramOverride{Path: dev}, []string{"programs.overrides.pi.run", "names no file to run"}},
		"config dir":  {"board", config.ProgramOverride{Path: filepath.Join(home, ".config", "yolo-jail")}, []string{"credential boundary"}},
	} {
		if strings.Contains(name, "config dir") {
			mkdirs(t, tc.ov.Path)
		}
		p, ok := overridePlans(t, ws, map[string]config.ProgramOverride{tc.bin: tc.ov})[tc.bin]
		if !ok {
			t.Errorf("%s: no plan", name)
			continue
		}
		if p.bindSource != "" || p.wire.Dir != "" {
			t.Errorf("%s: a refused override is still delivered: %+v", name, p)
		}
		for _, w := range append(tc.want, "programs.overrides."+tc.bin+" in ~/.config/yolo-jail/config.jsonc") {
			if !strings.Contains(p.wire.Refused, w) {
				t.Errorf("%s: refusal %q lacks %q", name, p.wire.Refused, w)
			}
		}
	}
}

func TestAnOverrideForAnUndeclaredBinDoesNothing(t *testing.T) {
	home := packHome(t)
	ws, dev := floortest.ResolvedTemp(t), floortest.ResolvedTemp(t)
	if got := overridePlans(t, ws, map[string]config.ProgramOverride{"elsewhere": {Path: dev}}); len(got) != 0 {
		t.Errorf("an override naming no selected program was applied: %+v", got)
	}
	writeConfigFile(t, filepath.Join(home, ".config", "yolo-jail", "config.jsonc"),
		`{"programs":{"overrides":{"elsewhere":{"path":"`+dev+`"},"board":{"path":"`+dev+`"}}}}`)
	lines := UndeclaredProgramOverrideLines(overridePacks(t))
	if len(lines) != 1 || !strings.Contains(lines[0], "programs.overrides.elsewhere") || !strings.Contains(lines[0], "does nothing") {
		t.Errorf("yolo check's information = %q", lines)
	}
}

// A PREBUILT OPT-IN THAT CAN DO NOTHING (BG-D18) is `yolo check` information: one naming a bin no
// selected pack declares, and one naming a program with no `prebuilt` to opt into (here board, which
// declares none, and the npm program pi). It is never a dev override, so no plan applies it.
func TestAPrebuiltOptInThatCanDoNothingIsReported(t *testing.T) {
	home := packHome(t)
	writeConfigFile(t, filepath.Join(home, ".config", "yolo-jail", "config.jsonc"),
		`{"programs":{"overrides":{"elsewhere":{"prebuilt":true},"board":{"prebuilt":true},"pi":{"prebuilt":true}}}}`)
	lines := UndeclaredProgramOverrideLines(overridePacks(t))
	if len(lines) != 3 {
		t.Fatalf("yolo check's information = %q, want three lines", lines)
	}
	for i, want := range []string{"programs.overrides.board.prebuilt: pack fx declares no prebuilt",
		"programs.overrides.elsewhere.prebuilt: no selected pack declares elsewhere",
		"programs.overrides.pi.prebuilt: pack fx declares no prebuilt"} {
		if !strings.Contains(lines[i], want) {
			t.Errorf("line %d = %q, want %q", i, lines[i], want)
		}
	}
	if got := config.ProgramOverrides(nil); len(got) != 0 {
		t.Errorf("a prebuilt opt-in read as a dev override: %+v", got)
	}
}

func TestDroppedPinsLeaveOnlyTheOverriddenBins(t *testing.T) {
	pins := []packload.ForkPin{{Fork: packload.Fork{Bin: "board"}}, {Fork: packload.Fork{Bin: "x"}}}
	got := dropOverriddenPins(pins, []programOverridePlan{{bin: "board"}})
	if len(got) != 1 || got[0].Fork.Bin != "x" || len(pins) != 2 {
		t.Errorf("got %+v (pins %+v)", got, pins)
	}
}

func TestTheDisclosureNamesWhichBytesRun(t *testing.T) {
	lines := programOverrideLines(map[string]entrypoint.ProgramOverride{
		"board": {Dir: "/ctx/program-overrides/board", Run: "dist/board.js", Host: "/home/me/code/board"},
		"pi":    {Host: "/nope", Refused: "/nope does not exist (programs.overrides.pi in ~/.config/yolo-jail/config.jsonc)"},
	})
	if len(lines) != 2 || lines[0] != "board: dev override — runs /home/me/code/board/dist/board.js (programs.overrides in ~/.config/yolo-jail/config.jsonc)" {
		t.Fatalf("lines = %q", lines)
	}
	if !strings.Contains(lines[1], "pi: dev override refused — /nope does not exist") {
		t.Errorf("the refused line = %q", lines[1])
	}
}

// TestALaunchRunsAnOverriddenProgramFromItsCheckout is the call-site test, at Run's own entry
// point: red if Run stops deciding overrides, assembly stops binding or handing them over, the
// launch stops naming them, or the overridden program's build still runs.
func TestALaunchRunsAnOverriddenProgramFromItsCheckout(t *testing.T) {
	home := packHome(t)
	t.Setenv("YOLO_VERSION", "")
	t.Setenv("YOLO_PACK_ROOT", "")
	pack := t.TempDir()
	mkdirs(t, filepath.Join(pack, "tools", "x"))
	if err := os.WriteFile(filepath.Join(pack, "pack.json"), []byte(`{"contributes":[`+
		`{"kind":"program","bin":"x","via":"pack","from":"tools/x","build":"make","run":"x"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pack, "tools", "x", "VERSION"), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dev := floortest.ResolvedTemp(t)
	writeConfigFile(t, filepath.Join(home, ".config", "yolo-jail", "config.jsonc"),
		`{"packs":[{"source":"file://`+pack+`","name":"fx"}],"programs":{"overrides":{"x":{"path":"`+dev+`"}}}}`)

	var built []packload.ForkPin
	argv, printed := fakePodmanLaunch(t, func(o *Options) {
		o.BuildForks = func(req ForkBuildRequest) map[string]entrypoint.ForkDelivery {
			built = append(built, req.Pins...)
			return nil
		}
	})
	if len(built) != 0 {
		t.Errorf("the overridden program's build ran: %+v", built)
	}
	if !strings.Contains(strings.Join(argv, "\n"), dev+":/ctx/program-overrides/x:ro") {
		t.Errorf("the checkout is not bound read-only:\n%s", strings.Join(argv, "\n"))
	}
	var wire map[string]entrypoint.ProgramOverride
	for _, e := range argvValues(argv, "-e") {
		if v, ok := strings.CutPrefix(e, entrypoint.ProgramOverridesEnv+"="); ok {
			var err error
			if wire, err = entrypoint.ParseProgramOverrides(v); err != nil {
				t.Fatal(err)
			}
		}
	}
	if wire["x"].Dir != "/ctx/program-overrides/x" || wire["x"].Run != "x" || wire["x"].Host != dev {
		t.Errorf("the jail is handed %+v\n%s", wire, printed)
	}
	if !strings.Contains(printed, "x: dev override — runs "+filepath.Join(dev, "x")) {
		t.Errorf("the launch did not name the override:\n%s", printed)
	}
}

// TestAnAttachNamesTheRunningJailsOverrides reads the line off the running jail's frozen env.
func TestAnAttachNamesTheRunningJailsOverrides(t *testing.T) {
	var out strings.Builder
	o := &Options{Stdout: &out, Getenv: func(string) string { return "" }}
	o.noteAttachProgramOverrides([]string{"PATH=/bin", entrypoint.ProgramOverridesEnv + "=" +
		entrypoint.ProgramOverridesWire(map[string]entrypoint.ProgramOverride{
			"x": {Dir: "/workspace/dev/x", Run: "x", Host: "/home/me/ws/dev/x"}})})
	if !strings.Contains(out.String(), "x: dev override — runs /home/me/ws/dev/x/x") {
		t.Errorf("attach printed %q", out.String())
	}
}

// `yolo host`'s resolution of an override (the plan's Task 9) makes the jail's checks, in one place
// (overrideCheckout): the checkout runs from the host path itself, a symlink is resolved first, the
// home is refused at the credential boundary, an npm program with no `run` is refused naming the
// field, and an override naming no selected bin does nothing. Each refusal names the key.
func TestYoloHostResolvesAnOverrideWithTheJailsChecks(t *testing.T) {
	home := packHome(t)
	dev := floortest.ResolvedTemp(t)
	links := t.TempDir()
	link := filepath.Join(links, "dev")
	if err := os.Symlink(dev, link); err != nil {
		t.Fatal(err)
	}
	got := HostProgramOverrides(overridePacks(t), map[string]config.ProgramOverride{
		"board": {Path: link}, "pi": {Path: dev}, "nosuchbin": {Path: dev, Run: "x"},
	})
	if b := got["board"]; b.Refused != "" || b.Dir != dev || b.Run != "dist/board.js" || b.Build != "make" ||
		b.File() != filepath.Join(dev, "dist", "board.js") {
		t.Errorf("board = %+v, want the resolved checkout running the pack's own run", b)
	}
	if p := got["pi"]; !strings.Contains(p.Refused, "programs.overrides.pi.run") ||
		!strings.Contains(p.Refused, "programs.overrides.pi in ~/.config/yolo-jail/config.jsonc") {
		t.Errorf("pi = %+v, want a refusal naming the run field and the key", p)
	}
	if _, ok := got["nosuchbin"]; ok {
		t.Error("an override naming no selected bin was resolved")
	}
	h := HostProgramOverrides(overridePacks(t), map[string]config.ProgramOverride{"board": {Path: home}})["board"]
	if !strings.Contains(h.Refused, "credential boundary") || h.Dir != "" {
		t.Errorf("an override of the home = %+v, want refused at the credential boundary", h)
	}
	lines := HostProgramOverrideLines(overridePacks(t), map[string]config.ProgramOverride{"board": {Path: home}, "pi": {Path: dev, Run: "bin/pi"}})
	if len(lines) != 2 || !strings.Contains(lines[0], "board: dev override refused") ||
		lines[1] != "pi: dev override — runs "+filepath.Join(dev, "bin", "pi")+" (programs.overrides in ~/.config/yolo-jail/config.jsonc)" {
		t.Errorf("lines = %q", lines)
	}
}
