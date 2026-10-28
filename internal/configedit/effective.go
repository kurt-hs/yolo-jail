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
		effective, consumed, err := config.LoadConfigWithReadInputResult(workspace, true, nil, input)
		if err != nil {
			return fmt.Errorf("read planned config: %w", err)
		}
		if !consumed {
			return fmt.Errorf("explicit target %s was not read by the standard config graph", physical)
		}
		plannedSnapshot, err = config.SnapshotJSON(effective)
		if err != nil {
			return fmt.Errorf("snapshot planned config: %w", err)
		}
		return nil
	}
	options.verifyTarget = func(physical string, _, after []byte) error {
		input, err := config.NewConfigReadInput(physical, after)
		if err != nil {
			return err
		}
		actual, consumed, err := config.LoadConfigWithReadInputResult(workspace, true, nil, input)
		if err != nil {
			return fmt.Errorf("read actual config: %w", err)
		}
		if !consumed {
			return fmt.Errorf("explicit target %s was not read by the standard config graph after write", physical)
		}
		actualSnapshot, err := config.SnapshotJSON(actual)
		if err != nil {
			return fmt.Errorf("snapshot actual config: %w", err)
		}
		if plannedSnapshot != actualSnapshot {
			return fmt.Errorf("effective config differs from the planned graph")
		}
		return nil
	}
	return applyWithOps(path, format, mutation, options, ops)
}
