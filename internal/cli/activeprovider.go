package cli

// activeprovider.go is `yolo active-provider`, the named verb for the readable config's
// `active_provider` table (docs/design/config-commands.md §4): which provider an agent (or every
// agent, "default") uses. It is sugar over `yolo config set active_provider.<agent> <name>`, plus
// the one thing a text edit cannot do: it validates the name against the selectable namespace the
// selected packs declare (config.LoadSelectableProfiles), `native` always valid. It writes the
// user file that already holds [active_provider] through the same edit machinery `yolo allow`
// uses, and approves nothing.

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

const activeProviderUsage = `usage: yolo active-provider <agent|default> <provider|provider/variant|native> [--dry-run]

Choose which provider an agent uses, in your user config's [active_provider] table (in the file
that already holds it). "default" is every agent the table does not name; "native" leaves the
agent to its own login. The name must be one a selected pack declares.

Examples:
  yolo active-provider default native
  yolo active-provider claude bedrock
  yolo active-provider claude bedrock/opus

The change takes effect at the next launch.`

// activeProviderSelectable is the names a selection may take: the selectable namespace over the
// packs the user config selects, a variable so a test can supply a declared set.
var activeProviderSelectable = func() ([]string, error) {
	packs, _ := config.UserScopeSelectedPacks()
	packs = nonNilPacks(packs)
	profiles, err := config.LoadSelectableProfiles(func(string) {}, config.UserScopeConfigOrEmpty(), packs)
	if err != nil {
		return nil, err
	}
	// The implied provider/variant names, the user's profiles, and the profiles the selected
	// packs declare (a provider's own name among them): the namespace a selection resolves.
	seen := map[string]bool{}
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		seen[name] = true
		names = append(names, name)
	}
	for _, name := range packload.DeclaredProfileNames(packs, profiles) {
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, nil
}

func runActiveProvider(args []string) int {
	rest := args
	if len(rest) > 0 {
		rest = rest[1:]
	}
	return activeProviderRunW(rest, os.Stdout, os.Stderr)
}

func activeProviderRunW(args []string, out, errw io.Writer) int {
	dryRun := false
	var positionals []string
	for _, a := range args {
		switch {
		case isHelpToken(a):
			fmt.Fprintln(out, activeProviderUsage)
			return 0
		case a == "--dry-run":
			dryRun = true
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(errw, "yolo active-provider: unknown flag %q\n\n%s\n", a, activeProviderUsage)
			return 2
		default:
			positionals = append(positionals, a)
		}
	}
	if len(positionals) != 2 {
		fmt.Fprintln(errw, activeProviderUsage)
		return 2
	}
	agent, name := positionals[0], positionals[1]
	if !bareConfigKey.MatchString(agent) {
		fmt.Fprintf(errw, "yolo active-provider: %q is not an agent name. Use an agent's name or `default`.\n", agent)
		return 2
	}

	if config.InJail() {
		argv := append([]string{"yolo", "active-provider"}, positionals...)
		fmt.Fprintf(errw, "yolo active-provider writes your user config, which only the host can edit.\n"+
			"  Ask the human to run, on the host:  %s\n  (or:  %s)\n",
			strings.Join(argv, " "), hostRunLineFor(argv))
		return 1
	}

	if name != config.NativeProvider {
		declared, err := activeProviderSelectable()
		if err != nil {
			fmt.Fprintf(errw, "yolo active-provider: cannot read what the selected packs declare: %v\n"+
				"  Run `yolo check` to see which pack is broken.\n", err)
			return 1
		}
		found := false
		for _, d := range declared {
			if d == name {
				found = true
				break
			}
		}
		if !found {
			listed := "none"
			if len(declared) > 0 {
				listed = strings.Join(declared, ", ")
			}
			fmt.Fprintf(errw, "yolo active-provider: no selected pack declares a provider %q. Declared: %s, native.\n"+
				"  If a pack provides it, select that pack first:  yolo pack add <pack>\n"+
				"  Otherwise pick one of the declared names, e.g.:  yolo active-provider %s native\n",
				name, listed, agent)
			return 1
		}
	}

	rc := namedVerbSet(configEditWorkspace(), "active_provider."+agent, name,
		config.EditTargetChoice{Kind: config.EditTargetUser}, dryRun, false, out, errw)
	if rc == 0 && !dryRun {
		fmt.Fprintln(out, "This takes effect at the next launch.")
	}
	return rc
}
