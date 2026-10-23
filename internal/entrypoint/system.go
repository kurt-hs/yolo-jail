package entrypoint

import (
	"os"
)

// readBundleBytes reads a PEM file, returning nil on any error (best-effort;
func readBundleBytes(path string) []byte {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return data
}

// splitPathList splits on the path separator. The entrypoint always runs on
// Linux, so ":" is correct.
func splitPathList(s string) []string {
	return splitByte(s, ':')
}

func splitByte(s string, sep byte) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

// trimSpace trims ASCII whitespace (for the common PEM-path case; the paths
// never contain exotic unicode whitespace).
func trimSpace(s string) string {
	start := 0
	for start < len(s) && isSpace(s[start]) {
		start++
	}
	end := len(s)
	for end > start && isSpace(s[end-1]) {
		end--
	}
	return s[start:end]
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\v' || b == '\f'
}
