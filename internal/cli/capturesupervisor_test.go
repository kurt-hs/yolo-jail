package cli

import (
	"github.com/mschulkind-oss/yolo-jail/internal/capture"
	"os"
)

func init() {
	if len(os.Args) > 2 && os.Args[1] == "internal" && os.Args[2] == capture.InstallerSupervisorVerb {
		os.Exit(runInternal(os.Args[2:]))
	}
}
