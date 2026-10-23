package run

// hostfonts.go is the container half of `share_host_fonts` (OQ-NX9, ruled 2026-10-23,
// docs/design/provisioner-sets.md): the user-scope opt-in that binds the user's own font
// folder READ-ONLY into a container jail, where the jail's fontconfig finds it. The macos-user
// half is internal/macosuser/fontsconf.go; `yolo host` runs as the user and needs nothing.
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

import (
	"path/filepath"

	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// hostFontsDir is the jail folder the binds sit under (fontconfig's xdg fonts dir).
const hostFontsDir = jailHome + "/.local/share/fonts"

// hostFontBind is one font folder a launch binds read-only.
type hostFontBind struct{ source, dest string }

func (b hostFontBind) bindArg() string { return b.source + ":" + b.dest + ":ro" }

// hostFontSources lists the user's font folders on this host, each with the jail folder it is
// bound at. home is the host user's home.
func (o *Options) hostFontSources(home string) []hostFontBind {
	if o.IsMacOS {
		return []hostFontBind{{filepath.Join(home, "Library", "Fonts"), hostFontsDir + "/yolo-host"}}
	}
	data := filepath.Join(home, ".local", "share")
	if x := o.Getenv("XDG_DATA_HOME"); filepath.IsAbs(x) {
		data = x
	}
	return []hostFontBind{
		{filepath.Join(data, "fonts"), hostFontsDir + "/yolo-host"},
		{filepath.Join(home, ".fonts"), hostFontsDir + "/yolo-host-dotfonts"},
	}
}

// hostFontBinds returns the binds a launch on rt makes for share_host_fonts, handing note one
// line per folder it binds or skips. Nothing when the user did not opt in.
func (o *Options) hostFontBinds(rt, home string, note func(string)) []hostFontBind {
	if !o.ShareHostFontsConfig() {
		return nil
	}
	userConfig := paths.UserConfigPath()
	var out []hostFontBind
	for _, b := range o.hostFontSources(home) {
		if !isDir(b.source) {
			note("[dim]share_host_fonts: " + b.source + " does not exist, so it is not shared. " +
				"Add fonts there, or remove share_host_fonts from " + userConfig + ".[/dim]")
			continue
		}
		// Apple Container below its read-only floor would hand over a WRITABLE bind of the
		// user's fonts: refused, as every other read-only bind is (roBindsUnsupported).
		if why := o.roBindsUnsupported(rt); why != "" {
			note("[yellow]share_host_fonts: not sharing " + b.source + ": " + why + "[/yellow]")
			continue
		}
		// A folder the Podman Machine does not share would refuse the whole launch at the
		// share pre-flight; an opt-in for fonts is skipped instead, and says how to share it.
		if o.machineCannotSee(rt, b.source) {
			note("[yellow]share_host_fonts: not sharing " + b.source + ": the Podman Machine " +
				"does not share it. Add it to the machine's volumes (podman machine init " +
				"--volume), or remove share_host_fonts from " + userConfig + ".[/yellow]")
			continue
		}
		out = append(out, b)
	}
	return out
}

// hostFontDisclosure is the line every launch with share_host_fonts prints per bound folder.
func hostFontDisclosure(b hostFontBind) string {
	return "Sharing your fonts read-only (share_host_fonts): " + b.source + " → " + b.dest
}
