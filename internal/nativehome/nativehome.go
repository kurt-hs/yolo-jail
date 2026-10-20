// Package nativehome is the account-owned lifetime and command environment of a native
// session home. The host's existing session record still owns host staging; these leases
// protect account-owned config, including children that outlive that host record.
package nativehome

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/mschulkind-oss/yolo-jail/internal/packload"
	"golang.org/x/sys/unix"
)

const ExecVerb = "native-session-exec"
const SessionFlag = "--session"
const metadataName = "environment.json"
const launchedName = ".launched"

func ValidID(id string) bool {
	if len(id) != 16 {
		return false
	}
	for _, r := range id {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

// Root qualifies homes by the canonical workspace the host already resolved. HOME does
// not move, and enumeration can never wander into another workspace's namespace.
func Root(home, workspace string) string {
	hash := sha256.Sum256([]byte(filepath.Clean(workspace)))
	return filepath.Join(home, ".yolo-native-sessions", hex.EncodeToString(hash[:]))
}

func Dir(home, workspace, id string) (string, error) {
	if !ValidID(id) || !filepath.IsAbs(home) || !filepath.IsAbs(workspace) {
		return "", fmt.Errorf("invalid native session identity or paths; retry yolo with its generated session ID")
	}
	return filepath.Join(Root(home, workspace), id), nil
}

// Prepare creates only real directories below HOME, never following an agent-editable
// namespace link. A never-launched home remains incomplete and is retained for retry.
func Prepare(home, workspace, id string) (string, error) {
	dir, err := Dir(home, workspace, id)
	if err != nil {
		return "", err
	}
	for _, p := range []string{filepath.Dir(Root(home, workspace)), Root(home, workspace), dir} {
		if err := os.Mkdir(p, 0700); err != nil && !os.IsExist(err) {
			return "", err
		}
		fi, err := os.Lstat(p)
		if err != nil || !fi.IsDir() {
			return "", fmt.Errorf("native session path %s is not a real directory; move the occupant and retry yolo", p)
		}
	}
	f, err := os.OpenFile(filepath.Join(dir, packload.EmbeddedLeaseName), os.O_CREATE|os.O_EXCL|os.O_RDONLY, 0400)
	if os.IsExist(err) {
		return "", fmt.Errorf("native session %s already exists; retry yolo to generate a fresh session", dir)
	}
	if err != nil {
		return "", err
	}
	_ = f.Close()
	return dir, nil
}

// Complete publishes only identity/declared relocation values after successful generation.
// Credentials stay in the existing host-owned env file, not in this account-owned metadata.
func Complete(dir string, vars map[string]string) error {
	b, err := json.Marshal(vars)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, metadataName), b, 0600)
}

// ClaimGone requires both a completed launch and an exclusive lease. The caller must also
// prove the sandbox account idle: detached programs may close inherited file descriptors.
func ClaimGone(dir string) (func(), bool) {
	if fi, err := os.Lstat(filepath.Join(dir, launchedName)); err != nil || !fi.Mode().IsRegular() {
		return func() {}, false
	}
	state, release, _ := packload.ProbeLease(dir)
	if state != packload.LeaseFree {
		release()
		return func() {}, false
	}
	return release, true
}

// Enter restores this session's declared command environment and returns its inherited lease.
// Provisioning uses launch=false: finishing installation must not make an unstarted command
// eligible for cleanup in the bootstrap-to-launch interval.
func Enter(dir string, launch bool) (*os.File, error) {
	f, err := os.OpenFile(filepath.Join(dir, packload.EmbeddedLeaseName), os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*os.File, error) { _ = f.Close(); return nil, err }
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_SH); err != nil {
		return fail(err)
	}
	held, herr := f.Stat()
	at, aerr := os.Lstat(filepath.Join(dir, packload.EmbeddedLeaseName))
	if herr != nil || aerr != nil || !os.SameFile(held, at) {
		return fail(fmt.Errorf("native session lease moved; retry yolo"))
	}
	b, err := os.ReadFile(filepath.Join(dir, metadataName))
	if err != nil {
		return fail(err)
	}
	var vars map[string]string
	if err = json.Unmarshal(b, &vars); err != nil {
		return fail(err)
	}
	if vars["YOLO_SESSION_ID"] != filepath.Base(dir) || !ValidID(vars["YOLO_SESSION_ID"]) {
		return fail(fmt.Errorf("native session identity mismatch; retry yolo"))
	}
	for k, v := range vars {
		if err = os.Setenv(k, v); err != nil {
			return fail(err)
		}
	}
	if launch {
		if err = os.WriteFile(filepath.Join(dir, launchedName), nil, 0600); err != nil {
			return fail(err)
		}
	}
	if _, err = unix.FcntlInt(f.Fd(), unix.F_SETFD, 0); err != nil {
		return fail(err)
	}
	return f, nil
}

// Retire removes only yolo's bookkeeping after all declared homes have been captured
// and removed. Unknown siblings survive: never recursively delete the session namespace.
func Retire(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, ent := range entries {
		if ent.Name() != metadataName && ent.Name() != launchedName && ent.Name() != packload.EmbeddedLeaseName {
			return fmt.Errorf("unshared entry %s", filepath.Join(dir, ent.Name()))
		}
	}
	for _, name := range []string{metadataName, launchedName, packload.EmbeddedLeaseName} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := os.Remove(dir); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
