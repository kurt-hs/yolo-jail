//go:build !darwin

package nativehome

import "fmt"

func AccountIdle() (bool, error) {
	return false, fmt.Errorf("native account liveness is available only on macOS")
}
