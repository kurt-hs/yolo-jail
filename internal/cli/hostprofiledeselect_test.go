package cli

// hostprofiledeselect_test.go pins the HOST notch's "reaches nothing" warning ending
// (docs/design/providers-and-profiles-redesign.md PP-D12): where the selection came from and
// the spelling that selects none for the command there. The host's -p takes `-p <cli>=` as a
// jail launch does, selecting no profile for that one command over the `profile` key, so the
// warning offers it; the key's null works here as in a jail, because the host folds the key
// through the same FoldProfiles. Each test drives the verb (hostMain), so deleting the Deselect
// the host hands its profile disclosure fails here.

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

// hostDeselectConfig is a user config whose `profile` (the value spelled, "" for none) reaches
// copilot, which has no Bedrock client of its own, so a bedrock selection reaches nothing for it.
// No claude pack: it needs the wire bridge, which carries copilot to Bedrock at the host too
// since a carrier starts the bridge's host half there (host-notch-services.md HS-D30).
func hostDeselectConfig(profile string) string {
	cfg := "{\n  \"packs\": [\"copilot\", \"bedrock\"],\n" +
		"  \"providers\": {\"bedrock\": {\"region\": \"eu-west-1\"}}"
	if profile != "" {
		cfg += ",\n  \"profile\": " + profile
	}
	return cfg + "\n}\n"
}

const hostDeselectWarning = `Warning: profile "bedrock" reaches nothing for copilot`

// A KEY-MADE SELECTION NAMES THE KEY AND ITS FIX AT THE HOST TOO. The warning used to end
// "or none for copilot" there, naming no way to select none, so `yolo host -- copilot` under
// `"profile": "bedrock"` repeated it on every command. It now names the key's file and line and
// the key respelled with a null for copilot, and `-p copilot=` for one command; written back, the
// key's spelling stops the warning and keeps the command running.
func TestHostReachesNothingWarningNamesTheKeyAndItsFix(t *testing.T) {
	rc, _, errs := hostGateRun(t, hostDeselectConfig(`"bedrock"`), nil, nil, "copilot")
	if rc != 0 {
		t.Fatalf("yolo host -- copilot: rc = %d\n%s", rc, errs)
	}
	if !strings.Contains(errs, hostDeselectWarning) {
		t.Fatalf("the fixture no longer warns %q:\n%s", hostDeselectWarning, errs)
	}
	const spelling = `"profile": {"*": "bedrock", "copilot": null}`
	if want := "the selection is your config's `profile` key at ~/.config/yolo-jail/config.jsonc:4:14, " +
		"so write `" + spelling + "` there to select none for copilot on every command, or add " +
		"`-p copilot=` for one command"; !strings.Contains(errs, want) {
		t.Errorf("the host warning does not name the key and its fix (%q):\n%s", want, errs)
	}

	spelled := regexp.MustCompile("write `\"profile\": ([^`]*)` there").FindStringSubmatch(errs)
	if spelled == nil {
		t.Fatalf("the host warning prints no persistent spelling:\n%s", errs)
	}
	if _, err := jsonx.Decode([]byte(spelled[1])); err != nil {
		t.Fatalf("the printed spelling %s does not parse: %v", spelled[1], err)
	}
	rc, _, fixed := hostGateRun(t, hostDeselectConfig(spelled[1]), nil, nil, "copilot")
	if rc != 0 {
		t.Fatalf("the printed spelling, written back: rc = %d\n%s", rc, fixed)
	}
	if strings.Contains(fixed, "reaches nothing for copilot") {
		t.Errorf("the printed spelling, written back, still warns at the host:\n%s", fixed)
	}
}

// A TYPED -p MADE THE SELECTION, so the clause says to leave it out, the one way the host undoes
// it; with no `profile` key behind it that is the whole fix, and the key is not mentioned.
func TestHostReachesNothingWarningNamesTheTypedFlag(t *testing.T) {
	rc, _, errs := hostGateRun(t, hostDeselectConfig(""), nil, []string{"-p", "bedrock"}, "copilot")
	if rc != 0 {
		t.Fatalf("yolo host -p bedrock -- copilot: rc = %d\n%s", rc, errs)
	}
	if !strings.Contains(errs, hostDeselectWarning) {
		t.Fatalf("the fixture no longer warns %q:\n%s", hostDeselectWarning, errs)
	}
	if want := "or none for copilot: the selection is this command's `-p`, so leave it out"; !strings.Contains(errs, want) {
		t.Errorf("a typed -p's host warning does not name its fix (%q):\n%s", want, errs)
	}
	for _, not := range []string{"`profile` key"} {
		if strings.Contains(errs, not) {
			t.Errorf("a typed -p's host warning says %q:\n%s", not, errs)
		}
	}
}

// A typed -p over a key that selects for the command too: leaving the -p out hands the command
// the key's selection, so the clause names the key's fix after it.
func TestHostReachesNothingWarningNamesTheKeyBehindATypedFlag(t *testing.T) {
	rc, _, errs := hostGateRun(t, hostDeselectConfig(`"bedrock"`), nil, []string{"-p", "bedrock"}, "copilot")
	if rc != 0 {
		t.Fatalf("yolo host -p bedrock -- copilot: rc = %d\n%s", rc, errs)
	}
	if want := "the selection is this command's `-p`, so make it `-p copilot=`; without it the selection is " +
		"your config's `profile` key at ~/.config/yolo-jail/config.jsonc:4:14, so write " +
		"`\"profile\": {\"*\": \"bedrock\", \"copilot\": null}` there to select none for copilot " +
		"on every command"; !strings.Contains(errs, want) {
		t.Errorf("the host warning does not name the key behind the -p (%q):\n%s", want, errs)
	}
}

