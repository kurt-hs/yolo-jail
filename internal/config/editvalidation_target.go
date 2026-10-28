package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/configdoc"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// EditTargetChoiceKind selects the standard target policy or one explicit config role/file.
type EditTargetChoiceKind uint8

const (
	EditTargetAutomatic EditTargetChoiceKind = iota + 1
	EditTargetUser
	EditTargetProject
	EditTargetLocal
	EditTargetFile
)

// EditTargetChoice is a typed override; File is used only with EditTargetFile.
type EditTargetChoice struct {
	Kind EditTargetChoiceKind
	File string
}

// EditMutationKind identifies the existing splice operation without depending on configedit.
type EditMutationKind uint8

const (
	EditMutationSet EditMutationKind = iota + 1
	EditMutationAdd
	EditMutationAppend
	EditMutationRemove
)

// EditMutationIntent keeps the structured key and operation together; key members are literal,
// including dots, and are never split as a display string.
type EditMutationIntent struct {
	Key  []configdoc.Step
	Kind EditMutationKind
}

// ResolvedEditTarget is a detached target binding. ExpectedBytes returns a copy of the
// preimage; it is discovery evidence, not a semantic validation result.
type ResolvedEditTarget struct {
	LogicalPath  string
	PhysicalPath string
	Format       configdoc.Format
	Role         ExplicitEditScope
	Target       ExplicitEditTarget
	expected     []byte
	intent       EditMutationIntent
}

// ExpectedBytes returns the copied physical preimage selected by the config reader graph.
func (r ResolvedEditTarget) ExpectedBytes() []byte { return append([]byte(nil), r.expected...) }

// MatchesMutation reports whether a public consumer is applying the exact resolved key/operation.
func (r ResolvedEditTarget) MatchesMutation(intent EditMutationIntent) bool {
	if r.intent.Kind != intent.Kind || len(r.intent.Key) != len(intent.Key) {
		return false
	}
	for i := range intent.Key {
		if r.intent.Key[i] != intent.Key[i] {
			return false
		}
	}
	return true
}

