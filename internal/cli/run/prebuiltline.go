package run

// prebuiltline.go is the DISCLOSURE of a pack-tree program delivered from its PREBUILT artifact
// (docs/design/pack-buildable-programs.md §4, §7, BG-D10): one line per program, on every fresh
// launch and every attach while it holds, naming the program, its pack, why the artifact stands in
// for a build, the sha256 that was verified before anything ran, and the URL. A disclosure, so it
// has no quiet switch (OQ-RO3), and it is said on a store hit too: which bytes run is the point,
// not whether they were fetched this time.

import (
	"sort"

	"github.com/mschulkind-oss/yolo-jail/internal/richtext"
)

// PrebuiltLine is a prebuilt delivery's disclosure, in the design's §4 form: "<bin>: prebuilt
// artifact from pack <pack> (<why>) — sha256 <first 12>… verified, <url>".
func PrebuiltLine(bin, pack, why, sha256, url string) string {
	short := sha256
	if len(short) > 12 {
		short = short[:12] + "…"
	}
	return bin + ": prebuilt artifact from pack " + pack + " (" + why + ") — sha256 " + short + " verified, " + url
}

// notePrebuiltDeliveries says the line for each pack-tree program this fresh launch hands its jail
// from a prebuilt artifact: one the slot answered with a store key. One it could not deliver is
// said by the slot's own warning and refused by the readiness act, so no "verified" is printed for
// it.
func (o *Options) notePrebuiltDeliveries() {
	var lines []string
	for _, p := range o.forkPinned {
		f := p.Fork
		if !f.IsPrebuilt() || o.forkDelivered[f.Bin].Key == "" {
			continue
		}
		lines = append(lines, PrebuiltLine(f.Bin, f.Pack, f.PrebuiltWhy, f.Prebuilt.SHA256, f.Prebuilt.URL))
	}
	sort.Strings(lines)
	for _, l := range lines {
		o.pr(o.Stderr).printf("%s", richtext.Escape(l))
	}
}

// handedPrebuilt fills h's prebuilt fields for bin when this launch delivers bin from a prebuilt
// artifact, so an attach to its jail says the same line (noteAttachForkBuilds).
func (o *Options) handedPrebuilt(bin string, h HandedFork) HandedFork {
	for _, p := range o.forkPinned {
		if f := p.Fork; f.Bin == bin && f.IsPrebuilt() {
			h.PrebuiltPack, h.PrebuiltWhy, h.PrebuiltSHA256, h.PrebuiltURL = f.Pack, f.PrebuiltWhy, f.Prebuilt.SHA256, f.Prebuilt.URL
		}
	}
	return h
}
