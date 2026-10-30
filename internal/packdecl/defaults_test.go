package packdecl

import (
	"reflect"
	"strings"
	"testing"
)

func TestDecodeTOMLProgramInstallDefaultsReachAccessors(t *testing.T) {
	const data = `[[program]]
bin = "fd"
install = { apt = "fd-find", dnf = "fd-find" }

[[program]]
bin = "fzf"

[[program]]
bin = "ripgrep"
install = "rg-package"
`
	m, problems := DecodeTOML([]byte(data))
	if len(problems) != 0 {
		t.Fatalf("DecodeTOML: %v", problems)
	}
	got := m.DepRequirements()
	if len(got) != 3 {
		t.Fatalf("DepRequirements = %#v", got)
	}
	wantFD := map[string]string{"brew": "fd", "apt": "fd-find", "dnf": "fd-find", "pacman": "fd", "nix": "fd"}
	if !reflect.DeepEqual(got[0].Hints, wantFD) {
		t.Errorf("fd install hints = %#v, want %#v", got[0].Hints, wantFD)
	}
	wantFZF := map[string]string{"brew": "fzf", "apt": "fzf", "dnf": "fzf", "pacman": "fzf", "nix": "fzf"}
	if !reflect.DeepEqual(got[1].Hints, wantFZF) {
		t.Errorf("fzf install hints = %#v, want %#v", got[1].Hints, wantFZF)
	}
	wantRG := map[string]string{"brew": "rg-package", "apt": "rg-package", "dnf": "rg-package", "pacman": "rg-package", "nix": "rg-package"}
	if !reflect.DeepEqual(got[2].Hints, wantRG) {
		t.Errorf("rg install hints = %#v, want %#v", got[2].Hints, wantRG)
	}
	if len(m.InstallContributions()) != 0 {
		t.Fatalf("manager hints turned no-recipe programs into installs: %#v", m.InstallContributions())
	}
}

func TestDecodeTOMLProgramInstallDefaultsMatchLegacyNeedProjection(t *testing.T) {
	legacy, problems := Decode([]byte(`{"contributes":[{"kind":"program","bin":"fd","install_hints":{"brew":"fd","apt":"fd-find","dnf":"fd-find","pacman":"fd","nix":"fd"}}]}`))
	if len(problems) != 0 {
		t.Fatalf("legacy Decode: %v", problems)
	}
	grouped, problems := DecodeTOML([]byte(`[[program]]
bin = "fd"
install = { apt = "fd-find", dnf = "fd-find" }
`))
	if len(problems) != 0 {
		t.Fatalf("DecodeTOML: %v", problems)
	}
	if !reflect.DeepEqual(legacy.RequiredBins(), grouped.RequiredBins()) {
		t.Fatalf("required-bin projection differs: legacy=%#v grouped=%#v", legacy.RequiredBins(), grouped.RequiredBins())
	}
	if !reflect.DeepEqual(legacy.DepRequirements(), grouped.DepRequirements()) {
		t.Fatalf("dependency projection differs: legacy=%#v grouped=%#v", legacy.DepRequirements(), grouped.DepRequirements())
	}
}

func TestDecodeTOMLProgramInstallDefaultAndNeedSemantics(t *testing.T) {
	m, problems := DecodeTOML([]byte(`[[program]]
bin = "jq"
install = { default = "jq-pkg", nix = "jq-nix" }
`))
	if len(problems) != 0 {
		t.Fatalf("DecodeTOML: %v", problems)
	}
	if got := m.Contributions(); len(got) != 1 || got[0].Provides() || !got[0].IsNeed() || got[0].Via != "" {
		t.Fatalf("install inference changed a need into a recipe: %#v", got)
	}
	want := map[string]string{"brew": "jq-pkg", "apt": "jq-pkg", "dnf": "jq-pkg", "pacman": "jq-pkg", "nix": "jq-nix"}
	if got := m.RequiredBins(); len(got) != 1 || !reflect.DeepEqual(got[0].Hints, want) {
		t.Fatalf("RequiredBins = %#v, want hint-bearing need %#v", got, want)
	}
}

func TestDecodeTOMLGroupedContributionsKeepAuthoredOrderAndReplace(t *testing.T) {
	m, problems := DecodeTOML([]byte(`[[overlay]]
surface = "pi/settings"
set.first = true

[[program]]
bin = "one"

[[overlay]]
surface = "pi/settings"
set.second = true
replace = true

[[program]]
bin = "two"
`))
	if len(problems) != 0 {
		t.Fatalf("DecodeTOML: %v", problems)
	}
	contributions := m.Contributions()
	if len(contributions) != 4 || contributions[0].Kind != KindConfigOverlay || contributions[1].Bin != "one" ||
		contributions[2].Kind != KindConfigOverlay || contributions[3].Bin != "two" {
		t.Fatalf("contribution order = %#v", contributions)
	}
	overlays := m.ConfigOverlayContributions()
	if len(overlays) != 2 || overlays[0].Surface != "pi/settings" || overlays[1].Surface != "pi/settings" {
		t.Fatalf("overlay projection = %#v", overlays)
	}
	if contributions[0].Replace || !contributions[2].Replace {
		t.Fatalf("replace metadata = %v, %v", contributions[0].Replace, contributions[2].Replace)
	}
}

