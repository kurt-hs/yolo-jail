package configedit

import (
	"fmt"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/configdoc"
)

// ApplyEffective edits one explicit existing file and verifies the result through the standard
// layered config reader. The target is not inferred from diagnostic source locations.
func ApplyEffective(workspace, path string, format configdoc.Format, mutation Mutation, options ApplyOptions) (*Plan, error) {
	return applyEffectiveWithOps(workspace, path, format, mutation, options, systemFileOps)
}

func applyEffectiveWithOps(workspace, path string, format configdoc.Format, mutation Mutation, options ApplyOptions, ops fileOps) (*Plan, error) {
	var plannedSnapshot string
	options.validateTarget = func(physical string, _, planned []byte) error {
		input, err := config.NewConfigReadInput(physical, planned)
		if err != nil {
			return err
		}
		result, err := config.ValidateExplicitEdit(workspace, options.Target, input)
		if err != nil {
			return fmt.Errorf("validate planned config: %w", err)
		}
		validatedSnapshot, ok := result.SnapshotJSON()
		if !ok {
			return fmt.Errorf("planned config validation returned no validated snapshot")
		}
		plannedSnapshot = validatedSnapshot
		if options.OnValidated != nil {
			options.OnValidated(result)
		}
		return nil
	}
	options.verifyTarget = func(physical string, _, after []byte) error {
		input, err := config.NewConfigReadInput(physical, after)
		if err != nil {
			return err
		}
		result, err := config.ValidateExplicitEdit(workspace, options.Target, input)
		if err != nil {
			return fmt.Errorf("validate actual config: %w", err)
		}
		actualSnapshot, ok := result.SnapshotJSON()
		if !ok {
			return fmt.Errorf("actual config validation returned no validated snapshot")
		}
		if plannedSnapshot != actualSnapshot {
			return fmt.Errorf("effective config differs from the planned graph")
		}
		return nil
	}
	return applyWithOps(path, format, mutation, options, ops)
}
