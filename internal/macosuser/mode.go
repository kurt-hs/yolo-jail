package macosuser

import (
	"fmt"

	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// Mode is which user a macos-user launch runs the sandbox as (docs/design/macos-user-model.md
// §7, MMP-D2 in docs/design/macos-user-model-plan.md). The profile is the same in both; only
// the crossing into it differs.
type Mode int

const (
	// ModeAccount runs the sandbox as the second account, SandboxUser, through the one sudo rule
	// `yolo macos-setup` installs. It is the zero value, so every plan built without a mode is
	// today's plan.
	ModeAccount Mode = iota
	// ModeSelf runs the sandbox as the invoking user: no account, no sudo.
	ModeSelf
)

// String is the mode's config spelling ("account" or "self").
func (m Mode) String() string {
	switch m {
	case ModeAccount:
		return "account"
	case ModeSelf:
		return "self"
	}
	return fmt.Sprintf("Mode(%d)", int(m))
}

// ParseMode reads the `macos_user_mode` spelling. An empty value is account mode, the
// production default until the plan's slice 11 flips it.
func ParseMode(s string) (Mode, error) {
	switch s {
	case "", "account":
		return ModeAccount, nil
	case "self":
		return ModeSelf, nil
	}
	return ModeAccount, fmt.Errorf("macos-user mode %q is neither \"self\" nor \"account\"", s)
}

// Homes are the homes one launch's sandbox runs with (MMP-D5 to MMP-D7 in
// docs/design/macos-user-model-plan.md):
//
//   - Home is HOME;
//   - Machine holds the machine tier — `.cache`, `Library/Caches` and the mise store
//     (SandboxMiseData(Machine));
//   - Shared is where `scope: machine` pack dirs live;
//   - Real is the invoking user's real home when it is NOT Home, or "" when HOME is already the
//     home the guest should judge workspace scope by (paths.DarwinRealHomeEnv).
type Homes struct {
	Home    string
	Machine string
	Shared  string
	Real    string
}

// LaunchHomes derives a launch's Homes from its mode, its workspace and the invoking user's
// resolved home.
//
// ModeAccount is today's: SandboxHome() for Home, Machine and Shared, and no Real, because the
// account's HOME is its real home. The workspace and hostHome are not read.
//
// ModeSelf is the run-as-you geometry: HOME is <workspace>/.yolo/home itself (MMP-D5); Machine is
// the darwin-only machine root and Shared the store podman binds credentials from, both under
// hostHome's state dir (MMP-D6) — never the shared cache or mise store, which hold Linux files on
// a Mac that also runs podman; Real is hostHome (MMP-D7).
func LaunchHomes(mode Mode, workspace, hostHome string) Homes {
	if mode != ModeSelf {
		return accountHomes(SandboxHome())
	}
	return Homes{
		Home:    paths.WorkspaceHomeState(workspace),
		Machine: paths.MacosUserMachineRootUnder(hostHome),
		Shared:  paths.GlobalHomeUnder(hostHome),
		Real:    hostHome,
	}
}

// accountHomes is account mode's Homes for one home: the same directory in every role but Real.
// It is what every builder taking a bare `home` (empty meaning SandboxHome()) renders with, so
// their argvs are byte for byte what they were before Homes existed.
func accountHomes(home string) Homes {
	if home == "" {
		home = SandboxHome()
	}
	return Homes{Home: home, Machine: home, Shared: home}
}

// RunAs is THE ONE CROSSING into the macos-user sandbox (MMP-D4). Every argv that starts a
// process as the sandbox's user is built by RunAs.Argv, so one value per launch decides whether
// that user is the second account or you, and no consumer can be left crossing into an account
// self mode does not have. TestEveryCrossingGoesThroughRunAs fails a file that builds the sudo
// crossing any other way.
type RunAs struct {
	Mode Mode
}

// Argv prefixes argv with the crossing. Account mode is RunAsSandboxArgv byte for byte —
// `/usr/bin/sudo -n [--set-home] --user=_yolojail /usr/bin/env <argv…>` — with setHome adding
// sudo's `--set-home`. Self mode is `/usr/bin/env <argv…>`: the same command the rule names, so
// every argv keeps its shape after the crossing, and no sudo; setHome means nothing there, since
// no user changes.
func (r RunAs) Argv(setHome bool, argv ...string) []string {
	if r.Mode == ModeSelf {
		return append([]string{envBin}, argv...)
	}
	var flags []string
	if setHome {
		flags = []string{"--set-home"}
	}
	return RunAsSandboxArgv(flags, argv...)
}
