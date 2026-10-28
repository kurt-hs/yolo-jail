package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/cli/check"
	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/configdoc"
	"github.com/mschulkind-oss/yolo-jail/internal/configedit"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/shquote"
)

// runConfigEdit implements the generic readable-config edit verbs. Public dispatch and help are
// owned by the serial CLI integration; callers pass the selected subcommand as args[0].
func runConfigEdit(args []string) int {
	return runConfigEditWithPostWriteCheck(args, os.Stdout, os.Stderr, configEditWorkspace())
}

func configEditWorkspace() string {
	if config.InJail() {
		return config.JailWorkspace()
	}
	if workspace, ok := resolveWorkspaceRoot(); ok {
		return workspace
	}
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return cwd
}

func runConfigEditWith(args []string, out, errw io.Writer, workspace string) int {
	return runConfigEditWithMode(args, out, errw, workspace, false)
}

func runConfigEditWithPostWriteCheck(args []string, out, errw io.Writer, workspace string) int {
	return runConfigEditWithMode(args, out, errw, workspace, true)
}

func runConfigEditWithMode(args []string, out, errw io.Writer, workspace string, postWriteCheck bool) int {
	if len(args) == 0 {
		fmt.Fprintln(errw, "usage: yolo config set|add|unset <key> [value] [--user|--project|--local|--file PATH] [--dry-run]")
		return 2
	}
	actionName := args[0]
	var action configedit.Action
	switch actionName {
	case "set":
		action = configedit.Set
	case "add":
		action = configedit.Append
	case "unset":
		action = configedit.Remove
	default:
		fmt.Fprintf(errw, "unknown config edit verb %q\n", actionName)
		return 2
	}

	choice := config.EditTargetChoice{Kind: config.EditTargetAutomatic}
	dryRun := false
	positionals := make([]string, 0, 2)
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--user", "--project", "--local":
			if choice.Kind != config.EditTargetAutomatic {
				fmt.Fprintln(errw, "choose only one of --user, --project, --local, or --file")
				return 2
			}
			switch args[i] {
			case "--user":
				choice.Kind = config.EditTargetUser
			case "--project":
				choice.Kind = config.EditTargetProject
			case "--local":
				choice.Kind = config.EditTargetLocal
			}
		case "--file":
			if choice.Kind != config.EditTargetAutomatic || i+1 == len(args) {
				fmt.Fprintln(errw, "--file requires one path and cannot be combined with another target flag")
				return 2
			}
			choice = config.EditTargetChoice{Kind: config.EditTargetFile, File: args[i+1]}
			i++
		case "--dry-run":
			dryRun = true
		default:
			if strings.HasPrefix(args[i], "--") {
				fmt.Fprintf(errw, "unknown config edit flag %q\n", args[i])
				return 2
			}
			positionals = append(positionals, args[i])
		}
	}
	wantPositionals := 1
	if action != configedit.Remove {
		wantPositionals = 2
	}
	if len(positionals) != wantPositionals {
		fmt.Fprintf(errw, "%s requires a key", actionName)
		if action != configedit.Remove {
			fmt.Fprint(errw, " and one value")
		}
		fmt.Fprintln(errw)
		return 2
	}

	key, err := parseConfigEditKey(positionals[0])
	if err != nil {
		fmt.Fprintf(errw, "invalid config key: %v\n", err)
		return 2
	}
	mutation := configedit.Mutation{Action: action, Path: key}
	if configEditNeedsHostForJailedUserScope(key, choice) {
		printConfigEditHostRun(errw, workspace, args)
		return 1
	}
	if action != configedit.Remove {
		// Target discovery gives the actual file format before interpreting a typed value.
		target, resolveErr := configedit.ResolveTarget(workspace, mutation, choice)
		if resolveErr != nil {
			fmt.Fprintf(errw, "cannot edit config: %v\n", resolveErr)
			return 1
		}
		if config.InJail() && target.Role == config.ExplicitEditUser {
			printConfigEditHostRun(errw, workspace, args)
			return 1
		}
		valuePath := key
		if action == configedit.Append {
			valuePath = append(append([]configdoc.Step(nil), key...), configdoc.Step{Key: "*"})
		}
		value, valueErr := parseConfigEditValue(positionals[1], valuePath)
		if valueErr != nil {
			fmt.Fprintf(errw, "invalid config value: %v\n", valueErr)
			return 1
		}
		mutation.Value = value
		return applyConfigEdit(workspace, target, mutation, dryRun, out, errw, postWriteCheck)
	}

	if config.InJail() && choice.Kind == config.EditTargetUser {
		printConfigEditHostRun(errw, workspace, args)
		return 1
	}
	target, err := configedit.ResolveTarget(workspace, mutation, choice)
	if err != nil {
		fmt.Fprintf(errw, "cannot edit config: %v\n", err)
		return 1
	}
	mutation, err = configEditUnsetMutation(target.Format, target.AuthoredPath())
	if err != nil {
		fmt.Fprintf(errw, "cannot unset config key: %v\n", err)
		return 1
	}
	mutation.Path = append([]configdoc.Step(nil), key...)
	if !configEditPathPresent(target.ExpectedBytes(), target.Format, target.AuthoredPath()) {
		effective, loadErr := config.LoadReadableConfig(workspace, true, nil)
		if loadErr != nil {
			fmt.Fprintf(errw, "cannot determine whether config key is already absent: %v\n", loadErr)
			return 1
		}
		if !configEditValuePresent(effective, key) {
			mutation = configedit.Mutation{Action: configedit.Remove, Path: append([]configdoc.Step(nil), key...)}
		}
	}
	// Resolve again with the effective authored spelling. The target identity is unchanged,
	// while the mutation intent is checked by ApplyTargeted before any write.
	resolved, err := configedit.ResolveTarget(workspace, mutation, choice)
	if err != nil {
		fmt.Fprintf(errw, "cannot edit config: %v\n", err)
		return 1
	}
	if config.InJail() && resolved.Role == config.ExplicitEditUser {
		printConfigEditHostRun(errw, workspace, args)
		return 1
	}
	return applyConfigEdit(workspace, resolved, mutation, dryRun, out, errw, postWriteCheck)
}

