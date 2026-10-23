package packdecl

import (
	"strings"
	"testing"
)

func goodPackTree() Contribution {
	return Contribution{
		Kind: KindProgram, Bin: "board", Via: ViaPack,
		From: "tools/board-cli", Build: "npm ci && npm run bundle", Run: "dist/board.js",
	}
}

func TestAPackTreeProgramValidates(t *testing.T) {
	if p := validateContribution("contributes[0]", goodPackTree()); len(p) > 0 {
		t.Fatalf("refused:\n%s", strings.Join(p, "\n"))
	}
	c := goodPackTree()
	c.Build = "" // no build line: the tree runs as shipped
	c.Prebuilt = map[string]PrebuiltBuild{"linux/amd64": {
		URL: "https://example.invalid/board", SHA256: strings.Repeat("a", 64)}}
	c.Platforms, c.NodeFloor = []string{"linux"}, "22.19"
	if p := validateContribution("contributes[0]", c); len(p) > 0 {
		t.Fatalf("refused:\n%s", strings.Join(p, "\n"))
	}
	// `from` "." is the pack root, a legal source directory.
	root := goodPackTree()
	root.From = "."
	if p := validateContribution("contributes[0]", root); len(p) > 0 {
		t.Fatalf("from \".\" refused:\n%s", strings.Join(p, "\n"))
	}
}

func TestPackTreeRefusals(t *testing.T) {
	for name, tc := range map[string]struct {
		mut  func(*Contribution)
		want string
	}{
		"no from":        {func(c *Contribution) { c.From = "" }, `"from"`},
		"absolute from":  {func(c *Contribution) { c.From = "/etc" }, "relative"},
		"escaping from":  {func(c *Contribution) { c.From = "../x" }, ".."},
		"unclean from":   {func(c *Contribution) { c.From = "tools//board" }, "cleaned"},
		"no run":         {func(c *Contribution) { c.Run = "" }, `"run"`},
		"escaping run":   {func(c *Contribution) { c.Run = "../../bin/sh" }, ".."},
		"two-line build": {func(c *Contribution) { c.Build = "a\nb" }, "one command line"},
		"fork_of":        {func(c *Contribution) { c.ForkOf = "pi" }, `"fork_of"`},
		"source":         {func(c *Contribution) { c.Source = "git+https://x/y?ref=main" }, `"source"`},
		"package":        {func(c *Contribution) { c.Package = "x" }, `"package"`},
		"url":            {func(c *Contribution) { c.URL = "https://x" }, `"url"`},
		"flags":          {func(c *Contribution) { c.Flags = []string{"--x"} }, `"flags"`},
		"update":         {func(c *Contribution) { c.Update = []string{"up"} }, `"update"`},
		"versions_dir":   {func(c *Contribution) { c.VersionsDir = "v" }, `"versions_dir"`},
		"installer_env":  {func(c *Contribution) { c.InstallerEnv = map[string]string{"A": "b"} }, `"installer_env"`},
		"produces":       {func(c *Contribution) { c.Produces = []string{".local/bin/board"} }, `"produces"`},
		"http prebuilt": {func(c *Contribution) {
			c.Prebuilt = map[string]PrebuiltBuild{"linux/amd64": {URL: "http://x", SHA256: strings.Repeat("a", 64)}}
		}, "https"},
		"credentialed prebuilt": {func(c *Contribution) {
			c.Prebuilt = map[string]PrebuiltBuild{"linux/amd64": {URL: "https://u:p@x/b", SHA256: strings.Repeat("a", 64)}}
		}, "credentials"},
		"prebuilt no sha": {func(c *Contribution) {
			c.Prebuilt = map[string]PrebuiltBuild{"linux/amd64": {URL: "https://x/b"}}
		}, "sha256"},
		"prebuilt bad sha": {func(c *Contribution) {
			c.Prebuilt = map[string]PrebuiltBuild{"linux/amd64": {URL: "https://x/b", SHA256: "ABC"}}
		}, "sha256"},
		"bad platform": {func(c *Contribution) {
			c.Prebuilt = map[string]PrebuiltBuild{"linux": {URL: "https://x", SHA256: strings.Repeat("a", 64)}}
		}, "goos/goarch"},
		"unknown goos": {func(c *Contribution) {
			c.Prebuilt = map[string]PrebuiltBuild{"plan9x/amd64": {URL: "https://x", SHA256: strings.Repeat("a", 64)}}
		}, "goos/goarch"},
		"empty prebuilt": {func(c *Contribution) { c.Prebuilt = map[string]PrebuiltBuild{} }, "at least one"},
	} {
		c := goodPackTree()
		tc.mut(&c)
		p := strings.Join(validateContribution("contributes[0]", c), "\n")
		if !strings.Contains(p, tc.want) {
			t.Errorf("%s: problems %q do not name %q", name, p, tc.want)
		}
	}
}

