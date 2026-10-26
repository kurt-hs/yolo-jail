package run

import (
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/oauthbroker"
)

// mintBrokerCertificates makes the Claude OAuth broker's CA and leaf exist BEFORE this launch's
// container argv is assembled, because the argv mounts them (the manifest's `ca_cert` and
// `state_files`) and the jail's own broker, which mints them at its start, is spawned by the
// keeper after the argv is built (HD-R1, hostdaemonperjail.go). Without it the first claude jail
// on a machine would start with no CA mounted and an in-jail terminator with no leaf, and only the
// next launch would mount them (the "skipping state file" warning's per-jail case).
//
// It is what the deleted early ensure (brokerEnsure) did as a side effect, under the same gate:
// the broker loophole on (brokerLoopholeActive), a container backend that wires its terminator
// (not Apple Container, whose jail gets no terminator and no CA), and not under the seal. It runs
// yolo's own mint in this process (oauthbroker.EnsureCAAndLeaf, idempotent under its cert.lock,
// so it is a stat on every launch but the first), never a pack's code.
//
// Not fatal: a mint that fails here is attempted again by the jail's broker when it starts, so
// the next launch mounts the files; the line says so.
func (o *Options) mintBrokerCertificates(rt string, cfg *jsonx.OrderedMap) {
	if rt == "container" || o.Sealed || !brokerLoopholeActive(cfg) { // parity: NotApplicable — an Apple Container jail gets no terminator and no CA mount, so there is nothing to mint for
		return
	}
	if err := mintBrokerCerts(); err != nil {
		o.pr(o.Stderr).printf("[yellow]Warning: could not mint the Claude OAuth broker's certificate "+
			"before this launch (%v), so this jail starts without it and Claude's token refreshes "+
			"fail in it. The jail's broker mints it when it starts, so the next launch mounts it; "+
			"`yolo check` says why if this repeats.[/yellow]", err)
	}
}

// mintBrokerCerts is the mint mintBrokerCertificates runs; a var so a test can observe the call.
var mintBrokerCerts = func() error {
	oauthbroker.ConfigureStore()
	return oauthbroker.EnsureCAAndLeaf(false)
}