func applyConfigEdit(workspace string, target config.ResolvedEditTarget, mutation configedit.Mutation, dryRun bool, out, errw io.Writer, postWriteCheck bool) int {
	result, err := configedit.ApplyTargeted(workspace, target, mutation, configedit.ApplyOptions{DryRun: dryRun})
	if err != nil {
		fmt.Fprintf(errw, "config edit refused: %v\n", err)
		return 1
	}
	if result.Plan.Noop {
		fmt.Fprintf(out, "%s\nNo change; no write.\n", result.Path)
		return 0
	}
	if dryRun {
		fmt.Fprintf(out, "%s\n", result.Path)
		writeConfigEditHunks(out, result.Plan.Hunks)
		fmt.Fprintln(out, "Dry run; nothing was written.")
		return 0
	}
	fmt.Fprintf(out, "%s\n", result.Path)
	writeConfigEditHunks(out, result.Plan.Hunks)
	if target.Role == config.ExplicitEditUser {
		fmt.Fprintln(out, "Written.")
	} else {
		fmt.Fprintln(out, "Written. Run `yolo check` in this workspace; the host must run `yolo config apply` to approve the workspace change.")
	}
	if postWriteCheck {
		checkRC := runConfigPostWriteCheck(workspace, out, errw)
		if checkRC != 0 {
			fmt.Fprintln(errw, "The config file was written, but the post-write check did not pass; follow its findings before continuing.")
			return checkRC
		}
	}
	return 0
}

func runConfigPostWriteCheck(workspace string, out, errw io.Writer) int {
	opts, ok := checkOptions([]string{"check", "--no-build"}, errw)
	if !ok {
		return 2
	}
	opts.Workspace = workspace
	opts.Stdout = out
	opts.Stderr = errw
	opts.Color = colorForWriter(out)
	return check.Check(opts)
}

func writeConfigEditHunks(out io.Writer, hunks []configedit.Hunk) {
	for _, hunk := range hunks {
		fmt.Fprintln(out, "@@")
		if len(hunk.Before) > 0 {
			fmt.Fprintf(out, "-%s", hunk.Before)
			if hunk.Before[len(hunk.Before)-1] != '\n' {
				fmt.Fprintln(out)
			}
		}
		if len(hunk.After) > 0 {
			fmt.Fprintf(out, "+%s", hunk.After)
			if hunk.After[len(hunk.After)-1] != '\n' {
				fmt.Fprintln(out)
			}
		}
	}
}

