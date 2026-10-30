package run

// migrateoffer.go is the launch's offer to convert old-format config files
// (docs/design/config-migration.md §1 and §3 step 1; RC-D19, RC-D21).
//
// This release reads both formats and keeps the old reader for a few releases, so an old file is
// never a refusal: the launch ASKS, once per launch, before anything reads the config.
//
//   - With a terminal: "[y/N]". `y` runs the same migration `yolo config migrate` runs (proven
//     equal or nothing written), then the launch continues on the new files. Anything else
//     launches on the old files and says how to convert later or stop the question.
//   - Without one (or for --dry-run, which writes nothing): one line, and the launch continues.
//   - `"convert_later": true` in the old user file, written by hand, silences the offer for every
//     old file this machine reads. No command writes it (config.ConvertLaterKey).
//
// WHERE IT SITS: before the pack refresh and the config load, so a converted config is the one
// the launch reads; and before the config-change prompt, since the migration respells the
// approval records and the boot baseline, so a converted workspace is not then asked about a
// change it did not make. Nothing here refuses a launch: a migration that cannot prove equality,
// or fails to write, leaves every old file as it was and the launch goes on with them.
//
// ONLY A LAUNCH THAT WILL START A JAIL IS ASKED, never an attach to a running one: that jail's config
// was frozen at its boot (an attach never re-stages), so a conversion under it is a change it does
// not see, and every extra terminal would be asked again. The attach decision itself is made far
// below, after the config load, under the launch lock and against the runtime the config names; so
// this asks the one thing knowable before the config is read: whether this workspace's jail has a
// keeper (probeKeeper, a local lock, no runtime call). A keeper is alive for as long as its jail
// runs, so a held lock is an attach, or a jail being ended; either way this launch says nothing
// and the next fresh launch asks. An attach that RESTARTS an older jail (the contract gate) is not
// asked either: it decides that only after the config is read. "Could not ask" is not an attach:
// the offer is made as before, and nothing here refuses a launch. --never-attach is always asked.
//
// What it covers is what `yolo config migrate` covers from here: on the host the user config and
// this workspace's files; in a jail only the workspace's, since the user config is the host's.

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/configdoc"
	"github.com/mschulkind-oss/yolo-jail/internal/configmigrate"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/richtext"
	yoloruntime "github.com/mschulkind-oss/yolo-jail/internal/runtime"
)

