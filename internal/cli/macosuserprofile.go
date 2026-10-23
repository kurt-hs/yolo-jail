package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/macosuser"
)

// macosUserProfileVerb is `yolo internal macos-user-profile`: it prints the deny-by-default
// Seatbelt profile (macosuser.DenyDefaultProfile) for the inputs it is given, with the three
// pending rulings at their production values unless a flag overrides one
// (docs/design/macos-user-model-plan.md slice 10). Hidden because its callers are the
// prototype's check script (run-checks.sh --profile-from-yolo) and a human diffing the
// generated profile against the prototype's profile.sb, not a launch. It reads nothing but
// its flags, the environment's HOME and user, and `getconf DARWIN_USER_TEMP_DIR`, and writes
// nothing but stdout.
//
// It renders what the prototype has: a workspace, the agent's HOME, the launch's own temp
// dir, the user's temp dir, the human's home and loopback ports. A launch's other inputs
// (machine dirs, context mounts, cache relocations, workspace_readonly, staged home content,
// devices) are left empty, as profile.sb has none of them; the staging root is the
// production one.
const macosUserProfileVerb = "macos-user-profile"

// macosUserProfileUsage is the verb's whole usage, printed on misuse.
const macosUserProfileUsage = "usage: yolo internal " + macosUserProfileVerb +
	" --mode self|account --workspace W --tmp T\n" +
	"         [--home H] [--host-home H] [--darwin-tmp D] [--account NAME] [--allow-port P]...\n" +
	"         [--loopback deny|allow] [--shared-tmp shared|private]\n" +
	"         [--host-software PATH]... [--no-host-software]\n" +
	"Prints the deny-by-default macos-user Seatbelt profile for these inputs. Paths are made\n" +
	"absolute and resolved through symlinks when they exist. --home defaults to W/.yolo/home,\n" +
	"--host-home to $HOME, --darwin-tmp to `getconf DARWIN_USER_TEMP_DIR` on macOS. The last\n" +
	"four flags override the pending rulings' production values (loopback, the shared /tmp,\n" +
	"the visible host software trees); without them the profile is the one a launch renders."

// macosUserProfileEnv is what the verb reads besides its flags, injected so the test needs
// no Mac and no particular user.
type macosUserProfileEnv struct {
	home        func() string // the invoking human's home ($HOME)
	currentUser func() string // the invoking user's name, the self-mode header's
	darwinTmp   func() string // `getconf DARWIN_USER_TEMP_DIR`, or "" off macOS
}

func runMacosUserProfile(args []string) int {
	return macosUserProfileMain(args, os.Stdout, os.Stderr, macosUserProfileEnv{
		home: func() string { return os.Getenv("HOME") },
		currentUser: func() string {
			if u, err := user.Current(); err == nil && u.Username != "" {
				return u.Username
			}
			return os.Getenv("USER")
		},
		darwinTmp: func() string {
			if goruntime.GOOS != "darwin" {
				return ""
			}
			out, err := exec.Command("/usr/bin/getconf", "DARWIN_USER_TEMP_DIR").Output()
			if err != nil {
				return ""
			}
			return strings.TrimSpace(string(out))
		},
	})
}

// repeatedFlag collects every value of a flag given more than once.
type repeatedFlag []string

func (r *repeatedFlag) String() string     { return strings.Join(*r, ",") }
func (r *repeatedFlag) Set(v string) error { *r = append(*r, v); return nil }

// physicalPath makes p absolute and resolves it through symlinks when it exists: the kernel
// resolves a path before Seatbelt consults the policy, so a rule on /tmp or an unresolved
// /var/folders path would match nothing. A path that does not exist is kept as given.
func physicalPath(p string) string {
	if p == "" {
		return ""
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real
	}
	return abs
}

