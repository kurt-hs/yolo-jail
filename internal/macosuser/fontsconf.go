package macosuser

// fontsconf.go is FONTS inside the macos-user sandbox: the core locator row OQ-NX4 ruled on
// 2026-10-23 (core rows only), the human's own ~/Library/Fonts in the run-as-you mode (ruled the
// same day), and the `share_host_fonts` opt-in for the separate-account mode (OQ-NX9, built now).
// The design is docs/design/provisioner-sets.md §16.4.
//
// nix fontconfig is built to read /etc/fonts/fonts.conf, which no Mac has, so with no variable
// set it prints `Fontconfig error: Cannot load default config file` and knows DejaVu Sans alone.
// No file nixpkgs ships lists the macOS font folders, so a pointer into the store does not help:
// at each launch the host-side yolo writes a fonts.conf per session, beside the session env file
// (never into the sandbox home, which every workspace shares), listing:
//
//   - /System/Library/Fonts, /Library/Fonts and the newest com_apple_MobileAsset_Font* folder
//     (where macOS keeps the fonts it downloads on demand), resolved at each launch;
//   - the human's own ~/Library/Fonts: always in the run-as-you mode, which is the human (the
//     folder itself; one that is a link to elsewhere is skipped with a line, since the profile
//     allow names the literal path); in the separate-account mode only with `share_host_fonts`,
//     and then as a yolo-owned COPY the account may read (fontcopy.go), never the folder itself:
//     the account is granted nothing inside your home;
//   - the tool profile's share/fonts, so a font package in `packages:` counts;
//
// with a cache directory the sandbox can write (`<cachedir prefix="xdg">`, the sandbox HOME's
// ~/.cache/fontconfig) and the upstream rule set by its absolute path in the profile. Then
// FONTCONFIG_FILE names it and FONTCONFIG_PATH its directory, each a default under the
// caller's own env layers.

import (
	"encoding/xml"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/richtext"
)

const (
	// fontsConfSuffix names a session's fonts.conf beside its env file.
	fontsConfSuffix = ".fonts.conf"
	// FontconfigFileVar and FontconfigPathVar are the two locator variables the row sets.
	FontconfigFileVar = "FONTCONFIG_FILE"
	FontconfigPathVar = "FONTCONFIG_PATH"
	// ShareHostFontsKey is the user-scope opt-in the disclosure names (config.ShareHostFonts).
	ShareHostFontsKey = "share_host_fonts"
	// profileFontsRel and profileFontRulesRel are the profile's font folder and the upstream
	// rule set, relative to the tool profile.
	profileFontsRel     = "share/fonts"
	profileFontRulesRel = "etc/fonts/conf.d"
	// userFontsRel is the human's own font folder, relative to their home.
	userFontsRel = "Library/Fonts"
)

// systemFontDirs are the Mac's own font folders, read by the profile's system read roots
// already (/System/Library and /Library/Fonts in seatbeltdeny.go's systemReadBlocks).
var systemFontDirs = []string{"/System/Library/Fonts", "/Library/Fonts"}

// mobileAssetFontGlobs are where macOS keeps on-demand fonts; Homebrew's fontconfig formula
// globs the same two (fontconfig.rb lines 50-69), and the newest match is listed.
var mobileAssetFontGlobs = []string{
	"/System/Library/Assets/com_apple_MobileAsset_Font*",
	"/System/Library/AssetsV2/com_apple_MobileAsset_Font*",
}

// FontsConfFile is a session's fonts.conf: <stateDir>/env/<key>.fonts.conf.
func FontsConfFile(key, sd string) string { return sessionEnvDirFile(key, sd, fontsConfSuffix) }

