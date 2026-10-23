package hostfloor

// override.go is the floor's arm for a DEV OVERRIDE (docs/design/pack-buildable-programs.md §5, a term
// the design coins: `programs.overrides.<bin>` in the user's config runs one program from a host
// checkout the user is developing). At `yolo host` the program runs from the host path itself (§5's
// delivery table), so the floor entry is a launcher in bin/ that execs `<dir>/<run>`, and nothing
// else: no install, no build and no copy (OQ-BG3's leaning), and never the pack's own copy in its
// place (§10, P5).
//
// The caller resolves each override (Floor.Overrides): the path with its symlinks resolved, the
// credential-boundary check, and `run` defaulted to the program's own. This file decides the rest from
// what is on disk at each status, so an edit in the checkout serves at the next invocation, and a run
// file that went missing refuses the program rather than running something else.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ViaOverride is Record.Via for a dev override's entry: not a manifest word, since no pack delivers
// it, and never equal to one, so a record of it is Pending the moment the override goes.
const ViaOverride = "override"

// Override is one resolved dev override of a program this floor serves.
type Override struct {
	// Host is the path as the user's config writes it, for a refusal to quote back.
	Host string
	// Dir is that path with its symlinks resolved: the directory the program runs from.
	Dir string
	// Run is the file inside Dir that runs, slash-separated: the override's own `run`, else the
	// program's.
	Run string
	// Build is the pack's build line for the program, "" for none: what a refusal of a missing run
	// file tells the user to run in the checkout.
	Build string
	// Refused is why the override cannot apply (a path that does not resolve to a directory, one at
	// the credential boundary, no file to run), naming the key; the program then has no floor entry,
	// and never falls back to its pack's copy.
	Refused string
}

// File is the program file the override runs.
func (o Override) File() string { return filepath.Join(o.Dir, filepath.FromSlash(o.Run)) }

// OverrideKey is the config key and file every override line names.
func OverrideKey(bin string) string {
	return "programs.overrides." + bin + " in ~/.config/yolo-jail/config.jsonc"
}

// override is p's dev override on this floor, if the user set one.
func (f *Floor) override(p Program) (Override, bool) {
	o, ok := f.Overrides[p.Bin()]
	return o, ok
}

// HasOverride reports whether a dev override serves p on this floor, refused or not: its floor entry
// is the checkout's, and a refusal is the override's own, never the pack's.
func (f *Floor) HasOverride(p Program) bool {
	_, ok := f.override(p)
	return ok
}

// overrideNoEntry is why p's override cannot run here now, "" when it can: the caller's refusal, or
// the run file missing or not executable, with the next step (§4's two override rows).
func overrideNoEntry(bin string, o Override) string {
	if o.Refused != "" {
		return "its dev override is refused: " + o.Refused
	}
	file := o.File()
	fi, err := os.Stat(file)
	switch {
	case err != nil || fi.IsDir():
		step := ""
		if o.Build != "" {
			step = "build it in that checkout (its pack's build line: " + o.Build + "), or "
		}
		return "its dev override runs " + file + ", which is not there — " + step + "remove " + OverrideKey(bin) +
			" to run its pack's own copy"
	case fi.Mode().Perm()&0o111 == 0:
		return "its dev override runs " + file + ", which is not executable — chmod +x it in that checkout, or " +
			"remove " + OverrideKey(bin) + " to run its pack's own copy"
	}
	return ""
}

// overrideStatus is Status for an overridden program: no entry when the override cannot run,
// provisioned when bin/<bin> already execs its file, missing otherwise.
func (f *Floor) overrideStatus(p Program, o Override) Status {
	st := Status{Program: p, Launcher: f.Launcher(p.Bin())}
	if why := overrideNoEntry(p.Bin(), o); why != "" {
		st.Disposition, st.Reason = NoEntry, why
		return st
	}
	rec, err := f.readRecord(p.Bin())
	switch {
	case err == nil && rec != nil && rec.Via == ViaOverride && rec.Entry == o.File() && usable(rec, st.Launcher):
		st.Disposition, st.Record = Provisioned, rec
	case err != nil && !os.IsNotExist(err):
		st.Disposition, st.Reason = Missing, "its record is unreadable ("+err.Error()+")"
		st.Newer = isNewer(err)
	default:
		st.Disposition, st.Reason = Missing, "its dev override is not in yolo's floor yet"
	}
	return st
}

// ensureOverride is Ensure for an overridden program: bin/<bin> pointed at the override's file, under
// the program's lock, and nothing installed.
func (f *Floor) ensureOverride(p Program) (Status, Outcome, error) {
	o, _ := f.override(p)
	st := f.overrideStatus(p, o)
	switch {
	case st.Disposition == NoEntry:
		return st, "", fmt.Errorf("%w for %s: %s", ErrNoEntry, p.Bin(), st.Reason)
	case st.Newer:
		return st, "", newerRecordError{st.Reason}
	case st.Disposition == Provisioned:
		return st, Current, nil
	}
	if err := f.ensureDir("bin", "programs", "records", "locks"); err != nil {
		return st, "", err
	}
	lk, err := acquire(f.lockPath(p.Bin()), true, func(pid int) {
		f.say("waiting for pid %d, which is installing %s into yolo's floor", pid, p.Bin())
	})
	if err != nil {
		return st, "", err
	}
	defer lk.release()
	if st = f.overrideStatus(p, o); st.Disposition == Provisioned || st.Newer {
		if st.Newer {
			return st, "", newerRecordError{st.Reason}
		}
		return st, Current, nil
	}
	file := o.File()
	rec := &Record{Schema: recordSchema, Bin: p.Bin(), Pack: p.Pack, Via: ViaOverride, Declared: o.Dir,
		Version: "dev override", Entry: file, Exec: []string{file}, Installed: f.now().UTC()}
	rec.Checked = rec.Installed
	// THE SWITCH, launcher then record, as an install's. The pack's own install directories stay
	// where they are: removing the override reinstalls (its record's Via no longer matches), and a
	// copy still current then serves again.
	if err := writeAtomic(f.Launcher(p.Bin()), []byte(launcherScript(rec)), 0o700); err != nil {
		return st, "", err
	}
	if err := f.writeRecord(rec); err != nil {
		return st, "", err
	}
	f.appendReceipt(receipt{Act: "override", Bin: p.Bin(), Pack: p.Pack, Via: ViaOverride, Declared: o.Dir,
		Version: rec.Version})
	return f.overrideStatus(p, o), Installed, nil
}

// isNewer reports whether err is a newer yolo's record.
func isNewer(err error) bool {
	return errors.Is(err, ErrNewerRecord)
}
