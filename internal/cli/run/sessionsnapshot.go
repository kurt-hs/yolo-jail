package run

// sessionsnapshot.go is the HOST HALF of the ENTRY SNAPSHOT (docs/design/per-session-profiles.md
// §4.3 step 1, SP-D12): one immutable directory per session id holding that entry's env files,
// written by the launcher before the entry's exec and read by that session alone.
//
// WHY. The shared yolo-user-env.sh and the per-agent env files are rewritten whole by EVERY
// entry (deliverChannel), so a session that reads them after a later attach reads that attach's
// profile: its nested shells reset their provider tables, and an agent it starts later runs on
// another entry's provider while its own policy forbids it. A snapshot is this entry's channel,
// frozen under its session id, so a session's readers (entrypoint's userEnvFilePath and the
// launcher and .bashrc text) have something no later entry touches.
//
// LAYOUT, the contract with internal/entrypoint: in the jail,
// ~/.config/yolo-agent-env/sessions/<id>/ (entrypoint.SessionSnapshotDirRel) holds
// yolo-user-env.sh, <agent>.sh, <agent>.<name> and provider-keys/<agent>.sh, the same bodies the
// shared files get from the same composition. On the host that is <wsState>/agent-env/sessions/<id>
// on podman, inside the directory the launch already binds `:ro` there, and
// <wsState>/.config/yolo-agent-env/sessions/<id> on Apple Container, which binds wsState itself
// at the jail home. No new mount. The jail can read a snapshot and, on podman, never write one.
//
// LIFETIME (SP-D7): until the jail is known gone. The shared files' rewrite keeps the sessions
// directory (writeAgentEnvFiles), and the teardown's clearAgentEnvFiles removes it with the rest.
// Nothing reaps one while the jail runs: a long session reads its snapshot whenever it starts
// an agent.

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
	"github.com/mschulkind-oss/yolo-jail/internal/richtext"
)

// writeSessionSnapshot publishes this entry's channel as session id's snapshot under wsState
// (the file's header has the layout). It writes into sessions/.<id>.tmp and renames that to
// sessions/<id>, so a reader sees a whole snapshot or none, and never replaces a published one:
// snapshots are immutable, and an id already present is an error. No id writes nothing.
//
// Every write is beneath wsState's os.Root, for writeUserEnvFile's reason: on Apple Container
// wsState is the jail's own home, and a link the jail left there must not carry a credential out.
func writeSessionSnapshot(wsState, rt, id string, channel *packChannel) error {
	if id == "" {
		return nil
	}
	if !entrypoint.ValidSessionID(id) {
		return fmt.Errorf("%q is not a session id", id)
	}
	base := agentEnvStateDir
	if rt == "container" { // parity: HonoredBy — Apple Container's agent files are written in the home it binds, at their in-jail path (deliverChannel), and so is the snapshot
		base = entrypoint.AgentEnvDirRel
	}
	sessions := filepath.Join(base, entrypoint.SessionSnapshotsSubdir)
	final := filepath.Join(sessions, id)
	tmp := filepath.Join(sessions, "."+id+".tmp")
	where := filepath.Join(wsState, final)
	fail := func(err error) error {
		return fmt.Errorf("could not write the entry snapshot %s: %w", where, err)
	}
	r, err := openStateRoot(wsState)
	if err != nil {
		return fail(err)
	}
	defer r.Close()
	if err := agentEnvDirBeneath(r, base); err != nil {
		return fail(err)
	}
	if err := agentEnvDirBeneath(r, sessions); err != nil {
		return fail(err)
	}
	if _, err := r.Lstat(final); err == nil {
		return fail(errors.New("a snapshot of that session already exists"))
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fail(err)
	}
	_ = r.RemoveAll(tmp)
	// The agent files first: writeAgentEnvFiles empties its directory of everything it did not
	// write, so the shared file is added after it. Their lines name the PUBLISHED path.
	if err := writeAgentEnvFiles(wsState, tmp, entrypoint.SessionSnapshotDirRel(id), jailHome, channel); err != nil {
		_ = r.RemoveAll(tmp)
		return fail(err)
	}
	// Written even when empty, as the shared file is: a snapshot with no yolo-user-env.sh would
	// send the session's readers back to the shared one.
	body := userEnvFileBody(channel.sharedEnvSourceWinners(), channel)
	if err := writeBeneath(r, filepath.Join(tmp, "yolo-user-env.sh"),
		userEnvFileMode, userEnvFileMode, writeBytes(body)); err != nil {
		_ = r.RemoveAll(tmp)
		return fail(err)
	}
	if err := r.Rename(tmp, final); err != nil {
		_ = r.RemoveAll(tmp)
		return fail(err)
	}
	return nil
}

// refuseSnapshotWrite is the refusal of an entry whose snapshot could not be written (SP-D12):
// such a session would read the shared files, which the next entry rewrites, so it does not
// start. The caller returns non-zero after it.
func (o *Options) refuseSnapshotWrite(err error) {
	out := o.pr(o.Stderr)
	out.printf("[bold red]Refusing to start this session: its environment snapshot could not be written.[/bold red]")
	out.print("")
	out.printf("%s", richtext.Escape(err.Error()))
	out.print("")
	out.print("[dim]Free the disk or fix that directory's permissions ('yolo check' reports both), then run the same command again.[/dim]")
}

// noteSessionSnapshotsUnsupported is SP-D8's one line: an attach to a jail whose contract tags
// lack session-homes rewrites the shared files that jail's sessions read, as every attach did
// before snapshots, so a session already running there may now read this entry's profile. The
// fix is a fresh launch, which runs this build's binaries. Silent for a jail with the tag and for
// one whose environment proves nothing (jailContractTags' "cannot prove, do not warn").
func (o *Options) noteSessionSnapshotsUnsupported(rt, cname string, envLines []string) {
	tags, known := jailContractTags(envLines)
	if !known || tags[contractSessionHomes] {
		return
	}
	o.pr(o.Stderr).printf("[yellow]Notice: this jail predates per-session profiles, so an agent session "+
		"already running in it may now read this entry's profile; %s once its sessions are done, then "+
		"launch again, removes that.[/yellow]", stopRemedy(rt, cname))
}
