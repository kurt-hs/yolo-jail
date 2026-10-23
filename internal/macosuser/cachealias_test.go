package macosuser

import (
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

// cachealias_test.go pins the macos-user half of package-cache aliasing in the run-as-you
// mode (docs/design/macos-user-model-plan.md slice 8, MMP-D12).

var pipAlias = CacheAlias{
	Tool:   "pip",
	Link:   "/Users/Shared/yolo/ws/.yolo/home/Library/Caches/pip",
	Target: "/Users/you/Library/Caches/pip",
}

// THE PROFILE HOOK: one block, its own id, opening each resolved target read and write, and
// nothing at all when there is no alias.
func TestCacheAliasAllowOpensEachTargetReadAndWrite(t *testing.T) {
	if got := CacheAliasAllow(nil); got != "" {
		t.Errorf("no alias rendered %q, want nothing", got)
	}
	block := CacheAliasAllow([]CacheAlias{pipAlias})
	if !strings.Contains(block, ";; #seatbelt-test-id:"+CacheAliasAllowID+"#\n") {
		t.Errorf("the block carries no %s id:\n%s", CacheAliasAllowID, block)
	}
	if !strings.Contains(block, "(allow file-read* file-write*\n") ||
		!strings.Contains(block, "(subpath "+sbplStr(pipAlias.Target)+")") {
		t.Errorf("the target is not opened read and write:\n%s", block)
	}
	if strings.Contains(block, pipAlias.Link) {
		t.Errorf("the block names the link, which Seatbelt never judges (it resolves first):\n%s", block)
	}
}

// THE MODE IS THE GATE, at the plan too: an account-mode plan carrying an alias is refused,
// because the second account's files in your cache would not be yours (OQ-BF11).
func TestAnAccountModePlanCarryingACacheAliasIsRefused(t *testing.T) {
	build := func(mode Mode) RunPlan {
		return BuildRunPlanWithDaemons(relocWS, jsonx.NewOrderedMap(), []string{"claude"},
			[]string{"/bin/zsh", "-l"}, "/usr/local/bin/yolo", hostStaged, HomeOverlay{},
			HostContext{CacheAliases: []CacheAlias{pipAlias}}, jsonx.NewOrderedMap(), nil, nil,
			JailDaemons{}, FloorStage{}, PlanSession{Mode: mode})
	}
	account := build(ModeAccount)
	if len(account.CacheAliases) != 1 || account.CacheAliases[0] != pipAlias {
		t.Fatalf("the plan carries %+v, want the host's alias", account.CacheAliases)
	}
	const want = "runs as the separate sandbox account"
	if probs := strings.Join(PlanInvariants(account), "\n"); !strings.Contains(probs, want) {
		t.Errorf("PlanInvariants did not refuse an account-mode alias:\n%s", probs)
	}
	if probs := strings.Join(PlanInvariants(build(ModeSelf)), "\n"); strings.Contains(probs, want) {
		t.Errorf("PlanInvariants refused a self-mode alias:\n%s", probs)
	}
	// And a plan with none says nothing about aliases in either mode.
	none := planWithRelocations(t)
	if probs := strings.Join(PlanInvariants(none), "\n"); strings.Contains(probs, "cache alias") {
		t.Errorf("a plan with no alias was refused for one:\n%s", probs)
	}
}

// A malformed alias is refused at the plan: the link must sit in a HOME and both paths must be
// absolute and clean, since the profile names the target verbatim.
func TestPlanInvariantsRefuseAMalformedCacheAlias(t *testing.T) {
	for name, a := range map[string]CacheAlias{
		"relative link":      {Tool: "pip", Link: "Library/Caches/pip", Target: pipAlias.Target},
		"relative target":    {Tool: "pip", Link: pipAlias.Link, Target: "Library/Caches/pip"},
		"unclean target":     {Tool: "pip", Link: pipAlias.Link, Target: "/Users/you/../you/Library/Caches/pip"},
		"link is the target": {Tool: "pip", Link: pipAlias.Target, Target: pipAlias.Target},
	} {
		t.Run(name, func(t *testing.T) {
			p := BuildRunPlanWithDaemons(relocWS, jsonx.NewOrderedMap(), []string{"claude"},
				[]string{"/bin/zsh", "-l"}, "/usr/local/bin/yolo", hostStaged, HomeOverlay{},
				HostContext{CacheAliases: []CacheAlias{a}}, jsonx.NewOrderedMap(), nil, nil,
				JailDaemons{}, FloorStage{}, PlanSession{Mode: ModeSelf})
			if probs := strings.Join(PlanInvariants(p), "\n"); !strings.Contains(probs, "cache alias") {
				t.Errorf("PlanInvariants accepted %+v:\n%s", a, probs)
			}
		})
	}
}

// A TARGET THAT IS OR CONTAINS YOUR HOME IS REFUSED: the profile opens a target read-write
// whole, so a cache path whose link resolved to the home (or above it) would open every file
// you have. Fails if cacheAliasProblems stops reading the home.
func TestPlanInvariantsRefuseACacheAliasTargetingTheHome(t *testing.T) {
	plan := func(a CacheAlias) RunPlan {
		return BuildRunPlanWithDaemons(relocWS, jsonx.NewOrderedMap(), []string{"claude"},
			[]string{"/bin/zsh", "-l"}, "/usr/local/bin/yolo", hostStaged, HomeOverlay{},
			HostContext{CacheAliases: []CacheAlias{a}}, jsonx.NewOrderedMap(), nil, nil,
			JailDaemons{}, FloorStage{}, PlanSession{Mode: ModeSelf, HostHome: "/Users/you", HostUser: "you"})
	}
	for _, target := range []string{"/Users/you", "/Users", "/"} {
		a := CacheAlias{Tool: "pip", Link: pipAlias.Link, Target: target}
		if probs := strings.Join(PlanInvariants(plan(a)), "\n"); !strings.Contains(probs, "is or contains your home") {
			t.Errorf("PlanInvariants accepted target %s:\n%s", target, probs)
		}
	}
	if probs := strings.Join(PlanInvariants(plan(pipAlias)), "\n"); strings.Contains(probs, "your home") {
		t.Errorf("a cache under the home was refused as the home:\n%s", probs)
	}
}
