package packdecl

// carry_test.go pins the manifest half of a CARRY (docs/design/patched-forks.md §3.5, OQ-PFK6,
// PF-D87): `carry` accepted in place of `patches` on a fork's program and on a `files` extension,
// its address checked as a source's, the pair refused at the strict decode naming the owner key,
// both fields and which to keep, and two contributions of one pack, one with each, accepted.

import (
	"strings"
	"testing"
)

const carryAddr = "git+https://github.com/you/pi-carry?ref=carry"

func goodCarryFork() Contribution {
	c := goodFork()
	c.Source = "git+https://github.com/earendil-works/pi?ref=main"
	c.Carry = carryAddr
	return c
}

func goodCarryExtension() Contribution {
	c := goodPatchedExtension()
	c.Patches = ""
	c.Carry = "git+https://github.com/you/pi-subagents?ref=carry"
	return c
}

func TestACarryValidatesOnAForkAndAnExtension(t *testing.T) {
	for _, follow := range []string{"", "head", "release"} {
		c := goodCarryFork()
		c.Follow = follow
		if probs := validateContribution("contributes[0]", c); len(probs) > 0 {
			t.Errorf("a carry fork following %q is refused:\n%s", follow, strings.Join(probs, "\n"))
		}
		if !c.IsPatchedFork() {
			t.Error("a fork with a carry does not read as a patched fork")
		}
	}
	c := goodCarryExtension()
	if probs := validateContribution("contributes[0]", c); len(probs) > 0 {
		t.Errorf("a carry extension is refused:\n%s", strings.Join(probs, "\n"))
	}
	if !c.IsPatchedExtension() || c.IsUnmodifiedExtension() || !c.IsBuiltTree() {
		t.Error("a files contribution with a carry does not read as a patched extension")
	}
	if c.TreeBuild() != c.Build {
		t.Errorf("a carry extension's build is %q, want its own %q", c.TreeBuild(), c.Build)
	}
}

func TestCarryAddressRefusals(t *testing.T) {
	cases := []struct {
		name, carry string
		want        []string
	}{
		{"no ref", "git+https://github.com/you/pi-carry", []string{"carry", "?ref="}},
		{"a directory", "file:///srv/pi-carry", []string{"carry", "git"}},
		{"HEAD", "git+https://github.com/you/pi-carry?ref=HEAD", []string{"carry", "HEAD", "?ref=carry"}},
		{"a subdirectory", "git+https://github.com/you/pi-carry//pkg?ref=carry", []string{"carry", "//pkg", "whole repository"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := goodCarryFork()
			c.Carry = tc.carry
			joined := strings.Join(validateContribution("contributes[0]", c), "\n")
			for _, w := range tc.want {
				if !strings.Contains(joined, w) {
					t.Errorf("want a problem containing %q, got:\n%s", w, joined)
				}
			}
		})
	}
}

// OFF A FORK OR AN EXTENSION, `carry` is read by nothing.
func TestCarryOffAForkIsRefused(t *testing.T) {
	c := Contribution{Kind: KindProgram, Bin: "pi", Via: "npm", Package: "pi", Carry: carryAddr}
	joined := strings.Join(validateContribution("contributes[0]", c), "\n")
	if !strings.Contains(joined, `"carry"`) {
		t.Errorf("a carry on an npm program is not refused:\n%s", joined)
	}
}

// PF-D87 AT THE STRICT DECODE: one contribution naming both is fatal, before anything is fetched,
// and the refusal names the owner key, both fields and which to keep for what.
func TestDecodeRefusesACarryBesideASeries(t *testing.T) {
	for _, tc := range []struct{ name, contribution, key string }{
		{"fork", `{"kind":"program","bin":"pi","via":"source","fork_of":"pi",
			"source":"git+https://github.com/earendil-works/pi?ref=main",
			"patches":"patches","carry":"` + carryAddr + `",
			"build":"npm ci","produces":[".npm-global/bin/pi"]}`, "mypi/pi"},
		{"extension", `{"kind":"files","into":".pi/agent/yolo-patched/sub",
			"source":"git+https://github.com/upstream/sub?ref=main",
			"patches":"patches","carry":"` + carryAddr + `"}`, "mypi/sub"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, probs := Decode([]byte(`{"name":"mypi","contributes":[` + tc.contribution + `]}`))
			joined := strings.Join(probs, "\n")
			for _, w := range []string{tc.key, `"patches"`, `"carry"`, "keep \"carry\"", "keep \"patches\"", "delete the other"} {
				if !strings.Contains(joined, w) {
					t.Errorf("want a problem containing %q, got:\n%s", w, joined)
				}
			}
		})
	}
}

// TWO CONTRIBUTIONS OF ONE PACK, one with each, are two owner keys and no conflict (PF-D87).
func TestTwoContributionsOneWithEachDecodeClean(t *testing.T) {
	_, probs := Decode([]byte(`{"name":"mypi","contributes":[
		{"kind":"files","into":".pi/agent/yolo-patched/a","source":"git+https://github.com/u/a?ref=main",
		 "patches":"patches/a"},
		{"kind":"files","into":".pi/agent/yolo-patched/b","source":"git+https://github.com/u/b?ref=main",
		 "carry":"git+https://github.com/you/b?ref=carry"}]}`))
	if len(probs) > 0 {
		t.Errorf("a pack with one series and one carry is refused:\n%s", strings.Join(probs, "\n"))
	}
}

// THE INSTALL CARRIES THE CARRY, so every reader of the base's program knows it is patched.
func TestTheInstallCarriesTheCarry(t *testing.T) {
	c := goodCarryFork()
	c.ForkOf = ""
	ins := (&Manifest{Name: "pi", Contributes: []Contribution{c}}).InstallContributions()
	if len(ins) != 1 || ins[0].Carry != carryAddr || !ins[0].IsPatchedFork() || ins[0].SourceRecipe() != "" {
		t.Fatalf("the install does not carry the carry: %+v", ins)
	}
}