// `-p <cli>=` AT THE HOST SELECTS NONE FOR THE COMMAND, over the `profile` key, as it does on a
// jail launch (ES-D27 and PP-D12 amended 2026-10-29): the command runs with no profile, so the key's
// bedrock reaches nothing and nothing warns. It used to refuse ("names no profile"), which left
// only the key's null to undo a key-made selection for one command. `yolo host env` takes the same
// spelling. The pair `-p copilot=null` is a profile literally named "null", and stays refused as
// undeclared.
func TestHostEmptyPairSelectsNoProfileOverTheKey(t *testing.T) {
	rc, env, errs := hostGateRun(t, hostDeselectConfig(`"bedrock"`), nil, []string{"-p", "copilot="}, "copilot")
	if rc != 0 || env == nil {
		t.Fatalf("yolo host -p copilot= -- copilot: rc = %d, env = %v\n%s", rc, env, errs)
	}
	if strings.Contains(errs, "reaches nothing for copilot") || strings.Contains(errs, `profile "bedrock"`) {
		t.Errorf("-p copilot= still selected the key's bedrock:\n%s", errs)
	}
	for k := range env {
		if strings.HasPrefix(k, "COPILOT_PROVIDER_") {
			t.Errorf("-p copilot= composed %s=%s", k, env[k])
		}
	}

	hostGateHome(t, hostDeselectConfig(`"bedrock"`), nil)
	var out, errw bytes.Buffer
	if rc := hostMain([]string{"env", "--agent", "copilot", "-p", "copilot=", "--format", "json"},
		&out, &errw, false, nil); rc != 0 {
		t.Errorf("yolo host env -p copilot=: rc = %d\n%s\n%s", rc, out.String(), errw.String())
	}
	if strings.Contains(errw.String(), `profile "bedrock"`) {
		t.Errorf("yolo host env -p copilot= still selected the key's bedrock:\n%s", errw.String())
	}

	rc, _, errs = hostGateRun(t, hostDeselectConfig(`"bedrock"`), nil, []string{"-p", "copilot=null"}, "copilot")
	if rc == 0 || !strings.Contains(errs, `"null"`) {
		t.Errorf("-p copilot=null must stay a profile named \"null\", refused as undeclared: rc = %d\n%s", rc, errs)
	}
}

// THE CASE THE AMENDMENT IS FOR: pi under a key that selects bedrock for every agent, run once on
// its own settings (its built-in GitHub Copilot login, say) with `yolo host -p pi= -- pi`. pi gets
// none of the key's Bedrock profile: no region, no provider policy, and no launch selection.
func TestHostEmptyPairRunsPiOnItsOwnSettings(t *testing.T) {
	const cfg = `{"packs": ["pi", "bedrock"], "providers": {"bedrock": {"region": "eu-west-1"}}, "profile": "bedrock"}`
	keyed, _ := hostGateLaunchWith(t, cfg, nil, nil, "pi")
	if keyed["AWS_REGION"] != "eu-west-1" {
		t.Fatalf("the fixture's key no longer selects bedrock for pi: %v", keyed)
	}
	env, errs := hostGateLaunchWith(t, cfg, nil, []string{"-p", "pi="}, "pi")
	if env["AWS_REGION"] == "eu-west-1" {
		t.Errorf("yolo host -p pi= -- pi composed the key's bedrock region\n%s", errs)
	}
	if v, ok := env["YOLO_PI_PROVIDER_POLICY"]; ok {
		t.Errorf("yolo host -p pi= -- pi composed a provider policy %s\n%s", v, errs)
	}
	// No launch selection is handed and none is missed: pi's session home is rendered for the
	// launch's own selection, which is none.
	for _, not := range []string{`Profile bedrock`, "composes no selection", "--provider", "--models"} {
		if strings.Contains(errs, not) {
			t.Errorf("yolo host -p pi= -- pi says %q:\n%s", not, errs)
		}
	}
}

// A PROGRAM WITH NO SESSION HOME keeps what its own file holds, which `yolo host apply` wrote for
// the configured profile and no launch word takes away, so `-p codex=` says so and names the
// config's way to select none.
func TestHostEmptyPairNamesTheFileAProgramWithNoSessionHomeKeeps(t *testing.T) {
	const cfg = `{"packs": ["codex", "bedrock"], "providers": {"bedrock": {"region": "eu-west-1"}}, "profile": "bedrock"}`
	_, errs := hostGateLaunchWith(t, cfg, nil, []string{"-p", "codex="}, "codex")
	if want := "-p codex= selects no profile for codex, but codex starts on the selection its own config holds"; !strings.Contains(errs, want) {
		t.Errorf("yolo host -p codex= -- codex does not say %q:\n%s", want, errs)
	}
	if strings.Contains(errs, "composes no selection") {
		t.Errorf("yolo host -p codex= -- codex reads the deselection as a profile:\n%s", errs)
	}
}
