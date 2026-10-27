package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/mschulkind-oss/yolo-jail/internal/macosuser"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

const sandboxTmpdirValidationVerb = "validate-sandbox-tmpdir"

// runSandboxTmpdirValidation is the no-argument checker called immediately before a native
// generated program launcher execs. Root metadata and names arrive only from the generated
// launcher's trusted literals; the values being checked are the process's final environment.
func runSandboxTmpdirValidation(args []string) int {
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, "yolo internal validate-sandbox-tmpdir takes no arguments")
		return 2
	}
	context, err := macosuser.ParseSandboxTmpdirValidationContext(os.Getenv(paths.SandboxTmpdirValidationContextEnv))
	if err != nil {
		fmt.Fprintf(os.Stderr, "yolo internal validate-sandbox-tmpdir: %v; restart this workspace with `yolo` to restore its native launch metadata\n", err)
		return 1
	}
	var names []string
	if raw := os.Getenv(paths.SandboxTmpdirValidationNamesEnv); raw == "" || json.Unmarshal([]byte(raw), &names) != nil || len(names) == 0 {
		fmt.Fprintln(os.Stderr, "yolo internal validate-sandbox-tmpdir: program temp declarations are missing or malformed; restart this workspace with `yolo`")
		return 1
	}
	if err := macosuser.ValidateSandboxTmpdirEnv(context, names, os.Getenv); err != nil {
		fmt.Fprintf(os.Stderr, "yolo internal validate-sandbox-tmpdir: %v\n", err)
		return 1
	}
	return 0
}
