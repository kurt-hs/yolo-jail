package loopholedecl_test

import (
	"path"
	"sort"
	"strings"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/loopholedecl"
)

// officialBinaryRefusal is the census's verdict on one official manifest: "" when it declares
// no `binaries`, and otherwise the refusal naming each binary and the next step.
//
// AN OFFICIAL PACK DECLARES NO BINARY (docs/reference/pack-binaries.md OQ-BP8, ruled
// 2026-10-18): yolo's own build no longer builds, pins, publishes or seeds a pack program, so
// an official `binaries` entry would name a file nothing produces, and `yolo pack install`
// would fail on it for every user of the pack. Every official loophole program is a
// subcommand of a binary yolo already ships, and a third-party pack's `binaries` — a download
// URL with its sha256 — is untouched by this.
func officialBinaryRefusal(manifestPath string, m *loopholedecl.Manifest) string {
	if len(m.Binaries) == 0 {
		return ""
	}
	var names []string
	for _, b := range m.Binaries {
		names = append(names, b.Name)
	}
	sort.Strings(names)
	return manifestPath + " declares `binaries` (" + strings.Join(names, ", ") + "), and an " +
		"official pack ships its programs as yolo subcommands, never as a download: yolo's " +
		"build does not produce or pin pack binaries (docs/reference/pack-binaries.md OQ-BP8). " +
		"Make the program a `yolo internal daemon <name>` or `yolo-jaild <name>` subcommand and " +
		"name that in host_daemon.cmd or jail_daemon.cmd; if there is a real reason to ship it " +
		"as a separate binary, ask the maintainer to revisit OQ-BP8 first"
}

// TestNoOfficialPackDeclaresABinary is that census, over every loophole manifest the packs embed
// carries: shippedManifestHome, which TestShippedManifestHomeIsTotal holds total over the embed,
// so a new official loophole cannot be skipped by it.
func TestNoOfficialPackDeclaresABinary(t *testing.T) {
	if len(shippedManifestHome) == 0 {
		t.Fatal("shippedManifestHome is empty, so this census checks nothing")
	}
	for name, pack := range shippedManifestHome {
		dir := path.Join("packs", pack, "loopholes", name)
		m, err := loopholedecl.Decode(shippedManifest(t, name), dir)
		if err != nil {
			t.Errorf("%s: %v", dir, err)
			continue
		}
		if refusal := officialBinaryRefusal(path.Join(dir, loopholedecl.ManifestName), m); refusal != "" {
			t.Error(refusal)
		}
	}
}

// TestTheOfficialBinaryRefusalNamesTheNextStep feeds the census a manifest that does declare a
// binary — the embed carries none, so without this the refusal would never run — and pins the
// words that tell its author what to do instead.
func TestTheOfficialBinaryRefusalNamesTheNextStep(t *testing.T) {
	m, err := decodeMap(t, "tool", toolManifest())
	if err != nil {
		t.Fatal(err)
	}
	got := officialBinaryRefusal("packs/tool/loopholes/tool/manifest.jsonc", m)
	for _, want := range []string{
		"packs/tool/loopholes/tool/manifest.jsonc", "(toold)", "yolo subcommands",
		"yolo internal daemon <name>", "yolo-jaild <name>", "OQ-BP8",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the refusal %q does not say %q", got, want)
		}
	}
	plain, err := decodeMap(t, "tool", toolManifestNoBinaries())
	if err != nil {
		t.Fatal(err)
	}
	if got := officialBinaryRefusal("packs/tool/loopholes/tool/manifest.jsonc", plain); got != "" {
		t.Errorf("a manifest with no `binaries` was refused: %s", got)
	}
}

// toolManifestNoBinaries is toolManifest's loophole with its program as a subcommand instead.
func toolManifestNoBinaries() map[string]any {
	return map[string]any{
		"name": "tool",
		"host_daemon": map[string]any{"cmd": []any{"yolo", "internal", "daemon", "tool", "--socket", "{socket}"},
			"publishes": "socket"},
	}
}