func TestRunAndPrebuiltOffAPackTreeAreRefused(t *testing.T) {
	c := Contribution{Kind: KindProgram, Bin: "x", Via: "npm", Package: "x", Run: "a", From: "b"}
	p := strings.Join(validateContribution("contributes[0]", c), "\n")
	for _, f := range []string{`"run"`, `"from"`} {
		if !strings.Contains(p, f) {
			t.Errorf("an npm program's %s validated: %q", f, p)
		}
	}
	// `prebuilt` and `run` on a non-program kind are refused too, by name.
	withRun := Contribution{Kind: KindSkills, From: "skills", Run: "a"}
	if p := strings.Join(validateContribution("contributes[0]", withRun), "\n"); !strings.Contains(p, `"run"`) {
		t.Errorf("a skills contribution's run validated: %q", p)
	}
	// `from` stays a content kind's own field, so a skills source is not refused by this rule.
	skills := Contribution{Kind: KindSkills, From: "skills"}
	if p := packTreeFieldPlacementProblems("contributes[0]", skills); len(p) > 0 {
		t.Errorf("from on a skills contribution refused by the program rule: %q", p)
	}
}

func TestBuildOnAForkOrNpmProgramNamesThePackVia(t *testing.T) {
	npm := Contribution{Kind: KindProgram, Bin: "x", Via: "npm", Package: "x", Build: "make"}
	p := strings.Join(validateContribution("contributes[0]", npm), "\n")
	if !strings.Contains(p, `"source" or "pack"`) {
		t.Errorf("a build beside npm names no reader of build: %q", p)
	}
}

// Every shipped pack still declares no pack-tree field, and an official pack refuses prebuilt by
// census (officialpacks_test.go), so the vocabulary is additive for every pack that exists.
func TestPackTreeProgramDecodesStrictlyAndTolerantly(t *testing.T) {
	manifest := `{"name":"board","contributes":[{"kind":"program","bin":"board","via":"pack",` +
		`"from":"tools/board-cli","build":"npm ci && npm run bundle","run":"dist/board.js"}]}`
	m, problems := Decode([]byte(manifest))
	if len(problems) != 0 {
		t.Fatalf("a valid pack-tree program was refused: %v", problems)
	}
	if got := m.InstallContributions(); len(got) != 1 || got[0].Kind != InstallKindSource {
		t.Fatalf("the projection of a pack-tree program = %+v, want one source install", got)
	}
	// A typo'd key is still a refusal on the strict path: the new fields are known, nothing else is.
	typo := strings.Replace(manifest, `"run"`, `"runs"`, 1)
	if _, problems := Decode([]byte(typo)); len(problems) == 0 {
		t.Error("a misspelled pack-tree key validated on the strict path")
	}
	// The tolerant path keeps a known via, so an unknown key there is the decoder's refusal, not a
	// skip: an older yolo that does not know `pack` skips the whole program with a note instead.
	_, _, skipped := DecodeTolerant([]byte(manifest))
	if len(skipped) != 0 {
		t.Errorf("DecodeTolerant skipped a pack-tree program this build knows: %v", skipped)
	}
}
