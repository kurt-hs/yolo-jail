package packdecl

// patchbranch_test.go pins the manifest half of a PATCH BRANCH (docs/reference/patched-forks.md §3.5, OQ-PFK6,
// PF-D87): `patch_branch` accepted in place of `patches` on a fork's program and on a `files` extension,
// its address checked as a source's, the pair refused at the strict decode naming the owner key,
// both fields and which to keep, and two contributions of one pack, one with each, accepted.

import (
	"strings"
	"testing"
)

const patchBranchAddr = "git+https://github.com/you/pi-patches?ref=mine"

func goodPatchBranchFork() Contribution {
	c := goodFork()
	c.Source = "git+https://github.com/earendil-works/pi?ref=main"
	c.PatchBranch = patchBranchAddr
	return c
}

func goodPatchBranchExtension() Contribution {
	c := goodPatchedExtension()
	c.Patches = ""
	c.PatchBranch = "git+https://github.com/you/pi-subagents?ref=mine"
	return c
}

func TestAPatchBranchValidatesOnAForkAndAnExtension(t *testing.T) {
	for _, follow := range []string{"", "head", "release"} {
		c := goodPatchBranchFork()
		c.Follow = follow
		if probs := validateContribution("contributes[0]", c); len(probs) > 0 {
			t.Errorf("a patch branch fork following %q is refused:\n%s", follow, strings.Join(probs, "\n"))
		}
		if !c.IsPatchedFork() {
			t.Error("a fork with a patch branch does not read as a patched fork")
		}
	}
	c := goodPatchBranchExtension()
	if probs := validateContribution("contributes[0]", c); len(probs) > 0 {
		t.Errorf("a patch branch extension is refused:\n%s", strings.Join(probs, "\n"))
	}
	if !c.IsPatchedExtension() || c.IsUnmodifiedExtension() || !c.IsBuiltTree() {
		t.Error("a files contribution with a patch branch does not read as a patched extension")
	}
	if c.TreeBuild() != c.Build {
		t.Errorf("a patch branch extension's build is %q, want its own %q", c.TreeBuild(), c.Build)
	}
}

func TestPatchBranchAddressRefusals(t *testing.T) {
	cases := []struct {
		name, patchBranch string
		want              []string
	}{
		{"no ref", "git+https://github.com/you/pi-patches", []string{"patch_branch", "?ref="}},
		{"a directory", "file:///srv/pi-patches", []string{"patch_branch", "git"}},
		{"HEAD", "git+https://github.com/you/pi-patches?ref=HEAD", []string{"patch_branch", "HEAD", "?ref=patches"}},
		{"a subdirectory", "git+https://github.com/you/pi-patches//pkg?ref=mine", []string{"patch_branch", "//pkg", "whole repository"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := goodPatchBranchFork()
			c.PatchBranch = tc.patchBranch
			joined := strings.Join(validateContribution("contributes[0]", c), "\n")
			for _, w := range tc.want {
				if !strings.Contains(joined, w) {
					t.Errorf("want a problem containing %q, got:\n%s", w, joined)
				}
			}
		})
	}
}

// OFF A FORK OR AN EXTENSION, `patch_branch` is read by nothing.
func TestPatchBranchOffAForkIsRefused(t *testing.T) {
	c := Contribution{Kind: KindProgram, Bin: "pi", Via: "npm", Package: "pi", PatchBranch: patchBranchAddr}
	joined := strings.Join(validateContribution("contributes[0]", c), "\n")
	if !strings.Contains(joined, `"patch_branch"`) {
		t.Errorf("a patch branch on an npm program is not refused:\n%s", joined)
	}
}

// PF-D87 AT THE STRICT DECODE: one contribution naming both is fatal, before anything is fetched,
// and the refusal names the owner key, both fields and which to keep for what.
func TestDecodeRefusesAPatchBranchBesideASeries(t *testing.T) {
	for _, tc := range []struct{ name, contribution, key string }{
		{"fork", `{"kind":"program","bin":"pi","via":"source","fork_of":"pi",
			"source":"git+https://github.com/earendil-works/pi?ref=main",
			"patches":"patches","patch_branch":"` + patchBranchAddr + `",
			"build":"npm ci","produces":[".npm-global/bin/pi"]}`, "mypi/pi"},
		{"extension", `{"kind":"files","into":".pi/agent/yolo-patched/sub",
			"source":"git+https://github.com/upstream/sub?ref=main",
			"patches":"patches","patch_branch":"` + patchBranchAddr + `"}`, "mypi/sub"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, probs := Decode([]byte(`{"name":"mypi","contributes":[` + tc.contribution + `]}`))
			joined := strings.Join(probs, "\n")
			for _, w := range []string{tc.key, `"patches"`, `"patch_branch"`, "keep \"patch_branch\"", "keep \"patches\"", "delete the other"} {
				if !strings.Contains(joined, w) {
					t.Errorf("want a problem containing %q, got:\n%s", w, joined)
				}
			}
		})
	}
}

// `carry`, the name the ruling used, is no alias (named `patch_branch` on 2026-10-16; nothing shipped it).
func TestCarryIsNotAnAliasForPatchBranch(t *testing.T) {
	_, probs := Decode([]byte(`{"name":"mypi","contributes":[{"kind":"files","into":".pi/agent/yolo-patched/b",
		"source":"git+https://github.com/u/b?ref=main","carry":"git+https://github.com/you/b?ref=patches"}]}`))
	if !strings.Contains(strings.Join(probs, "\n"), `unknown field "carry"`) {
		t.Errorf("a manifest naming `carry` decodes: %v", probs)
	}
}

// TWO CONTRIBUTIONS OF ONE PACK, one with each, are two owner keys and no conflict (PF-D87).
func TestTwoContributionsOneWithEachDecodeClean(t *testing.T) {
	_, probs := Decode([]byte(`{"name":"mypi","contributes":[
		{"kind":"files","into":".pi/agent/yolo-patched/a","source":"git+https://github.com/u/a?ref=main",
		 "patches":"patches/a"},
		{"kind":"files","into":".pi/agent/yolo-patched/b","source":"git+https://github.com/u/b?ref=main",
		 "patch_branch":"git+https://github.com/you/b?ref=mine"}]}`))
	if len(probs) > 0 {
		t.Errorf("a pack with one series and one patch branch is refused:\n%s", strings.Join(probs, "\n"))
	}
}

// THE INSTALL CARRIES THE PATCH BRANCH, so every reader of the base's program knows it is patched.
func TestTheInstallCarriesThePatchBranch(t *testing.T) {
	c := goodPatchBranchFork()
	c.ForkOf = ""
	ins := (&Manifest{Name: "pi", Contributes: []Contribution{c}}).InstallContributions()
	if len(ins) != 1 || ins[0].PatchBranch != patchBranchAddr || !ins[0].IsPatchedFork() || ins[0].SourceRecipe() != "" {
		t.Fatalf("the install does not patch branch the patch branch: %+v", ins)
	}
}