// FontSetup is what one launch composed for fonts. The zero value composed nothing (no tool
// profile: a dry run, a capture), and sets no variable.
type FontSetup struct {
	// Profile is the tool profile's store path; "" means nothing was composed.
	Profile string
	// SystemDirs are the Mac's font folders, the MobileAsset one included when there is one.
	SystemDirs []string
	// ExtraReads are real paths the Seatbelt profile must open for a listed folder: a MobileAsset
	// folder whose real path is outside /System/Library.
	ExtraReads []string
	// UserFonts is the font folder of yours the file lists, else "": ~/Library/Fonts in the
	// run-as-you mode, the shared copy of it in the separate-account mode.
	UserFonts string
	// UserFontsLinked is ~/Library/Fonts in the run-as-you mode when it is a link to elsewhere,
	// and so not listed.
	UserFontsLinked string
	// SharedFrom is the folder share_host_fonts copied from, when UserFonts is its copy.
	SharedFrom string
	// SharedMissing is the folder share_host_fonts asked for when it does not exist.
	SharedMissing string
	// Copy is what refreshing the shared copy found (share_host_fonts, separate-account mode).
	Copy FontCopy
}

// ComposeFonts composes this launch's fonts from the tool profile and the Mac's font folders. A
// READ OF THE HOST, and in the separate-account mode a refresh (share on) or removal (share off)
// of the shared copy of your fonts, so it runs in the orchestrator, never in the pure plan
// builder or a dry run. self is the run-as-you mode; share is the user-scope share_host_fonts
// opt-in; hostHome is your home.
func ComposeFonts(deps Deps, darwin *Darwin, hostHome string, self, share bool) FontSetup {
	if darwin == nil || darwin.ProfilePath == "" {
		return FontSetup{}
	}
	f := FontSetup{Profile: strings.TrimRight(darwin.ProfilePath, "/")}
	isDir := func(p string) bool { return deps.PathIsDir != nil && deps.PathIsDir(p) }
	f.SystemDirs = append(f.SystemDirs, systemFontDirs...)
	if asset := newestMobileAssetFontDir(deps.Glob); asset != "" {
		f.SystemDirs = append(f.SystemDirs, asset)
		if deps.EvalSymlinks != nil {
			if real, ok := deps.EvalSymlinks(asset); ok && real != asset && !within(real, "/System/Library") {
				f.ExtraReads = append(f.ExtraReads, real)
			}
		}
	}
	hostHome = strings.TrimRight(hostHome, "/")
	if !strings.HasPrefix(hostHome, "/") {
		return f
	}
	user := hostHome + "/" + userFontsRel
	switch {
	case self:
		if !isDir(user) {
			return f
		}
		// The profile allow names the literal path, so a link to elsewhere is not listed: its
		// target would be denied, and allowing the target would open a folder you did not name.
		if deps.EvalSymlinks != nil {
			if real, ok := deps.EvalSymlinks(user); ok && real != user {
				f.UserFontsLinked = user
				return f
			}
		}
		f.UserFonts = user
	case share:
		if !isDir(user) {
			f.SharedMissing = user
			removeSharedFontCopy(deps)
			return f
		}
		f.SharedFrom = user
		if deps.StageFontCopy == nil {
			f.Copy = FontCopy{Err: "this build cannot copy fonts"}
			return f
		}
		f.Copy = deps.StageFontCopy(user, fontCopyRoot())
		f.UserFonts = f.Copy.Dir
	default:
		// The opt-in is off: no copy of your fonts stays behind.
		removeSharedFontCopy(deps)
	}
	return f
}

// removeSharedFontCopy removes the separate account's copy of your fonts.
func removeSharedFontCopy(deps Deps) {
	if deps.RemoveFontCopy != nil {
		deps.RemoveFontCopy(fontCopyRoot())
	}
}

// newestMobileAssetFontDir is the newest com_apple_MobileAsset_Font<N> folder, by N, or "".
func newestMobileAssetFontDir(glob func(string) []string) string {
	if glob == nil {
		return ""
	}
	best, bestN := "", -1
	for _, g := range mobileAssetFontGlobs {
		for _, m := range glob(g) {
			n, err := strconv.Atoi(strings.TrimPrefix(path.Base(m), "com_apple_MobileAsset_Font"))
			if err != nil {
				n = 0
			}
			if n > bestN || (n == bestN && m > best) {
				best, bestN = m, n
			}
		}
	}
	return best
}

// SeatbeltReads are the paths a profile must open for this setup beyond its system read roots:
// the font folder of yours it lists (the folder in the run-as-you mode, the copy in the
// separate-account mode), and a MobileAsset folder's real path outside /System/Library.
func (f FontSetup) SeatbeltReads() []string {
	var out []string
	if f.UserFonts != "" {
		out = append(out, f.UserFonts)
	}
	return append(out, f.ExtraReads...)
}

