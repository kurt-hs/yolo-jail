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
//   - the human's own ~/Library/Fonts: always in the run-as-you mode, which is the human; in the
//     separate-account mode only with `share_host_fonts`, which also grants that account read on
//     it (fontAccessGrants) and opens it in the Seatbelt profile;
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
	// UserFonts is the human's ~/Library/Fonts when it is listed, else "".
	UserFonts string
	// Shared is true when UserFonts is listed because of share_host_fonts (the separate-account
	// mode), which also grants that account read on it.
	Shared bool
	// SharedMissing is the folder share_host_fonts asked for when it does not exist.
	SharedMissing string
	// Grants are the access entries the separate account needs to reach UserFonts, run as you;
	// one already present is left out.
	Grants [][]string
}

// ComposeFonts composes this launch's fonts from the tool profile and the Mac's font folders. A
// READ OF THE HOST, so it runs in the orchestrator, never in the pure plan builder. self is the
// run-as-you mode; share is the user-scope share_host_fonts opt-in; hostHome is your home.
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
		if isDir(user) {
			f.UserFonts = user
		}
	case share:
		if !isDir(user) {
			f.SharedMissing = user
			return f
		}
		f.UserFonts, f.Shared = user, true
		for _, g := range fontAccessGrants(hostHome) {
			if !aceAlreadyGranted(deps, g[len(g)-1], g[2]) {
				f.Grants = append(f.Grants, g)
			}
		}
	}
	return f
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

// fontAccessGrants are the access entries that let the separate account read your font folder:
// traverse (search, not list) on your home and on ~/Library, and list and read on
// ~/Library/Fonts, inherited by what is added to it later. `user:` ACEs, as every grant to the
// sandbox account is (sandboxFileReadAce says why), run as you, the owner, with no sudo.
func fontAccessGrants(hostHome string) [][]string {
	fonts := hostHome + "/" + userFontsRel
	return [][]string{
		{chmodBin, "+a", "user:" + SandboxUser + " allow search", hostHome},
		{chmodBin, "+a", "user:" + SandboxUser + " allow search", hostHome + "/Library"},
		{chmodBin, "+a", "user:" + SandboxUser + " allow list,search," + sandboxFileReadRights +
			",file_inherit,directory_inherit", fonts},
	}
}

// aceAlreadyGranted reports whether `ls -lde path` already shows ace, so a relaunch does not add
// the same entry again. Unknown (no reader, a failed read) is not granted: the grant runs.
func aceAlreadyGranted(deps Deps, p, ace string) bool {
	if deps.RunCapture == nil {
		return false
	}
	out, rc := deps.RunCapture([]string{"/bin/ls", "-lde", p})
	return rc == 0 && strings.Contains(out, ace)
}

// SeatbeltReads are the paths a profile must open for this setup beyond its system read roots:
// your font folder when it is listed, and a MobileAsset folder's real path outside
// /System/Library.
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
		";; roots (OQ-NX4, OQ-NX9): your ~/Library/Fonts and a MobileAsset font folder's real path.\n" +
		";; READ only, each folder exactly; metadata alone on the directories above it.\n" +
		";; #seatbelt-test-id:font-folder-read-allow#\n" +
		form("allow file-read-metadata", meta.String()) +
		form("allow file-read*", subpathLines(reads))
}

// printFonts is the launch's disclosure of the fonts it gave the sandbox, read off the plan: one
// line naming your shared font folder (share_host_fonts), one when that folder does not exist,
// and one dim line naming the file and what it lists.
func printFonts(out printer, f FontSetup, file string, userConfig string) {
	if f.Profile == "" {
		return
	}
	if f.SharedMissing != "" {
		out.printf("[dim]%s: %s does not exist, so no font folder of yours is shared. Add fonts "+
			"there, or remove %s from %s.[/dim]", ShareHostFontsKey,
			richtext.Escape(f.SharedMissing), ShareHostFontsKey, richtext.Escape(userConfig))
	}
	if f.Shared {
		out.printf("Sharing your fonts read-only (%s): %s, readable by %s.", ShareHostFontsKey,
			richtext.Escape(f.UserFonts), SandboxUser)
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