// ResolveEditTarget selects one already-existing file consumed by the normal readable config
// graph. Schema scope supplies only the default; ApplyEffective remains the write authority.
func ResolveEditTarget(workspace string, intent EditMutationIntent, choice EditTargetChoice) (ResolvedEditTarget, error) {
	key := intent.Key
	if intent.Kind < EditMutationSet || intent.Kind > EditMutationRemove {
		return ResolvedEditTarget{}, fmt.Errorf("config edit target requires a supported mutation intent")
	}
	if len(key) == 0 {
		return ResolvedEditTarget{}, fmt.Errorf("config edit target requires a structured key")
	}
	parts := make([]string, len(key))
	for i, step := range key {
		if step.IsIndex || step.Key == "" {
			return ResolvedEditTarget{}, fmt.Errorf("config edit target key must contain object members only")
		}
		parts[i] = step.Key
	}
	rule, ok := readableSchemaRule(parts)
	if !ok {
		return ResolvedEditTarget{}, fmt.Errorf("config key %s has no readable schema target", strings.Join(parts, "."))
	}
	if workspace == "" {
		workspace = cwd()
	}
	resolvedWorkspace, err := resolvedAbsolutePath(workspace)
	if err != nil {
		return ResolvedEditTarget{}, fmt.Errorf("resolve config edit workspace: %w", err)
	}
	var roleAudit []editTargetReadObservation
	_, node, err := composeReadableConfigWithInputUseAudit(resolvedWorkspace, true, nil, true, true, nil, nil, &roleAudit)
	if err != nil {
		return ResolvedEditTarget{}, fmt.Errorf("read config graph for edit target: %w", err)
	}
	files, err := editTargetFiles(sourcesOf(node), roleAudit)
	if err != nil {
		return ResolvedEditTarget{}, err
	}

	role := ExplicitEditProject
	switch choice.Kind {
	case EditTargetAutomatic:
		switch rule.Scope {
		case SchemaScopeUserOnly:
			role = ExplicitEditUser
		case SchemaScopeBoth, SchemaScopeWorkspaceOnly, SchemaScopeFieldSensitive:
			role = ExplicitEditProject
		default:
			return ResolvedEditTarget{}, fmt.Errorf("config key %s has no editable scope", strings.Join(parts, "."))
		}
	case EditTargetUser:
		role = ExplicitEditUser
	case EditTargetProject:
		role = ExplicitEditProject
	case EditTargetLocal:
		role = ExplicitEditLocal
	case EditTargetFile:
		if choice.File == "" {
			return ResolvedEditTarget{}, fmt.Errorf("named config edit target requires a file path")
		}
	default:
		return ResolvedEditTarget{}, fmt.Errorf("config edit target choice is invalid")
	}
	if choice.Kind != EditTargetFile && rule.Scope == SchemaScopeUserOnly && role != ExplicitEditUser {
		return ResolvedEditTarget{}, fmt.Errorf("config.%s is user-scope only; select a User config file", strings.Join(parts, "."))
	}
	if choice.Kind != EditTargetFile && rule.Scope == SchemaScopeWorkspaceOnly && role == ExplicitEditUser {
		return ResolvedEditTarget{}, fmt.Errorf("config.%s is workspace-only; move requests to the workspace config", strings.Join(parts, "."))
	}

	var selected editTargetFile
	if choice.Kind == EditTargetFile {
		physical, resolveErr := filepath.EvalSymlinks(choice.File)
		if resolveErr != nil {
			return ResolvedEditTarget{}, fmt.Errorf("resolve named config file %s: %w", choice.File, resolveErr)
		}
		physical, resolveErr = filepath.Abs(physical)
		if resolveErr != nil {
			return ResolvedEditTarget{}, resolveErr
		}
		selected, ok = files[filepath.Clean(physical)]
		if !ok {
			return ResolvedEditTarget{}, fmt.Errorf("named config file %s is not consumed by the scoped config graph", choice.File)
		}
		if len(selected.roles) != 1 || len(selected.formats) != 1 {
			return ResolvedEditTarget{}, fmt.Errorf("named config file %s has conflicting reader roles or formats", choice.File)
		}
		namedPath, resolveErr := filepath.Abs(choice.File)
		if resolveErr != nil {
			return ResolvedEditTarget{}, resolveErr
		}
		namedPath = filepath.Clean(namedPath)
		if !selected.logicalPaths[namedPath] && namedPath != selected.physical {
			return ResolvedEditTarget{}, fmt.Errorf("named config file %s was not read as a logical file by the scoped config graph", choice.File)
		}
		if selected.logicalPaths[namedPath] {
			selected.logical = namedPath
		}
		role, ok = scopeForReadRole(selected.role())
		if !ok {
			return ResolvedEditTarget{}, fmt.Errorf("named config file %s has no editable reader role", choice.File)
		}
		if rule.Scope == SchemaScopeUserOnly && role != ExplicitEditUser {
			return ResolvedEditTarget{}, fmt.Errorf("config.%s is user-scope only; named file %s is not User config", strings.Join(parts, "."), choice.File)
		}
		if rule.Scope == SchemaScopeWorkspaceOnly && role == ExplicitEditUser {
			return ResolvedEditTarget{}, fmt.Errorf("config.%s is workspace-only; move requests to the workspace config", strings.Join(parts, "."))
		}
	} else if role == ExplicitEditUser {
		selected, err = userSectionTarget(sourcesOf(node), parts, files)
		if err != nil {
			return ResolvedEditTarget{}, err
		}
		if selected.physical == "" {
			path, _, _, pathErr := resolveUserConfigPath(paths.UserConfigPath())
			if pathErr != nil {
				return ResolvedEditTarget{}, pathErr
			}
			selected, err = consumedDefaultTarget(path, ExplicitEditUser, files)
			if err != nil {
				return ResolvedEditTarget{}, err
			}
		}
	} else {
		name := WorkspaceConfigName
		if role == ExplicitEditLocal {
			name = WorkspaceLocalConfigName
		}
		path, _, pathErr := resolveWorkspaceConfigPath(resolvedWorkspace, name)
		if pathErr != nil {
			return ResolvedEditTarget{}, pathErr
		}
		selected, err = consumedDefaultTarget(path, role, files)
		if err != nil {
			return ResolvedEditTarget{}, err
		}
	}
	if len(selected.roles) != 1 || len(selected.formats) != 1 || selected.role() != readRoleForScope(role) {
		return ResolvedEditTarget{}, fmt.Errorf("config target %s was not uniquely read as %s config", selected.logical, roleName(role))
	}
	if _, err := os.Stat(selected.physical); err != nil {
		return ResolvedEditTarget{}, fmt.Errorf("config target %s does not exist; create that config file first, then retry (no file was written): %w", selected.logical, err)
	}
	format := selected.format()
	preimage, err := os.ReadFile(selected.physical)
	if err != nil {
		return ResolvedEditTarget{}, fmt.Errorf("read config target %s: %w", selected.logical, err)
	}
	target, err := NewExplicitEditTarget(resolvedWorkspace, selected.physical, role, format)
	if err != nil {
		return ResolvedEditTarget{}, err
	}
	return ResolvedEditTarget{LogicalPath: selected.logical, PhysicalPath: selected.physical, Format: format, Role: role, Target: target, expected: append([]byte(nil), preimage...), intent: cloneEditMutationIntent(intent)}, nil
}

func cloneEditMutationIntent(intent EditMutationIntent) EditMutationIntent {
	intent.Key = append([]configdoc.Step(nil), intent.Key...)
	return intent
}

type editTargetReadObservation struct {
	path string
	role editReadRole
}

type editTargetFile struct {
	logical      string
	physical     string
	logicalPaths map[string]bool
	roles        map[editReadRole]bool
	formats      map[configdoc.Format]bool
}

func (f editTargetFile) role() editReadRole {
	for role := range f.roles {
		return role
	}
	return editRoleUnspecified
}

func (f editTargetFile) format() configdoc.Format {
	for format := range f.formats {
		return format
	}
	return 0
}

