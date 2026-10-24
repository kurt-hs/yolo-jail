package gitbroker

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mschulkind-oss/yolo-jail/internal/brokerscope"
	"github.com/mschulkind-oss/yolo-jail/internal/gitproto"
	"github.com/mschulkind-oss/yolo-jail/internal/paths"
)

// https_test.go serves the fixture's bare repository with `git http-backend` behind
// net/http/cgi on an httptest TLS server with HTTP basic auth, and puts a fake host `git`
// first on the broker's PATH that answers `credential fill` (docs/design/git-broker-plan.md
// task 6). No real network: the server is on 127.0.0.1.

const (
	httpsUser = "fixture"
	httpsPass = "s3cret"
)

// seenRequest is one request the test server saw.
type seenRequest struct {
	Method, URI, GitProtocol string
	Authed                   bool // carried the right basic credentials
	AnyAuth                  bool // carried an Authorization header at all
	Status                   int
	Command                  string // a POST body's first line: a v2 command, or a push's first command
	Chunked                  bool   // a POST sent with no length (Transfer-Encoding: chunked)
}

func (r seenRequest) String() string {
	s := fmt.Sprintf("%s %s Git-Protocol=%q auth=%v -> %d", r.Method, r.URI, r.GitProtocol, r.AnyAuth, r.Status)
	if r.Command != "" {
		s += " [" + r.Command + "]"
	}
	if r.Chunked {
		s += " chunked"
	}
	return s
}

// firstCommand is a request body's first pkt-line, without its newline or capabilities.
func firstCommand(body []byte) string {
	p, err := gitproto.NewReader(bytes.NewReader(body)).Read()
	if err != nil || p.Kind != gitproto.Data {
		return ""
	}
	line, _, _ := strings.Cut(strings.TrimSuffix(string(p.Data), "\n"), "\x00")
	return line
}

type httpsServer struct {
	mu   sync.Mutex
	seen []seenRequest
}

func (s *httpsServer) requests() []seenRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]seenRequest(nil), s.seen...)
}

