package macosuser

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
)

// cachealias.go is the macos-user half of package-cache aliasing in the run-as-you mode
// (docs/design/macos-user-model-plan.md MMP-D12, slice 8; docs/reference/disk-footprint.md
// OQ-BF11). The decision is internal/hostcas's, bound by internal/cli/run's
// planMacosUserHostCAS; this file is what a decided alias becomes here: a link in the
// sandbox's HOME, at the path its own copy of the tool uses on macOS, to the host's real
// cache, plus a profile block opening that cache read and write — the link-plus-allow shape
// `cache_relocations` already has on this backend, never a bind.
//
// LIVE IN THE RUN-AS-YOU MODE ONLY (macos_user_mode "self", slice 6). The account mode, still
// the default, is declined for every store by hostcas, and PlanInvariants refuses an alias on
// it here too. The two halves a self-mode plan carries:
//
//   - the PROFILE: DenyDefaultProfile (seatbeltdeny.go) splices CacheAliasAllow among its write
//     allows, so the write carve-outs after it stay terminal and the closing denies beat it;
//   - the LINK: the bootstrap's home layout (entrypoint's darwinhomelayout.go) lays
//     Link → Target for each alias, named to it as entrypoint.DarwinCacheAliasesEnv.

// CacheAliasAllowID is the profile id of the block CacheAliasAllow renders.
const CacheAliasAllowID = "cache-alias-allow"

// CacheAlias is one host package cache a self-mode launch shares with its sandbox.
type CacheAlias struct {
	// Tool names the cache's program, for messages.
	Tool string
	// Link is where the link sits: <sandbox HOME>/<hostcas.Store.DarwinRel>.
	Link string
	// Target is the host cache, symlink-resolved ON THE HOST by the caller: the profile names
	// it verbatim, and a rule on an unresolved spelling matches nothing.
	Target string
}

// CacheAliasAllow is the profile block opening every alias's target read and write, or ""
// with none. The link itself sits in the sandbox's HOME, which the profile already opens;
// Seatbelt judges the resolved target, so this block is what makes the link usable.
func CacheAliasAllow(aliases []CacheAlias) string {
	if len(aliases) == 0 {
		return ""
	}
	targets := make([]string, 0, len(aliases))
	for _, a := range aliases {
		targets = append(targets, a.Target)
	}
	return "\n" +
		";; --- package caches shared in the run-as-you mode (OQ-BF11, MMP-D12): each is a\n" +
		";;     link in HOME to the host's own cache, by its RESOLVED path.  Files the sandbox\n" +
		";;     writes there are yours, because the sandbox runs as you. ---\n" +
		";; #seatbelt-test-id:" + CacheAliasAllowID + "#\n" +
		"(allow file-read* file-write*\n" + subpathClauses(targets) + ")\n"
}

// cacheAliasProblems is PlanInvariants' half for the aliases a plan carries: none in the
// account mode, and each well-formed.
func cacheAliasProblems(plan RunPlan) []string {
	if len(plan.CacheAliases) == 0 {
		return nil
	}
	var problems []string
	if plan.RunAs.Mode != ModeSelf {
		problems = append(problems, "the plan carries a cache alias but runs as the separate "+
			"sandbox account, whose files in your cache would not be yours (OQ-BF11)")
	}
	for _, a := range plan.CacheAliases {
		switch {
		case !filepath.IsAbs(a.Link) || filepath.Clean(a.Link) != a.Link:
			problems = append(problems, "cache alias for "+a.Tool+" has link "+a.Link+
				", not a clean absolute path in the sandbox's HOME")
		case !filepath.IsAbs(a.Target) || filepath.Clean(a.Target) != a.Target:
			problems = append(problems, "cache alias for "+a.Tool+" has target "+a.Target+
				", not a resolved absolute path; the profile's rule on it would match nothing")
		case a.Link == a.Target:
			problems = append(problems, "cache alias for "+a.Tool+" links "+a.Link+" to itself")
		case plan.RunAs.Homes.Real != "" && within(filepath.Clean(plan.RunAs.Homes.Real), a.Target):
			// The profile opens the target read-write whole: a target that is the home, or an
			// ancestor of it, opens every file you have (a planted link resolves anywhere).
			problems = append(problems, "cache alias for "+a.Tool+" has target "+a.Target+
				", which is or contains your home "+plan.RunAs.Homes.Real+
				"; the profile would open all of it read-write")
		}
	}
	if plan.RunAs.Mode != ModeSelf {
		return problems
	}
	// BOTH HALVES, read off the plan: the profile opens each target, and the bootstrap is told to
	// lay each link. Either alone is a dead alias — a link into a refused path, or an allow on a
	// cache nothing points at.
	allow := profileBlock(plan.Seatbelt, CacheAliasAllowID)
	links := map[string]string{}
	if wire, ok := argvEnvValue(plan.BootstrapArgv, entrypoint.DarwinCacheAliasesEnv); ok {
		_ = json.Unmarshal([]byte(wire), &links)
	}
	for _, a := range plan.CacheAliases {
		if !strings.Contains(allow, "(subpath "+sbplStr(a.Target)+")") {
			problems = append(problems, "cache alias for "+a.Tool+": the Seatbelt profile does not open "+
				a.Target+"; the link would lead into a refused path")
		}
		if links[a.Link] != a.Target {
			problems = append(problems, "cache alias for "+a.Tool+" is not named to the bootstrap ("+
				entrypoint.DarwinCacheAliasesEnv+"); no link would be laid at "+a.Link)
		}
	}
	return problems
}
