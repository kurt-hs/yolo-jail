package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/configdoc"
)

// prospectiveSelectionReceipt records that the standard workspace layer selector chose one
// absent TOML basefile beneath a stable existing parent. It is not evidence of an actual read.
type prospectiveSelectionReceipt struct {
	workspace  string
	logical    string
	parent     string
	parentInfo os.FileInfo
	role       editReadRole
	format     configdoc.Format
}

type prospectiveRead struct {
	bound           *prospectiveSelectionReceipt
	selected        *prospectiveSelectionReceipt
	candidate       []byte
	decodeCandidate bool
}

func copyProspectiveSelection(source *prospectiveSelectionReceipt) *prospectiveSelectionReceipt {
	if source == nil {
		return nil
	}
	copy := *source
	return &copy
}

func cloneProspectiveRead(source *prospectiveRead) *prospectiveRead {
	if source == nil {
		return nil
	}
	return &prospectiveRead{
		bound: copyProspectiveSelection(source.bound), selected: copyProspectiveSelection(source.selected),
		candidate: append([]byte(nil), source.candidate...), decodeCandidate: source.decodeCandidate,
	}
}

func prospectiveRole(scope ExplicitEditScope) editReadRole {
	switch scope {
	case ExplicitEditProject:
		return editRoleProject
	case ExplicitEditLocal:
		return editRoleLocal
	default:
		return editRoleUnspecified
	}
}

func prospectiveScope(role editReadRole) ExplicitEditScope {
	switch role {
	case editRoleProject:
		return ExplicitEditProject
	case editRoleLocal:
		return ExplicitEditLocal
	default:
		return 0
	}
}

func newProspectiveEditTarget(workspace string, receipt *prospectiveSelectionReceipt) ExplicitEditTarget {
	return ExplicitEditTarget{
		workspace: filepath.Clean(workspace), physical: receipt.logical, scope: prospectiveScope(receipt.role),
		format: receipt.format, prospective: copyProspectiveSelection(receipt),
	}
}

func newProspectiveSelectionInput(workspace, path string, role editReadRole) *ConfigReadInput {
	return &ConfigReadInput{prospective: &prospectiveRead{bound: &prospectiveSelectionReceipt{
		workspace: filepath.Clean(workspace), logical: filepath.Clean(path), role: role, format: configdoc.TOML,
	}}}
}

func newProspectiveCandidateInput(receipt *prospectiveSelectionReceipt, candidate []byte) ConfigReadInput {
	return ConfigReadInput{target: receipt.logical, bytes: append([]byte(nil), candidate...), prospective: &prospectiveRead{
		bound: copyProspectiveSelection(receipt), candidate: append([]byte(nil), candidate...), decodeCandidate: true,
	}}
}

// recordProspectiveSelection is called only from the standard top-level Project/Local selector,
// after it chooses the format-preferred filename and before its normal reader runs.
func recordProspectiveSelection(workspace, selectedPath string, role editReadRole, input *ConfigReadInput) error {
	if input == nil || input.prospective == nil || input.prospective.bound == nil {
		return nil
	}
	request := input.prospective
	bound := request.bound
	if role != bound.role || filepath.Clean(selectedPath) != bound.logical || !strings.EqualFold(filepath.Ext(selectedPath), ".toml") {
		return nil
	}
	parent, err := filepath.Abs(filepath.Dir(selectedPath))
	if err != nil {
		return err
	}
	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return fmt.Errorf("resolve prospective config parent %s: %w", parent, err)
	}
	resolvedParent = filepath.Clean(resolvedParent)
	if filepath.Clean(workspace) != bound.workspace || filepath.Clean(resolvedParent) != filepath.Clean(parent) {
		return fmt.Errorf("prospective config parent %s is not the selected workspace directory", parent)
	}
	parentFile, err := os.Open(parent)
	if err != nil {
		return fmt.Errorf("open prospective config parent %s: %w", parent, err)
	}
	parentInfo, statErr := parentFile.Stat()
	closeErr := parentFile.Close()
	if statErr != nil {
		return fmt.Errorf("stat prospective config parent %s: %w", parent, statErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close prospective config parent %s: %w", parent, closeErr)
	}
	if !parentInfo.IsDir() {
		return fmt.Errorf("prospective config parent %s is not a directory", parent)
	}
	if bound.parentInfo != nil && (bound.parent != parent || !os.SameFile(bound.parentInfo, parentInfo)) {
		return fmt.Errorf("prospective config parent %s changed since target selection; reread and retry", parent)
	}
	info, err := os.Lstat(selectedPath)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			if _, targetErr := os.Stat(selectedPath); os.IsNotExist(targetErr) {
				return fmt.Errorf("prospective config target %s is occupied by a dangling symlink", selectedPath)
			}
		}
		return nil // Existing files use the ordinary reader and actual-read witness.
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("inspect prospective config target %s: %w", selectedPath, err)
	}
	receipt := &prospectiveSelectionReceipt{
		workspace: bound.workspace, logical: bound.logical, parent: parent, parentInfo: parentInfo,
		role: role, format: configdoc.TOML,
	}
	if bound.parentInfo != nil {
		if bound.parent != receipt.parent || !os.SameFile(bound.parentInfo, receipt.parentInfo) {
			return fmt.Errorf("prospective config parent %s changed since target selection; reread and retry", parent)
		}
	}
	request.selected = receipt
	request.bound = copyProspectiveSelection(receipt)
	return nil
}

func verifyProspectiveParent(receipt *prospectiveSelectionReceipt) error {
	if receipt == nil || receipt.workspace == "" || receipt.logical == "" || receipt.parent == "" || receipt.parentInfo == nil ||
		receipt.role != editRoleProject && receipt.role != editRoleLocal || receipt.format != configdoc.TOML {
		return fmt.Errorf("prospective target selection receipt is incomplete")
	}
	if filepath.Dir(receipt.logical) != receipt.parent || filepath.Dir(receipt.logical) != receipt.workspace {
		return fmt.Errorf("prospective target %s is not a standard workspace basefile", receipt.logical)
	}
	currentParent, err := os.Stat(receipt.parent)
	if err != nil || !currentParent.IsDir() || !os.SameFile(receipt.parentInfo, currentParent) {
		return fmt.Errorf("prospective config parent %s changed; preserve the workspace and retry", receipt.parent)
	}
	return nil
}

func verifyProspectiveSelection(receipt *prospectiveSelectionReceipt) error {
	if err := verifyProspectiveParent(receipt); err != nil {
		return err
	}
	baseName := WorkspaceConfigName
	if receipt.role == editRoleLocal {
		baseName = WorkspaceLocalConfigName
	}
	selected, _, selectionErr := resolveWorkspaceConfigPath(receipt.workspace, baseName)
	if selectionErr != nil {
		return fmt.Errorf("recheck prospective config selection: %w", selectionErr)
	}
	if filepath.Clean(selected) != receipt.logical {
		return fmt.Errorf("prospective config target %s is no longer selected; preserve the existing sibling and retry", receipt.logical)
	}
	if _, err := os.Lstat(receipt.logical); err == nil {
		return fmt.Errorf("prospective config target %s is no longer absent; reread and retry", receipt.logical)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect prospective config target %s: %w", receipt.logical, err)
	}
	return nil
}

func sameProspectiveSelection(a, b *prospectiveSelectionReceipt) bool {
	return a != nil && b != nil && a.workspace == b.workspace && a.logical == b.logical && a.parent == b.parent &&
		a.role == b.role && a.format == b.format && a.parentInfo != nil && b.parentInfo != nil && os.SameFile(a.parentInfo, b.parentInfo)
}