func macosUserProfileMain(args []string, stdout, stderr io.Writer, env macosUserProfileEnv) int {
	fs := flag.NewFlagSet("yolo internal "+macosUserProfileVerb, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	mode := fs.String("mode", "", "self or account: the user the profile runs as (header only)")
	workspace := fs.String("workspace", "", "the workspace, read-write")
	home := fs.String("home", "", "the agent's HOME (default <workspace>/.yolo/home)")
	hostHome := fs.String("host-home", "", "the invoking human's home (default $HOME)")
	tmp := fs.String("tmp", "", "the launch's own TMPDIR, read-write")
	darwinTmp := fs.String("darwin-tmp", "", "the running user's DARWIN_USER_TEMP_DIR")
	account := fs.String("account", macosuser.SandboxUser, "the account --mode account runs as")
	loopback := fs.String("loopback", "", "deny or allow (default: the production ruling)")
	sharedTmp := fs.String("shared-tmp", "", "shared or private (default: the production ruling)")
	noHostSoftware := fs.Bool("no-host-software", false, "hide every host software tree")
	var ports, hostSoftware repeatedFlag
	fs.Var(&ports, "allow-port", "a loopback port the sandbox may connect to (repeatable)")
	fs.Var(&hostSoftware, "host-software", "a host install tree the sandbox may read (repeatable)")

	misuse := func(why string) int {
		fmt.Fprintf(stderr, "yolo internal %s: %s\n%s\n", macosUserProfileVerb, why, macosUserProfileUsage)
		return 2
	}
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			fmt.Fprintln(stdout, macosUserProfileUsage)
			return 0
		}
		return misuse(err.Error())
	}
	if fs.NArg() != 0 {
		return misuse(fmt.Sprintf("unexpected argument %q", fs.Arg(0)))
	}
	m, err := macosuser.ParseMode(*mode)
	if err != nil || *mode == "" {
		return misuse("--mode must be self or account")
	}
	if *workspace == "" || *tmp == "" {
		return misuse("--workspace and --tmp are required")
	}

	ws := physicalPath(*workspace)
	p := macosuser.ProfileParams{
		Workspace:   ws,
		Home:        physicalPath(*home),
		HostHome:    physicalPath(*hostHome),
		SandboxTmp:  physicalPath(*tmp),
		StagingRoot: macosuser.StagingRoot(),
	}
	if p.Home == "" {
		p.Home = filepath.Join(ws, ".yolo", "home")
	}
	if p.HostHome == "" {
		p.HostHome = physicalPath(env.home())
	}
	if *darwinTmp != "" {
		p.DarwinUserTempDir = physicalPath(*darwinTmp)
	} else {
		p.DarwinUserTempDir = physicalPath(env.darwinTmp())
	}
	if m == macosuser.ModeSelf {
		p.RunAsUser = env.currentUser()
	} else {
		p.RunAsUser = *account
	}
	for _, s := range ports {
		port, err := strconv.Atoi(s)
		if err != nil || port < 1 || port > 65535 {
			return misuse(fmt.Sprintf("--allow-port needs a port number from 1 to 65535, got %q", s))
		}
		p.Loopback.Ports = append(p.Loopback.Ports, port)
	}
	listed := p.Loopback.Ports

	// The production rulings first, then each flag the caller set.
	p = p.WithProductionRulings()
	switch *loopback {
	case "":
	case "deny":
		p.Loopback = macosuser.LoopbackPolicy{Ports: listed}
	case "allow":
		p.Loopback = macosuser.LoopbackPolicy{AllowAll: true}
	default:
		return misuse(fmt.Sprintf("--loopback must be deny or allow, got %q", *loopback))
	}
	switch *sharedTmp {
	case "":
	case "shared":
		p.Tmp = macosuser.TmpShared
	case "private":
		p.Tmp = macosuser.TmpPrivate
	default:
		return misuse(fmt.Sprintf("--shared-tmp must be shared or private, got %q", *sharedTmp))
	}
	if *noHostSoftware && len(hostSoftware) > 0 {
		return misuse("--no-host-software and --host-software contradict each other; pass one")
	}
	if *noHostSoftware {
		p.HostSoftware = nil
	} else if len(hostSoftware) > 0 {
		p.HostSoftware = nil
		for _, h := range hostSoftware {
			p.HostSoftware = append(p.HostSoftware, physicalPath(h))
		}
	}

	fmt.Fprint(stdout, macosuser.DenyDefaultProfile(p))
	return 0
}
