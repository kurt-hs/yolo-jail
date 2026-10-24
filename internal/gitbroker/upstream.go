package gitbroker

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/mschulkind-oss/yolo-jail/internal/gitproto"
)

// upstream.go is the leg from the broker to the remote: which transports it carries, the
// host ssh it runs and the environment it runs it in (docs/design/git-broker-plan.md ID-7,
// docs/design/git-broker.md §4.3). The URL it connects to is always the approved scope entry,
// which the request matched exactly, never anything else the jail sent.

const (
	transportSSH   = "ssh"
	transportHTTPS = "https"
)

// transportOf says which upstream an approved remote URL takes, or why the broker carries
// none. Only ssh://, https:// and scp-style [user@]host:path remotes cross: git's
// `<transport>::<address>` remote-helper form (ext::, fd::, …), file://, http://, git://, any
// other scheme and a local path are refused, as is anything starting with "-" that ssh could
// read as an option. The scope file accepts any transport string; this is where it stops.
func transportOf(remote string) (string, error) {
	if strings.HasPrefix(remote, "-") {
		return "", errors.New("it starts with \"-\", which the host's ssh would read as an option")
	}
	if i := strings.Index(remote, "::"); i > 0 && isURLSchemeChars(remote[:i]) {
		return "", fmt.Errorf("it is git's %s:: remote-helper form, which runs a program on the host", remote[:i])
	}
	if scheme, _, ok := strings.Cut(remote, "://"); ok {
		switch strings.ToLower(scheme) {
		case "ssh", "https":
		default:
			return "", fmt.Errorf("it uses %s://", scheme)
		}
		if _, err := gitproto.Identity(remote); err != nil {
			return "", err
		}
		if strings.EqualFold(scheme, "https") {
			return transportHTTPS, nil
		}
		return transportSSH, nil
	}
	if _, err := gitproto.Identity(remote); err != nil {
		return "", err
	}
	if _, path, _ := strings.Cut(remote, ":"); strings.HasPrefix(path, "-") {
		return "", errors.New("its path starts with \"-\", which git-upload-pack would read as an option")
	}
	return transportSSH, nil
}

// isURLSchemeChars is git's is_urlschemechar over s: a letter first, then letters, digits,
// "+", "-" and ".". git reads `<s>::<address>` as a remote helper when s is one.
func isURLSchemeChars(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		alpha := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
		if i == 0 && !alpha {
			return false
		}
		if !alpha && !(c >= '0' && c <= '9') && c != '+' && c != '-' && c != '.' {
			return false
		}
	}
	return s != ""
}

// sshTarget is an SSH remote taken apart the way git's connect.c does.
type sshTarget struct {
	dest string // [user@]host, as ssh's destination argument
	port string
	path string // the path git sends the remote command, unquoted
}

// parseSSH takes an ssh:// or scp-style remote apart. For ssh://, a path starting "/~" loses
// its leading slash, as git's own does; an scp-style path is used as written.
func parseSSH(remote string) (sshTarget, error) {
	if _, err := transportOf(remote); err != nil {
		return sshTarget{}, err
	}
	var t sshTarget
	if strings.Contains(remote, "://") {
		u, err := url.Parse(remote)
		if err != nil {
			return t, err
		}
		if !strings.EqualFold(u.Scheme, "ssh") {
			return t, fmt.Errorf("%s:// is not an SSH remote", u.Scheme)
		}
		t.dest = u.Hostname()
		if u.User != nil && u.User.Username() != "" {
			t.dest = u.User.Username() + "@" + t.dest
		}
		t.port = u.Port()
		t.path = u.Path
		if strings.HasPrefix(t.path, "/~") {
			t.path = t.path[1:]
		}
	} else {
		colon := strings.IndexByte(remote, ':')
		t.dest, t.path = remote[:colon], remote[colon+1:]
	}
	switch {
	case t.dest == "" || strings.HasPrefix(t.dest, "-"):
		return t, errors.New("its host is empty or starts with \"-\"")
	case t.path == "" || strings.HasPrefix(t.path, "-"):
		return t, errors.New("its path is empty or starts with \"-\"")
	}
	return t, nil
}