func TestDecodeTOMLProgramRunAndScopeDefaults(t *testing.T) {
	// Defaults are applied only to the kinds and delivery modes whose grammar owns them.
	m, problems := DecodeTOML([]byte(`[[program]]
bin = "runner"
via = "pack"
from = "."
` + "\n[[state]]\nat = \".cache/tool\"\n"))
	if len(problems) != 0 {
		t.Fatalf("DecodeTOML: %v", problems)
	}
	installs := m.InstallContributions()
	if len(installs) != 1 || installs[0].PackFrom != "." || installs[0].PackRun != "runner" {
		t.Fatalf("pack-tree defaults = %#v, want from=. and run=bin", installs)
	}
	contribs := m.Contributions()
	if len(contribs) != 2 || contribs[1].Kind != KindState || contribs[1].Scope != "workspace" {
		t.Fatalf("state scope default = %#v, want workspace", contribs)
	}
}

func TestDecodeTOMLProgramInstallRejectsInvalidFormsAndLegacySpelling(t *testing.T) {
	for _, tc := range []struct{ name, field, want string }{
		{"wrong scalar type", `install = 42`, "a package name, false (no hint for any manager), or a table"},
		{"array", `install = ["pkg"]`, "a package name, false (no hint for any manager), or a table"},
		{"invalid default type", `install = { default = 1 }`, "install.default must be a package name, or false"},
		{"true", `install = true`, "a package name, false (no hint for any manager), or a table"},
		{"empty package", `install = ""`, "install = false for no hint"},
		{"true manager", `install = { apt = true }`, "install.apt must be a package name, or false"},
		{"empty manager", `install = { apt = "" }`, "install.apt must be a package name, or false"},
		{"unknown manager", `install = { aptish = "pkg" }`, "unknown manager"},
		{"old spelling", `install_hints = { apt = "pkg" }`, `use "install"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, problems := DecodeTOML([]byte("[[program]]\nbin = \"tool\"\n" + tc.field + "\n"))
			joined := strings.Join(problems, "\n")
			if !strings.Contains(joined, tc.want) {
				t.Fatalf("DecodeTOML problems %v, want %q", problems, tc.want)
			}
		})
	}
}

// TestDecodeTOMLInstallFalseMeansNoHint pins the grouped spelling for "no hint for this manager":
// false leaves the manager out of install_hints, exactly as a legacy manifest that does not name
// it, where an unwritten manager would get the bin.
func TestDecodeTOMLInstallFalseMeansNoHint(t *testing.T) {
	for _, tc := range []struct {
		name, field string
		want        map[string]string
	}{
		{"whole key", `install = false`, nil},
		{"default", `install = { default = false, brew = "pi-coding-agent" }`, map[string]string{"brew": "pi-coding-agent"}},
		{"one manager", `install = { apt = false }`, map[string]string{"brew": "tool", "dnf": "tool", "pacman": "tool", "nix": "tool"}},
		{"cask only", `install = { default = false, brew-cask = "tool-app" }`, map[string]string{"brew-cask": "tool-app"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, problems := DecodeTOML([]byte("[[program]]\nbin = \"tool\"\n" + tc.field + "\n"))
			if len(problems) != 0 {
				t.Fatal(problems)
			}
			if got := m.Contributions()[0].InstallHints; !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("install_hints = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestDecodeTOMLSourceForkKeepsRecipeWithoutGenericInstallDefault(t *testing.T) {
	m, problems := DecodeTOML([]byte(`[[program]]
bin = "pi"
via = "source"
fork_of = "pi"
source = "git+https://github.com/you/pi-fork?ref=main"
build = "make install"
produces = [".npm-global/bin/pi", ".npm-global/lib/node_modules/pi-fork"]
`))
	if len(problems) != 0 {
		t.Fatalf("DecodeTOML: %v", problems)
	}
	c := m.Contributions()[0]
	if c.Via != ViaSource || c.ForkOf != "pi" || c.Source != "git+https://github.com/you/pi-fork?ref=main" ||
		c.Build != "make install" || !reflect.DeepEqual(c.Produces, []string{".npm-global/bin/pi", ".npm-global/lib/node_modules/pi-fork"}) {
		t.Fatalf("source fork recipe changed: %#v", c)
	}
	if c.InstallHints != nil || len(m.InstallContributions()) != 0 || len(m.DepRequirements()) != 0 {
		t.Fatalf("source fork got a generic install projection: contribution=%#v installs=%#v deps=%#v",
			c, m.InstallContributions(), m.DepRequirements())
	}
	_, problems = DecodeTOML([]byte(`[[program]]
bin = "pi"
via = "source"
fork_of = "pi"
source = "git+https://github.com/you/pi-fork?ref=main"
build = "make install"
produces = [".npm-global/bin/pi", ".npm-global/lib/node_modules/pi-fork"]
install = "pi"
`))
	if len(problems) == 0 || !strings.Contains(strings.Join(problems, "\n"), "not available on a source fork") {
		t.Fatalf("source fork install hint was not refused: %v", problems)
	}
}

func TestDecodeTOMLRunDefaultOnlyAppliesToPackPrograms(t *testing.T) {
	m, problems := DecodeTOML([]byte(`[[program]]
bin = "tool"
via = "installer"
url = "https://example.invalid/install.sh"
`))
	if len(problems) != 0 {
		t.Fatalf("DecodeTOML: %v", problems)
	}
	if got := m.Contributions()[0].Run; got != "" {
		t.Fatalf("installer recipe received pack-tree run default %q", got)
	}
	_, problems = DecodeTOML([]byte(`[[program]]
bin = "tool"
via = "installer"
url = "https://example.invalid/install.sh"
run = "source/tool"
`))
	if len(problems) == 0 || !strings.Contains(strings.Join(problems, "\n"), `does not take "run"`) {
		t.Fatalf("non-pack program run field was not refused: %v", problems)
	}
}