func (s *httpsServer) log(t *testing.T) string {
	var b strings.Builder
	for _, r := range s.requests() {
		b.WriteString(r.String() + "\n")
	}
	return b.String()
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

type httpsOpts struct {
	// credBody replaces the fake host git's answer to `credential fill`.
	credBody string
	// v0 strips the Git-Protocol header before git http-backend sees it, so the server answers
	// the v2 probe with a v0 advertisement.
	v0 bool
	// failFetch answers a v2 fetch command's POST with HTTP 500.
	failFetch bool
	// redirect answers every request with a redirect to another host.
	redirect bool
	// untrusted leaves the server's certificate out of the broker's trust roots.
	untrusted bool
	// cover stands in for a grant (Broker.cover).
	cover func(gitproto.Command) (string, string, bool)
	// rejectPushPost answers every push POST with 401, whatever credential it carries.
	rejectPushPost bool
	// authOnlyPushPost serves everything without a credential except a push POST.
	authOnlyPushPost bool
	// grants are the scope file's approved user grants.
	grants []brokerscope.Grant
	// acceptChunked de-chunks a chunked POST for http-backend. Without it the server answers a
	// chunked POST 411, as a CGI host that cannot take one does (net/http/cgi refuses it).
	acceptChunked bool
}

type httpsFixture struct {
	*fixture
	srv    *httpsServer
	url    string // the approved remote
	host   string // 127.0.0.1:<port>
	credRc string // what the fake host git records
}

// newHTTPSFixture is the SSH fixture with origin moved to the TLS server.
func newHTTPSFixture(t *testing.T, o httpsOpts) *httpsFixture {
	t.Helper()
	backend, err := exec.Command("git", "--exec-path").Output()
	if err != nil {
		t.Skip("no git on PATH")
	}
	backendPath := filepath.Join(strings.TrimSpace(string(backend)), "git-http-backend")
	if _, err := os.Stat(backendPath); err != nil {
		t.Skipf("no git-http-backend: %v", err)
	}
	h := &httpsFixture{srv: &httpsServer{}}
	var ts *httptest.Server
	h.fixture = newFixtureOpts(t, fixtureOpts{
		grants: o.grants,
		prepare: func(f *fixture) ([]string, string) {
			srvRoot := filepath.Join(f.root, "srv")
			mustGit(t, f.bare, "config", "http.receivepack", "true")
			cgiH := &cgi.Handler{Path: backendPath, Root: "/",
				Env: []string{"GIT_PROJECT_ROOT=" + srvRoot, "GIT_HTTP_EXPORT_ALL=1", "HOME=" + f.root,
					"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null"}}
			ts = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				user, pass, ok := r.BasicAuth()
				rec := seenRequest{Method: r.Method, URI: r.URL.RequestURI(), GitProtocol: r.Header.Get("Git-Protocol"),
					Authed: ok && user == httpsUser && pass == httpsPass, AnyAuth: r.Header.Get("Authorization") != ""}
				rec.Chunked = r.Method == http.MethodPost && r.ContentLength < 0
				if r.Method == http.MethodPost {
					body, _ := io.ReadAll(r.Body)
					rec.Command = firstCommand(body)
					// net/http/cgi refuses a chunked body, which the broker streams a push over
					// 1 MiB as (git's own client does too); only acceptChunked hands
					// http-backend the buffered body with a length.
					r.Body = io.NopCloser(bytes.NewReader(body))
					if o.acceptChunked {
						r.ContentLength, r.TransferEncoding = int64(len(body)), nil
					}
				}
				sw := &statusRecorder{ResponseWriter: w, status: 200}
				defer func() {
					rec.Status = sw.status
					h.srv.mu.Lock()
					h.srv.seen = append(h.srv.seen, rec)
					h.srv.mu.Unlock()
				}()
				if rec.Chunked && !o.acceptChunked {
					sw.WriteHeader(http.StatusLengthRequired)
					return
				}
				pushPost := r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git-receive-pack")
				needAuth := !o.authOnlyPushPost || pushPost
				if (needAuth && !rec.Authed) || (o.rejectPushPost && pushPost) {
					sw.Header().Set("WWW-Authenticate", `Basic realm="fixture"`)
					sw.WriteHeader(http.StatusUnauthorized)
					return
				}
				if o.redirect {
					http.Redirect(sw, r, "https://elsewhere.test"+r.URL.RequestURI(), http.StatusFound)
					return
				}
				if o.failFetch && rec.Command == "command=fetch" {
					sw.WriteHeader(http.StatusInternalServerError)
					return
				}
				if o.v0 {
					r.Header.Del("Git-Protocol")
				}
				cgiH.ServeHTTP(sw, r)
			}))
			t.Cleanup(ts.Close)
			h.host = strings.TrimPrefix(ts.URL, "https://")
			h.url = ts.URL + "/o/r.git"
			mustGit(t, f.ws, "remote", "set-url", "origin", h.url)

			credBin := filepath.Join(f.root, "credbin")
			h.credRc = filepath.Join(f.record, "cred")
			if err := os.MkdirAll(h.credRc, 0o755); err != nil {
				t.Fatal(err)
			}
			body := o.credBody
			if body == "" {
				body = "printf 'username=" + httpsUser + "\\npassword=" + httpsPass + "\\n'"
			}
			writeExec(t, filepath.Join(credBin, "git"), `#!/bin/sh
n=$(ls `+h.credRc+` | wc -l)
d=`+h.credRc+`/$n-$2
mkdir -p "$d"
printf '%s\n' "$@" > "$d/argv"
pwd > "$d/cwd"
env > "$d/env"
cat > "$d/stdin"
if [ "$2" = fill ]; then
`+body+`
fi
`)
			return []string{h.url}, credBin
		},
		// Set before the broker serves, so no request races these writes.
		bound: func(f *fixture) {
			pool := x509.NewCertPool()
			if !o.untrusted {
				pool.AddCert(ts.Certificate())
			}
			f.broker.rootCAs = pool
			f.broker.cover = o.cover
		},
	})
	return h
}

// credCalls returns the fake host git's recorded calls, in order, as directory paths.
func (h *httpsFixture) credCalls() []string {
	entries, _ := os.ReadDir(h.credRc)
	out := make([]string, len(entries))
	for _, e := range entries {
		var n int
		fmt.Sscanf(e.Name(), "%d-", &n)
		if n < len(out) {
			out[n] = filepath.Join(h.credRc, e.Name())
		}
	}
	return out
}