// fontsConfContent is the fonts.conf for f, or "" when nothing was composed.
func fontsConfContent(f FontSetup) string {
	if f.Profile == "" {
		return ""
	}
	esc := func(s string) string {
		var b strings.Builder
		_ = xml.EscapeText(&b, []byte(s))
		return b.String()
	}
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\"?>\n<!DOCTYPE fontconfig SYSTEM \"urn:fontconfig:fonts.dtd\">\n" +
		"<!-- Written by yolo for one macos-user session (docs/design/provisioner-sets.md §16.4). -->\n" +
		"<fontconfig>\n")
	dirs := slices.Clone(f.SystemDirs)
	if f.UserFonts != "" {
		dirs = append(dirs, f.UserFonts)
	}
	dirs = append(dirs, f.Profile+"/"+profileFontsRel)
	for _, d := range dirs {
		b.WriteString("  <dir>" + esc(d) + "</dir>\n")
	}
	b.WriteString("  <cachedir prefix=\"xdg\">fontconfig</cachedir>\n")
	b.WriteString("  <include ignore_missing=\"yes\">" + esc(f.Profile+"/"+profileFontRulesRel) + "</include>\n")
	b.WriteString("</fontconfig>\n")
	return b.String()
}

// applyFonts sets FONTCONFIG_FILE and FONTCONFIG_PATH on a copy of env, each a default under the
// caller's own layers, and returns the copy with the session's fonts.conf path, "" when nothing
// names it: nothing was composed, or the caller set FONTCONFIG_FILE themselves.
func applyFonts(env *jsonx.OrderedMap, f FontSetup, key string) (*jsonx.OrderedMap, string) {
	if f.Profile == "" || envSets(env, FontconfigFileVar) {
		return env, ""
	}
	file := FontsConfFile(key, "")
	if file == "" {
		return env, ""
	}
	env = withEnvVar(env, FontconfigFileVar, file)
	return withEnvDefault(env, FontconfigPathVar, pathParent(file)), file
}

// fontsFiles is the plan's fonts.conf as an installable file: the directory commands only when
// the env file did not already prepare the same directory, then the read ACE after the write.
func fontsFiles(envFile, file, content string) []sessionFilePlan {
	if file == "" {
		return nil
	}
	dir := SandboxEnvDirCommands(file, "")
	if envFile != "" && pathParent(envFile) == pathParent(file) {
		dir = nil
	}
	return []sessionFilePlan{{path: file, content: content, dir: dir,
		grant: SandboxEnvGrantCommands(file, "")}}
}

// userFontsReadAllow is the Seatbelt block opening reads paths list (FontSetup.SeatbeltReads):
// each folder whole, and metadata alone on the directories above it, so a lookup can stat the
// chain while their listings stay denied. "" when there is nothing to open.
func userFontsReadAllow(reads []string) string {
	reads = absCleanAll(reads...)
	if len(reads) == 0 {
		return ""
	}
	var meta strings.Builder
	seen := map[string]bool{}
	for _, r := range reads {
		for d := path.Dir(r); d != "/"; d = path.Dir(d) {
			if !seen[d] {
				seen[d] = true
				meta.WriteString("    (literal " + sbplStr(d) + ")\n")
			}
		}
	}
	return "\n;; [yolo] Font folders the session's generated fonts.conf lists beyond the system read\n" +
		";; roots (OQ-NX4, OQ-NX9): your ~/Library/Fonts (run as you) or yolo's copy of it (the\n" +
		";; separate account, share_host_fonts), and a MobileAsset font folder's real path.\n" +
		";; READ only, each folder exactly; metadata alone on the directories above it.\n" +
		";; #seatbelt-test-id:font-folder-read-allow#\n" +
		form("allow file-read-metadata", meta.String()) +
		form("allow file-read*", subpathLines(reads))
}

