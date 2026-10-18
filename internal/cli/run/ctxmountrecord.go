package run

// ctxmountrecord.go is the host's record of the config `mounts` a running podman jail has
// (docs/design/live-config-apply.md §5, the plan's Task C). It is the one source of mount state for
// `yolo config apply`: the boot baseline (.yolo/config-boot.json) cannot be, because read-write
// elements are read from user scope (config.LoadRWMounts) and never appear in it.
//
// WHERE: paths.AgentsDir()/<cname>/ctx-mounts/record.json, beside the --with-credentials grant
// (jailgrant.go) and for its reason: the launcher's own state, outside every directory a jail
// binds, so the jail cannot rewrite what the host believes it has mounted.
//
// WHICH JAIL: a fresh podman launch mints a random TOKEN, freezes it into the container as
// YOLO_CTX_MOUNTS_TOKEN and writes it into the record with the mounts it bound. A record answers
// only for a jail whose inspected env carries the same token, so a record a previous container of
// the same name left behind (one whose teardown never ran) is never read as this jail's.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// ctxMountsTokenEnv carries a podman jail's record token (above). Read by the host only, off the
// container's inspect env; nothing in the jail reads it.
const ctxMountsTokenEnv = "YOLO_CTX_MOUNTS_TOKEN"

const (
	ctxMountRecordDirLeaf  = "ctx-mounts"
	ctxMountRecordFileLeaf = "record.json"
)

// ctxMountEntry is one config mount a running jail has: its resolved host source, its jail
// destination, and whether it is read-write.
type ctxMountEntry struct {
	Source string `json:"source"`
	Dest   string `json:"dest"`
	RW     bool   `json:"rw,omitempty"`
}

// ctxMountRecord is the file's shape.
type ctxMountRecord struct {
	Token   string          `json:"token"`
	Entries []ctxMountEntry `json:"entries"`
}

// ctxMountRecordPath is the record's host path for the jail named cname.
func ctxMountRecordPath(cname string) string {
	return filepath.Join(paths.AgentsDir(), cname, ctxMountRecordDirLeaf, ctxMountRecordFileLeaf)
}

// newCtxMountsToken is a fresh launch's token: 128 random bits, hex.
func newCtxMountsToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// ctxMountEntriesOf is the record's form of the mounts a launch bound (configCtxMounts' list).
func ctxMountEntriesOf(ms []configCtxMount) []ctxMountEntry {
	out := make([]ctxMountEntry, 0, len(ms))
	for _, m := range ms {
		out = append(out, ctxMountEntry{Source: m.source, Dest: m.dest, RW: m.rw})
	}
	return out
}

// writeCtxMountRecord writes the record for cname: 0600 in a 0700 directory, as the grant is,
// because it names host paths.
func writeCtxMountRecord(cname, token string, entries []ctxMountEntry) error {
	if entries == nil {
		entries = []ctxMountEntry{}
	}
	data, err := json.MarshalIndent(ctxMountRecord{Token: token, Entries: entries}, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(ctxMountRecordPath(cname))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("could not make the directory for this jail's live-mounts record (%s): %w", dir, err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("could not narrow %s to its owner: %w", dir, err)
	}
	if err := writeFileBeneathMode(dir, ctxMountRecordFileLeaf, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("could not write this jail's live-mounts record (%s): %w", ctxMountRecordPath(cname), err)
	}
	return nil
}

// loadCtxMountRecord reads cname's record, whatever its token.
func loadCtxMountRecord(cname string) (ctxMountRecord, error) {
	var rec ctxMountRecord
	data, err := os.ReadFile(ctxMountRecordPath(cname))
	if err != nil {
		return rec, err
	}
	if err := json.Unmarshal(data, &rec); err != nil {
		return rec, fmt.Errorf("%s: %w", ctxMountRecordPath(cname), err)
	}
	return rec, nil
}

// readCtxMountRecord returns the config mounts the running jail cname has, from the record, and
// true only when the record exists, parses, and carries the token the jail's inspected env
// (envLines) does. Any other answer is false: the jail's mounts are not known, so nothing may be
// injected into or removed from it.
func readCtxMountRecord(cname string, envLines []string) ([]ctxMountEntry, bool) {
	token, ok := envLineLookup(envLines, ctxMountsTokenEnv)
	if !ok || token == "" {
		return nil, false
	}
	rec, err := loadCtxMountRecord(cname)
	if err != nil || rec.Token != token {
		return nil, false
	}
	if rec.Entries == nil {
		rec.Entries = []ctxMountEntry{}
	}
	return rec.Entries, true
}

// updateCtxMountRecord rewrites cname's record with the mounts the jail has now, keeping its
// token: `yolo config apply` calls it after each injection or removal step, so the record never
// claims a mount the jail lacks or misses one it has. A missing or unreadable record is the error,
// since there is no token to keep.
func updateCtxMountRecord(cname string, entries []ctxMountEntry) error {
	rec, err := loadCtxMountRecord(cname)
	if err != nil {
		return err
	}
	if rec.Token == "" {
		return errors.New(ctxMountRecordPath(cname) + " carries no token")
	}
	return writeCtxMountRecord(cname, rec.Token, entries)
}

// diffCtxMounts compares the mounts a jail has with the ones the config now wants. A destination
// is a mount's identity in the jail, so an entry whose source or mode changed is removed and added
// again. add keeps want's order and remove keeps have's.
func diffCtxMounts(have []ctxMountEntry, want []configCtxMount) (add []configCtxMount, remove []ctxMountEntry) {
	wanted := map[ctxMountEntry]bool{}
	for _, m := range want {
		wanted[ctxMountEntry{Source: m.source, Dest: m.dest, RW: m.rw}] = true
	}
	had := map[ctxMountEntry]bool{}
	for _, e := range have {
		had[e] = true
		if !wanted[e] {
			remove = append(remove, e)
		}
	}
	for _, m := range want {
		if !had[ctxMountEntry{Source: m.source, Dest: m.dest, RW: m.rw}] {
			add = append(add, m)
		}
	}
	return add, remove
}

// prepareCtxMountsToken mints a fresh podman launch's record token into in (and returns it).
// Every other backend gets none: only podman makes the /ctx tmpfs a live mount needs.
func (o *Options) prepareCtxMountsToken(in *assembleInput) (string, error) {
	in.ctxMountsToken = ""
	// THE SEAL (seal.go): a fork build is handed no `mounts` element, so it has nothing to
	// record and gets no token, which would be one more crossing on its -e allowlist.
	if in.sealed {
		return "", nil
	}
	if in.rt != "podman" { // parity: NotApplicable — Apple Container's shares are fixed when its VM boots and macos-user has no mounts (design §5.3)
		return "", nil
	}
	tok, err := newCtxMountsToken()
	if err != nil {
		return "", err
	}
	in.ctxMountsToken = tok
	return tok, nil
}

// recordLaunchCtxMounts writes the record of the config mounts a fresh launch's argv bound
// (in.ctxMountsBound) under the token it froze. A launch with no token writes nothing; one whose
// container of this name left an older record replaces it.
func (o *Options) recordLaunchCtxMounts(in *assembleInput) error {
	if in.ctxMountsToken == "" {
		return nil
	}
	return writeCtxMountRecord(in.cname, in.ctxMountsToken, ctxMountEntriesOf(in.ctxMountsBound))
}
