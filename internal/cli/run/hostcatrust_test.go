package run

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/catrust"
	"github.com/mschulkind-oss/yolo-jail/internal/entrypoint"
)

// hostCATestCA mints one self-signed CA with the common name cn and returns it as the host read
// would, labelled with source.
func hostCATestCA(t *testing.T, cn, source string) catrust.CA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return catrust.CA{Name: cn, Source: source, PEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))}
}

// stubHostTrust replaces the host read for one test.
func stubHostTrust(t *testing.T, ht hostTrust) {
	t.Helper()
	prev := readHostTrust
	readHostTrust = func() hostTrust { return ht }
	t.Cleanup(func() { readHostTrust = prev })
}

func testPrinter(buf *bytes.Buffer) printer {
	return (&Options{}).pr(buf)
}

// A fresh podman launch hands its jail the host's CAs as ONE read-only file and the variable
// naming it: the whole launch runs, so deleting either call site (run.go's prepareHostCATrust,
// assemble.go's hostCAArgs) fails this.
func TestALaunchHandsTheJailTheHostsCAsAsOneFile(t *testing.T) {
	ca := hostCATestCA(t, "Example Corp Inspection CA", catrust.HostBundleSource("/etc/ssl/certs/ca-certificates.crt"))
	stubHostTrust(t, hostTrust{cas: []catrust.CA{ca}})
	argv, ws, _, _ := sealedLaunch(t, false)

	if !slices.Contains(argvValues(argv, "-e"), entrypoint.HostCAFileEnv+"="+entrypoint.HostCAFileContainerPath) {
		t.Errorf("the launch does not name the host's CA file to the jail; -e values: %v", argvValues(argv, "-e"))
	}
	src, ok := sealedBindSources(argv)[entrypoint.HostCAFileContainerPath]
	if !ok {
		t.Fatalf("the launch binds nothing at %s; -v values: %v", entrypoint.HostCAFileContainerPath, argvValues(argv, "-v"))
	}
	if !strings.HasPrefix(src, ws+string(filepath.Separator)) || filepath.Base(src) != hostCAFileName {
		t.Errorf("the host's CA file is bound from %s, want the launch's own %s under %s", src, hostCAFileName, ws)
	}
	for _, v := range argvValues(argv, "-v") {
		if strings.HasSuffix(v, ":"+entrypoint.HostCAFileContainerPath+":ro") {
			goto bound
		}
	}
	t.Errorf("the host's CA file is not bound read-only: %v", argvValues(argv, "-v"))
bound:
	body, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	got := catrust.ParseLabelled(string(body))
	if len(got) != 1 || got[0].Name != ca.Name || got[0].Source != ca.Source || got[0].PEM != ca.PEM {
		t.Errorf("the composed file holds %+v, want the one host CA with its source", got)
	}
}

// A sealed build reads none of the host's trust and is handed none: the seal allowlists no such
// crossing (PS-D16).
func TestASealedLaunchGetsNoHostCAs(t *testing.T) {
	called := false
	prev := readHostTrust
	readHostTrust = func() hostTrust {
		called = true
		return hostTrust{cas: []catrust.CA{hostCATestCA(t, "Sealed Test CA", "x")}}
	}
	t.Cleanup(func() { readHostTrust = prev })
	argv, _, _, _ := sealedLaunch(t, true)
	if called {
		t.Error("a sealed launch read the host's trust")
	}
	if _, ok := sealedBindSources(argv)[entrypoint.HostCAFileContainerPath]; ok {
		t.Error("a sealed launch binds the host's CA file")
	}
	for _, e := range argvValues(argv, "-e") {
		if strings.HasPrefix(e, entrypoint.HostCAFileEnv+"=") {
			t.Errorf("a sealed launch hands the jail %s", e)
		}
	}
}

// A host with no CA to add gets no file and no variable, and every source it could not read is
// printed, naming the next step.
func TestAHostWithNoCAsHandsTheJailNothingAndSaysWhy(t *testing.T) {
	stubHostTrust(t, hostTrust{notes: []string{"Warning: could not read this host's CA bundle (none) — set SSL_CERT_FILE in env_sources"}})
	var buf bytes.Buffer
	ws := t.TempDir()
	if got := (&Options{}).prepareHostCATrust("podman", ws, testPrinter(&buf)); got != "" {
		t.Errorf("prepareHostCATrust = %q, want none", got)
	}
	if !strings.Contains(buf.String(), "could not read this host's CA bundle") {
		t.Errorf("the read failure was not printed: %q", buf.String())
	}
	if _, err := os.Stat(filepath.Join(ws, hostCAFileName)); !os.IsNotExist(err) {
		t.Errorf("a file was written with no CA to hold: %v", err)
	}
	if args := hostCAArgs("podman", ws, ""); args != nil {
		t.Errorf("hostCAArgs with no file = %v, want nothing", args)
	}
}

// Apple Container binds the workspace's home whole at /home/agent, so the file is written into
// that home and named there, with no bind of its own.
func TestAppleContainerGetsTheHostsCAsInItsHome(t *testing.T) {
	ca := hostCATestCA(t, "Example Corp Inspection CA", catrust.SourceSystemKeychain)
	stubHostTrust(t, hostTrust{cas: []catrust.CA{ca}})
	var buf bytes.Buffer
	ws := t.TempDir()
	got := (&Options{}).prepareHostCATrust("container", ws, testPrinter(&buf))
	want := "/home/agent/" + entrypoint.HostCAFileHomeRel
	if got != want {
		t.Fatalf("prepareHostCATrust = %q, want %q", got, want)
	}
	body, err := os.ReadFile(filepath.Join(ws, filepath.FromSlash(entrypoint.HostCAFileHomeRel)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), ca.PEM) || !strings.Contains(string(body), catrust.SourceSystemKeychain) {
		t.Errorf("the home copy does not hold the labelled CA:\n%s", body)
	}
	args := hostCAArgs("container", ws, got)
	if want := []string{"-e", entrypoint.HostCAFileEnv + "=" + want}; !slices.Equal(args, want) {
		t.Errorf("hostCAArgs = %v, want %v", args, want)
	}
}
