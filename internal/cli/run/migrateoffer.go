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
)

// offerConfigMigration offers to convert this launch's old-format config files, and converts
// them when the person says yes. It never stops the launch.
func (o *Options) offerConfigMigration() {
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
	subject := old[0].Logical + " is in the old format"
	if len(old) > 1 {
		subject = fmt.Sprintf("%s and %d other config %s are in the old format", old[0].Logical, len(old)-1,
			plural(len(old)-1, "file", "files"))
	}
	stop := ""
	if hasLegacyUser {
		stop = fmt.Sprintf("add \"%s\": true to %s", config.ConvertLaterKey, userFile)
	}
	if o.DryRun || !o.IsTTYStdin() || !o.IsTTYStderr() {
		line := subject + "; launching with it, as this yolo will for a few more releases. " +
			"`yolo config migrate` converts it once every machine that shares it runs a yolo that reads TOML."
		if stop != "" {
			line += " To stop this line, " + stop + "."
		}
		o.pr(o.Stderr).printf("[dim]%s[/dim]", richtext.Escape(line))
		return
	}
	verb := "Convert it now?"
	if len(old) > 1 {
		verb = "Convert them now?"
	}
	if _, err := fmt.Fprintf(o.Stderr, "%s. %s [y/N] ", subject, verb); err != nil {
		return
	}
	reply := ""
	if scanner := bufio.NewScanner(o.Stdin); scanner.Scan() {
		reply = strings.ToLower(strings.TrimSpace(scanner.Text()))
	}
	if reply != "y" && reply != "yes" {
		msg := "Launching with the old file; this yolo reads it for a few more releases. `yolo config migrate` " +
			"converts it\n  once every machine that shares it runs a yolo that reads TOML."
		if stop != "" {
			msg += " To stop this prompt until then,\n  " + stop + "."
		}
		o.pr(o.Stderr).print(richtext.Escape(msg))
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
	if _, err := configmigrate.Apply(plan, configmigrate.ApplyOptions{}); err != nil {
		fmt.Fprintf(o.Stderr, "%v\n", err)
		o.pr(o.Stderr).print("Launching with the old files.")
		return
	}
	o.pr(o.Stderr).print("Written, keeping each original as <name>.pre-migrate. Launching on the converted files.")
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