// upstreamCommit adds a commit to the bare repository's main that the workspace lacks, so a
// fetch transfers a pack.
func (h *httpsFixture) upstreamCommit() {
	h.t.Helper()
	seed := filepath.Join(h.root, "seed")
	if err := os.WriteFile(filepath.Join(seed, "upstream"), []byte("upstream\n"), 0o644); err != nil {
		h.t.Fatal(err)
	}
	mustGit(h.t, seed, "add", "upstream")
	mustGit(h.t, seed, "commit", "-q", "-m", "upstream")
	mustGit(h.t, seed, "push", "-q", h.bare, "main")
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestHTTPSFetchProtocolV2(t *testing.T) {
	h := newHTTPSFixture(t, httpsOpts{})
	h.upstreamCommit()
	out, err := h.git("fetch", "origin")
	t.Logf("request log of one fetch:\n%s", h.srv.log(t))
	if err != nil {
		t.Fatalf("git fetch: %v\n%s\nbroker log:\n%s", err, out, h.log.String())
	}
	if got, want := mustGit(t, h.ws, "rev-parse", "refs/remotes/origin/main"), mustGit(t, h.bare, "rev-parse", "main"); got != want {
		t.Fatalf("origin/main %s, bare main %s", got, want)
	}
	reqs := h.srv.requests()
	if len(reqs) < 2 {
		t.Fatalf("the server saw %d requests:\n%s", len(reqs), h.srv.log(t))
	}
	commands := map[string]bool{}
	for _, r := range reqs {
		if r.GitProtocol != "version=2" {
			t.Fatalf("a request without Git-Protocol: version=2:\n%s", h.srv.log(t))
		}
		commands[r.Command] = true
	}
	if !commands["command=ls-refs"] || !commands["command=fetch"] {
		t.Fatalf("the fetch did not POST both ls-refs and fetch:\n%s", h.srv.log(t))
	}
	raw := auditRaw(t)
	for _, want := range []string{`"set":"read"`, `"repo":"` + h.host + `/o/r"`, `"outcome":"ran"`} {
		if !strings.Contains(raw, want) {
			t.Fatalf("audit log lacks %s:\n%s", want, raw)
		}
	}
	// A second fetch, with the refs already present, is a request-response too.
	if out, err := h.git("fetch", "origin"); err != nil {
		t.Fatalf("second fetch: %v\n%s", err, out)
	}
	if out, err := h.git("ls-remote", "origin"); err != nil || !strings.Contains(out, "refs/heads/main") {
		t.Fatalf("ls-remote: %v\n%s", err, out)
	}
}

func TestHTTPSPushRefusedNothingForwarded(t *testing.T) {
	h := newHTTPSFixture(t, httpsOpts{})
	before := mustGit(t, h.bare, "rev-parse", "main")
	h.localCommit()
	out, err := h.git("push", "origin", "HEAD:main")
	if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 1 {
		t.Fatalf("git push: %v (want exit 1)\n%s", err, out)
	}
	for _, want := range []string{"! [remote rejected]", reasonNeedsApproval} {
		if !strings.Contains(out, want) {
			t.Fatalf("push output lacks %q:\n%s", want, out)
		}
	}
	sawAdv := false
	for _, r := range h.srv.requests() {
		if r.Method == http.MethodPost {
			t.Fatalf("a refused push reached the server with a POST:\n%s", h.srv.log(t))
		}
		if r.Method == http.MethodGet && r.URI == "/o/r.git/info/refs?service=git-receive-pack" && r.Status == 200 {
			sawAdv = true
		}
		if r.GitProtocol != "" {
			t.Fatalf("a push request carried Git-Protocol %q:\n%s", r.GitProtocol, h.srv.log(t))
		}
	}
	if !sawAdv {
		t.Fatalf("no receive-pack advertisement GET:\n%s", h.srv.log(t))
	}
	if after := mustGit(t, h.bare, "rev-parse", "main"); after != before {
		t.Fatalf("bare main moved %s -> %s", before, after)
	}
	if !strings.Contains(auditRaw(t), `"set":"refused"`) {
		t.Fatalf("no refused audit event:\n%s", auditRaw(t))
	}
}

// A push a grant covers is POSTed whole and the server's report relayed (the forward path
// task 9's create-branch reaches; here the cover seam stands in for the grant).
func TestHTTPSPushForwardedWhenCovered(t *testing.T) {
	h := newHTTPSFixture(t, httpsOpts{
		cover: func(gitproto.Command) (string, string, bool) { return "create-branch", "create-branch", true }})
	h.localCommit()
	out, err := h.git("push", "origin", "HEAD:refs/heads/agent/x")
	if err != nil {
		t.Fatalf("git push: %v\n%s\nrequests:\n%s", err, out, h.srv.log(t))
	}
	if got, want := mustGit(t, h.bare, "rev-parse", "agent/x"), mustGit(t, h.ws, "rev-parse", "HEAD"); got != want {
		t.Fatalf("bare agent/x %s, local HEAD %s", got, want)
	}
	// A delete carries no pack, and must not wait for one.
	if out, err := h.git("push", "origin", ":refs/heads/agent/x"); err != nil {
		t.Fatalf("git push delete: %v\n%s", err, out)
	}
	if _, err := run(t, h.bare, gitEnv(), "rev-parse", "--verify", "-q", "refs/heads/agent/x"); err == nil {
		t.Fatal("agent/x survived its delete")
	}
	t.Logf("requests:\n%s", h.srv.log(t))
}

func TestHTTPSV0ServerRefused(t *testing.T) {
	h := newHTTPSFixture(t, httpsOpts{v0: true})
	out, err := h.git("fetch", "origin")
	if err == nil {
		t.Fatalf("fetch from a v0 server succeeded:\n%s", out)
	}
	if !strings.Contains(out, "does not speak protocol v2; use its SSH remote") {
		t.Fatalf("fetch output:\n%s", out)
	}
	for _, r := range h.srv.requests() {
		if r.Method == http.MethodPost {
			t.Fatalf("a POST after a v0 advertisement:\n%s", h.srv.log(t))
		}
	}
}

func TestCredentialFillRunsInAnEmptyBrokerDir(t *testing.T) {
	t.Setenv("GH_TOKEN", "leak")
	t.Setenv("GIT_ASKPASS", "/leak/askpass")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/run/fixture-bus")
	h := newHTTPSFixture(t, httpsOpts{})
	if out, err := h.git("fetch", "origin"); err != nil {
		t.Fatalf("git fetch: %v\n%s", err, out)
	}
	calls := h.credCalls()
	if len(calls) < 2 {
		t.Fatalf("the host git ran %d times, want a fill and an approve", len(calls))
	}
	fill, approve := calls[0], calls[1]
	if argv := readFile(t, filepath.Join(fill, "argv")); argv != "credential\nfill\n" {
		t.Fatalf("first host git argv %q", argv)
	}
	if argv := readFile(t, filepath.Join(approve, "argv")); argv != "credential\napprove\n" {
		t.Fatalf("second host git argv %q", argv)
	}
	stdin := readFile(t, filepath.Join(fill, "stdin"))
	for _, want := range []string{"protocol=https\n", "host=" + h.host + "\n", "path=o/r.git\n"} {
		if !strings.Contains(stdin, want) {
			t.Fatalf("fill stdin lacks %q:\n%s", want, stdin)
		}
	}
	if strings.Contains(stdin, "password") {
		t.Fatalf("fill was handed a password:\n%s", stdin)
	}
	if a := readFile(t, filepath.Join(approve, "stdin")); !strings.Contains(a, "password="+httpsPass+"\n") {
		t.Fatalf("approve stdin:\n%s", a)
	}
	cwd := strings.TrimSpace(readFile(t, filepath.Join(fill, "cwd")))
	under, err := filepath.Rel(paths.BrokerSourceDir(Source), cwd)
	if err != nil || strings.HasPrefix(under, "..") || under == "." || strings.HasPrefix(cwd, h.ws) {
		t.Fatalf("fill ran in %q, not a directory under %q", cwd, paths.BrokerSourceDir(Source))
	}
	if _, err := os.Stat(cwd); !os.IsNotExist(err) {
		t.Fatalf("the credential directory %q outlived the call (%v)", cwd, err)
	}
	env := readFile(t, filepath.Join(fill, "env"))
	for _, want := range []string{"GIT_TERMINAL_PROMPT=0\n", "GIT_CEILING_DIRECTORIES=" + cwd + "\n", "GIT_ASKPASS=\n",
		"GCM_INTERACTIVE=never\n", "DBUS_SESSION_BUS_ADDRESS=unix:path=/run/fixture-bus\n"} {
		if !strings.Contains(env, want) {
			t.Fatalf("fill env lacks %q:\n%s", want, env)
		}
	}
	if strings.Contains(env, "leak") {
		t.Fatalf("fill env carries the broker's own environment:\n%s", env)
	}
}

func TestNoCredentialNamesTheFix(t *testing.T) {
	for name, body := range map[string]string{
		"no password": "printf 'username=fixture\\n'",
		"helper dies": "echo 'fatal: could not read Username: terminal prompts disabled' >&2; exit 128",
	} {
		t.Run(name, func(t *testing.T) {
			h := newHTTPSFixture(t, httpsOpts{credBody: body})
			out, err := h.git("fetch", "origin")
			if err == nil {
				t.Fatalf("fetch succeeded:\n%s", out)
			}
			if !strings.Contains(out, "git credential fill") || !strings.Contains(out, "never prompts") {
				t.Fatalf("fetch output:\n%s", out)
			}
			for _, r := range h.srv.requests() {
				if r.AnyAuth {
					t.Fatalf("a request carried credentials:\n%s", h.srv.log(t))
				}
			}
			if !strings.Contains(auditRaw(t), "the host has no credential") {
				t.Fatalf("audit:\n%s", auditRaw(t))
			}
		})
	}
}

// A POST that fails after the advertisement reached git is said inside git's protocol: git
// has protocol bytes and ignores the helper's exit code, so only an ERR packet fails the fetch.
func TestHTTPSFailureAfterAdvertisementFailsTheFetch(t *testing.T) {
	h := newHTTPSFixture(t, httpsOpts{failFetch: true})
	h.upstreamCommit()
	out, err := h.git("fetch", "origin")
	if err == nil {
		t.Fatalf("fetch succeeded despite the failed POST:\n%s", out)
	}
	if !strings.Contains(out, "HTTP 500") || !strings.Contains(out, "remote error: the git-broker") {
		t.Fatalf("fetch output:\n%s", out)
	}
	if !strings.Contains(auditRaw(t), `"outcome":"failed"`) {
		t.Fatalf("audit:\n%s", auditRaw(t))
	}
}

// A redirect is answered, never followed: the broker connects to the approved URL alone.
func TestHTTPSRedirectNotFollowed(t *testing.T) {
	h := newHTTPSFixture(t, httpsOpts{redirect: true})
	out, err := h.git("fetch", "origin")
	if err == nil || !strings.Contains(out, "redirected") || !strings.Contains(out, "elsewhere.test") {
		t.Fatalf("fetch: %v\n%s", err, out)
	}
}

// The server's certificate is checked against the trust roots: one the broker does not trust
// fails the fetch, and nothing turns that off.
func TestHTTPSUntrustedCertificateRefused(t *testing.T) {
	h := newHTTPSFixture(t, httpsOpts{untrusted: true})
	out, err := h.git("fetch", "origin")
	if err == nil || !strings.Contains(out, "certificate") {
		t.Fatalf("fetch: %v\n%s", err, out)
	}
	if len(h.credCalls()) != 0 {
		t.Fatal("credential fill ran for a server whose certificate failed")
	}
}

// callsOf returns the recorded host git calls for one credential action.
func (h *httpsFixture) callsOf(action string) []string {
	var out []string
	for _, c := range h.credCalls() {
		if strings.HasSuffix(c, "-"+action) {
			out = append(out, c)
		}
	}
	return out
}

// approve and reject send back every field fill answered, verbatim, so a helper's refresh
// token and expiry survive a jail fetch (git-credential-oauth, credential-cache).
func TestCredentialFieldsRoundTrip(t *testing.T) {
	h := newHTTPSFixture(t, httpsOpts{credBody: `printf 'protocol=https\nhost=evil.test\nusername=fixture\npassword=s3cret\npassword_expiry_utc=4102444800\noauth_refresh_token=r3fresh\nwwwauth[]=Basic realm="fixture"\n'`})
	if out, err := h.git("fetch", "origin"); err != nil {
		t.Fatalf("git fetch: %v\n%s", err, out)
	}
	approves := h.callsOf("approve")
	if len(approves) != 1 {
		t.Fatalf("approve calls %v", h.credCalls())
	}
	got := readFile(t, filepath.Join(approves[0], "stdin"))
	want := "protocol=https\nhost=" + h.host + "\npath=o/r.git\nusername=fixture\npassword=s3cret\n" +
		"password_expiry_utc=4102444800\noauth_refresh_token=r3fresh\nwwwauth[]=Basic realm=\"fixture\"\n\n"
	if got != want {
		t.Fatalf("approve stdin %q\nwant %q", got, want)
	}
}

// A credential the server refuses is reported rejected, with every field fill answered.
func TestRefusedCredentialIsRejected(t *testing.T) {
	h := newHTTPSFixture(t, httpsOpts{credBody: `printf 'username=fixture\npassword=wrong\noauth_refresh_token=r3fresh\n'`})
	out, err := h.git("fetch", "origin")
	if err == nil || !strings.Contains(out, "refused the credential") {
		t.Fatalf("fetch: %v\n%s", err, out)
	}
	rejects := h.callsOf("reject")
	if len(rejects) != 1 || len(h.callsOf("approve")) != 0 {
		t.Fatalf("credential calls %v", h.credCalls())
	}
	if s := readFile(t, filepath.Join(rejects[0], "stdin")); !strings.Contains(s, "password=wrong\noauth_refresh_token=r3fresh\n") {
		t.Fatalf("reject stdin %q", s)
	}
}

// A fill answer holding a control character is no credential, and nothing reaches the server.
func TestCredentialControlCharacterRefused(t *testing.T) {
	h := newHTTPSFixture(t, httpsOpts{credBody: `printf 'username=fixture\npassword=s3c\rret\n'`})
	out, err := h.git("fetch", "origin")
	if err == nil || !strings.Contains(out, "git credential fill") {
		t.Fatalf("fetch: %v\n%s", err, out)
	}
	for _, r := range h.srv.requests() {
		if r.AnyAuth {
			t.Fatalf("a request carried credentials:\n%s", h.srv.log(t))
		}
	}
}

func TestCredentialSafe(t *testing.T) {
	for line, want := range map[string]bool{"username=a": true, "path=o/r\n": false, "password=a\rb": false,
		"x=a\x00": false, "x=\x7f": false, `wwwauth[]=Basic realm="x"`: true} {
		if got := credentialSafe(line); got != want {
			t.Errorf("credentialSafe(%q) = %v", line, got)
		}
	}
}

// A push POST the server refuses with 401 rejects the credential, and git gets a report.
func TestHTTPSPushPostRefusedCredentialRejected(t *testing.T) {
	h := newHTTPSFixture(t, httpsOpts{rejectPushPost: true,
		cover: func(gitproto.Command) (string, string, bool) { return "create-branch", "create-branch", true }})
	h.localCommit()
	out, err := h.git("push", "origin", "HEAD:refs/heads/agent/x")
	if err == nil || !strings.Contains(out, "! [remote rejected]") || !strings.Contains(out, "refused the credential") {
		t.Fatalf("push: %v\n%s", err, out)
	}
	if len(h.callsOf("approve")) != 1 || len(h.callsOf("reject")) != 1 {
		t.Fatalf("credential calls %v", h.credCalls())
	}
}

// A server that wants a credential only at the push's upload gets one for a push of at most
// 1 MiB: the buffered body is replayed with the host's credential, as every other request is.
func TestHTTPSPushPostAuthReplaysASmallPush(t *testing.T) {
	h := newHTTPSFixture(t, httpsOpts{authOnlyPushPost: true,
		cover: func(gitproto.Command) (string, string, bool) { return "create-branch", "create-branch", true }})
	h.localCommit()
	if out, err := h.git("push", "origin", "HEAD:refs/heads/agent/x"); err != nil {
		t.Fatalf("push: %v\n%s\nrequests:\n%s", err, out, h.srv.log(t))
	}
	if got, want := mustGit(t, h.bare, "rev-parse", "agent/x"), mustGit(t, h.ws, "rev-parse", "HEAD"); got != want {
		t.Fatalf("bare agent/x %s, local HEAD %s", got, want)
	}
	if len(h.callsOf("fill")) != 1 || len(h.callsOf("approve")) != 1 {
		t.Fatalf("credential calls %v", h.credCalls())
	}
}

// A server that wants a credential only at the upload of a push over 1 MiB cannot get one
// (the streamed body is not replayed), and the message does not claim one was filled.
func TestHTTPSPushPostAuthWithoutCredential(t *testing.T) {
	h := newHTTPSFixture(t, httpsOpts{authOnlyPushPost: true, acceptChunked: true,
		cover: func(gitproto.Command) (string, string, bool) { return "create-branch", "create-branch", true }})
	h.bigCommit()
	out, err := h.git("push", "origin", "HEAD:refs/heads/agent/x")
	if err == nil || !strings.Contains(out, "asked for a credential only when the push was uploaded") ||
		strings.Contains(out, "refused the credential") {
		t.Fatalf("push: %v\n%s", err, out)
	}
	if len(h.credCalls()) != 0 {
		t.Fatalf("credential calls %v", h.credCalls())
	}
}

// A push of no commands sends nothing upstream.
func TestHTTPSEmptyPushPostsNothing(t *testing.T) {
	h := newHTTPSFixture(t, httpsOpts{
		cover: func(gitproto.Command) (string, string, bool) { return "create-branch", "create-branch", true }})
	var out, errb bytes.Buffer
	rc := h.broker.serveRequest(context.Background(), request{Service: serviceReceivePack, URL: h.url}, "fixture",
		strings.NewReader("0000"), &out, &errb)
	if rc != 0 {
		t.Fatalf("rc %d stderr %q", rc, errb.String())
	}
	for _, r := range h.srv.requests() {
		if r.Method == http.MethodPost {
			t.Fatalf("an empty push was POSTed:\n%s", h.srv.log(t))
		}
	}
}

func TestHTTPSRequestOverTheCapRefused(t *testing.T) {
	saved := maxRequest
	maxRequest = 64
	t.Cleanup(func() { maxRequest = saved })
	h := newHTTPSFixture(t, httpsOpts{})
	out, err := h.git("fetch", "origin")
	if err == nil || !strings.Contains(out, "remote error: the git-broker refused a fetch request over 64 bytes") ||
		!strings.Contains(out, "fetch this on the host") {
		t.Fatalf("fetch: %v\n%s", err, out)
	}
	for _, r := range h.srv.requests() {
		if r.Method == http.MethodPost {
			t.Fatalf("an over-cap request was POSTed:\n%s", h.srv.log(t))
		}
	}
}

func TestHTTPSAdvertisementOverTheCapRefused(t *testing.T) {
	saved := maxAdvertisement
	maxAdvertisement = 16
	t.Cleanup(func() { maxAdvertisement = saved })
	h := newHTTPSFixture(t, httpsOpts{})
	out, err := h.git("fetch", "origin")
	if err == nil || !strings.Contains(out, "is over 16 bytes") {
		t.Fatalf("fetch: %v\n%s", err, out)
	}
	h.localCommit()
	out, err = h.git("push", "origin", "HEAD:main")
	if err == nil || !strings.Contains(out, "push advertisement is over 16 bytes") {
		t.Fatalf("push: %v\n%s", err, out)
	}
}

// A creation under the create-branch grant crosses HTTPS whole, lands in the bare repository,
// and joins the working set from the upstream's own `ok` report (docs/design/git-broker-plan.md
// task 9).
func TestCreateBranchOverHTTPS(t *testing.T) {
	h := newHTTPSFixture(t, httpsOpts{grants: []brokerscope.Grant{{Kind: "create-branch", Repo: brokerscope.AnyRepo}}})
	allowKinds(t, "create-branch")
	h.localCommit()
	out, err := h.git("push", "origin", "HEAD:refs/heads/agent/x")
	t.Logf("requests:\n%s", h.srv.log(t))
	if err != nil {
		t.Fatalf("git push: %v\n%s\nbroker log:\n%s", err, out, h.log.String())
	}
	if got, want := mustGit(t, h.bare, "rev-parse", "agent/x"), mustGit(t, h.ws, "rev-parse", "HEAD"); got != want {
		t.Fatalf("bare agent/x %s, local HEAD %s", got, want)
	}
	live := liveEntries(t)
	if len(live) != 1 || live[0].Repo != h.host+"/o/r" || live[0].Resource != "refs/heads/agent/x" || live[0].By != "create-branch" {
		t.Fatalf("live entries %+v", live)
	}
	// The next push is an update the entry covers, POSTed as the creation was.
	h.commit("two")
	if out, err := h.git("push", "origin", "HEAD:refs/heads/agent/x"); err != nil {
		t.Fatalf("second push: %v\n%s", err, out)
	}
	if p := lastPush(t); p.Set != "working-set" || len(p.Refs) != 1 || p.Refs[0].CoveredBy != live[0].ID {
		t.Fatalf("update audit %+v", p)
	}
}

// bigCommit commits a file of random bytes larger than pushBufferMax, so its push streams.
func (h *httpsFixture) bigCommit() {
	h.t.Helper()
	b := make([]byte, pushBufferMax+pushBufferMax/2)
	if _, err := rand.Read(b); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.ws, "big"), b, 0o644); err != nil {
		h.t.Fatal(err)
	}
	mustGit(h.t, h.ws, "add", "big")
	mustGit(h.t, h.ws, "commit", "-q", "-m", "big")
}

