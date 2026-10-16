package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
)

// refreshservers.go is `yolo internal refresh-servers` — the transitive half of evergreen
// agent updates (docs/design/program-delivery.md §3.5, OQ-PD12a).
//
// # Who calls it, and from where
//
// The generated agent launchers, once, after they are done with their own program and BEFORE
// they exec it. The explicit `yolo run --update` prelude also calls the hidden command in
// fail-closed `--fresh` mode over the same selected declarations. Not a boot genStep: the normal
// trigger is the agent that connects to the servers, so a launch that starts no agent refreshes
// nothing and a workspace that never types `claude` never pays for claude's servers.
//
// The walk is a filesystem-and-registry decision per package, and the alternative is a second
// copy of it in each of the two bash templates.
//
// # Hidden, for capture-materialize's reason
//
// This writes into the home it is pointed at; it is only for an agent launcher or the launch's
// explicit preparation prelude, not an independently selectable management verb.
//
// # Its exit status is advisory or fail-closed, by caller
//
// Ordinary refresh reports an absent server it could not install and remains advisory, preserving
// the launcher's established behavior. An explicit `--fresh` request returns non-zero on a failed
// refresh, so the prelude never starts the target on bytes it cannot claim are fresh. A stale
// server whose NEWER version was found and did not install exits entrypoint.UpdateRefusedStatus,
// its refusal said (docs/design/program-delivery.md PD-NF1), and the launcher passes that status on
// instead of exec'ing its agent; `--bin` names that agent, the command the refusal says to retry.

// There is no `--go` list any more: its only source was the deleted LSP recipe's go arm
// (docs/reference/mcp-configuration.md#oq-lsp1), and the launchers stopped passing it in the
// same change.
const refreshServersUsage = "usage: yolo internal refresh-servers [--home=DIR] " +
	"[--npm=\"pkg pkg\"] [--updates=0|1] [--fresh] [--bin=NAME]"

// runRefreshServers is the `yolo internal refresh-servers` entry.
func runRefreshServers(args []string) int {
	home := os.Getenv("HOME")
	npmSpecs := ""
	// The POLICY DEFAULT IS TRUE, and it inverts host_apply_on_launch's fail-closed
	// precedent on purpose (the plan's trap 9): an absent or unreadable agent_updates means
	// updates are ON, because the failure mode of the other default is every agent in every
	// jail silently frozen. A launcher always bakes the flag, so this default is only ever
	// reached by a hand invocation.
	updates := true
	fresh := false
	bin := ""
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "--home="):
			home = strings.TrimPrefix(a, "--home=")
		case strings.HasPrefix(a, "--npm="):
			npmSpecs = strings.TrimPrefix(a, "--npm=")
		case strings.HasPrefix(a, "--updates="):
			updates = strings.TrimPrefix(a, "--updates=") == "1"
		case a == "--fresh":
			fresh = true
		case strings.HasPrefix(a, "--bin="):
			bin = strings.TrimPrefix(a, "--bin=")
		default:
			fmt.Fprintf(os.Stderr, "refresh-servers: unexpected argument %q\n%s\n",
				a, refreshServersUsage)
			return 2
		}
	}
	if home == "" {
		fmt.Fprintln(os.Stderr, refreshServersUsage)
		return 2
	}

	if fresh {
		jailEnv := entrypoint.JailEnvFromOS()
		if jailEnv.Getenv("YOLO_PACK_ROOT") == "" {
			fmt.Fprintln(os.Stderr, "refresh-servers --fresh requires a staged selected pack tree — run `yolo --update` from a fresh jail launch")
			return 1
		}
		packs, err := entrypoint.LoadJailPacks(jailEnv)
		if err != nil {
			fmt.Fprintf(os.Stderr, "refresh-servers --fresh: reading selected packs: %v\n", err)
			return 1
		}
		if npmSpecs == "" {
			npmSpecs = entrypoint.ServerRefreshNpmSpecs(jailEnv, packs)
		}
		home = jailEnv.Home
	}
	// NewEnv rather than a second path resolution: Home and NpmPrefix are exactly the values
	// this needs, and the prefix's default ($HOME/.npm-global) is the same one the launcher
	// templates spell in bash. --home is authoritative over the
	// environment because the launcher passes its own $HOME, which under macos-user is the
	// sandbox home and not whatever this process inherited.
	vars := envMap(os.Environ())
	vars["HOME"] = home
	vars["JAIL_HOME"] = home
	e := entrypoint.NewEnv(vars)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	serverErr := entrypoint.RefreshServers(entrypoint.ServerRefreshRequest{
		Context:  ctx,
		Fresh:    fresh,
		Env:      e,
		NpmSpecs: npmSpecs,
		Updates:  updates,
		Stderr:   os.Stderr,
		Bin:      bin,
	})
	if serverErr == nil && ctx.Err() != nil {
		serverErr = ctx.Err()
	}
	// A server whose newer version did not install has had its refusal said (PD-NF1): the status
	// tells the launcher not to exec its agent.
	var refusal *entrypoint.ServerUpdateRefusal
	if errors.As(serverErr, &refusal) {
		return entrypoint.UpdateRefusedStatus
	}
	if serverErr != nil {
		fmt.Fprintln(os.Stderr, "refresh-servers:", serverErr)
		return 1
	}
	return 0
}
