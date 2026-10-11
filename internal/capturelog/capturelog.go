// Package capturelog is the CAPTURE LOG: one timestamped line for every change any writer makes
// to a surface's capture overlay (the sidecar holding a surface's captured in-jail edits, which
// docs/reference/config-migration-to-prism.md §5 defines), kept beside the overlays as
// capture.log, bounded.
//
// WHY IT EXISTS. The overlay is durable state that changed silently. On 2026-10-11 a boot said
// "~/.claude/settings.json: 4 keys from captured in-jail edits", and by the next boot two of the
// four were gone with nothing recording what they had been — the very two that, by every other
// sign, had switched Claude Code onto Bedrock while yolo had selected its subscription. The
// standing rule is that a log under yolo's control records what the next investigation will
// need, so every writer of an overlay — the boot render, the capture at shutdown or by
// `yolo config capture`, `yolo config reset` — appends here what it added, changed or took out.
//
// WHAT A LINE CARRIES: the time, the surface ("claude/settings"), the writer, and one leaf
// (dotted, "env.CLAUDE_CODE_USE_BEDROCK") with its old and new values. A value is printed only
// when it is not credential-shaped (Value): a key whose name looks like a credential, or a
// string that looks like a token, is written as its length alone, so the log never holds a
// secret the overlay does.
//
// The package is PURE apart from AppendFile: the host CLI writes into a workspace directory the
// jail can write, and must do so through its own link-refusing file handles (internal/cli's
// captureFile), so it takes Lines and Trim and does its own I/O.
package capturelog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// FileName is the log's name, in the directory holding the overlays.
const FileName = "capture.log"

// MaxBytes bounds the log: past it, Trim keeps the newest lines that fit in half of it, so a
// long-lived workspace keeps weeks of changes and never grows without end.
const MaxBytes = 256 << 10

// Reason annotates one removed leaf: why the writer took it out, keyed by the dotted leaf.
type Reason map[string]string

