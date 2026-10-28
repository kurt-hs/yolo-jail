package configedit

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/config"
	"github.com/mschulkind-oss/yolo-jail/internal/configdoc"
	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

// ApplyEffective edits one explicit existing file and verifies the result through the standard
// layered config reader. The target is not inferred from diagnostic source locations.
func ApplyEffective(workspace, path string, format configdoc.Format, mutation Mutation, options ApplyOptions) (*Plan, error) {
	return applyEffectiveWithOps(workspace, path, format, mutation, options, systemFileOps)
}

func applyEffectiveWithOps(workspace, path string, format configdoc.Format, mutation Mutation, options ApplyOptions, ops fileOps) (*Plan, error) {
	var plannedSnapshot string
	options.validateTarget = func(physical string, before, planned []byte) error {
		if mutation.Action == Remove && validatesOriginalScalarRemoval(format, mutation.Path, before) {
			originalInput, err := config.NewConfigReadInput(physical, before)
			if err != nil {
				return err
			}
			if _, err := config.ValidateExplicitEdit(workspace, options.Target, originalInput); err != nil {
				return fmt.Errorf("validate original config before removal: %w", err)
			}
		}
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

func validatesOriginalScalarRemoval(format configdoc.Format, path []configdoc.Step, data []byte) bool {
	if format != configdoc.TOML && format != configdoc.JSONC {
		return false
	}
	parts := make([]string, len(path))
	for i, step := range path {
		if step.IsIndex {
			return false
		}
		parts[i] = step.Key
	}
	rules := config.ReadableSchemaKeys()
	if format == configdoc.JSONC {
		rules = append(rules, config.SchemaKeys()...)
	}
	var shape config.SchemaType
	found := false
	for _, rule := range rules {
		components := strings.Split(rule.Path, ".")
		if len(components) == len(parts) && schemaPathMatches(components, parts) {
			shape, found = rule.Type, true
			break
		}
	}
	if !found || (shape != config.SchemaTypeString && shape != config.SchemaTypeBoolean &&
		shape != config.SchemaTypeNumber && shape != config.SchemaTypeMixed) {
		return false
	}
	doc, err := configdoc.Parse(data, format)
	if err != nil {
		return false
	}
	var value any = doc.Values
	for _, step := range path {
		if step.IsIndex {
			items, ok := value.([]any)
			if !ok || step.Index < 0 || step.Index >= len(items) {
				return true
			}
			value = items[step.Index]
			continue
		}
		object, ok := value.(*jsonx.OrderedMap)
		if !ok {
			return true
		}
		var present bool
		value, present = object.Get(step.Key)
		if !present {
			return true
		}
	}
	if value == nil {
		return false
	}
	kind := reflect.ValueOf(value).Kind()
	switch kind {
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}

func schemaPathMatches(components, path []string) bool {
	if len(components) != len(path) {
		return false
	}
	for i, component := range components {
		if component != "*" && component != path[i] {
			return false
		}
	}
	return true
}
