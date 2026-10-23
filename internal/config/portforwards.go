package config

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mschulkind-oss/yolo-jail/internal/jsonx"
)

// portforwards.go is the one reader of `network.forward_host_ports` entries. It lives here,
// below both of its consumers, so the container path's socat forwards (internal/cli/run) and the
// macos-user profile's loopback allow list (internal/macosuser's loopbackports.go) read one
// grammar rather than two.

// PortForward is one parsed forward: the port inside the jail (LocalPort) and the host
// 127.0.0.1 port it bridges to (HostPort).
type PortForward struct {
	LocalPort int
	HostPort  int
}

// ParsePortForwards parses forward_host_ports config entries into
// (localPort, hostPort) pairs:
//
//   - a JSON integer      → (n, n)
//   - a string "a:b"      → (int(a), int(b))   [split once]
//   - a plain string "n"  → (n, n)
//   - anything else       → a warning (returned via the warn callback), skipped
//
// A non-numeric string port surfaces as a returned error, aborting the parse —
// the caller decides. warn receives the "Warning: invalid port forward entry:
// %v" text for non-int/str entries; pass nil to ignore.
func ParsePortForwards(entries []any, warn func(string)) ([]PortForward, error) {
	var result []PortForward
	for _, entry := range entries {
		// A JSON integer literal (jsonx decodes ints as jsonInt).
		if jsonx.IsInt(entry) {
			n, _ := jsonx.AsInt(entry)
			result = append(result, PortForward{int(n), int(n)})
			continue
		}
		// A raw Go int (callers that build entries directly).
		if n, ok := entry.(int); ok {
			result = append(result, PortForward{n, n})
			continue
		}
		s, ok := entry.(string)
		if !ok {
			if warn != nil {
				warn(fmt.Sprintf("Warning: invalid port forward entry: %v", entry))
			}
			continue
		}
		// Only the first ':' splits (like split(":", 1)).
		if idx := strings.IndexByte(s, ':'); idx >= 0 {
			a, err := strconv.Atoi(s[:idx])
			if err != nil {
				return nil, fmt.Errorf("invalid port forward %q: %w", s, err)
			}
			b, err := strconv.Atoi(s[idx+1:])
			if err != nil {
				return nil, fmt.Errorf("invalid port forward %q: %w", s, err)
			}
			result = append(result, PortForward{a, b})
			continue
		}
		p, err := strconv.Atoi(s)
		if err != nil {
			return nil, fmt.Errorf("invalid port forward %q: %w", s, err)
		}
		result = append(result, PortForward{p, p})
	}
	return result, nil
}
