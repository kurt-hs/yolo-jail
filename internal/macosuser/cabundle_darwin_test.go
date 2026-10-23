//go:build darwin

package macosuser

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/catrust"
)

// cabundle_darwin_test.go RUNS THE REAL KEYCHAIN READ a launch makes (cabundle.go), on whatever
// Mac runs it (ci.yml's check-macos runs `go test -short ./...` on macos-latest): `security
// find-certificate -a -p` over the System keychain as this user, with no sudo, then the parse
// and filter the launch applies. It asserts only that the read works unprivileged and that
// every block it returns is a certificate; what a runner's keychain holds is not this test's to
// say, so the counts and the time it took are logged, the cost
// docs/design/provisioner-sets.md §16.6 item 8 asked a Mac to record.
func TestTheSystemKeychainIsReadableWithoutSudo(t *testing.T) {
	if _, err := os.Stat(systemKeychainPath); err != nil {
		t.Skipf("this Mac has no %s: %v", systemKeychainPath, err)
	}
	start := time.Now()
	out, err := readSystemKeychainReal()
	read := time.Since(start)
	if err != nil {
		t.Fatalf("reading the System keychain as this user failed: %v", err)
	}
	blocks := strings.Count(out, "-----BEGIN CERTIFICATE-----")
	candidates, left := catrust.ParseCAs(out, catrust.SourceSystemKeychain, time.Now())
	if len(candidates)+left > blocks {
		t.Errorf("%d candidates and %d left out from %d certificate blocks", len(candidates), left, blocks)
	}
	start = time.Now()
	trusted := 0
	for _, c := range candidates {
		if verifyCAReal(c.PEM) {
			trusted++
		}
	}
	t.Logf("System keychain: %d certificate(s) exported in %s; %d TLS CA candidate(s), %d left "+
		"out by the filter; macOS trusts %d of the candidates for TLS (verify-cert took %s)",
		blocks, read.Round(time.Millisecond), len(candidates), left, trusted,
		time.Since(start).Round(time.Millisecond))
}

// THE REAL LOGIN KEYCHAIN READ (OQ-PS15, PS-D14), as this user and with no prompt to wait on: it
// either returns certificates or fails within its deadline, and a failure is logged, never fatal,
// since a CI runner's login keychain may be absent or locked; the launch then trusts the System
// keychain's CAs alone. The time it took is logged, the measurement PS-D14 leaves to a Mac.
func TestTheLoginKeychainReadNeverWaitsOnAPrompt(t *testing.T) {
	start := time.Now()
	out, err := readLoginKeychainReal()
	took := time.Since(start)
	if took > 20*time.Second {
		t.Errorf("the login keychain read took %s, past its deadline", took)
	}
	if err != nil {
		t.Logf("login keychain not read (%s): %v", took.Round(time.Millisecond), err)
		return
	}
	candidates, left := catrust.ParseCAs(out, catrust.SourceSystemKeychain, time.Now())
	t.Logf("login keychain: %d certificate block(s) in %s; %d TLS CA candidate(s), %d left out",
		strings.Count(out, "-----BEGIN CERTIFICATE-----"), took.Round(time.Millisecond), len(candidates), left)
}
