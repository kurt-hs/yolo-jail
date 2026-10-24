package gitremote

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/frameproto"
	"github.com/mschulkind-oss/yolo-jail/internal/hostservice"
	"github.com/mschulkind-oss/yolo-jail/internal/svcendpoint"
)

// serve starts an in-test loopback-TLS endpoint running handler and returns its endpoint file.
func serve(t *testing.T, handler hostservice.Handler) string {
	t.Helper()
	t.Setenv(svcendpoint.AdvertiseHostEnv, "127.0.0.1")
	dir, err := os.MkdirTemp("/tmp", "yj-gitremote-test-")
	if err != nil {
		t.Fatal(err)
	}
	endpoint := filepath.Join(dir, "git-broker.endpoint")
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = hostservice.ServeEndpoint(handler, endpoint, stop)
	}()
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(endpoint); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Cleanup(func() {
		close(stop)
		<-done
		_ = os.RemoveAll(dir)
	})
	return endpoint
}

func envOf(vars map[string]string, stdin string, stdout, stderr *bytes.Buffer) Env {
	return Env{
		Getenv: func(k string) string { return vars[k] },
		Stdin:  strings.NewReader(stdin),
		Stdout: stdout,
		Stderr: stderr,
	}
}

func TestHelperAdvertisesConnectOnly(t *testing.T) {
	var out, errb bytes.Buffer
	rc := Run([]string{"origin", "ssh://git@github.com/o/r"}, envOf(nil, "capabilities\n", &out, &errb))
	if rc != 0 || out.String() != "connect\n\n" {
		t.Fatalf("rc %d stdout %q stderr %q", rc, out.String(), errb.String())
	}
}

func TestHelperConnectRelaysBothWays(t *testing.T) {
	gotRequest := make(chan string, 1)
	endpoint := serve(t, func(s *hostservice.Session) {
		var parts []string
		for _, k := range s.Request.Keys() {
			v, _ := s.Request.Get(k)
			parts = append(parts, fmt.Sprintf("%q:%q", k, v))
		}
		gotRequest <- "{" + strings.Join(parts, ",") + "}"
		for {
			f, err := frameproto.ReadFrame(s.Conn())
			if err != nil {
				s.Exit(1)
				return
			}
			if len(f.Payload) == 0 {
				s.Exit(0)
				return
			}
			s.StdoutBytes(append([]byte("echo:"), f.Payload...))
		}
	})
	var out, errb bytes.Buffer
	vars := map[string]string{EndpointEnv: endpoint, "GIT_PROTOCOL": "version=2"}
	rc := Run([]string{"--", "origin", "ssh://git@github.com/o/r"},
		envOf(vars, "capabilities\nconnect git-upload-pack\nhello", &out, &errb))
	if rc != 0 {
		t.Fatalf("rc %d stderr %q", rc, errb.String())
	}
	want := `{"service":"git-upload-pack","url":"ssh://git@github.com/o/r","protocol":"version=2"}`
	if got := <-gotRequest; got != want {
		t.Fatalf("request frame %s, want %s", got, want)
	}
	if out.String() != "connect\n\n"+"\n"+"echo:hello" {
		t.Fatalf("stdout %q", out.String())
	}
}

func TestHelperRefusalHasNoConnectedLine(t *testing.T) {
	endpoint := serve(t, func(s *hostservice.Session) {
		s.Stderr("not in this workspace's approved scope\n")
		s.Exit(77)
	})
	var out, errb bytes.Buffer
	rc := Run([]string{"origin", "git@other.test:o/r.git"},
		envOf(map[string]string{EndpointEnv: endpoint}, "capabilities\nconnect git-receive-pack\n", &out, &errb))
	if rc != 77 {
		t.Fatalf("rc %d stderr %q", rc, errb.String())
	}
	if out.String() != "connect\n\n" {
		t.Fatalf("stdout after the capabilities %q", out.String())
	}
	if !strings.Contains(errb.String(), "not in this workspace's approved scope") {
		t.Fatalf("stderr %q", errb.String())
	}
}

func TestHelperNoEndpointNamesTheSwitch(t *testing.T) {
	if EndpointEnv != "YOLO_SERVICE_GIT_BROKER_ENDPOINT" {
		t.Fatalf("EndpointEnv %q", EndpointEnv)
	}
	var out, errb bytes.Buffer
	rc := Run([]string{"origin", "git@github.com:o/r.git"},
		envOf(map[string]string{"YOLO_HOST_DIR": "/home/u/app"}, "capabilities\nconnect git-upload-pack\n", &out, &errb))
	if rc != 69 {
		t.Fatalf("rc %d", rc)
	}
	if !strings.Contains(errb.String(), "yolo loopholes enable git-broker --workspace /home/u/app") {
		t.Fatalf("stderr %q", errb.String())
	}
	if out.String() != "connect\n\n" {
		t.Fatalf("stdout %q", out.String())
	}
}

// Only upload-pack and receive-pack cross; anything else is refused without dialing.
func TestHelperRefusesOtherServicesAndCommands(t *testing.T) {
	for _, in := range []string{"connect git-upload-archive\n", "list\n", "stateless-connect git-upload-pack\n"} {
		var out, errb bytes.Buffer
		rc := Run([]string{"origin", "ssh://git@github.com/o/r"}, envOf(nil, in, &out, &errb))
		if rc == 0 || out.Len() != 0 || !strings.Contains(errb.String(), "git-remote-yolo") {
			t.Errorf("%q: rc %d stdout %q stderr %q", in, rc, out.String(), errb.String())
		}
	}
}

// A blank line from git ends the helper cleanly.
func TestHelperBlankLineEnds(t *testing.T) {
	var out, errb bytes.Buffer
	if rc := Run(nil, envOf(nil, "capabilities\n\n", &out, &errb)); rc != 0 || out.String() != "connect\n\n" {
		t.Fatalf("rc %d stdout %q stderr %q", rc, out.String(), errb.String())
	}
}