func consumedDefaultTarget(path string, scope ExplicitEditScope, files map[string]editTargetFile) (editTargetFile, error) {
	physical, err := filepath.EvalSymlinks(path)
	if err != nil {
		if os.IsNotExist(err) {
			return editTargetFile{}, fmt.Errorf("config target %s does not exist; create that config file first, then retry (no file was written)", path)
		}
		return editTargetFile{}, fmt.Errorf("resolve default config target %s: %w", path, err)
	}
	physical, err = filepath.Abs(physical)
	if err != nil {
		return editTargetFile{}, err
	}
	physical = filepath.Clean(physical)
	selected, ok := files[physical]
	if !ok {
		return editTargetFile{}, fmt.Errorf("default config target %s exists but was not consumed by the scoped config graph", path)
	}
	if len(selected.roles) != 1 || len(selected.formats) != 1 || selected.role() != readRoleForScope(scope) {
		return editTargetFile{}, fmt.Errorf("default config target %s has conflicting or unauthorized reader roles/formats", path)
	}
	return selected, nil
}

func editTargetFiles(sources *Sources, audit []editTargetReadObservation) (map[string]editTargetFile, error) {
	files := map[string]editTargetFile{}
	seen := map[*srcNode]bool{}
	var walk func(*srcNode) error
	walk = func(node *srcNode) error {
		if node == nil || seen[node] {
			return nil
		}
		seen[node] = true
		for _, origin := range node.writers {
			f := origin.file
			if f == nil || f.role == editRoleUnspecified || origin.host != "" {
				continue
			}
			physical, err := filepath.EvalSymlinks(f.path)
			if err != nil {
				return fmt.Errorf("resolve consumed config path %s: %w", f.path, err)
			}
			physical, err = filepath.Abs(physical)
			if err != nil {
				return err
			}
			physical = filepath.Clean(physical)
			entry := files[physical]
			if entry.physical == "" {
				entry = editTargetFile{logical: f.path, physical: physical, logicalPaths: map[string]bool{}, roles: map[editReadRole]bool{}, formats: map[configdoc.Format]bool{}}
			}
			logical, err := filepath.Abs(f.path)
			if err != nil {
				return err
			}
			entry.logicalPaths[filepath.Clean(logical)] = true
			entry.roles[f.role] = true
			entry.formats[f.format] = true
			files[physical] = entry
		}
		for _, child := range node.keys {
			if err := walk(child); err != nil {
				return err
			}
		}
		for _, child := range node.elems {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	if sources != nil {
		if err := walk(sources.root); err != nil {
			return nil, err
		}
	}
	for _, observation := range audit {
		physical, err := filepath.EvalSymlinks(observation.path)
		if err != nil {
			return nil, fmt.Errorf("resolve observed config path %s: %w", observation.path, err)
		}
		physical, err = filepath.Abs(physical)
		if err != nil {
			return nil, err
		}
		physical = filepath.Clean(physical)
		entry, ok := files[physical]
		if !ok {
			continue
		} // attempts can veto a consumed target but never establish consumption
		entry.roles[observation.role] = true
		files[physical] = entry
	}
	return files, nil
}

func userSectionTarget(sources *Sources, key []string, files map[string]editTargetFile) (editTargetFile, error) {
	node := sources.node()
	var ancestors []*srcNode
	for _, part := range key {
		node = node.key(part)
		if node == nil {
			break
		}
		ancestors = append(ancestors, node)
	}
	for n := len(ancestors) - 1; n >= 0; n-- {
		selected := ancestors[n]
		for i := len(selected.writers) - 1; i >= 0; i-- {
			f := selected.writers[i].file
			if f == nil || f.role != editRoleUser || selected.writers[i].host != "" {
				continue
			}
			physical, err := filepath.EvalSymlinks(f.path)
			if err != nil {
				return editTargetFile{}, fmt.Errorf("resolve User section source %s: %w", f.path, err)
			}
			physical, err = filepath.Abs(physical)
			if err != nil {
				return editTargetFile{}, err
			}
			if entry, ok := files[filepath.Clean(physical)]; ok {
				entry.logical = f.path
				return entry, nil
			}
			return editTargetFile{}, fmt.Errorf("User section source %s was not consumed by the scoped graph", f.path)
		}
	}
	return editTargetFile{}, nil
}

func scopeForReadRole(role editReadRole) (ExplicitEditScope, bool) {
	switch role {
	case editRoleUser:
		return ExplicitEditUser, true
	case editRoleProject:
		return ExplicitEditProject, true
	case editRoleLocal:
		return ExplicitEditLocal, true
	default:
		return 0, false
	}
}

func readRoleForScope(scope ExplicitEditScope) editReadRole {
	switch scope {
	case ExplicitEditUser:
		return editRoleUser
	case ExplicitEditProject:
		return editRoleProject
	case ExplicitEditLocal:
		return editRoleLocal
	default:
		return editRoleUnspecified
	}
}

func roleName(scope ExplicitEditScope) string {
	switch scope {
	case ExplicitEditUser:
		return "User"
	case ExplicitEditProject:
		return "Project"
	case ExplicitEditLocal:
		return "Local"
	default:
		return "unknown"
	}
}
