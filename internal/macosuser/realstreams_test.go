package macosuser

import (
	"bytes"
	"io"
	"os"
	"testing"
)

func TestRunStreamsToKeepsNamedChildStreamsSeparate(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := RunStreamsTo(&stdout, &stderr)([]string{"/bin/sh", "-c", "printf COMMAND-OUT; printf LAUNCH-ERR >&2"})
	if rc != 0 {
		t.Fatalf("RunStreamsTo exit = %d", rc)
	}
	if got := stdout.String(); got != "COMMAND-OUT" {
		t.Errorf("stdout = %q, want COMMAND-OUT", got)
	}
	if got := stderr.String(); got != "LAUNCH-ERR" {
		t.Errorf("stderr = %q, want LAUNCH-ERR", got)
	}
}

func TestRunStreamsToNilKeepsProcessStreams(t *testing.T) {
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previousStdout, previousStderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = stdoutW, stderrW
	defer func() {
		os.Stdout, os.Stderr = previousStdout, previousStderr
		_ = stdoutR.Close()
		_ = stdoutW.Close()
		_ = stderrR.Close()
		_ = stderrW.Close()
	}()

	rc := RunStreamsTo(nil, nil)([]string{"/bin/sh", "-c", "printf INHERITED-OUT; printf INHERITED-ERR >&2"})
	os.Stdout, os.Stderr = previousStdout, previousStderr
	if err := stdoutW.Close(); err != nil {
		t.Fatal(err)
	}
	if err := stderrW.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(stdoutR)
	if err != nil {
		t.Fatal(err)
	}
	errout, err := io.ReadAll(stderrR)
	if err != nil {
		t.Fatal(err)
	}
	if rc != 0 {
		t.Fatalf("RunStreamsTo(nil, nil) exit = %d", rc)
	}
	if string(out) != "INHERITED-OUT" || string(errout) != "INHERITED-ERR" {
		t.Errorf("inherited stdout=%q stderr=%q, want INHERITED-OUT / INHERITED-ERR", out, errout)
	}
}
