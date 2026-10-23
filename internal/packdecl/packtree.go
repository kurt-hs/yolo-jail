package packdecl

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/loopholedecl"
)

// ViaPack is the `via` a program built from its pack's OWN tree declares (the design
// docs/design/pack-buildable-programs.md, BG-D1): the pack ships a directory of source, an
// optional one-line build runs in it, and the program is the file `run` names afterwards. It is
// not a relaxed `via: "source"`, which keeps its fork meaning: a fork's bytes come from a pinned
// address in another repository, and a pack-tree program's bytes are the pack's own.
//
// Its delivery reaches the launch through the source launcher, so it projects to the
// InstallKindSource install kind (contributes.go, knownVias). Nothing here builds, fetches or
// installs anything: this file is the vocabulary and its refusals.
const ViaPack = "pack"

// PrebuiltBuild is one platform's download of a pack-tree program, the value of its `prebuilt`
// map keyed by `<goos>/<goarch>`. It follows `binaries`' rules exactly (loopholedecl's
// DownloadURLRefusal and IsSHA256Digest): an https URL with no credentials, and a sha256 the
// download is verified against before anything runs.
//
// `prebuilt` is not `fallback`: that key is taken by an unmodified extension's list entry
// (Contribution.Fallback), and an object under it would not decode.
type PrebuiltBuild struct {
	// URL is where the file is fetched from: https, no credentials in it.
	URL string `json:"url"`
	// SHA256 is the file's digest, 64 lowercase hex digits. It is the pin.
	SHA256 string `json:"sha256"`
}

// packTreeBaseField is one delivery field of another via, which a pack-tree program does not take.
type packTreeBaseField struct {
	name, why string
	set       bool
}

// packTreeBaseFields are the delivery fields of the other vias, which a pack-tree program does not
// take: each names a mechanism the pack-tree program does not use. The fork fields are refused by
// forkFieldPlacementProblems, which runs beside this for every program.
func packTreeBaseFields(c Contribution) []packTreeBaseField {
	return []packTreeBaseField{
		{"package", "it names an npm package, and a pack-tree program's bytes come from its own `from` tree", c.Package != ""},
		{"url", "it names a vendor installer, and a pack-tree program's bytes come from its own `from` tree", c.URL != ""},
		{"flags", "they are npm install flags, and a pack-tree program is not installed by npm", c.Flags != nil},
		{"update", "a self-update verb would replace the pack's program with a vendor's release, which the pack does not pin", c.Update != nil},
		{"versions_dir", "it is where a vendor installer keeps its releases, which a pack-tree program has none of", c.VersionsDir != ""},
		{"installer_env", "it is the environment of a vendor installer, and a pack-tree program has none", c.InstallerEnv != nil},
	}
}

// packTreeProblems validates a `program` delivered `via: "pack"`: `from` and `run` required and
// well-formed, `build` one command line when present, `prebuilt` well-formed when present, and
// every delivery field of another via refused by name. `platforms` and `node_floor` are allowed,
// as on any program, and so is every behavior field: the pack owns this program.
func packTreeProblems(label string, c Contribution) []string {
	var problems []string
	for _, f := range packTreeBaseFields(c) {
		if f.set {
			problems = append(problems, fmt.Sprintf("%s: a program delivered via %q does not take %q — %s",
				label, ViaPack, f.name, f.why))
		}
	}
	if c.From == "" {
		problems = append(problems, label+": a program delivered via \"pack\" needs \"from\" — the "+
			"pack-relative directory holding its source, such as \"tools/board-cli\" (\".\" is the pack root)")
	} else {
		problems = appendPathProblems(problems, label+".from", c.From)
		problems = appendCleanPathProblem(problems, label+".from", c.From)
	}
	if c.Run == "" {
		problems = append(problems, label+": a program delivered via \"pack\" needs \"run\" — the file "+
			"the program is, relative to \"from\", such as \"dist/board.js\"")
	} else {
		problems = appendPathProblems(problems, label+".run", c.Run)
		problems = appendCleanPathProblem(problems, label+".run", c.Run)
	}
	if strings.ContainsAny(c.Build, "\r\n\x00") {
		problems = append(problems, label+".build: must be one command line — a newline makes it a "+
			"script, which belongs in the pack's source tree where the command can call it")
	}
	if c.Prebuilt != nil {
		problems = append(problems, prebuiltProblems(label, c.Prebuilt)...)
	}
	return problems
}

// appendCleanPathProblem refuses a relative path that path.Clean would change ("a//b", "a/",
// "./a"), so the manifest says the path it means. "." is the pack root and is clean. Absolute and
// ".." paths are refused by appendPathProblems, so they are passed over here.
func appendCleanPathProblem(problems []string, field, p string) []string {
	if p == "" || p == "." || strings.HasPrefix(p, "/") || strings.Contains(p, "..") {
		return problems
	}
	if clean := path.Clean(p); clean != p {
		return append(problems, fmt.Sprintf("%s: %q is not a cleaned relative path — write %q", field, p, clean))
	}
	return problems
}

