package catrust

import (
	"errors"
	"strings"
	"time"
)

// LinuxBundleFiles are where a Linux distribution keeps its system CA bundle, the file its own
// trust tools (update-ca-certificates, update-ca-trust, p11-kit's extract) regenerate when an
// administrator adds a CA. Go's own list, in its order (crypto/x509 root_linux.go, go1.26), so a
// host's Go programs and this read agree on which file is the host's.
var LinuxBundleFiles = []string{
	"/etc/ssl/certs/ca-certificates.crt",                // Debian, Ubuntu, Gentoo, Arch, NixOS
	"/etc/pki/tls/certs/ca-bundle.crt",                  // Fedora, RHEL 6
	"/etc/ssl/ca-bundle.pem",                            // openSUSE
	"/etc/pki/tls/cacert.pem",                           // OpenELEC
	"/etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem", // CentOS, RHEL 7
	"/etc/ssl/cert.pem",                                 // Alpine
}

// HostBundle is what a Linux host's bundle read found: the file, every in-date certificate it holds
// (ParseBundle), and how many it left out (unparseable or out of date).
type HostBundle struct {
	Path    string
	CAs     []CA
	LeftOut int
}

// ReadLinuxHostBundle reads the host's own CA bundle, as the host's OpenSSL finds it: the file
// SSL_CERT_FILE names when the host sets it (OpenSSL's own override, so a bundle the user pointed
// their host at is that host's trust), else the first of LinuxBundleFiles that reads. getenv and
// readFile are the host's (os.Getenv, os.ReadFile), seams for a test. An error says which files
// were tried; it is never a refusal, the jail then trusting its image's public roots alone.
func ReadLinuxHostBundle(getenv func(string) string, readFile func(string) ([]byte, error), now time.Time) (HostBundle, error) {
	candidates := LinuxBundleFiles
	if p := strings.TrimSpace(getenv("SSL_CERT_FILE")); p != "" {
		candidates = []string{p}
	}
	for _, p := range candidates {
		data, err := readFile(p)
		if err != nil || len(data) == 0 {
			continue
		}
		cas, left := ParseBundle(string(data), HostBundleSource(p), now)
		return HostBundle{Path: p, CAs: cas, LeftOut: left}, nil
	}
	if len(candidates) == 1 && candidates[0] != LinuxBundleFiles[0] {
		return HostBundle{}, errors.New("SSL_CERT_FILE names " + candidates[0] + ", which could not be read")
	}
	return HostBundle{}, errors.New("none of " + strings.Join(candidates, ", ") + " could be read")
}
