package loopholedecl

// enums.go is the CLOSED vocabulary the schema is built on: a manifest does not
// DEFINE a transport or a publication mode, it SELECTS one. The set is closed
// because the run pipeline, the checks and the manifests all have to agree on
// each value, and a typo in any one of those places silently selects the other
// mechanism.
//
// The values live here rather than in internal/loopholes because they are what
// the manifest may SAY — every reader of a manifest needs them, including the
// footprint and `pack lint`, neither of which may import the runtime.

// DefaultBrokerIP is the container runtime's host-gateway sentinel, and the
// default for `broker_ip`. The runtime translates it into the right
// host-reachable address.
const DefaultBrokerIP = "host-gateway"

// Transport values.
//
// THERE ARE TWO, and that is the whole point of the unification
// (docs/reference/loophole-transport.md §7.4). `transport` now answers exactly one
// question — "does this loophole have a host daemon a jail dials, and if so how"
// — where it used to conflate that with "does this loophole intercept TLS".
const (
	// TransportLoopbackTLS is THE transport (internal/svcendpoint): the framework
	// publishes an endpoint file and the daemon never learns what carried its
	// bytes.
	TransportLoopbackTLS = "loopback-tls"
	// TransportNone means NO DAEMON, not a different transport. It stays.
	TransportNone = "none"
)

// Retired transport values, kept ONLY to recognize them and say what to write
// instead. Neither is in validTransports, so a manifest naming one is rejected —
// deliberately, because a value that still validates is a value someone will use.
//
// A manifest that declared either one loses nothing by declaring loopback-tls:
//
//   - "tls-intercept" never selected a transport. Intercept-ness is carried
//     entirely by `intercepts` + `broker_ip` + `ca_cert`, which is why the one
//     behavioural reader (RuntimeArgsFor's Apple Container skip) now keys on
//     len(Intercepts) instead. It named hop A (the in-jail TLS terminator) while
//     saying nothing about hop B, the hop that actually crosses to the host.
//   - "unix-socket" is the transport that does not work on macOS + podman at all
//     (virtiofs shares a socket's inode, not its connection endpoint), which is
//     the defect the unification exists to fix.
const (
	RetiredTransportTLSIntercept = "tls-intercept"
	RetiredTransportUnixSocket   = "unix-socket"
)

// host_daemon.publishes values: WHAT THE DAEMON ITSELF PUBLISHES. The retired
// `unix-socket` transport conflated two facts — what the jail dials and what
// the daemon binds — and this field is the honest split's second half
// (docs/reference/loophole-transport.md#two-server-shapes-and-how-a-manifest-selects-one).
// `transport` stays loopback-tls either way,
// because the transport is what the jail dials and that does not change.
const (
	// PublishesEndpoint (the default): the daemon publishes the loopback-TLS
	// endpoint file itself at {endpoint} — what all bundled loopholes do.
	PublishesEndpoint = "endpoint"
	// PublishesSocket: the daemon binds a plain AF_UNIX socket at {socket};
	// yolo waits for that socket, then runs the TLS front (svcendpoint) and
	// publishes the endpoint file in front of it. The daemon never learns what
	// carried its bytes — this is what makes a non-Go daemon expressible.
	PublishesSocket = "socket"
)

// host_daemon.request_end values: HOW A REQUEST ENDS on the daemon's socket,
// meaningful under publishes:"socket"
// (docs/reference/loophole-transport.md#request_end--how-a-request-ends-behind-the-front).
const (
	// RequestEndFramed (the default): the protocol is length-prefixed (or
	// otherwise self-delimiting), so the front never propagates the client's
	// EOF upstream — today's relay-parity behaviour, bit-identical.
	RequestEndFramed = "framed"
	// RequestEndEOF: the daemon reads its request TO EOF, so the front
	// half-closes the upstream socket when the client's request direction
	// ends. Without this such a daemon works on a bare socket and hangs
	// forever behind the front.
	RequestEndEOF = "eof"
)

