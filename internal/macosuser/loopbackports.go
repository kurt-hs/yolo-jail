package macosuser

// loopbackports.go collects the LOOPBACK ALLOW LIST of a launch (MMP-D10 of
// docs/design/macos-user-model-plan.md): the ports of the Mac's loopback the deny-by-default
// profile re-allows while the loopback policy denies it (OQ-MM3, ruled; OQ-MM7, pending, may
// allow the whole loopback instead, and then nothing here is rendered).
//
// Three sources, with no config key of its own:
//
//   - each published host-service endpoint the launch tells the sandbox about: the port of every
//     `YOLO_SERVICE_*_ENDPOINT` file (isServiceEndpointVar), read after the host services started.
//     The env is the manifest, for endpointGrantCommands' reason: the run pipeline set one
//     variable per service that published, so this re-derives no selector;
//   - each SERVED ADDRESS of the launch (internal/cli/run's servedaddresses.go): the loopback
//     address a jail daemon in the guest, a credential doorway or a launch-owned service answers
//     on, which the run pipeline hands over on JailDaemons.Served;
//   - the JAIL port of each `network.forward_host_ports` entry, read by the key's one reader
//     (config.ParsePortForwards). The key already means "host loopback ports this jail may reach",
//     and every workspace-config change already goes through the approval diff.
//
// ⚠ A PORT IS KNOWN ONLY ONCE ITS SERVICE PUBLISHED (the plan's Traps, "Ephemeral loophole
// ports"): svcendpoint binds 127.0.0.1:0. An endpoint file that cannot be read at plan time
// refuses the launch, naming the variable; a profile that silently omitted the port would be
// refused by the in-sandbox witness anyway, and less clearly. A dry run, whose endpoint paths are
// placeholders, names each such port as read at launch instead.

import (
	"errors"
	"fmt"
	"net"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/svcendpoint"
)

// forwardHostPortsKey is the config key the third source reads, spelled as the user writes it.
const forwardHostPortsKey = "network.forward_host_ports"

// LoopbackAllow is one port of the allow list and every reason it is on it, sorted.
type LoopbackAllow struct {
	Port int
	Why  []string
}

// LoopbackUnread is a published endpoint whose port could not be read: the variable naming it,
// the file, and the read's error.
type LoopbackUnread struct {
	Var, Path string
	Err       error
}

// LoopbackCollection is a launch's collected allow list: the ports, sorted and unique, and the
// endpoints whose port could not be read. AtLaunch says the endpoints are a plan render's
// placeholders, read only by a live launch, so an unread one is not a problem.
type LoopbackCollection struct {
	Allows   []LoopbackAllow
	Unread   []LoopbackUnread
	AtLaunch bool
}

// Ports is the collection's ports, ascending.
func (c LoopbackCollection) Ports() []int {
	out := make([]int, 0, len(c.Allows))
	for _, a := range c.Allows {
		out = append(out, a.Port)
	}
	return out
}

// CollectLoopback collects the allow list from the launch's served addresses, the config's
// `network.forward_host_ports` entries and every env that may name a published endpoint (the
// session's and the jail daemons'). Deterministic: ports ascend, each reason list is sorted, and
// unread endpoints are ordered by variable.
func CollectLoopback(served []string, forwardHostPorts []any, envs ...*jsonx.OrderedMap) LoopbackCollection {
	why := map[int]map[string]bool{}
	add := func(port int, reason string) {
		if port < 1 || port > 65535 {
			return
		}
		if why[port] == nil {
			why[port] = map[string]bool{}
		}
		why[port][reason] = true
	}
	var c LoopbackCollection
	seenVar := map[string]bool{}
	for _, env := range envs {
		if env == nil {
			continue
		}
		for _, k := range env.Keys() {
			if !isServiceEndpointVar(k) {
				continue
			}
			v, _ := env.Get(k)
			path := asStr(v)
			if path == "" || seenVar[k+"\x00"+path] {
				continue
			}
			seenVar[k+"\x00"+path] = true
			hostPort, err := endpointHostPort(path)
			if err != nil {
				c.Unread = append(c.Unread, LoopbackUnread{Var: k, Path: path, Err: err})
				continue
			}
			add(portOf(hostPort), k)
		}
	}
	for _, addr := range served {
		add(portOf(addr), "served address "+addr)
	}
	for _, e := range forwardHostPorts {
		// One entry at a time, so a reason stays paired with its entry; validation has refused
		// an entry this cannot read before a launch gets here.
		parsed, err := config.ParsePortForwards([]any{e}, nil)
		if err != nil || len(parsed) != 1 {
			continue
		}
		add(parsed[0].LocalPort, fmt.Sprintf("%s %q", forwardHostPortsKey, fmt.Sprint(e)))
	}
	ports := make([]int, 0, len(why))
	for p := range why {
		ports = append(ports, p)
	}
	slices.Sort(ports)
	for _, p := range ports {
		reasons := make([]string, 0, len(why[p]))
		for r := range why[p] {
			reasons = append(reasons, r)
		}
		sort.Strings(reasons)
		c.Allows = append(c.Allows, LoopbackAllow{Port: p, Why: reasons})
	}
	sort.SliceStable(c.Unread, func(i, j int) bool { return c.Unread[i].Var < c.Unread[j].Var })
	return c
}