// sshArgv is the argv after `ssh` for service against remote (ID-7): no forwarding of any
// kind, no local command, a connect timeout, and no BatchMode, so a key needing a touch or a
// passphrase prompts on the host. The remote command is git's own spelling, the path
// single-quoted.
func sshArgv(remote, service, protocol string) ([]string, error) {
	t, err := parseSSH(remote)
	if err != nil {
		return nil, err
	}
	argv := []string{"-x", "-T", "-o", "ClearAllForwardings=yes", "-o", "PermitLocalCommand=no", "-o", "ConnectTimeout=30"}
	if t.port != "" {
		argv = append(argv, "-p", t.port)
	}
	if validProtocol(protocol) {
		argv = append(argv, "-o", "SendEnv=GIT_PROTOCOL")
	}
	return append(argv, "--", t.dest, service+" "+sqQuote(t.path)), nil
}

// sqQuote single-quotes s as git's sq_quote_buf does: a quote becomes '\” and a "!"
// becomes '\!', so the remote shell reads s as one word whatever it holds. shquote.Quote is
// not used: it leaves a safe word bare, and git, and so every server's forced command,
// expects the quotes.
func sqQuote(s string) string {
	var b strings.Builder
	b.WriteByte('\'')
	for _, r := range s {
		switch r {
		case '\'':
			b.WriteString(`'\''`)
		case '!':
			b.WriteString(`'\!'`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('\'')
	return b.String()
}

// validProtocol says p is a GIT_PROTOCOL value the broker passes on: version=2 or version=1,
// nothing else. git sends a connect-only helper none (the plan's pitfalls), so in practice
// this is always empty; a value from the jail is never passed through unchecked.
func validProtocol(p string) bool { return p == "version=2" || p == "version=1" }

// sshEnv is the host ssh's environment, built from nothing (ID-7): the host's PATH, HOME and
// USER, the launching session's agent socket, and what a key prompt on the host needs. No
// variable of the broker's own environment reaches it otherwise, so no token the host
// session holds (GH_TOKEN, AWS_*) does.
func sshEnv(getenv func(string) string, protocol string) []string {
	env := []string{"PATH=" + getenv("PATH"), "HOME=" + getenv("HOME")}
	for _, k := range []string{"USER", "SSH_AUTH_SOCK", "DISPLAY", "WAYLAND_DISPLAY", "SSH_ASKPASS", "SSH_ASKPASS_REQUIRE"} {
		if v := getenv(k); v != "" {
			env = append(env, k+"="+v)
		}
	}
	if validProtocol(protocol) {
		env = append(env, "GIT_PROTOCOL="+protocol)
	}
	return env
}

// lookPath finds name on pathEnv, as exec.LookPath does on the process's own PATH.
func lookPath(name, pathEnv string) (string, error) {
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}
		p := filepath.Join(dir, name)
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", exec.ErrNotFound
}

// upstream is one running host ssh.
type upstream struct {
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	stdout     *bufio.Reader
	stderrDone chan struct{}
	tail       *tailBuffer
}

// startUpstream runs the host program at path with argv and env, in dir. Its stderr is
// passed to the jail's stderr as it arrives, and its tail kept for the broker's own message.
func startUpstream(ctx context.Context, path string, argv, env []string, dir string, stderr io.Writer) (*upstream, error) {
	cmd := exec.CommandContext(ctx, path, argv...)
	cmd.Env = env
	cmd.Dir = dir
	cmd.SysProcAttr = childProcAttr()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	errPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	u := &upstream{cmd: cmd, stdin: stdin, stdout: bufio.NewReaderSize(stdout, 64<<10),
		stderrDone: make(chan struct{}), tail: &tailBuffer{max: 4096}}
	go func() {
		defer close(u.stderrDone)
		_, _ = io.Copy(io.MultiWriter(stderr, u.tail), errPipe)
	}()
	return u, nil
}

// wait returns the program's exit code once its stderr is drained. The caller has read its
// stdout to the end, or stopped needing it.
func (u *upstream) wait() int {
	<-u.stderrDone
	err := u.cmd.Wait()
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() >= 0 {
		return ee.ExitCode()
	}
	return 1
}

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	mu  sync.Mutex
	max int
	b   []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.b = append(t.b, p...)
	if len(t.b) > t.max {
		t.b = t.b[len(t.b)-t.max:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.b)
}