// offerConfigMigration offers to convert this launch's old-format config files, and converts
// them when the person says yes. It never stops the launch.
func (o *Options) offerConfigMigration() {
	if o.launchWillAttach() {
		return
	}
	req := configmigrate.Request{Workspace: o.Workspace}
	if !config.InJail() {
		req.UserConfigDir = filepath.Dir(paths.UserConfigPath())
	}
	graph, err := configmigrate.Discover(req)
	if err != nil {
		// The launch's own read below reports what is wrong with these files.
		return
	}
	old := graph.Old()
	if len(old) == 0 {
		return
	}
	userFile, hasLegacyUser, silenced := convertLaterState()
	if silenced {
		return
	}
	names := make([]string, len(old))
	for i, f := range old {
		names[i] = f.Logical
	}
	them := "them"
	if len(old) == 1 {
		them = "it"
	}
	stop := ""
	if hasLegacyUser {
		stop = fmt.Sprintf("add \"%s\": true to %s", config.ConvertLaterKey, userFile)
	}
	if o.DryRun || !o.IsTTYStdin() || !o.IsTTYStderr() {
		line := fmt.Sprintf("Config in the old format (JSONC), which still works for a few more releases: %s. "+
			"Preview converting %s to TOML with `yolo config migrate --dry-run`.", strings.Join(names, ", "), them)
		if stop != "" {
			line += " To stop this line, " + stop + "."
		}
		o.pr(o.Stderr).printf("[dim]%s[/dim]", richtext.Escape(line))
		return
	}
	// Everything a person needs to decide comes BEFORE the question: what changed, that no is
	// safe, what yes does, and how to look first or never be asked again.
	var intro strings.Builder
	fmt.Fprintf(&intro, "yolo's config files are TOML now. %s still in the old format (JSONC):\n",
		plural(len(old), "This one is", "These are"))
	for _, n := range names {
		fmt.Fprintf(&intro, "  %s\n", n)
	}
	fmt.Fprintf(&intro, "%s keep%s working: this yolo reads the old format for a few more releases, so you can "+
		"answer no\nand nothing changes. Converting writes each as TOML only when the new file means exactly "+
		"the same,\nand keeps the original as <name>.pre-migrate. Wait if another machine shares %s and runs "+
		"an older yolo,\nwhich cannot read TOML.\n", strings.ToUpper(them[:1])+them[1:], plural(len(old), "s", ""), them)
	fmt.Fprintf(&intro, "  See the conversion first:  yolo config migrate --dry-run\n")
	if stop != "" {
		fmt.Fprintf(&intro, "  Stop this question:        %s\n", stop)
	}
	if _, err := fmt.Fprintf(o.Stderr, "%sConvert %s now? [y/N] ", intro.String(), them); err != nil {
		return
	}
	reply := ""
	if scanner := bufio.NewScanner(o.Stdin); scanner.Scan() {
		reply = strings.ToLower(strings.TrimSpace(scanner.Text()))
	}
	if reply != "y" && reply != "yes" {
		o.pr(o.Stderr).print(richtext.Escape("Launching with the old format. Convert later with `yolo config migrate`."))
		return
	}
	plan, err := configmigrate.Plan(graph)
	if plan != nil {
		fmt.Fprint(o.Stderr, plan.Transcript())
	}
	if err != nil {
		if plan == nil {
			fmt.Fprintf(o.Stderr, "%v\n", err)
		}
		o.pr(o.Stderr).print("Launching with the old files; nothing was written.")
		return
	}
	result, err := configmigrate.Apply(plan, configmigrate.ApplyOptions{})
	if err != nil {
		fmt.Fprintf(o.Stderr, "%v\n", err)
		o.pr(o.Stderr).print("Launching with the old files.")
		return
	}
	for _, ig := range result.Ignores {
		fmt.Fprintln(o.Stderr, ig.Report(o.Workspace))
	}
	o.pr(o.Stderr).print("Written, keeping each original as <name>.pre-migrate. Launching on the converted files.")
}

// launchWillAttach is whether this launch will most likely attach to this workspace's running jail,
// read before the config is: a keeper holds the jail's liveness lock. keeperUnknown is not an
// attach, so a probe that could not answer leaves the offer as it was.
func (o *Options) launchWillAttach() bool {
	if o.NeverAttach {
		return false
	}
	return probeKeeper(yoloruntime.FromWorkspace(o.Workspace)) == keeperAlive
}

// convertLaterState reads the user's old-format config file: where a person would write
// convert_later (userFile), whether that is a place it can go (hasLegacyUser: an old-format
// user file, or no user file at all, since a new config.jsonc is still an old-format user
// file), and whether it is already set to true there. A TOML user config has no place for it.
func convertLaterState() (userFile string, hasLegacyUser, silenced bool) {
	userFile = paths.UserConfigPath()
	dir := filepath.Dir(userFile)
	if _, err := os.Lstat(filepath.Join(dir, paths.UserConfigTOMLName)); err == nil {
		return userFile, false, false
	}
	for _, name := range []string{paths.UserConfigLegacyJSONCName, paths.UserConfigLegacyJSONName} {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		doc, err := configdoc.Parse(data, configdoc.JSONC)
		if err != nil {
			return path, true, false
		}
		value, _ := doc.Values.Get(config.ConvertLaterKey)
		on, _ := value.(bool)
		return path, true, on
	}
	return userFile, true, false
}
