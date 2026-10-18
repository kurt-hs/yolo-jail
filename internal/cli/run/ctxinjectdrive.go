package run

// ctxinjectdrive.go is the HOST DRIVER of live mount injection (docs/design/live-config-apply.md
// §5, the plan's Task C): it decides whether a mount change can reach a running jail, and runs the
// injector, `yolo internal ctx-attach` (internalctxattach.go, internal/ctxinject), against it.
//
// WHERE THE INJECTOR RUNS (§5.3), decided from `podman info` BEFORE anything runs and never by
// retrying a failure, as the image copy decides its own prefix:
//
//   - rootless podman: under `podman unshare`, in podman's rootless user and mount namespaces,
//     where the host source is visible and from which the jail's user namespace is a child;
//   - a nested launch (podman forced onto --userns host) and a rootful podman yolo drives as
//     root: directly;
//   - a rootful podman driven by anyone else: not at all. The command never asks for sudo, so
//     the change waits for the next restart;
//   - a macOS Podman Machine and Apple Container: not built (§5.3), at next restart.

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// ctxInjectDeadline bounds one injector run, unshare prefix included.
const ctxInjectDeadline = 10 * time.Second

// ctxAttachVerbName is the injector's verb (internal/cli's ctxAttachVerb, which this package
// cannot import).
const ctxAttachVerbName = "ctx-attach"

// ctxGeteuid is os.Geteuid, a variable for the rootful branch's tests.
var ctxGeteuid = os.Geteuid

// ctxInjectBranch is where the injector runs for rt: unshare is true for the `podman unshare`
// prefix. reason is non-empty when it cannot run at all, in the outcome line's words.
func (o *Options) ctxInjectBranch(rt string) (unshare bool, reason string) {
	switch {
	case rt == "container": // parity: NotApplicable — Apple Container has no verb to add a share to a running VM (design §5.3, M4)
		return false, "Apple Container shares are fixed when its VM boots"
	case rt != "podman": // parity: NotApplicable — macos-user has no mounts, only links a new session re-stages (design §5.3)
		return false, "a macos-user session has no mounts to add to"
	case o.IsMacOS:
		return false, "the Podman Machine path is not built yet"
	case o.inContainer():
		// A nested podman runs on --userns host: the injector and the jail share one user
		// namespace, so the verb runs directly. It proves nothing about rootless (AGENTS.md).
		return false, ""
	}
	if o.podmanFacts == nil {
		if o.Exec == nil {
			return false, "podman info could not be run"
		}
		res := o.Exec([]string{"podman", "info", "--format", "json"}, "", nil, ctxInjectDeadline)
		if !res.Ran || res.RC != 0 || res.Timeout {
			return false, "podman info did not answer" + firstLineSuffix(res.Stderr)
		}
		o.acceptPodmanFacts(res.Stdout)
	}
	if !o.podmanFacts.parsed || o.podmanFacts.info.Host.Security.Rootless == nil {
		return false, "podman info did not say whether this podman is rootless"
	}
	if *o.podmanFacts.info.Host.Security.Rootless {
		return true, ""
	}
	if ctxGeteuid() != 0 {
		return false, "this podman is rootful and yolo is not running as root"
	}
	return false, ""
}

// ctxMountsLive reports whether config mount changes can be applied to the running jail cname
// now, and the outcome line's reason when not. envLines is the jail's inspected env.
func (o *Options) ctxMountsLive(rt, cname string, envLines []string) (ok bool, reason string) {
	// The backend first: every container backend freezes the same tag list (contracttags.go).
	if rt != "podman" || o.IsMacOS { // parity: Warned — every other backend's mount change waits for a restart, with ctxInjectBranch's reason as the outcome line
		_, reason := o.ctxInjectBranch(rt)
		return false, reason
	}
	tags, known := jailContractTags(envLines)
	if !known {
		return false, "yolo could not read this jail's environment"
	}
	if !tags[contractLiveCtxMounts] {
		return false, "this jail started before live mounts existed"
	}
	if _, ok := readCtxMountRecord(cname, envLines); !ok {
		return false, "this jail started before live mounts existed"
	}
	if _, reason := o.ctxInjectBranch(rt); reason != "" {
		return false, reason
	}
	return true, ""
}

// injectCtxMount mounts m into the running jail cname.
func (o *Options) injectCtxMount(rt, cname string, m configCtxMount) error {
	args := []string{"--source", m.source, "--dest", m.dest}
	if m.rw {
		args = append(args, "--rw")
	}
	return o.runCtxAttach(rt, cname, args)
}

// removeCtxMount unmounts e from the running jail cname and removes its mountpoint.
func (o *Options) removeCtxMount(rt, cname string, e ctxMountEntry) error {
	return o.runCtxAttach(rt, cname, []string{"--dest", e.Dest, "--remove"})
}

// runCtxAttach runs the injector verb against cname's main process with args.
func (o *Options) runCtxAttach(rt, cname string, args []string) error {
	unshare, reason := o.ctxInjectBranch(rt)
	if reason != "" {
		return errors.New(reason)
	}
	pid, err := o.jailMainPid(rt, cname)
	if err != nil {
		return err
	}
	argv := o.selfExecArgv(append([]string{"yolo", "internal", ctxAttachVerbName, "--pid", strconv.Itoa(pid)}, args...))
	if unshare {
		argv = append([]string{"podman", "unshare", "--"}, argv...)
	}
	res := o.Exec(argv, "", nil, ctxInjectDeadline)
	switch {
	case !res.Ran:
		return fmt.Errorf("could not run %s", argv[0])
	case res.Timeout:
		return fmt.Errorf("the mount step did not finish within %s", ctxInjectDeadline)
	case res.RC != 0:
		msg := strings.TrimSpace(res.Stderr)
		msg = strings.TrimPrefix(msg, "yolo internal "+ctxAttachVerbName+": ")
		if msg == "" {
			msg = "exit " + strconv.Itoa(res.RC)
		}
		return errors.New(msg)
	}
	return nil
}

// jailMainPid is the host pid of cname's main process, the process whose mount namespace is the
// jail's.
func (o *Options) jailMainPid(rt, cname string) (int, error) {
	if o.Exec == nil {
		return 0, errors.New("could not ask " + rt + " for the jail's pid")
	}
	res := o.Exec([]string{rt, "inspect", "--format", "{{.State.Pid}}", cname}, "", nil, 3*time.Second)
	if !res.Ran || res.RC != 0 || res.Timeout {
		return 0, fmt.Errorf("%s inspect %s did not answer%s", rt, cname, firstLineSuffix(res.Stderr))
	}
	pid, err := strconv.Atoi(strings.TrimSpace(res.Stdout))
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("%s reports no running process for %s (pid %q)", rt, cname, strings.TrimSpace(res.Stdout))
	}
	return pid, nil
}

// firstLineSuffix is ": <first line of s>", or "" for an empty s.
func firstLineSuffix(s string) string {
	if l := firstLine(s); l != "" {
		return ": " + l
	}
	return ""
}
