package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/configdoc"
)

// ExplicitEditScope is the normal config-graph role the caller intends to edit. Its zero value
// is deliberately invalid; provenance and filenames never grant write authority.
type ExplicitEditScope uint8

const (
	ExplicitEditUser ExplicitEditScope = iota + 1
	ExplicitEditProject
	ExplicitEditLocal
)

type editReadRole uint8

func (r editReadRole) String() string {
	switch r {
	case editRoleUser:
		return "user"
	case editRoleProject:
		return "project"
	case editRoleLocal:
		return "local"
	case editRoleInherited:
		return "inherited"
	default:
		return "unspecified"
	}
}

const (
	editRoleUnspecified editReadRole = iota
	editRoleUser
	editRoleProject
	editRoleLocal
	editRoleInherited
)

type editReadWitness struct {
	reads []editReadObservation
}

type editReadObservation struct {
	role   editReadRole
	format configdoc.Format
}

func (w *editReadWitness) record(role editReadRole, format configdoc.Format) {
	if w == nil {
		return
	}
	for _, prior := range w.reads {
		if prior == (editReadObservation{role: role, format: format}) {
			return
		}
	}
	w.reads = append(w.reads, editReadObservation{role: role, format: format})
}

func (w *editReadWitness) observeSeenPath(path string, input *ConfigReadInput, role editReadRole) {
	if w == nil || input == nil || role == editRoleUnspecified {
		return
	}
	opened, err := os.Stat(path)
	if err != nil {
		return
	}
	target, err := os.Stat(input.target)
	if err != nil || !os.SameFile(opened, target) {
		return
	}
	format := configdoc.JSONC
	if strings.EqualFold(filepath.Ext(path), ".toml") {
		format = configdoc.TOML
	}
	w.record(role, format)
}

func currentEditWitness(input *ConfigReadInput) *editReadWitness {
	if input == nil {
		return nil
	}
	return input.witness
}

// ExplicitEditTarget binds caller-declared scope, workspace, physical file and decoder format.
// Fields are private so a zero or partially filled target cannot acquire authority.
type ExplicitEditTarget struct {
	workspace string
	physical  string
	scope     ExplicitEditScope
	format    configdoc.Format
}

// NewExplicitEditTarget constructs a complete explicit edit binding. physicalTarget must name
// the physical file used by the edit transaction, not a source label or inferred origin.
func NewExplicitEditTarget(workspace, physicalTarget string, scope ExplicitEditScope, format configdoc.Format) (ExplicitEditTarget, error) {
	if scope != ExplicitEditUser && scope != ExplicitEditProject && scope != ExplicitEditLocal {
		return ExplicitEditTarget{}, fmt.Errorf("explicit edit scope is required and must be user, project, or local")
	}
	if format != configdoc.JSONC && format != configdoc.TOML {
		return ExplicitEditTarget{}, fmt.Errorf("explicit edit format must be JSONC or TOML")
	}
	if !filepath.IsAbs(physicalTarget) {
		return ExplicitEditTarget{}, fmt.Errorf("explicit edit target must be an absolute physical path: %q", physicalTarget)
	}
	physical := filepath.Clean(physicalTarget)
	resolved, err := filepath.EvalSymlinks(physical)
	if err != nil {
		return ExplicitEditTarget{}, fmt.Errorf("resolve explicit edit target %s: %w", physical, err)
	}
	if filepath.Clean(resolved) != physical {
		return ExplicitEditTarget{}, fmt.Errorf("explicit edit target must be the resolved physical path: %s", resolved)
	}
	workspace, err = resolvedAbsolutePath(workspace)
	if err != nil {
		return ExplicitEditTarget{}, fmt.Errorf("resolve explicit edit workspace: %w", err)
	}
	return ExplicitEditTarget{workspace: workspace, physical: physical, scope: scope, format: format}, nil
}

// ExplicitEditReadResult is an immutable view of one planned graph read. It proves input
// consumption and role binding only; it is NOT a semantic validation result.
type ExplicitEditReadResult struct{ snapshot string }

// SnapshotJSON returns the standard canonical snapshot of the composed effective graph.
func (r ExplicitEditReadResult) SnapshotJSON() string { return r.snapshot }

func resolvedAbsolutePath(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("path must be absolute: %q", path)
	}
	resolved, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	return filepath.Clean(resolved), nil
}

// ReadExplicitEditConfig substitutes planned bytes through the normal graph and proves that the
// physical file was consumed under exactly the caller-requested role and decoder. This read/bind
// primitive deliberately does not claim semantic validation; callers must not treat its snapshot
// as validated until invocation-aware common/F/G validation is wired through the same context.
func ReadExplicitEditConfig(workspace string, target ExplicitEditTarget, input ConfigReadInput) (ExplicitEditReadResult, error) {
	if target.scope == 0 || target.physical == "" || target.workspace == "" || target.format == 0 {
		return ExplicitEditReadResult{}, fmt.Errorf("explicit edit target is incomplete")
	}
	actualWorkspace, err := resolvedAbsolutePath(workspace)
	if err != nil {
		return ExplicitEditReadResult{}, fmt.Errorf("resolve explicit edit workspace: %w", err)
	}
	if actualWorkspace != target.workspace {
		return ExplicitEditReadResult{}, fmt.Errorf("explicit edit workspace %s does not match config graph workspace %s", target.workspace, actualWorkspace)
	}
	if input.target == "" || filepath.Clean(input.target) != target.physical {
		return ExplicitEditReadResult{}, fmt.Errorf("explicit edit input does not match the bound physical target %s", target.physical)
	}
	if inJail() && target.scope == ExplicitEditUser {
		return ExplicitEditReadResult{}, fmt.Errorf("user-scope config edits are only allowed on the host; run the edit command for %s on the host", target.physical)
	}
	witness := &editReadWitness{}
	invocation := *input.clone()
	invocation.witness = witness
	cfg, _, err := composeReadableConfigWithInputUse(actualWorkspace, true, nil, true, true, &invocation, nil)
	if err != nil {
		return ExplicitEditReadResult{}, err
	}
	if len(witness.reads) == 0 {
		return ExplicitEditReadResult{}, fmt.Errorf("explicit target %s was not consumed by the standard config graph", target.physical)
	}
	wantRole := map[ExplicitEditScope]editReadRole{
		ExplicitEditUser:    editRoleUser,
		ExplicitEditProject: editRoleProject,
		ExplicitEditLocal:   editRoleLocal,
	}[target.scope]
	for _, read := range witness.reads {
		if read.role != wantRole {
			return ExplicitEditReadResult{}, fmt.Errorf("explicit target %s was read under conflicting or unauthorized config role %s", target.physical, read.role.String())
		}
		if read.format != target.format {
			return ExplicitEditReadResult{}, fmt.Errorf("explicit target %s was read as a different config format", target.physical)
		}
	}
	snapshot, err := SnapshotJSON(cfg)
	if err != nil {
		return ExplicitEditReadResult{}, fmt.Errorf("snapshot explicit edit config read: %w", err)
	}
	return ExplicitEditReadResult{snapshot: snapshot}, nil
}
