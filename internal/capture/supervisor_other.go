//go:build !linux

package capture

import (
	"context"
	"fmt"
)

const InstallerSupervisorVerb = "capture-installer-supervise"

func InstallerSupervisorMain([]string) int { return 1 }

func (d *driver) installArtifacts(context.Context) error {
	return fmt.Errorf("program capture: descendant ownership is unsupported on this platform — use the supported sealed Linux capture route; legacy native installation is unchanged")
}

func (d *driver) artifactSnapshot() (snapshot, error) {
	return nil, fmt.Errorf("program capture: use the supported sealed Linux capture route")
}
func (d *driver) transferArtifacts(snapshot, *Result) error {
	return fmt.Errorf("program capture: use the supported sealed Linux capture route")
}

func (d *driver) pinArtifactHome() error {
	return fmt.Errorf("program capture: descendant ownership unsupported — use the supported sealed Linux capture route; legacy native installation is unchanged")
}
