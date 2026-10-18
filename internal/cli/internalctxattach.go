package cli

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/mschulkind-oss/yolo-jail/internal/ctxinject"
)

// ctxAttachVerb is `yolo internal ctx-attach`: the INJECTOR of docs/design/live-config-apply.md
// §5.1, which adds one context mount to a running podman jail (or, with --remove, takes one
// away). Hidden because its caller is the host driver (run's ctxinjectdrive.go), which runs it
// directly on a rootful or nested podman and under `podman unshare` on a rootless one, where the
// jail's user namespace is a child of podman's. Short-lived: it holds no secret and never runs as
// a daemon (§5.6).
//
// The source arrives already resolved by the host (§5.6: host paths are resolved before setns,
// never after); the destination is the one jail-side input.
const ctxAttachVerb = "ctx-attach"

// ctxInjector is the verb's effect, injected for its tests.
type ctxInjector struct {
	attach func(pid int, source, dest string, rw bool) error
	detach func(pid int, dest string) error
}

func runCtxAttach(args []string) int {
	return ctxAttachMain(args, os.Stderr, ctxInjector{attach: ctxinject.Attach, detach: ctxinject.Detach})
}

func ctxAttachMain(args []string, stderr io.Writer, inj ctxInjector) int {
	fs := flag.NewFlagSet("yolo internal "+ctxAttachVerb, flag.ContinueOnError)
	fs.SetOutput(stderr)
	pid := fs.Int("pid", 0, "the jail's main process, as the host sees it (podman inspect .State.Pid)")
	source := fs.String("source", "", "the resolved host directory to mount")
	dest := fs.String("dest", "", "the jail path, under /ctx/")
	rw := fs.Bool("rw", false, "mount read-write (default read-only)")
	remove := fs.Bool("remove", false, "unmount --dest and remove its mountpoint instead")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	bad := *pid <= 0 || *dest == "" || fs.NArg() != 0
	if *remove {
		bad = bad || *source != "" || *rw
	} else {
		bad = bad || *source == ""
	}
	if bad {
		fmt.Fprintln(stderr, "usage: yolo internal "+ctxAttachVerb+
			" --pid <pid> --source <dir> --dest /ctx/<name> [--rw] | --pid <pid> --dest /ctx/<name> --remove")
		return 2
	}
	var err error
	if *remove {
		err = inj.detach(*pid, *dest)
	} else {
		err = inj.attach(*pid, *source, *dest, *rw)
	}
	if err != nil {
		fmt.Fprintf(stderr, "yolo internal %s: %v\n", ctxAttachVerb, err)
		return 1
	}
	return 0
}