// pushPosts is the server's receive-pack POSTs.
func (h *httpsFixture) pushPosts() []seenRequest {
	var posts []seenRequest
	for _, r := range h.srv.requests() {
		if r.Method == http.MethodPost && strings.HasSuffix(strings.SplitN(r.URI, "?", 2)[0], "/git-receive-pack") {
			posts = append(posts, r)
		}
	}
	return posts
}

// A push of at most 1 MiB is sent with a Content-Length, so a server or CGI host that refuses
// a chunked body (this one answers 411) takes it.
func TestHTTPSSmallPushSentWithLength(t *testing.T) {
	h := newHTTPSFixture(t, httpsOpts{
		cover: func(gitproto.Command) (string, string, bool) { return "create-branch", "create-branch", true }})
	h.localCommit()
	if out, err := h.git("push", "origin", "HEAD:refs/heads/agent/x"); err != nil {
		t.Fatalf("push to a server that refuses chunked bodies: %v\n%s\nrequests:\n%s", err, out, h.srv.log(t))
	}
	if got, want := mustGit(t, h.bare, "rev-parse", "agent/x"), mustGit(t, h.ws, "rev-parse", "HEAD"); got != want {
		t.Fatalf("bare agent/x %s, local HEAD %s", got, want)
	}
	posts := h.pushPosts()
	if len(posts) != 1 || posts[0].Chunked {
		t.Fatalf("push POSTs %+v, want one with a length", posts)
	}
}

// A push over 1 MiB streams chunked, as git's own client streams one.
func TestHTTPSLargePushStreamsChunked(t *testing.T) {
	h := newHTTPSFixture(t, httpsOpts{acceptChunked: true,
		cover: func(gitproto.Command) (string, string, bool) { return "create-branch", "create-branch", true }})
	h.bigCommit()
	if out, err := h.git("push", "origin", "HEAD:refs/heads/agent/x"); err != nil {
		t.Fatalf("push: %v\n%s\nrequests:\n%s", err, out, h.srv.log(t))
	}
	if got, want := mustGit(t, h.bare, "rev-parse", "agent/x"), mustGit(t, h.ws, "rev-parse", "HEAD"); got != want {
		t.Fatalf("bare agent/x %s, local HEAD %s", got, want)
	}
	posts := h.pushPosts()
	if len(posts) != 1 || !posts[0].Chunked {
		t.Fatalf("push POSTs %+v, want one chunked", posts)
	}
}
