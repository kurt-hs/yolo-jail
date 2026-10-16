package run

import (
	"github.com/mschulkind-oss/yolo-jail/internal/patchrecovery"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// programReadinessArgs forwards the jail readiness act's two dials from the host environment
// into the container, each only when the user set it (docs/reference/program-readiness.md,
// OQ-JR1): paths.AllowMissingProgramsEnv, the hatch its refusal offers, and
// paths.NoProgramReadinessEnv, which turns the act off.
//
// The forwarding is reachabilityOptOutArgs' argument exactly. The act runs in the jail's
// provisioning stage, the user types the variable on the HOST in front of `yolo`, and a
// container inherits nothing from the launcher's environment, so a dial that is only read
// in-jail is one nobody can reach; and the user the hatch exists for is the one whose jail will
// not start, who has no in-jail shell to set it from. The entrypoint bakes both values into the
// bootstrap it generates (entrypoint.BootstrapScript).
//
// EMITTED ONLY WHEN SET, for holdOnRefusalArgs' reason: the golden argv is a contract, and a
// launch that asked for neither must carry neither in its frozen environment.
//
// Container backends only, by where it is called: macos-user's stage does not run the readiness
// act yet (JR-D2), so there is nothing there for either dial to reach.
//
// A capture or build jail (Options.NoProgramReadiness) carries the off-switch with its own value
// instead of the host's, so its stage says why the act is off in that jail's terms.
//
// It also carries the launchers' update hatch (useInstalledArgs), the other dial a user types on the
// host for a program yolo installs.
func (o *Options) programReadinessArgs() []string {
	var out []string
	if o.Update {
		out = append(out, "-e", paths.LaunchUpdateEnv+"=1")
	}
	for _, k := range []string{paths.AllowMissingProgramsEnv, paths.NoProgramReadinessEnv} {
		v := o.Getenv(k)
		if k == paths.NoProgramReadinessEnv && o.NoProgramReadiness {
			v = paths.NoProgramReadinessCaptureJail
		}
		if v != "" {
			out = append(out, "-e", k+"="+v)
		}
	}
	return append(out, o.useInstalledArgs()...)
}

// useInstalledArgs forwards patchrecovery.UseInstalledEnv from the host into the container when
// the user set it (docs/design/program-delivery.md PD-NF2): `YOLO_USE_INSTALLED=claude yolo -- claude`
// must reach the launcher that refuses to run the installed claude after a failed update, and a
// container inherits nothing from the launcher's environment. The LAUNCHER reads it, at run time,
// from the environment of the command the user typed, so it rides the container's environment on a
// fresh launch and the exec's on an attach (attachUseInstalledArgs).
func (o *Options) useInstalledArgs() []string {
	if v := o.Getenv(patchrecovery.UseInstalledEnv); v != "" {
		return []string{"-e", patchrecovery.UseInstalledEnv + "=" + v}
	}
	return nil
}

// attachUseInstalledArgs is useInstalledArgs for an attach: this invocation's value rides the exec,
// and a value the fresh launch froze into the container is cleared when this invocation has none
// (an empty value is unset to the launcher, which is the attachNoColorEnvArgs shape), because the
// hatch is the command's it is typed in front of, not the jail's.
func (o *Options) attachUseInstalledArgs(envLines []string) []string {
	if args := o.useInstalledArgs(); args != nil {
		return args
	}
	if envLineValue(envLines, patchrecovery.UseInstalledEnv) != "" {
		return []string{"-e", patchrecovery.UseInstalledEnv + "="}
	}
	return nil
}
