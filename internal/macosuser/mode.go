package macosuser

import "fmt"

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
