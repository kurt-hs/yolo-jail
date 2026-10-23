package catrust

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"
)

// certPEM makes one self-signed certificate as PEM.
func certPEM(t *testing.T, cn string, isCA bool, notBefore, notAfter time.Time, eku ...x509.ExtKeyUsage) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		IsCA:                  isCA,
		BasicConstraintsValid: true,
		ExtKeyUsage:           eku,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func validCA(t *testing.T, cn string, eku ...x509.ExtKeyUsage) string {
	now := time.Now()
	return certPEM(t, cn, true, now.Add(-time.Hour), now.Add(24*time.Hour), eku...)
}

// THE FILTER: a TLS CA is kept once, labelled with its source; a leaf, an expired CA, a client-only
// CA and garbage are counted left out; a key block is skipped uncounted.
func TestParseCAsKeepsTLSAnchorsOnce(t *testing.T) {
	now := time.Now()
	ok := validCA(t, "Corp CA", x509.ExtKeyUsageServerAuth)
	anyEKU := validCA(t, "Any CA", x509.ExtKeyUsageAny)
	input := ok + anyEKU + ok +
		certPEM(t, "leaf", false, now.Add(-time.Hour), now.Add(time.Hour)) +
		certPEM(t, "old", true, now.Add(-48*time.Hour), now.Add(-24*time.Hour)) +
		validCA(t, "client", x509.ExtKeyUsageClientAuth) +
		"-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n" +
		"-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----\n"
	kept, left := ParseCAs(input, SourceLoginKeychain, now)
	if len(kept) != 2 || kept[0].Name != "Corp CA" || kept[1].Name != "Any CA" {
		t.Fatalf("kept %+v, want Corp CA and Any CA", kept)
	}
	for _, c := range kept {
		if c.Source != SourceLoginKeychain {
			t.Errorf("%s is labelled %q", c.Name, c.Source)
		}
	}
	if kept[0].PEM != ok {
		t.Errorf("the kept PEM is not the certificate's:\n%s", kept[0].PEM)
	}
	if left != 4 {
		t.Errorf("left = %d, want 4 (leaf, expired, client-only, unparseable)", left)
	}
}

// THE FIRST SOURCE NAMES A CERTIFICATE BOTH HOLD.
func TestMergeKeepsTheFirstSourcesLabel(t *testing.T) {
	a, b := validCA(t, "A"), validCA(t, "B")
	sys, _ := ParseCAs(a, SourceSystemKeychain, time.Now())
	login, _ := ParseCAs(a+b, SourceLoginKeychain, time.Now())
	got := Merge(sys, login)
	if len(got) != 2 || got[0].Source != SourceSystemKeychain || got[1].Name != "B" ||
		got[1].Source != SourceLoginKeychain {
		t.Fatalf("Merge = %+v", got)
	}
}

// THE LABELLED FILE round-trips, and is also a plain PEM bundle OpenSSL and encoding/pem read.
func TestRenderAndParseLabelledRoundTrip(t *testing.T) {
	cas := []CA{}
	for _, src := range []string{SourceSystemKeychain, SourceLoginKeychain, HostBundleSource("/etc/ssl/cert.pem")} {
		k, _ := ParseCAs(validCA(t, "CA of "+src), src, time.Now())
		cas = append(cas, k...)
	}
	text := Render(cas)
	got := ParseLabelled(text)
	if len(got) != 3 {
		t.Fatalf("parsed %d CAs from:\n%s", len(got), text)
	}
	for i := range cas {
		if got[i] != cas[i] {
			t.Errorf("CA %d = %+v, want %+v", i, got[i], cas[i])
		}
	}
	plain, left := ParseCAs(text, "", time.Now())
	if len(plain) != 3 || left != 0 {
		t.Errorf("the labelled file is not a plain bundle: %d kept, %d left", len(plain), left)
	}
}