// printFonts is the launch's disclosure of the fonts it gave the sandbox, read off the plan: a
// line naming the copy of your fonts the separate account reads (share_host_fonts) and what it
// left out, one when the folder does not exist or is not listed, and one dim line naming the
// file and what it lists.
func printFonts(out printer, f FontSetup, file string, userConfig string) {
	if f.Profile == "" {
		return
	}
	cfg := richtext.Escape(userConfig)
	if f.SharedMissing != "" {
		out.printf("[dim]%s: %s does not exist, so no font folder of yours is shared. Add fonts "+
			"there, or remove %s from %s.[/dim]", ShareHostFontsKey,
			richtext.Escape(f.SharedMissing), ShareHostFontsKey, cfg)
	}
	if f.UserFontsLinked != "" {
		out.printf("[dim]Fonts in the sandbox: %s is a link to another folder, which the sandbox "+
			"profile does not open, so it is not listed. Keep your fonts in the folder itself to "+
			"have them read.[/dim]", richtext.Escape(f.UserFontsLinked))
	}
	if f.SharedFrom != "" {
		src := richtext.Escape(f.SharedFrom)
		switch {
		case f.Copy.SourceIsLink:
			out.printf("[yellow]%s: %s is a symbolic link, so it is not shared; yolo copies only "+
				"the folder itself.[/yellow] Replace the link with the folder, or remove %s from %s.",
				ShareHostFontsKey, src, ShareHostFontsKey, cfg)
		case f.Copy.Dir == "":
			out.printf("[yellow]%s: could not copy %s for %s (%s), so no font of yours is shared "+
				"this launch.[/yellow] Launch again, and if this repeats, remove %s from %s.",
				ShareHostFontsKey, src, SandboxUser, richtext.Escape(f.Copy.Err), ShareHostFontsKey, cfg)
		default:
			out.printf("Sharing your fonts read-only (%s): a copy of %s at %s, readable by %s.",
				ShareHostFontsKey, src, richtext.Escape(f.Copy.Dir), SandboxUser)
		}
		if n := len(f.Copy.Unreadable); n > 0 {
			out.printf("[dim]%s: %d file(s) in %s could not be read, so they are not shared: %s. "+
				"Make them readable by you to share them.[/dim]", ShareHostFontsKey, n, src,
				richtext.Escape(strings.Join(f.Copy.Unreadable, ", ")))
		}
		if f.Copy.Links > 0 {
			out.printf("[dim]%s: %d symbolic link(s) in %s were skipped; only files are copied.[/dim]",
				ShareHostFontsKey, f.Copy.Links, src)
		}
	}
	if file == "" {
		out.printf("[dim]Fonts in the sandbox: your own environment sets %s, so yolo's fonts.conf "+
			"is not written.[/dim]", FontconfigFileVar)
		return
	}
	listed := "this Mac's font folders"
	if f.UserFonts != "" {
		listed += ", " + richtext.Escape(f.UserFonts)
	}
	out.printf("[dim]Fonts in the sandbox: %s names %s, listing %s and the tool profile's "+
		"share/fonts.[/dim]", FontconfigFileVar, file, listed)
}

// installFontsFiles writes the session's fonts.conf as you, 0600, with the sandbox account's read
// ACE in the account mode: installCATrustFiles' order and its reason to refuse, since the env
// file already names the file, with messages of its own that name the next step.
func installFontsFiles(deps Deps, out printer, files []sessionFilePlan) bool {
	for _, f := range files {
		for _, cmd := range f.dir {
			if deps.Run(cmd) != 0 {
				out.printf("[bold red]Could not prepare the session environment directory "+
					"(%s).[/bold red] %s", strings.Join(cmd, " "), fontsRetry)
				return false
			}
		}
		if !deps.WriteHostFile(f.path, f.content, 0o600) {
			out.printf("[bold red]Could not write the session's fonts.conf %s.[/bold red] %s",
				f.path, fontsRetry)
			return false
		}
		for _, cmd := range f.grant {
			if deps.Run(cmd) != 0 {
				out.printf("[bold red]Could not grant %s read on %s (%s).[/bold red] %s",
					SandboxUser, f.path, strings.Join(cmd, " "), fontsRetry)
				return false
			}
		}
	}
	return true
}

// fontsRetry is the next step every refusal of installFontsFiles names.
const fontsRetry = "The sandbox's " + FontconfigFileVar + " names this file, so the launch stops " +
	"here; run it again, and if this repeats, `yolo run --dry-run` prints every command it runs."
