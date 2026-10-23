package run

// hostfonts.go is the container half of `share_host_fonts` (OQ-NX9, ruled 2026-10-23,
// docs/design/provisioner-sets.md): the user-scope opt-in that binds the user's own font
// folder READ-ONLY into a container jail, where the jail's fontconfig finds it. The macos-user
// half is internal/macosuser/fontsconf.go and fontcopy.go; `yolo host` runs as the user and
// needs nothing.
//
// WHERE: under the jail's ~/.local/share/fonts, which is fontconfig's `<dir prefix="xdg">fonts</dir>`
// with XDG_DATA_HOME unset. Both of the jail's fontconfig configs list it: the image's
// /etc/fonts/fonts.conf, and a store-delivered launch's, which includes the profile's own
// fonts.conf (entrypoint storepackages.go). fontconfig scans a listed directory recursively,
// so a child folder per source is found, and the jail's own fonts beside them are untouched.
//
// WHICH: on a Linux host, $XDG_DATA_HOME/fonts (else ~/.local/share/fonts) and ~/.fonts, the
// two folders a Linux user's fonts live in; on a macOS host (Podman Machine or Apple
// Container), ~/Library/Fonts. A folder that does not exist is skipped with a dim line, and
// every bound folder is named by a disclosure line on every launch, printed where the bind is
// emitted so no path makes one without the other.
//
// WHAT IS BOUND IS THE RESOLVED FOLDER, and only one that stays a font folder: a source whose
// real path is outside your home, is or holds your home or yolo's own directories
// (paths.WritableSourceScopeBreach), or is or holds a credential directory
// (hostFontCredentialDirs) is skipped with a line, so a link at ~/.fonts cannot hand the jail
// ~/.ssh read-only.

import (
	"path/filepath"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/shquote"
)

// hostFontsDir is the jail folder the binds sit under (fontconfig's xdg fonts dir).
const hostFontsDir = jailHome + "/.local/share/fonts"

// hostFontCredentialDirs are home-relative folders that hold credentials or tool config with
// tokens; a font source that is, holds or sits inside one is never bound.
var hostFontCredentialDirs = []string{
	".ssh", ".gnupg", ".aws", ".azure", ".config", ".docker", ".kube", ".netrc",
	".password-store", ".local/share/keyrings", "Library/Keychains",
	// The coding agents' own homes, which hold their login tokens.
	".claude", ".codex", ".copilot",
}

// hostFontBind is one font folder a launch binds read-only: source as the user named it, real
// the resolved folder the bind uses.
type hostFontBind struct{ source, real, dest string }

func (b hostFontBind) bindArg() string { return b.real + ":" + b.dest + ":ro" }

// hostFontSources lists the user's font folders on this host, each with the jail folder it is
// bound at. home is the host user's home.
func (o *Options) hostFontSources(home string) []hostFontBind {
	if o.IsMacOS {
		return []hostFontBind{{source: filepath.Join(home, "Library", "Fonts"), dest: hostFontsDir + "/yolo-host"}}
	}
	data := filepath.Join(home, ".local", "share")
	if x := o.Getenv("XDG_DATA_HOME"); filepath.IsAbs(x) {
		data = x
	}
	return []hostFontBind{
		{source: filepath.Join(data, "fonts"), dest: hostFontsDir + "/yolo-host"},
		{source: filepath.Join(home, ".fonts"), dest: hostFontsDir + "/yolo-host-dotfonts"},
	}
}

// hostFontSourceRefusal says why source, resolved to real, is not bound, or "".
func hostFontSourceRefusal(real, home string) string {
	rhome := home
	if r, err := filepath.EvalSymlinks(home); err == nil {
		rhome = r
	}
	if real == rhome || !strings.HasPrefix(real, rhome+string(filepath.Separator)) {
		return "it resolves to " + real + ", which is not a folder inside your home " + rhome
	}
	if b := paths.WritableSourceScopeBreach(real); b != nil {
		return b.WhatFor("it")
	}
	for _, c := range hostFontCredentialDirs {
		cred := filepath.Join(rhome, c)
		if real == cred || strings.HasPrefix(real, cred+string(filepath.Separator)) ||
			strings.HasPrefix(cred, real+string(filepath.Separator)) {
			return "it resolves to " + real + ", which is, holds or sits inside " + cred +
				", where credentials live"
		}
	}
	return ""
}

// hostFontBinds returns the binds a launch on rt makes for share_host_fonts, handing note one
// line per folder it binds or skips. Nothing when the user did not opt in.
func (o *Options) hostFontBinds(rt, home string, note func(string)) []hostFontBind {
	if !o.ShareHostFontsConfig() {
		return nil
	}
	userConfig := paths.UserConfigPath()
	removeHint := "remove share_host_fonts from " + userConfig
	var out []hostFontBind
	for _, b := range o.hostFontSources(home) {
		if !isDir(b.source) {
			note("[dim]share_host_fonts: " + b.source + " does not exist, so it is not shared. " +
				"Add fonts there, or " + removeHint + ".[/dim]")
			continue
		}
		real, err := filepath.EvalSymlinks(b.source)
		if err != nil {
			note("[yellow]share_host_fonts: not sharing " + b.source + ": it cannot be resolved (" +
				err.Error() + "). Fix the folder, or " + removeHint + ".[/yellow]")
			continue
		}
		if why := hostFontSourceRefusal(real, home); why != "" {
			note("[yellow]share_host_fonts: not sharing " + b.source + ": " + why + ". Keep your " +
				"fonts in a folder of their own there, or " + removeHint + ".[/yellow]")
			continue
		}
		b.real = real
		// Apple Container below its read-only floor would hand over a WRITABLE bind of the
		// user's fonts: refused, as every other read-only bind is (roBindsUnsupported).
		if why := o.roBindsUnsupported(rt); why != "" {
			note("[yellow]share_host_fonts: not sharing " + b.source + ": " + why + "[/yellow]")
			continue
		}
		// A folder the Podman Machine does not share would refuse the whole launch at the
		// share pre-flight; an opt-in for fonts is skipped instead, with the commands that
		// share it.
		if o.machineCannotSee(rt, b.real) {
			note("[yellow]share_host_fonts: not sharing " + b.source + ": the Podman Machine " +
				"does not share it.[/yellow] " + o.machineShareCommands(b.real) + " Or " +
				removeHint + ".")
			continue
		}
		out = append(out, b)
	}
	return out
}

// machineShareCommands are the exact commands that recreate the Podman Machine sharing folder,
// as the share refusal prints them (runtime.MachineShares.UnsharedRefusal).
func (o *Options) machineShareCommands(folder string) string {
	shares, _ := o.podmanMachineShares()
	stop := "podman machine stop && podman machine rm"
	if shares.Machine != "" && shares.Machine != "podman-machine-default" {
		name := shquote.Quote(shares.Machine)
		stop = "podman machine stop " + name + " && podman machine rm " + name
	}
	return "To share it, recreate the machine with the folder added (this deletes the machine's " +
		"images and containers; keep any --cpus, --memory and --disk-size options you use): " +
		stop + "; " + shares.MachineInitCommand([]string{folder}) + "; podman machine start."
}

// hostFontDisclosure is the line every launch with share_host_fonts prints per bound folder.
func hostFontDisclosure(b hostFontBind) string {
	return "Sharing your fonts read-only (share_host_fonts): " + b.source + " → " + b.dest
}
