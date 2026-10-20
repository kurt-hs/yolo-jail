package cli

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

func warnUnknownYOLOEnv(known []string, environ []string, out io.Writer) {
	allowed := make(map[string]bool, len(known))
	for _, name := range known {
		allowed[name] = true
	}
	var unknown []string
	seen := map[string]bool{}
	for _, entry := range environ {
		name, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(name, "YOLO_") || allowed[name] || seen[name] {
			continue
		}
		seen[name] = true
		unknown = append(unknown, name)
	}
	sort.Strings(unknown)
	for _, name := range unknown {
		fmt.Fprintf(out, "yolo: warning: %s is not a recognized launch setting; unset %s. "+
			"To choose a container runtime, use YOLO_RUNTIME=podman yolo.\n", name, name)
	}
}

func warnUnknownYOLOEnvFromProcess(known []string) {
	warnUnknownYOLOEnv(known, os.Environ(), os.Stderr)
}
