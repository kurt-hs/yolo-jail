//go:build !linux

package cli

func stopExecMonitorRecovery(string, string, stopCommandRunner) bool { return false }
