// init.go implements `yolo init` and `yolo init-user-config` — the config
// scaffolders. `init` writes a three-line pointer file, yolo-jail.toml, adds .yolo/ to
// .git/info/exclude and prints an agent briefing; it scaffolds no defaults, because a project
// with no settings needs none. init-user-config writes the user-level defaults.
package cli

import (
	_ "embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/gitexclude"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/richtext"
)

//go:embed userconfig.jsonc
var userConfigContent string

//go:embed briefing.txt
var briefingContent string

// workspacePointerHead is the whole of what `yolo init` writes: three comment lines and no key.
// A project with no file runs on defaults, so the file exists only to say where settings go
// (docs/design/config-project.md §2, convention C5). It names the docs and the one command that
// adds a key, which is what a reader opening an empty config wants to know.
const workspacePointerHead = "# yolo-jail: this project needs no settings; with none, yolo runs on its defaults.\n" +
	"# What can go here: https://docs.yolo-jail.mschulkind.dev, or `yolo config-ref` for every key.\n" +
	"# Add a key with:  yolo config set <key> <value> --project\n"

// pointerFile is the pointer text, plus a `mounts` list when the user passed --mount: an
// explicit request is not default scaffolding, so it is written, as TOML.
func pointerFile(mounts []string) string {
	if len(mounts) == 0 {
		return workspacePointerHead
	}
	var b strings.Builder
	b.WriteString(workspacePointerHead)
	b.WriteString("\n# Extra host paths mounted read-only at /ctx/<basename>, or \"host:container\".\nmounts = [\n")
	for _, m := range mounts {
		enc, _ := jsonx.DumpsCompact(m)
		b.WriteString("  " + enc + ",\n")
	}
	b.WriteString("]\n")
	return b.String()
}

// existingWorkspaceConfig names the workspace config the loader would read, in either format:
// yolo-jail.toml first, then the .jsonc and .json it still reads. Empty when there is none.
func existingWorkspaceConfig(cwd string) string {
	for _, name := range []string{paths.WorkspaceConfigTOMLName, config.WorkspaceConfigName,
		strings.TrimSuffix(config.WorkspaceConfigName, "c")} {
		if _, err := os.Stat(filepath.Join(cwd, name)); err == nil {
			return name
		}
	}
	return ""
}

// Init runs `yolo init` in cwd. It writes the pointer file yolo-jail.toml (unless the workspace
// already has a config of either format, which it leaves byte-for-byte alone), adds `.yolo/` to the
// repository's .git/info/exclude when there is a git directory (the project's own .gitignore is
// never touched), and prints the briefing to out (color per the caller). Returns the exit code.
func Init(cwd string, mounts []string, out io.Writer, color bool) int {
	if name := existingWorkspaceConfig(cwd); name != "" {
		fmt.Fprint(out, name, " ", richtext.Render("[yellow]already exists.[/yellow]\n", color))
		printBriefing(out, filepath.Join(cwd, name), color)
		return 0
	}
	configName := paths.WorkspaceConfigTOMLName
	configPath := filepath.Join(cwd, configName)
	if err := os.WriteFile(configPath, []byte(pointerFile(mounts)), 0o644); err != nil {
		printInitStatus(out, color, "bold red", fmt.Sprintf("Error writing config: %v", err))
		return 1
	}
	printInitStatus(out, color, "green", "Created "+configName)

	if excluded, err := excludeWorkspaceState(cwd); err != nil {
		fmt.Fprintf(out, "Could not add .yolo/ to the repository's exclude file: %v\n"+
			"Add the line `.yolo/` to .git/info/exclude yourself, so the jail's state stays out of commits.\n", err)
	} else if excluded != "" {
		fmt.Fprintf(out, "Added .yolo/ to %s\n", excluded)
	}

	printBriefing(out, configPath, color)
	return 0
}

// excludeWorkspaceState appends `.yolo/` to the repository's info/exclude and returns the file's
// path, or "" when cwd is no git work tree or the line is already there. info/exclude is local to
// the clone, so the project's tracked .gitignore stays the project's.
func excludeWorkspaceState(cwd string) (string, error) {
	path := gitexclude.File(cwd)
	if path == "" {
		return "", nil // not a work tree, or no git: nothing to exclude from
	}
	existing, readErr := os.ReadFile(path)
	if readErr != nil && !os.IsNotExist(readErr) {
		return "", readErr
	}
	for _, line := range strings.Split(string(existing), "\n") {
		if t := strings.TrimSpace(line); t == ".yolo/" || t == ".yolo" || t == "/.yolo/" || t == "/.yolo" {
			return "", nil
		}
	}
	if err := gitexclude.Append(path, ".yolo/"); err != nil {
		return "", err
	}
	return path, nil
}

