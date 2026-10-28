package prune

// hostsessionhomes.go reaps `yolo host`'s per-launch SESSION HOMES (docs/design/per-session-
// profiles.md §4.7, OQ-SP4; "session home" is a term coined there: a per-session copy of the one
// directory a pack's program reads its config from, its rendered files the session's own and
// everything else linked to the shared copy).
//
// A launch makes one directory per pack that declares a session home, under
// <state dir>/host-session-homes/, holding a `.lease` file it locks SHARED and keeps locked across
// its exec, so the program it became holds the lock for its whole life (internal/cli's
// hostsessionhome.go). A launch that stays resident removes its own directory when the program
// exits; an exec'd one cannot, so its directory is reaped here once the kernel has dropped the
// lock: by the next `yolo host` launch and by `yolo prune`. The rule is the embedded fallback
// tree's (embeddedtrees.go), by the same lease, because the population has the same shape: a
// process of unbounded life, and an exit no code runs on.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/packload"
)

// HostSessionHomesLeaf is the state-dir child holding the per-launch session homes. In the state
// dir and never under ~/.cache, which every jail mounts read-write: a jail must not be able to
// plant a file a host program then reads as its config.
const HostSessionHomesLeaf = "host-session-homes"

// HostSessionHomesKeptLeaf is the state-dir child an ended session home is MOVED to when the
// sweep may not simply delete it: it held entries yolo neither rendered nor linked (a STRAY,
// docs/design/session-home-contents-and-reaping.md §3 — a directory the program wrote into its
// own config dir, or a rendered file it renamed), or its capture could not be trusted. Its
// config edits are captured first where that is safe, then the whole directory is kept here and
// one line names it. Outside HostSessionHomesLeaf on purpose: it must not be judged by the
// sweep again, and its bytes are the user's to inspect and remove.
const HostSessionHomesKeptLeaf = "host-session-homes-kept"

// hostSessionReapFloor: no session home younger than this is judged, whatever its lease says. Its
// launch locks the lease a moment after making the directory, so a minute is ample, and a launch
// waits on nothing this long before the exec. hostSessionUnleasedFloor: one with no lease file at
// all is an interrupted creation, reaped only past this.
const (
	hostSessionReapFloor     = time.Minute
	hostSessionUnleasedFloor = time.Hour
)

// ReapHostSessionHomes sweeps root (<state dir>/HostSessionHomesLeaf): every real directory this
// user owns, at least hostSessionReapFloor old, whose lease no process holds; or past
// hostSessionUnleasedFloor with no lease file. A held lease, one that cannot be asked, another
// user's directory and anything not a directory are kept. An absent root is no work. When apply
// is true, capture runs after the lease is proven free and before removal; an error retains the
// source and is recorded on that entry.
// A capture that reports a directory it moved aside instead (one it cannot account for) puts it
// in Aside, not Removed.
func ReapHostSessionHomes(root string, apply bool, now time.Time, capture func(string) (keptAt, why string, err error)) EmbeddedReap {
	var r EmbeddedReap
	if root == "" {
		return r
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			r.Declined = fmt.Sprintf("could not list %s: %v", root, err)
		}
		return r
	}
	for _, de := range entries {
		p := filepath.Join(root, de.Name())
		e := EmbeddedEntry{Name: de.Name(), Path: p}
		fi, ok := ownRealDir(p)
		if !ok {
			r.keep(e, whyUnrecognized, "not a directory this user owns")
			continue
		}
		age := now.Sub(fi.ModTime())
		if age < hostSessionReapFloor {
			r.keep(e, whyYoung, "younger than "+hostSessionReapFloor.String())
			continue
		}
		state, unlock, perr := packload.ProbeLease(p)
		switch state {
		case packload.LeaseHeld:
			unlock()
			r.keep(e, "its program is still running", "")
			continue
		case packload.LeaseUnknown:
			unlock()
			r.keep(e, whyCouldNotAsk, errText(perr))
			continue
		case packload.LeaseAbsent:
			if age < hostSessionUnleasedFloor {
				r.keep(e, whyYoung, "no lease, younger than "+hostSessionUnleasedFloor.String())
				continue
			}
			e.Why = "an interrupted launch's, with no lease"
		case packload.LeaseFree:
			e.Why = "its launch has ended"
		}
		e.Bytes = dirSizeBytes(p)
		var keptAt string
		if apply {
			if capture == nil {
				e.Err = errors.New("host session-home capture is unavailable; source retained")
			} else {
				var why string
				keptAt, why, e.Err = capture(p)
				if e.Err == nil && keptAt != "" {
					e.Why, e.Aside = why, keptAt
				}
			}
			if e.Err != nil {
				unlock()
				r.Removed = append(r.Removed, e)
				continue
			}
			if keptAt == "" {
				e.Err = os.RemoveAll(p)
			}
		}
		unlock()
		if keptAt != "" {
			// Moved, not reclaimed: those bytes are still on this disk.
			r.Aside = append(r.Aside, e)
			continue
		}
		if e.Err == nil {
			r.Bytes += e.Bytes
		}
		r.Removed = append(r.Removed, e)
	}
	return r
}