func configEditNeedsHostForJailedUserScope(path []configdoc.Step, choice config.EditTargetChoice) bool {
	if !config.InJail() {
		return false
	}
	rule, ok := configEditSchemaRule(path, configdoc.TOML)
	if !ok {
		return false
	}
	if rule.Scope == config.SchemaScopeWorkspaceOnly {
		return false
	}
	if choice.Kind == config.EditTargetFile && configEditIsCanonicalUserFile(choice.File) {
		return true
	}
	return choice.Kind == config.EditTargetUser || (choice.Kind == config.EditTargetAutomatic && rule.Scope == config.SchemaScopeUserOnly)
}

func configEditIsCanonicalUserFile(file string) bool {
	absolute, err := filepath.Abs(file)
	if err != nil {
		return false
	}
	userDir := filepath.Dir(paths.UserConfigPath())
	for _, name := range []string{paths.UserConfigTOMLName, paths.UserConfigLegacyJSONCName, paths.UserConfigLegacyJSONName} {
		if filepath.Clean(absolute) == filepath.Join(userDir, name) {
			return true
		}
	}
	return false
}

func printConfigEditHostRun(errw io.Writer, workspace string, argv []string) {
	command := shquote.Join(append([]string{"yolo", "config"}, argv...))
	fmt.Fprintf(errw, "user-scope config edits must run on the host; run: yolo host-run -C %s --name config-edit -- %s\n",
		shquote.QuoteDisplay(workspace), command)
}

func configEditPathPresent(data []byte, format configdoc.Format, path []configdoc.Step) bool {
	doc, err := configdoc.Parse(data, format)
	if err != nil {
		return false
	}
	_, found, err := doc.Index.Value(path...)
	return err == nil && found
}

func configEditValuePresent(root any, path []configdoc.Step) bool {
	value := root
	for _, step := range path {
		if step.IsIndex {
			items, ok := value.([]any)
			if !ok || step.Index < 0 || step.Index >= len(items) {
				return false
			}
			value = items[step.Index]
			continue
		}
		object, ok := value.(*jsonx.OrderedMap)
		if !ok {
			return false
		}
		var present bool
		value, present = object.Get(step.Key)
		if !present {
			return false
		}
	}
	return true
}

func configEditUnsetMutation(format configdoc.Format, path []configdoc.Step) (configedit.Mutation, error) {
	rule, ok := configEditSchemaRule(path, format)
	if !ok {
		return configedit.Mutation{}, fmt.Errorf("key has no removal policy in the selected config schema")
	}
	mutation := configedit.Mutation{Action: configedit.Set, Path: append([]configdoc.Step(nil), path...)}
	switch rule.Removal {
	case config.RemovalNull, config.RemovalTombstone:
		if format != configdoc.JSONC {
			return configedit.Mutation{}, fmt.Errorf("the selected TOML schema does not define a null removal")
		}
		mutation.Value = nil
	case config.RemovalEmptyList:
		if format != configdoc.TOML {
			return configedit.Mutation{}, fmt.Errorf("the selected legacy schema does not define empty-list removal here")
		}
		mutation.Value = []any{}
	case config.RemovalOffString:
		if format != configdoc.TOML {
			return configedit.Mutation{}, fmt.Errorf("the selected legacy schema does not define off-string removal here")
		}
		mutation.Value = "off"
	case config.RemovalNativeString:
		if format != configdoc.TOML {
			return configedit.Mutation{}, fmt.Errorf("the selected legacy schema does not define native-string removal here")
		}
		mutation.Value = "native"
	case config.RemovalUnsetList:
		return configedit.Mutation{}, fmt.Errorf("this key's removal requires updating both env.set and env.unset; no multi-key edit is available")
	default:
		return configedit.Mutation{}, fmt.Errorf("the selected config schema does not define a removal for this key")
	}
	return mutation, nil
}

func parseConfigEditValue(raw string, path []configdoc.Step) (any, error) {
	rule, ok := configEditSchemaRule(path, configdoc.TOML)
	if !ok {
		return nil, fmt.Errorf("key has no value type in the readable config schema")
	}
	if rule.Type == config.SchemaTypeString && !looksLikeTOMLString(raw) {
		return raw, nil
	}
	value, err := parseTOMLConfigValue(raw)
	if err != nil {
		return nil, err
	}
	if rule.Type == config.SchemaTypeString {
		if _, ok := value.(string); !ok {
			return nil, fmt.Errorf("expected a string; quote it as a TOML string if needed")
		}
	}
	return value, nil
}

