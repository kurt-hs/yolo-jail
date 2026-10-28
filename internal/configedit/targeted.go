package configedit

import (
	"fmt"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/configdoc"
)

// TargetedResult names the actual selected file for later user-facing disclosure.
type TargetedResult struct {
	Path string
	Plan *Plan
}

// ApplyTargeted applies an edit to a config-owned resolved target through the existing
// mandatory effective-config transaction and its independent post-write verification.
func ApplyTargeted(workspace string, target config.ResolvedEditTarget, mutation Mutation, options ApplyOptions) (TargetedResult, error) {
	if !target.MatchesMutation(editMutationIntent(mutation)) {
		return TargetedResult{}, fmt.Errorf("config mutation does not match the resolved target intent")
	}
	options.Target = target.Target
	options.ExpectedBytes = target.ExpectedBytes()
	mutation.Path = target.AuthoredPath()
	if target.IsProspective() {
		plan, err := applyProspectiveTargeted(workspace, target, mutation, options)
		if err != nil {
			return TargetedResult{}, err
		}
		return TargetedResult{Path: target.LogicalPath, Plan: plan}, nil
	}
	plan, err := ApplyEffective(workspace, target.PhysicalPath, target.Format, mutation, options)
	if err != nil {
		return TargetedResult{}, err
	}
	return TargetedResult{Path: target.LogicalPath, Plan: plan}, nil
}

// ResolveTarget is the public configedit consumer seam for schema-driven target selection.
func ResolveTarget(workspace string, mutation Mutation, choice config.EditTargetChoice) (config.ResolvedEditTarget, error) {
	return config.ResolveEditTarget(workspace, editMutationIntent(mutation), choice)
}

func editMutationIntent(mutation Mutation) config.EditMutationIntent {
	kind := config.EditMutationKind(0)
	switch mutation.Action {
	case Set:
		kind = config.EditMutationSet
	case Add:
		kind = config.EditMutationAdd
	case Append:
		kind = config.EditMutationAppend
	case Remove:
		kind = config.EditMutationRemove
	}
	return config.EditMutationIntent{Key: append([]configdoc.Step(nil), mutation.Path...), Kind: kind, AllowProspectiveCreation: true}
}
