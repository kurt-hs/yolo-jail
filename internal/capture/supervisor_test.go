package capture

import "os"

// Self-exec tests dispatch the same helper as the shipped CLI, without installing
// a test binary or changing the normal test process's subreaper state.
func init() {
	if len(os.Args) > 2 && os.Args[1] == "internal" && os.Args[2] == InstallerSupervisorVerb {
		os.Exit(InstallerSupervisorMain(os.Args[3:]))
	}
}