// host_daemon.scope values: WHO THE DAEMON BELONGS TO. One answer is left.
//
// The key used to have two: ScopeJail, and "host", ONE daemon per host serving every jail on
// it, ensured under a host-wide flock and never stopped by a jail's end. "host" is RETIRED
// (docs/design/host-daemon-ownership.md, HD-R1, ruled 2026-09-20): every host daemon is now
// spawned by the launch that wants it and ends with it. The reason the value existed, that a
// second copy of the Claude OAuth broker would race the single-use refresh token, was false in
// the code: each daemon serializes on a flock keyed by its state file, never by the process, so
// N copies in one home take one kernel lock (that doc's §1.1). What stays shared is that state.
//
// The key survives so a manifest that spells the default still decodes; "host" is refused by
// the strict (authoring) decoder with the migration (retiredScopeHint), the way a retired
// transport is, and read as ScopeJail with a skew note across the version boundary
// (readRetiredScopeAsJail), since every jail an older yolo launched booted manifests saying it.
const (
	// ScopeJail (the default, and the only value): one daemon per jail. yolo spawns it at
	// launch, waits for it, and stops it when the jail ends (`detach` says how).
	ScopeJail = "jail"
	// RetiredScopeHost is the retired value, named so its refusal can carry the migration.
	RetiredScopeHost = "host"
)

// retiredScopeHint is the migration a manifest declaring `"scope": "host"` is refused with.
func retiredScopeHint() string {
	return " — 'host' was retired (docs/design/host-daemon-ownership.md, HD-R1): every jail now " +
		"starts its own host daemon and ends it with the jail, and the state the daemons share " +
		"(a credentials file and the flock beside it) is what serializes them. Delete 'scope'. " +
		"A daemon whose work in flight must finish when its jail ends declares 'detach': true."
}

// Valid enum values. Kept as ordered slices in sorted order so the
// "not in [...]" error strings render deterministically. Unexported because a
// package-level slice is mutable; the accessors below hand out copies.
var (
	validTransports  = []string{TransportLoopbackTLS, TransportNone}
	validLifecycles  = []string{"external", "spawned"}
	validRestarts    = []string{"always", "on-failure", "no"}
	validPublishes   = []string{PublishesEndpoint, PublishesSocket}
	validRequestEnds = []string{RequestEndEOF, RequestEndFramed}
	validScopes      = []string{ScopeJail}
)

// ValidTransports returns the accepted `transport` values.
func ValidTransports() []string { return copyOf(validTransports) }

// ValidLifecycles returns the accepted `lifecycle` values.
func ValidLifecycles() []string { return copyOf(validLifecycles) }

// ValidRestarts returns the accepted `jail_daemon.restart` values.
func ValidRestarts() []string { return copyOf(validRestarts) }

// ValidPublishes returns the accepted `host_daemon.publishes` values.
func ValidPublishes() []string { return copyOf(validPublishes) }

// ValidRequestEnds returns the accepted `host_daemon.request_end` values.
func ValidRequestEnds() []string { return copyOf(validRequestEnds) }

// ValidScopes returns the accepted `host_daemon.scope` values.
func ValidScopes() []string { return copyOf(validScopes) }

func copyOf(list []string) []string { return append([]string(nil), list...) }

// retiredTransportHint appends the migration instruction to the "not in [...]"
// error when the rejected value is one this repo used to ship.
//
// The bare enum error is technically complete and practically useless here: the
// reader wrote a value the docs told them to write, and the consequence of the
// rejection is that their loophole VANISHES (loadModuleDirs warns and moves on). The
// hint turns a breaking change into a self-documenting one, which is the price of
// removing a value rather than deprecating it.
func retiredTransportHint(transport string) string {
	switch transport {
	case RetiredTransportTLSIntercept:
		return " — 'tls-intercept' was retired: it named the in-jail TLS terminator," +
			" not a transport. Write 'loopback-tls' and keep 'intercepts'/'broker_ip'/" +
			"'ca_cert' exactly as they are; those are what wire the interception."
	case RetiredTransportUnixSocket:
		return " — 'unix-socket' was retired (docs/reference/loophole-transport.md):" +
			" it cannot cross virtiofs on macOS + podman. Write 'loopback-tls' and add" +
			" 'publishes': 'socket' to 'host_daemon': the daemon keeps binding its" +
			" AF_UNIX socket at the path yolo substitutes into '{socket}', and yolo" +
			" runs the TLS front over it and publishes the endpoint file for you."
	}
	return ""
}