// InitUserConfig runs `yolo init-user-config`. Writes the user-level defaults at
// USER_CONFIG_PATH unless it exists.
func InitUserConfig(out io.Writer, color bool) int {
	// The legacy path on purpose: this writer creates the legacy JSONC defaults file.
	p := paths.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		printInitStatus(out, color, "bold red", fmt.Sprintf("Error creating config dir: %v", err))
		return 1
	}
	if _, err := os.Stat(p); err == nil {
		fmt.Fprint(out, p, " ", richtext.Render("[yellow]already exists.[/yellow]\n", color))
		// The likeliest reason to run this again is a jail with no agent, so an existing
		// file that selects no pack gets the same steps a new one does. One that cannot
		// be read says nothing more here: `yolo check` reports that, in its own words.
		problems := 0
		entries, err := config.LoadPacks(func(string) { problems++ })
		if err == nil && problems == 0 && !config.HasConfiguredPack(entries) {
			printUserConfigNextSteps(out, config.UserScopeSources().Locations("config.packs"))
		}
		return 0
	}
	if err := os.WriteFile(p, []byte(userConfigContent), 0o644); err != nil {
		printInitStatus(out, color, "bold red", fmt.Sprintf("Error writing config: %v", err))
		return 1
	}
	printInitStatus(out, color, "green", "Created "+p)
	// The template selects no pack, so this is always the case for a file just written.
	printUserConfigNextSteps(out, nil)
	return 0
}

// printInitStatus styles only the scaffolder's status line. The message is
// never passed to the markup renderer: paths and filesystem errors stay literal
// without richtext.Escape's zero-width joiner, preserving their stripped bytes.
func printInitStatus(out io.Writer, color bool, style, message string) {
	fmt.Fprint(out, richtext.Render("["+style+"]", color), message, richtext.Render("[/"+style+"]\n", color))
}

// printUserConfigNextSteps ends `yolo init-user-config` with what to do next, for a user
// config that selects no pack: as written, the next jail has no coding agent, and the
// command used to end at `Created <path>` without saying so
// (docs/reference/happy-path-principle.md, rule 2). Choosing the agent is the user's call,
// so the step is the edit (rung 3), not a write made for them. The example agent is the one
// the empty-packs notice names (config.NoPacksGuidance), so a new user hears one answer.
//
// It follows the line naming the file, so "that file" is the path the user just read.
//
// packsAt is where an existing user scope already writes an empty `packs` list
// (config.Sources.Locations, highest precedence first). A second `packs` line added above
// it would change nothing, the later key winning, so the step then names that list instead.
func printUserConfigNextSteps(out io.Writer, packsAt []string) {
	choose := "  1. Choose your agent: add this line to that file, right after the opening {\n" +
		"       \"packs\": [\"claude\"],\n"
	if len(packsAt) > 0 {
		choose = "  1. Choose your agent: make the \"packs\" list at " + packsAt[0] + "\n" +
			"       [\"claude\"]\n"
	}
	io.WriteString(out, "It selects no packs yet, so a jail has no coding agent. Next:\n"+
		choose+
		"     (`yolo pack --help` says what other packs deliver)\n"+
		"  2. Check the edit:  yolo check\n"+
		"  3. From your project's directory, launch it:  yolo -- claude\n")
}

// printBriefing renders the post-init agent briefing with {config_path}
// interpolated. Rich markup → ANSI when color, stripped otherwise (info-parity).
func printBriefing(out io.Writer, configPath string, color bool) {
	parts := strings.Split(briefingContent, "{config_path}")
	for i, part := range parts {
		io.WriteString(out, renderMarkup(part, color))
		if i < len(parts)-1 {
			// The path is user-controlled text. Keep it outside the markup renderer so
			// brackets stay literal without richtext.Escape's zero-width joiner.
			io.WriteString(out, configPath)
		}
	}
	io.WriteString(out, "\n")
}
