package entrypoint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// requires_test.go covers the jail half of a `program` need (no `via`, the `requires` kind
// until OQ-PS11): it generates NOTHING, and an absent needed binary is reported BY NAME rather
// than discovered later as a tool that mysteriously does nothing.

// writeNeedPack stages a one-pack tree declaring a need for each named bin, and
// returns the pack root to point YOLO_PACK_ROOT at.
func writeNeedPack(t *testing.T, hints string, bins ...string) string {
	t.Helper()
	packRoot := t.TempDir()
	packDir := filepath.Join(packRoot, "needy")
	if err := os.MkdirAll(packDir, 0o755); err != nil {
		t.Fatal(err)
	}
	var entries []string
	for _, b := range bins {
		e := `{"kind":"program","bin":"` + b + `"`
		if hints != "" {
			e += `,"install_hints":` + hints
		}
		entries = append(entries, e+"}")
	}
	manifest := `{"name":"needy","contributes":[` + strings.Join(entries, ",") + `]}`
	if err := os.WriteFile(filepath.Join(packDir, "pack.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return packRoot
}

// TestANeedGeneratesNoLauncher is the structural half of the kind, and the reason it is
// not just "program without the install": a need puts nothing on PATH, so it cannot
// shadow the very binary it is asserting. Defect 11.1 is not merely ordered away for this
// kind — there is no file to order.
func TestANeedGeneratesNoLauncher(t *testing.T) {
	home := t.TempDir()
	packRoot := writeNeedPack(t, "", "fzf", "fd")

	e := NewEnv(map[string]string{"JAIL_HOME": home, "YOLO_PACK_ROOT": packRoot})
	if err := GenerateAgentLaunchers(e); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(e.LaunchDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		var names []string
		for _, ent := range entries {
			names = append(names, ent.Name())
		}
		t.Errorf("a need generated %v — it must generate NOTHING; a "+
			"launcher for a binary yolo does not install is exactly the shadowing hazard "+
			"the kind exists to avoid", names)
	}
}

// TestAssertRequiredBinsNamesTheMissingBin: the whole reason the kind beats staying silent
// (the fzf example pack's old workaround) is that an absent dependency is REPORTED. The
// message must name the bin, the pack, and — when the pack carries them — that host hints
// exist.
func TestAssertRequiredBinsNamesTheMissingBin(t *testing.T) {
	home := t.TempDir()
	packRoot := writeNeedPack(t, `{"brew":"fzf","apt":"fzf"}`, "definitely-not-a-real-binary")

	var warnings strings.Builder
	e := NewEnv(map[string]string{"JAIL_HOME": home, "YOLO_PACK_ROOT": packRoot})
	e.Stderr = &warnings
	AssertRequiredBins(e)

	got := warnings.String()
	if !strings.Contains(got, "definitely-not-a-real-binary") {
		t.Errorf("the warning must NAME the missing bin:\n%s", got)
	}
	if !strings.Contains(got, "needy") {
		t.Errorf("the warning must name the pack that requires it:\n%s", got)
	}
	if !strings.Contains(got, "apt, brew") {
		t.Errorf("the warning should list the pack's host hints, sorted:\n%s", got)
	}
}

// A required binary that IS on PATH produces no output at all. A check that warns either way
// trains people to ignore it.
func TestAssertRequiredBinsSilentWhenPresent(t *testing.T) {
	home := t.TempDir()
	packRoot := writeNeedPack(t, "", "presenttool")

	// Put the binary somewhere BootPath actually looks. $HOME/.local/bin is on it, and using
	// a real PATH entry rather than stubbing the probe is what makes this test cover the
	// "which PATH do we ask about" question — the answer must be the AGENT's PATH (BootPath),
	// not the entrypoint process's, which at this point in the boot is still the container
	// default.
	binDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "presenttool"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	var warnings strings.Builder
	e := NewEnv(map[string]string{"JAIL_HOME": home, "YOLO_PACK_ROOT": packRoot})
	e.Stderr = &warnings
	AssertRequiredBins(e)

	if got := warnings.String(); got != "" {
		t.Errorf("a present required binary must produce no output, got:\n%s", got)
	}
}

// A non-executable file of the right name does not satisfy a requirement — the assertion is
// "this command runs", not "this path exists".
func TestAssertRequiredBinsIgnoresANonExecutable(t *testing.T) {
	home := t.TempDir()
	packRoot := writeNeedPack(t, "", "notexec")

	binDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "notexec"), []byte("data\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var warnings strings.Builder
	e := NewEnv(map[string]string{"JAIL_HOME": home, "YOLO_PACK_ROOT": packRoot})
	e.Stderr = &warnings
	AssertRequiredBins(e)

	if !strings.Contains(warnings.String(), "notexec") {
		t.Errorf("a non-executable file must not satisfy a requirement:\n%s", warnings.String())
	}
}

// On macos-user the PATH the agent gets is NOT BootPath: `packages:` materializes into
// the native nix store, which BootPath has no entry for. Searching BootPath there
// reported every declared tool missing — measured 2026-09-03 on a real launch, where
// `fzf` was in `packages:`, was built, and warned as absent. Homebrew tools miss for the
// same reason (/opt/homebrew/bin is not on BootPath either).
//
// $YOLO_DARWIN_LOGIN_PATH is the sandbox's real PATH and is set only by the macos-user
// launcher, so this takes effect there and nowhere else. The binary here lives ONLY on
// that path — never on BootPath — so a warning proves the probe consulted the wrong one.
func TestAssertRequiredBinsSearchesTheDarwinLoginPath(t *testing.T) {
	home := t.TempDir()
	packRoot := writeNeedPack(t, "", "fzf-probe")

	storeBin := t.TempDir()
	if err := os.WriteFile(filepath.Join(storeBin, "fzf-probe"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	var warnings strings.Builder
	e := NewEnv(map[string]string{
		"JAIL_HOME":              home,
		"YOLO_PACK_ROOT":         packRoot,
		"YOLO_DARWIN_LOGIN_PATH": storeBin,
	})
	e.Stderr = &warnings
	AssertRequiredBins(e)

	if strings.Contains(warnings.String(), "fzf-probe") {
		t.Errorf("warned about a tool that IS on the sandbox PATH:\n%s", warnings.String())
	}
}

// The converse, or the override above would mask a genuinely missing binary: a tool on
// neither path must still be reported by name.
func TestAssertRequiredBinsStillWarnsWhenAbsentFromBothPaths(t *testing.T) {
	home := t.TempDir()
	packRoot := writeNeedPack(t, "", "definitely-not-installed")

	var warnings strings.Builder
	e := NewEnv(map[string]string{
		"JAIL_HOME":              home,
		"YOLO_PACK_ROOT":         packRoot,
		"YOLO_DARWIN_LOGIN_PATH": t.TempDir(),
	})
	e.Stderr = &warnings
	AssertRequiredBins(e)

	if !strings.Contains(warnings.String(), "definitely-not-installed") {
		t.Errorf("an absent required binary was not reported:\n%s", warnings.String())
	}
}

// writeNeedAndProvider stages needy (a need for bin) beside provb (a recipe for bin with the
// given `platforms` JSON, or none when ""), and returns the pack root.
func writeNeedAndProvider(t *testing.T, bin, platforms string) string {
	t.Helper()
	packRoot := writeNeedPack(t, "", bin)
	dir := filepath.Join(packRoot, "provb")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	recipe := `{"kind":"program","bin":"` + bin + `","via":"npm","package":"` + bin + `-pkg"`
	if platforms != "" {
		recipe += `,"platforms":` + platforms
	}
	manifest := `{"name":"provb","contributes":[` + recipe + `}]}`
	if err := os.WriteFile(filepath.Join(dir, "pack.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return packRoot
}

// TestAssertRequiredBinsSkipsANeedAnotherPackProvides pins the boot's call into
// packload.ProvidedNeeds (OQ-PS5): a need another selected pack's recipe provides is not
// probed — that pack's launcher installs it on first use — so it is not warned about even
// while it is absent, and the boot log names the provider. Delete the skip and the absent
// binary is warned about.
func TestAssertRequiredBinsSkipsANeedAnotherPackProvides(t *testing.T) {
	home := t.TempDir()
	packRoot := writeNeedAndProvider(t, "definitely-not-a-real-binary", "")

	var warnings, log strings.Builder
	e := NewEnv(map[string]string{"JAIL_HOME": home, "YOLO_PACK_ROOT": packRoot})
	e.Stderr = &warnings
	e.LogOnly = &log
	AssertRequiredBins(e)

	if got := warnings.String(); got != "" {
		t.Errorf("a provided need must not be warned about:\n%s", got)
	}
	want := "program definitely-not-a-real-binary, needed by pack needy, is provided by pack provb"
	if !strings.Contains(log.String(), want) {
		t.Errorf("the boot log must name the provider (%q):\n%s", want, log.String())
	}
}

// TestAssertRequiredBinsWarnsWhenTheProviderIsNotPublishedHere: a provider whose only recipe is
// for another platform gets no launcher on this machine, so it meets no need here and the
// absent binary is warned about as an unprovided one.
func TestAssertRequiredBinsWarnsWhenTheProviderIsNotPublishedHere(t *testing.T) {
	home := t.TempDir()
	packRoot := writeNeedAndProvider(t, "definitely-not-a-real-binary", `["plan9/mips"]`)

	var warnings strings.Builder
	e := NewEnv(map[string]string{"JAIL_HOME": home, "YOLO_PACK_ROOT": packRoot})
	e.Stderr = &warnings
	AssertRequiredBins(e)

	got := warnings.String()
	if !strings.Contains(got, "needs program definitely-not-a-real-binary") ||
		!strings.Contains(got, "no selected pack provides it") {
		t.Errorf("a provider not published here must not meet the need:\n%s", got)
	}
}