// prebuiltProblems validates a program's `prebuilt` map: at least one platform, every key a
// <goos>/<goarch> pair Go spells, and every entry an https URL with no credentials and a sha256.
func prebuiltProblems(label string, prebuilt map[string]PrebuiltBuild) []string {
	if len(prebuilt) == 0 {
		return []string{label + ": \"prebuilt\" declares no platform — name at least one, e.g. " +
			"\"linux/amd64\", or drop \"prebuilt\" and build from \"from\""}
	}
	platforms := make([]string, 0, len(prebuilt))
	for platform := range prebuilt {
		platforms = append(platforms, platform)
	}
	sort.Strings(platforms)
	var problems []string
	for _, platform := range platforms {
		field := fmt.Sprintf("%s.prebuilt[%q]", label, platform)
		goos, goarch, hasArch := strings.Cut(platform, "/")
		if !hasArch || goarch == "" || strings.Contains(goarch, "/") ||
			!loopholedecl.IsKnownGOOS(goos) || !loopholedecl.IsKnownGOARCH(goarch) {
			problems = append(problems, fmt.Sprintf("%s: the key must be a goos/goarch pair as Go "+
				"spells it, e.g. \"linux/amd64\"", field))
			continue
		}
		b := prebuilt[platform]
		switch loopholedecl.DownloadURLRefusal(b.URL) {
		case loopholedecl.URLNotAbsolute:
			problems = append(problems, fmt.Sprintf("%s.url %q is not an absolute URL", field, b.URL))
		case loopholedecl.URLNotHTTPS:
			problems = append(problems, fmt.Sprintf("%s.url %q must be https — the sha256 pins the bytes, "+
				"and https keeps what was asked for private on the way", field, b.URL))
		case loopholedecl.URLCredentials:
			problems = append(problems, fmt.Sprintf("%s.url carries credentials — every footprint of this "+
				"pack prints the URL, so a credential in it is published; serve the file without one", field))
		}
		switch {
		case b.SHA256 == "":
			problems = append(problems, fmt.Sprintf("%s.sha256 is required — a download without a digest "+
				"leaves the program unpinned (a release asset can be re-uploaded under the same URL); "+
				"write the file's sha256", field))
		case !loopholedecl.IsSHA256Digest(b.SHA256):
			problems = append(problems, fmt.Sprintf("%s.sha256 %q is not a sha256 digest — 64 lowercase "+
				"hex digits, as `sha256sum` prints it", field, b.SHA256))
		}
	}
	return problems
}

// packTreeFieldPlacementProblems refuses `from`, `run` and `prebuilt` where no via reads them: `from`
// on a program delivered any other way, and `run` and `prebuilt` on any other program or kind.
// `from` is a content kind's field too (skills, files, briefing), so it is refused on a program
// only; the content kinds keep their own rules.
func packTreeFieldPlacementProblems(label string, c Contribution) []string {
	var problems []string
	if c.Kind == KindProgram && c.Via != ViaPack && c.From != "" {
		problems = append(problems, fmt.Sprintf("%s: a program delivered via %q does not take \"from\" — "+
			"it is the pack-relative source directory of a program built from its own tree, which only "+
			"a \"program\" with via %q reads; drop \"from\", or deliver it with via %q", label, c.Via, ViaPack, ViaPack))
	}
	if c.Kind != KindProgram || c.Via != ViaPack {
		if c.Run != "" {
			problems = append(problems, fmt.Sprintf("%s: kind %q does not take \"run\" — it names the file a "+
				"program built from its pack tree runs, which only a \"program\" with via %q reads", label, c.Kind, ViaPack))
		}
		if c.Prebuilt != nil {
			problems = append(problems, fmt.Sprintf("%s: kind %q does not take \"prebuilt\" — it names the "+
				"downloads of a program built from its pack tree, which only a \"program\" with via %q reads", label, c.Kind, ViaPack))
		}
	}
	return problems
}

// PackTreeWarnings is what `yolo pack lint` warns about for the pack-tree programs of a manifest
// (BG-D16): a program whose `from` directory holds no staged file and that declares no `prebuilt`
// has nothing to build or run, and a launch would refuse it. present reports whether a
// pack-relative directory holds any staged file; "." is the pack root and always present. A
// warning, not a refusal: an absent directory is legal in the manifest, and a program delivered
// some other way may still be the pack's intent.
func PackTreeWarnings(contributions []Contribution, present func(dir string) bool) []string {
	var out []string
	for _, c := range contributions {
		if c.Kind != KindProgram || c.Via != ViaPack || c.Prebuilt != nil {
			continue
		}
		if path.Clean(c.From) == "." || present(path.Clean(c.From)) {
			continue
		}
		out = append(out, fmt.Sprintf("program %q: \"from\" %q holds no file in this pack, and no \"prebuilt\" "+
			"is declared, so there is nothing to build or run — add the source directory, or declare a "+
			"\"prebuilt\" download for each platform it serves", c.Bin, c.From))
	}
	return out
}