// LoopbackPorts is CollectLoopback's ports, ascending and unique, and an error naming each
// endpoint variable whose file could not be read.
func LoopbackPorts(served []string, forwardHostPorts []any, envs ...*jsonx.OrderedMap) ([]int, error) {
	c := CollectLoopback(served, forwardHostPorts, envs...)
	var errs []error
	for _, u := range c.Unread {
		errs = append(errs, fmt.Errorf("%s (%s): %w", u.Var, u.Path, u.Err))
	}
	return c.Ports(), errors.Join(errs...)
}

// loopbackProblems is the plan problem for each endpoint whose port could not be read, when the
// profile denies loopback and the collection is a live launch's.
func loopbackProblems(c LoopbackCollection) []string {
	if c.AtLaunch {
		return nil
	}
	var out []string
	for _, u := range c.Unread {
		out = append(out, u.Var+" names a host service's endpoint file whose port the profile "+
			"cannot read ("+u.Path+": "+errStr(u.Err)+"), so the deny-by-default profile would "+
			"refuse the sandbox that service. The service may have stopped after it published; "+
			"run `yolo` again to start it fresh")
	}
	return out
}

// loopbackDisclosure is the lines a launch prints for its allow list when the profile denies
// loopback: each port and why, or that none is open; and in a plan render, each endpoint whose
// port only a live launch reads.
func loopbackDisclosure(c LoopbackCollection) []string {
	var out []string
	if len(c.Allows) == 0 && (len(c.Unread) == 0 || !c.AtLaunch) {
		return []string{"[dim]loopback: the sandbox reaches no port of your loopback (OQ-MM3)[/dim]"}
	}
	for _, a := range c.Allows {
		out = append(out, fmt.Sprintf("loopback: port %d is open to the sandbox [dim](%s)[/dim]",
			a.Port, strings.Join(a.Why, ", ")))
	}
	if c.AtLaunch {
		for _, u := range c.Unread {
			out = append(out, fmt.Sprintf("loopback: the port %s publishes is open to the sandbox "+
				"[dim](read from %s at launch)[/dim]", u.Var, u.Path))
		}
	}
	return out
}

// endpointHostPort is the advertised `host:port` of a published endpoint file, in either format
// a host service writes (the loopback-TLS triple, or the plain one-field line).
func endpointHostPort(path string) (string, error) {
	ep, err := svcendpoint.Read(path)
	if err == nil {
		return ep.HostPort, nil
	}
	if addr, perr := svcendpoint.ReadPlain(path); perr == nil {
		return addr, nil
	}
	return "", err
}

// configForwardHostPorts is the config's `network.forward_host_ports` entries, nil when it
// names none.
func configForwardHostPorts(cfg *jsonx.OrderedMap) []any {
	netSec := cfgSection(cfg, "network")
	if netSec == nil {
		return nil
	}
	v, _ := netSec.Get("forward_host_ports")
	list, _ := v.([]any)
	return list
}

// portOf is addr's port, or 0 when addr is not a `host:port`.
func portOf(addr string) int {
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		return 0
	}
	return n
}