func looksLikeTOMLString(raw string) bool {
	return strings.HasPrefix(raw, "\"") || strings.HasPrefix(raw, "'")
}

func parseTOMLConfigValue(raw string) (any, error) {
	doc, err := configdoc.Parse([]byte("value = "+raw+"\n"), configdoc.TOML)
	if err != nil {
		return nil, fmt.Errorf("expected a TOML value: %w", err)
	}
	keys := doc.Values.Keys()
	if len(keys) != 1 || keys[0] != "value" {
		return nil, fmt.Errorf("expected one TOML value; additional top-level keys are not accepted")
	}
	value, ok := doc.Values.Get("value")
	if !ok {
		return nil, fmt.Errorf("TOML value was not parsed")
	}
	return value, nil
}

func configEditSchemaRule(path []configdoc.Step, format configdoc.Format) (config.SchemaKey, bool) {
	parts := make([]string, len(path))
	for i, step := range path {
		if step.IsIndex {
			parts[i] = "*"
		} else {
			parts[i] = step.Key
		}
	}
	var rules []config.SchemaKey
	if format == configdoc.TOML {
		rules = config.ReadableSchemaKeys()
	} else {
		rules = config.SchemaKeys()
	}
	bestWildcards := len(parts) + 1
	var selected config.SchemaKey
	for _, candidate := range rules {
		candidateParts := strings.Split(candidate.Path, ".")
		if len(candidateParts) != len(parts) {
			continue
		}
		wildcards, matches := 0, true
		for i, component := range candidateParts {
			if component == "*" {
				wildcards++
				continue
			}
			if component != parts[i] {
				matches = false
				break
			}
		}
		if matches && wildcards < bestWildcards {
			selected = candidate
			bestWildcards = wildcards
		}
	}
	return selected, bestWildcards <= len(parts)
}

func parseConfigEditKey(raw string) ([]configdoc.Step, error) {
	var steps []configdoc.Step
	for i := 0; i < len(raw); {
		if raw[i] == '.' {
			return nil, fmt.Errorf("empty key component")
		}
		if raw[i] == '[' {
			i++
			start := i
			for i < len(raw) && raw[i] >= '0' && raw[i] <= '9' {
				i++
			}
			if start == i || i >= len(raw) || raw[i] != ']' {
				return nil, fmt.Errorf("array indexes must be non-negative integers in brackets")
			}
			index, err := strconv.Atoi(raw[start:i])
			if err != nil {
				return nil, err
			}
			i++
			steps = append(steps, configdoc.Step{Index: index, IsIndex: true})
		} else {
			component, next, err := parseConfigKeyComponent(raw, i)
			if err != nil {
				return nil, err
			}
			i = next
			steps = append(steps, configdoc.Step{Key: component})
		}
		if i == len(raw) {
			break
		}
		if raw[i] == '.' {
			i++
			if i == len(raw) {
				return nil, fmt.Errorf("empty key component")
			}
			continue
		}
		if raw[i] != '[' {
			return nil, fmt.Errorf("expected dot or array index after byte %d", i)
		}
	}
	if len(steps) == 0 {
		return nil, fmt.Errorf("key is empty")
	}
	return steps, nil
}

func parseConfigKeyComponent(raw string, start int) (string, int, error) {
	if raw[start] == '"' {
		for i := start + 1; i < len(raw); i++ {
			if raw[i] == '\\' {
				i++
				continue
			}
			if raw[i] == '"' {
				value, err := strconv.Unquote(raw[start : i+1])
				return value, i + 1, err
			}
		}
		return "", start, fmt.Errorf("unterminated quoted key component")
	}
	if raw[start] == '\'' {
		end := strings.IndexByte(raw[start+1:], '\'')
		if end < 0 {
			return "", start, fmt.Errorf("unterminated literal key component")
		}
		end += start + 1
		return raw[start+1 : end], end + 1, nil
	}
	end := start
	for end < len(raw) && raw[end] != '.' && raw[end] != '[' {
		if raw[end] == ']' || raw[end] == ' ' || raw[end] == '\t' || raw[end] == '\n' {
			return "", start, fmt.Errorf("invalid byte in unquoted key component")
		}
		end++
	}
	if end == start {
		return "", start, fmt.Errorf("empty key component")
	}
	return raw[start:end], end, nil
}