// Lines are the log lines for the change from the overlay before to the overlay after (both the
// sidecar's JSON; nil or empty is "no overlay"), one per changed leaf, sorted by leaf. reasons
// names why a removed leaf went; removedWhy is said of every other removal. No change, no lines.
func Lines(now time.Time, surface, writer string, before, after []byte, reasons Reason,
	removedWhy string) []string {
	old, cur := flatten(decode(before)), flatten(decode(after))
	keys := map[string]bool{}
	for k := range old {
		keys[k] = true
	}
	for k := range cur {
		keys[k] = true
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	stamp := now.Format(time.RFC3339)
	var out []string
	for _, k := range sorted {
		ov, had := old[k]
		nv, has := cur[k]
		var what string
		switch {
		case !had && has && nv == nil:
			what = fmt.Sprintf("+ %s (captured deletion)", k)
		case !had && has:
			what = fmt.Sprintf("+ %s = %s (captured)", k, Value(k, nv))
		case had && !has:
			why := reasons[k]
			if why == "" {
				why = removedWhy
			}
			what = fmt.Sprintf("- %s (was %s): %s", k, Value(k, ov), why)
		case same(ov, nv):
			continue
		default:
			what = fmt.Sprintf("~ %s %s -> %s (captured)", k, Value(k, ov), Value(k, nv))
		}
		out = append(out, stamp+" "+surface+" "+writer+": "+what)
	}
	return out
}

// Trim is existing with lines appended, bounded by MaxBytes: when the result would pass it, the
// oldest lines go until it fits in half, and a marker line says so.
func Trim(existing []byte, lines []string) []byte {
	var b bytes.Buffer
	b.Write(existing)
	if b.Len() > 0 && !bytes.HasSuffix(existing, []byte("\n")) {
		b.WriteByte('\n')
	}
	for _, l := range lines {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	data := b.Bytes()
	if len(data) <= MaxBytes {
		return data
	}
	keep := data[len(data)-MaxBytes/2:]
	if i := bytes.IndexByte(keep, '\n'); i >= 0 {
		keep = keep[i+1:]
	}
	return append([]byte("# older lines trimmed: this log keeps about "+
		fmt.Sprint(MaxBytes/2/1024)+" KiB\n"), keep...)
}

// AppendFile appends lines to the log in dir at mode, for a writer that may use plain paths
// (the jail's own boot, and the host notch's store in the user's real home). Nothing to append
// writes nothing.
func AppendFile(dir string, lines []string, mode os.FileMode) error {
	if len(lines) == 0 || dir == "" {
		return nil
	}
	path := filepath.Join(dir, FileName)
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, Trim(existing, lines), mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// credentialName is a key whose name says it holds a credential.
var credentialName = regexp.MustCompile(`(?i)(token|secret|passw|credential|api_?key|access_?key|` +
	`private|cookie|bearer|session|auth)`)

// modelName is a key whose value is a model id, never a credential.
var modelName = regexp.MustCompile(`(?i)model`)

// tokenShaped is a string that looks like a credential: long, unbroken, of token characters, and
// mixing letters with digits.
var tokenShaped = regexp.MustCompile(`^[A-Za-z0-9+/=_.:-]{32,}$`)

// Value is how the log prints v at the dotted leaf key: as JSON, unless the key's last segment
// names a credential or the value looks like one, when only its size is printed. A long value is
// cut, so one leaf is one readable line.
func Value(key string, v any) string {
	last := key
	if i := strings.LastIndexByte(key, '.'); i >= 0 {
		last = key[i+1:]
	}
	if v != nil && credentialName.MatchString(last) {
		return redacted(v)
	}
	// A model id is long, dotted and mixes letters with digits — token-shaped by the rule below
	// — and is exactly what an investigation needs to read, so a key naming a model is exempt.
	if secretIn(v) && !modelName.MatchString(last) {
		return redacted(v)
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	s := string(raw)
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

func redacted(v any) string {
	if s, ok := v.(string); ok {
		return fmt.Sprintf("<redacted, %d characters>", len(s))
	}
	return "<redacted>"
}

// secretIn reports whether v is, or (for an array) holds, a token-shaped string.
func secretIn(v any) bool {
	switch t := v.(type) {
	case string:
		return tokenShaped.MatchString(t) && strings.ContainsAny(t, "0123456789") &&
			strings.IndexFunc(t, func(r rune) bool { return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') }) >= 0
	case []any:
		for _, e := range t {
			if secretIn(e) {
				return true
			}
		}
	}
	return false
}

func decode(data []byte) any {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	var v any
	if json.Unmarshal(data, &v) != nil {
		return nil
	}
	return v
}

// flatten is v's leaves by dotted key: objects recurse, and everything else — an array, a
// scalar, a null tombstone, an empty object — is a leaf. A keyless surface's overlay (a whole
// file's text or lines) is the one leaf "(file)".
func flatten(v any) map[string]any {
	out := map[string]any{}
	m, ok := v.(map[string]any)
	if !ok {
		if v != nil {
			out["(file)"] = v
		}
		return out
	}
	var walk func(prefix string, m map[string]any)
	walk = func(prefix string, m map[string]any) {
		for k, sub := range m {
			key := k
			if prefix != "" {
				key = prefix + "." + k
			}
			if sm, isObj := sub.(map[string]any); isObj && len(sm) > 0 {
				walk(key, sm)
				continue
			}
			out[key] = sub
		}
	}
	walk("", m)
	return out
}

func same(a, b any) bool {
	ra, _ := json.Marshal(a)
	rb, _ := json.Marshal(b)
	return bytes.Equal(ra, rb)
}

// Held reports whether the overlay JSON holds a value (a tombstone included) at the dotted key.
func Held(overlay []byte, key string) bool {
	_, ok := flatten(decode(overlay))[key]
	return ok
}

// Dropped is the line for a value a writer took in and dropped in the same write, so the
// overlay before and after both lack it and Lines alone would say nothing: the boot's capture
// of a provider key, which the render then refuses (agentcfg/providerkeys.go).
func Dropped(now time.Time, surface, writer, key string, v any, why string) string {
	return fmt.Sprintf("%s %s %s: x %s = %s: %s", now.Format(time.RFC3339), surface, writer, key,
		Value(key, v), why)
}