func TestSourcesPhrase(t *testing.T) {
	id := func(s string) string { return s }
	one := []CA{{Name: "A", Source: SourceSystemKeychain}, {Name: "B", Source: SourceSystemKeychain}}
	if got, want := SourcesPhrase(one, "", id), " from this Mac's System keychain: A, B"; got != want {
		t.Errorf("one source: %q, want %q", got, want)
	}
	if got, want := SourcesPhrase(one, " in Node only", id), " from this Mac's System keychain in Node only: A, B"; got != want {
		t.Errorf("one source, qualified: %q, want %q", got, want)
	}
	two := append(one, CA{Name: "C", Source: SourceLoginKeychain})
	if got, want := SourcesPhrase(two, "", id),
		", 2 from this Mac's System keychain (A, B) and 1 from your login keychain (C)"; got != want {
		t.Errorf("two sources: %q, want %q", got, want)
	}
	if SourcesPhrase(nil, "", id) != "" {
		t.Error("no CA is not the empty phrase")
	}
}

// THE LINUX HOST'S BUNDLE: SSL_CERT_FILE when the host sets it, else Go's list in order; an
// unreadable host is an error that names what was tried, never a panic or a refusal.
func TestReadLinuxHostBundle(t *testing.T) {
	corp := validCA(t, "Corp CA")
	files := map[string]string{"/etc/pki/tls/certs/ca-bundle.crt": corp, "/fixture/host.pem": corp}
	read := func(p string) ([]byte, error) {
		if s, ok := files[p]; ok {
			return []byte(s), nil
		}
		return nil, os.ErrNotExist
	}
	hb, err := ReadLinuxHostBundle(func(string) string { return "" }, read, time.Now())
	if err != nil || hb.Path != "/etc/pki/tls/certs/ca-bundle.crt" || len(hb.CAs) != 1 ||
		hb.CAs[0].Source != HostBundleSource(hb.Path) {
		t.Fatalf("distro list: %+v, %v", hb, err)
	}
	env := func(k string) string {
		if k == "SSL_CERT_FILE" {
			return "/fixture/host.pem"
		}
		return ""
	}
	if hb, err := ReadLinuxHostBundle(env, read, time.Now()); err != nil || hb.Path != "/fixture/host.pem" {
		t.Errorf("SSL_CERT_FILE is not the host's bundle: %+v, %v", hb, err)
	}
	none := func(string) ([]byte, error) { return nil, errors.New("no") }
	if _, err := ReadLinuxHostBundle(func(string) string { return "" }, none, time.Now()); err == nil ||
		!strings.Contains(err.Error(), LinuxBundleFiles[0]) {
		t.Errorf("an unreadable host is %v", err)
	}
	if _, err := ReadLinuxHostBundle(env, none, time.Now()); err == nil || !strings.Contains(err.Error(), "SSL_CERT_FILE") {
		t.Errorf("an unreadable SSL_CERT_FILE is %v", err)
	}
}

// A HOST'S OWN BUNDLE IS ITS TRUST DECISION (PS-D16): every certificate in it that parses and is in
// date is kept, a self-signed server certificate and a client-only CA included, and only what is
// out of date or unparseable is left out and counted. A keychain export keeps ParseCAs' filter.
func TestReadLinuxHostBundleKeepsEveryInDateCertificate(t *testing.T) {
	now := time.Now()
	leaf := certPEM(t, "internal.example", false, now.Add(-time.Hour), now.Add(time.Hour))
	clientCA := validCA(t, "Client-only CA", x509.ExtKeyUsageClientAuth)
	expired := certPEM(t, "Expired CA", true, now.Add(-48*time.Hour), now.Add(-24*time.Hour))
	corp := validCA(t, "Corp CA")
	body := corp + leaf + clientCA + expired + "-----BEGIN CERTIFICATE-----\nbm90IGEgY2VydA==\n-----END CERTIFICATE-----\n"
	read := func(string) ([]byte, error) { return []byte(body), nil }
	hb, err := ReadLinuxHostBundle(func(string) string { return "" }, read, now)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range hb.CAs {
		names = append(names, c.Name)
	}
	if strings.Join(names, ",") != "Corp CA,internal.example,Client-only CA" || hb.LeftOut != 2 {
		t.Errorf("kept %v, left out %d; want every in-date certificate and 2 left out", names, hb.LeftOut)
	}
	if kept, _ := ParseCAs(body, SourceSystemKeychain, now); len(kept) != 1 {
		t.Errorf("a keychain export keeps %d, want only the TLS anchor", len(kept))
	}
}
