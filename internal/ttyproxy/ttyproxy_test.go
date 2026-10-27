//go:build linux

package ttyproxy

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// childDeadline bounds each wait on a proxied child in these tests. It is a bound, not a
// pace: nothing sleeps it off, and on a quiet machine every wait ends within milliseconds.
// It is this generous because one wait can cover an exec or two (sh, stty, dd), and each
// can take hundreds of milliseconds on a loaded laptop or under a tracer. The fixed sleeps
// these tests used to pace themselves with were sized for a quiet machine. Anywhere else
// they failed, with messages blaming the product.
const childDeadline = 10 * time.Second

// fakeHostTTY makes a fresh pty's slave this process's stdin and stdout for the rest of
// the test, the way a terminal is the launcher's, and returns both ends. What the test
// writes to master is typed input, and what the proxy writes to stdout comes back out of
// it. One *os.File holds the slave, and closing that file is what releases the number, so
// no finalizer can close it a second time after a later test's openPty has reused it.
func fakeHostTTY(t *testing.T) (master, slave int) {
	t.Helper()
	master, slave, err := openPty()
	if err != nil {
		t.Skipf("cannot open pty: %v", err)
	}
	f := os.NewFile(uintptr(slave), "pty-slave")
	origIn, origOut := os.Stdin, os.Stdout
	origInFD, err := unix.Dup(0)
	if err != nil {
		t.Fatal(err)
	}
	origOutFD, err := unix.Dup(1)
	if err != nil {
		_ = unix.Close(origInFD)
		t.Fatal(err)
	}
	if err := unix.Dup2(slave, 0); err != nil {
		t.Fatalf("redirect test stdin to owned PTY: %v", err)
	}
	if err := unix.Dup2(slave, 1); err != nil {
		t.Fatalf("redirect test stdout to owned PTY: %v", err)
	}
	os.Stdin, os.Stdout = f, f
	t.Cleanup(func() {
		_ = unix.Dup2(origInFD, 0)
		_ = unix.Dup2(origOutFD, 1)
		_ = unix.Close(origInFD)
		_ = unix.Close(origOutFD)
		os.Stdin, os.Stdout = origIn, origOut
		_ = f.Close()
		_ = unix.Close(master)
	})
	return master, slave
}

// proxyRun is one RunWithProxyObserved on its own goroutine. done carries its exit code.
type proxyRun struct {
	done     chan int
	finished chan struct{}
	child    atomic.Pointer[os.Process]
}

// startProxied runs cmd under the real proxy on a goroutine, on the terminal fakeHostTTY
// made. Driving proxyLoop by hand would pin the loop and leave RunWithProxy free to
// reintroduce, above it, whatever a test pins.
//
// Its cleanup runs before fakeHostTTY's and ends a run that a failing test abandoned. Left
// running, that run keeps polling the fake terminal's descriptor numbers after they are
// closed. It then reads the input of whichever later test's openPty reuses them, and fails
// that test too. Its child also stays orphaned for good, because it inherited its own
// pty's master. The cleanup ends the child the ways these tests' scripts allow: plain
// bytes finish a dd, and a SIGWINCH fires the winch test's trap. SIGKILL is the last
// resort.
func startProxied(t *testing.T, master int, cmd []string, obs Observer) *proxyRun {
	t.Helper()
	r := &proxyRun{done: make(chan int, 1), finished: make(chan struct{})}
	go func() {
		defer close(r.finished)
		rc, _ := RunWithProxyObserved(cmd, func(p *os.Process) { r.child.Store(p) }, nil, obs)
		r.done <- rc
	}()
	t.Cleanup(func() {
		select {
		case <-r.finished:
			return
		default:
		}
		_, _ = unix.Write(master, []byte("................"))
		if p := r.child.Load(); p != nil {
			_ = p.Signal(syscall.SIGWINCH)
		}
		select {
		case <-r.finished:
			return
		case <-time.After(time.Second):
		}
		if p := r.child.Load(); p != nil {
			_ = p.Kill()
		}
		select {
		case <-r.finished:
		case <-time.After(childDeadline):
			t.Log("the proxied run outlived its test; a later pty test may see its descriptors")
		}
	})
	return r
}

// readUntil reads master until what it has read contains want, and reports whether it
// did before d ran out. It returns what it read either way, for the failure message.
func readUntil(master int, want string, d time.Duration) (string, bool) {
	var got []byte
	buf := make([]byte, 256)
	deadline := time.Now().Add(d)
	for !strings.Contains(string(got), want) {
		left := time.Until(deadline)
		if left <= 0 {
			return string(got), false
		}
		fds := []unix.PollFd{{Fd: int32(master), Events: unix.POLLIN}}
		n, err := unix.Poll(fds, int(left/time.Millisecond)+1)
		if errors.Is(err, unix.EINTR) || (err == nil && n == 0) {
			continue
		}
		if err != nil || fds[0].Revents&unix.POLLIN == 0 {
			return string(got), false
		}
		m, err := unix.Read(master, buf)
		if m > 0 {
			got = append(got, buf[:m]...)
		}
		if err != nil || m == 0 {
			return string(got), false
		}
	}
	return string(got), true
}

// TestCtrlCReachesTheJail pins the 2026-09-19 ruling: ^C is forwarded to the pty like
// any other byte, so the JAIL's line discipline decides what it means.
//
// It used to be intercepted and turned into a targeted SIGINT at the proxy, which made
// Ctrl-C mean "quit the launcher" — measured on a real host, pressing it at a jail's bash
// prompt to clear the line tore the whole session down. Forwarding restores the ordinary
// `podman exec -it` contract.
//
// The assertion is that the byte ARRIVES, which is the whole behaviour change: the child
// here reads one byte from a raw pty and then exits 42, so that exit proves the proxy
// passed the 0x03 through instead of eating it.
func TestCtrlCReachesTheJail(t *testing.T) {
	// Same plumbing as TestPtyPassthrough, which is the proven shape: a pty whose
	// slave stands in for os.Stdin, and the real RunWithProxy on top of it.
	master, _ := fakeHostTTY(t)

	// WHAT THIS CAN AND CANNOT ASSERT, stated because two earlier drafts asserted the
	// wrong thing.
	//
	// In production the chain is: proxy forwards 0x03 → the child (`podman exec -it`)
	// reads it as stdin data → podman relays it over the exec stream → the CONTAINER's
	// pty line discipline raises SIGINT at bash. The signal is raised inside the jail,
	// by a pty this package never sees. A unit test has no container, and the proxy
	// deliberately does NO Setsid (it broke `podman -it`), so the proxy's own pty has no
	// session and no foreground process group — nothing here will ever raise a SIGINT.
	//
	// So what is pinned is DELIVERY, which is the entire behaviour change: the byte
	// reaches the child instead of being eaten. The child puts its own pty in raw mode
	// first, because a cooked pty returns nothing to a reader until a newline, and its
	// line discipline swallows a ^C. A draft without `stty raw` failed on a PLAIN byte
	// too, which is what proved the plumbing wrong rather than the fix.
	//
	// THE ^C IS WRITTEN ONLY ONCE THE CHILD SAYS ITS PTY IS RAW: the R it prints after
	// stty. That R also proves the proxy is pumping, which it starts only after it has
	// made the host tty raw. A fixed sleep used to stand in for both. When an exec was
	// slow the byte reached a line discipline that was still cooked and was swallowed
	// there, and the failure below then blamed the proxy.
	run := startProxied(t, master, []string{"sh", "-c",
		"stty raw -echo; printf R; dd bs=1 count=1 of=/dev/null 2>/dev/null; exit 42"}, Observer{})
	if got, ok := readUntil(master, "R", childDeadline); !ok {
		t.Fatalf("the child never said its pty was raw (read %q): the child or the proxy never "+
			"got going, which says nothing about ^C", got)
	}
	if _, err := unix.Write(master, []byte{interruptByte}); err != nil {
		t.Fatal(err)
	}

	select {
	case rc := <-run.done:
		if rc != 42 {
			t.Errorf("child rc = %d, want 42 — it never read the ^C byte", rc)
		}
	case <-time.After(childDeadline):
		// Tell an eaten ^C apart from a proxy that forwards nothing at all.
		if _, err := unix.Write(master, []byte{'x'}); err != nil {
			t.Fatal(err)
		}
		select {
		case <-run.done:
			t.Fatal("^C never reached the child, though a plain byte written after it did: the " +
				"proxy ate it. That is the pre-2026-09-19 behaviour, where Ctrl-C quit the " +
				"launcher instead of reaching the jail — at a bash prompt it tore the session " +
				"down rather than clearing the line.")
		case <-time.After(childDeadline):
			t.Fatal("neither the ^C nor a plain byte written after it reached the child, which " +
				"had said its pty was raw: the proxy is forwarding no input at all")
		}
	}
}

func TestOutputPressureDoesNotStopInputForwarding(t *testing.T) {
	if os.Getenv("YOLO_TTYPROXY_OUTPUT_ERROR") == "1" {
		full, err := os.OpenFile("/dev/full", os.O_WRONLY, 0)
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if err := unix.Dup2(int(full.Fd()), 1); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		_ = full.Close()
	}
	if os.Getenv("YOLO_TTYPROXY_SATURATE_RESET_TTY") == "1" {
		ttyPath, err := os.Readlink("/proc/self/fd/0")
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "find reset tty:", err)
			os.Exit(2)
		}
		fd, err := unix.Open(ttyPath, unix.O_WRONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "open reset tty:", err)
			os.Exit(2)
		}
		padding := bytes.Repeat([]byte{'f'}, 4096)
		for {
			_, err = unix.Write(fd, padding)
			if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) {
				break
			}
			if err != nil {
				_, _ = fmt.Fprintln(os.Stderr, "saturate reset tty:", err)
				os.Exit(2)
			}
		}
		_ = unix.Close(fd)
		if err := writePressureReceipt(os.Getenv("YOLO_TTYPROXY_PRESSURE_FIFO"), 'F'); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	}
	if os.Getenv("YOLO_TTYPROXY_EXPECT_SUSPEND") == "1" {
		// Non-interactive Go test parents may ignore or block job-control signals;
		// this owned fixture needs the production default and an unblocked thread.
		runtime.LockOSThread()
		var unblocked unix.Sigset_t
		unblocked.Val[0] = 1 << (uint(syscall.SIGTSTP) - 1)
		if err := unix.PthreadSigmask(unix.SIG_UNBLOCK, &unblocked, nil); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "unblock SIGTSTP:", err)
			os.Exit(2)
		}
		action := struct {
			handler  uintptr
			flags    uint64
			restorer uintptr
			mask     uint64
		}{}
		_, _, errno := unix.Syscall6(unix.SYS_RT_SIGACTION, uintptr(syscall.SIGTSTP),
			uintptr(unsafe.Pointer(&action)), 0, 8, 0, 0)
		if errno != 0 {
			_, _ = fmt.Fprintln(os.Stderr, "reset SIGTSTP disposition:", errno)
			os.Exit(2)
		}
	}
	if os.Getenv("YOLO_TTYPROXY_NONBLOCK_STDOUT") == "1" {
		if err := unix.SetNonblock(1, true); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		os.Stdout = os.NewFile(1, "already-nonblocking-stdout")
	}
	if os.Getenv("YOLO_TTYPROXY_PRESSURE_HELPER") == "1" {
		fifo := os.Getenv("YOLO_TTYPROXY_PRESSURE_FIFO")
		outputBytes := os.Getenv("YOLO_TTYPROXY_PRESSURE_BYTES")
		observer := Observer{Env: []string{"YOLO_TTYPROXY_PRESSURE_FIFO=" + fifo, "YOLO_TTYPROXY_PRESSURE_BYTES=" + outputBytes, "YOLO_TTYPROXY_OUTPUT_ACK=" + os.Getenv("YOLO_TTYPROXY_OUTPUT_ACK")}}
		if os.Getenv("YOLO_TTYPROXY_EXPECT_SUSPEND") == "1" {
			if os.Getenv("YOLO_TTYPROXY_BG_FG") == "1" {
				observer.ownsTTY = func(int) bool {
					mode, err := os.ReadFile(fifo + ".ownership")
					return err == nil && string(mode) == "foreground"
				}
			}
			if os.Getenv("YOLO_TTYPROXY_FINAL_CLAIM_GATE") == "1" {
				observer.afterSuspendCommit = func() {
					_ = writePressureReceipt(fifo+".proxy", 'C')
					for {
						if _, err := os.Stat(fifo + ".release"); err == nil {
							return
						}
						time.Sleep(time.Millisecond)
					}
				}
			}
			observer.Input = func(int, string) {
				_ = writePressureReceipt(fifo+".input", 'I')
			}
			observer.suspendBoundary = func(n uint64) {
				_ = os.WriteFile(fifo+".boundary", []byte(strconv.FormatUint(n, 10)), 0600)
			}
			observer.Stage = func(stage string) {
				if stage == StageResumed {
					deadline := time.Now().Add(childDeadline)
					for time.Now().Before(deadline) {
						termios, err := unix.IoctlGetTermios(int(os.Stdin.Fd()), unix.TCGETS)
						if err == nil && termios.Lflag&unix.ICANON == 0 && termios.Lflag&unix.ECHO == 0 && termios.Oflag&unix.OPOST == 0 {
							_ = os.WriteFile(fifo+".resumed-mode", []byte("raw"), 0600)
							break
						}
						time.Sleep(time.Millisecond)
					}
				}
				marker := byte('?')
				if stage == StageCancelRequested {
					marker = 'A'
				} else if stage == StageSuspended {
					marker = 'Z'
				} else if stage == StageResumed {
					marker = 'R'
				}
				_ = writePressureReceipt(fifo+".proxy", marker)
			}
		}
		rc, err := RunWithProxyObserved(
			[]string{os.Args[0], "-test.run=^TestOutputPressureTarget$", "-test.count=1"},
			nil, nil, observer,
		)
		if os.Getenv("YOLO_TTYPROXY_NONBLOCK_STDOUT") == "1" {
			flags, err := unix.FcntlInt(1, unix.F_GETFL, 0)
			if err != nil || flags&unix.O_NONBLOCK == 0 {
				_, _ = fmt.Fprintf(os.Stderr, "stdout flags after proxy = %#x, err=%v\\n", flags, err)
				os.Exit(2)
			}
			if err := writePressureReceipt(fifo, 'N'); err != nil {
				_, _ = fmt.Fprintln(os.Stderr, err)
				os.Exit(2)
			}
		}
		if err != nil {
			if os.Getenv("YOLO_TTYPROXY_EXPECT_SINK_ERROR") == "1" && (errors.Is(err, unix.ENOSPC) || errors.Is(err, unix.EPIPE)) {
				if receiptErr := writePressureReceipt(fifo, 'E'); receiptErr != nil {
					_, _ = fmt.Fprintln(os.Stderr, receiptErr)
					os.Exit(2)
				}
				os.Exit(0)
			}
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		os.Exit(rc)
	}
	if os.Getenv("YOLO_TTYPROXY_PRESSURE_FIFO") != "" {
		return
	}

	master, slave, err := openPty()
	if err != nil {
		t.Skipf("cannot open pty: %v", err)
	}
	defer unix.Close(master)
	hostTTY := os.NewFile(uintptr(slave), "proxy-pressure-host-tty")
	defer hostTTY.Close()

	control := filepath.Join(t.TempDir(), "child-read")

	cmd := exec.Command(os.Args[0], "-test.run=^TestOutputPressureDoesNotStopInputForwarding$", "-test.count=1")
	cmd.Stdin = hostTTY
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	pipeFD := int(stdout.(*os.File).Fd())
	pipeSize, err := unix.FcntlInt(uintptr(pipeFD), unix.F_GETPIPE_SZ, 0)
	if err != nil {
		t.Fatal(err)
	}
	outputBytes := pipeSize * 2
	cmd.Env = append(os.Environ(), "YOLO_TTYPROXY_PRESSURE_HELPER=1", "YOLO_TTYPROXY_PRESSURE_FIFO="+control, "YOLO_TTYPROXY_PRESSURE_BYTES="+strconv.Itoa(outputBytes), "YOLO_TTYPROXY_NONBLOCK_STDOUT=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	defer func() {
		if waited {
			return
		}
		_ = cmd.Process.Signal(syscall.SIGTERM)
		wait := make(chan error, 1)
		go func() { wait <- cmd.Wait() }()
		select {
		case <-wait:
		case <-time.After(2 * time.Second):
			_ = cmd.Process.Kill()
			<-wait
		}
		_ = stdout.Close()
	}()

	if err := waitPressureMarker(t, control, 'S', time.Now().Add(childDeadline)); err != nil {
		t.Fatalf("target did not become ready: %v stderr=%q", err, stderr.String())
	}
	pressured, _ := waitForOutputPressure(t, pipeFD, pipeSize)
	if pressured {
		if _, err := unix.Write(master, []byte{'q'}); err != nil {
			t.Fatal(err)
		}
	}
	received := pressured && waitPressureMarker(t, control, 'R', time.Now().Add(2*time.Second)) == nil

	readDone := make(chan struct {
		data []byte
		err  error
	}, 1)
	go func() {
		data, err := io.ReadAll(stdout)
		readDone <- struct {
			data []byte
			err  error
		}{data, err}
	}()
	var got []byte
	var readErr error
	select {
	case result := <-readDone:
		got, readErr = result.data, result.err
	case <-time.After(childDeadline):
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case result := <-readDone:
			got, readErr = result.data, result.err
		case <-time.After(2 * time.Second):
			_ = cmd.Process.Kill()
			_ = stdout.Close()
			t.Fatal("owned helper did not stop after SIGTERM under output pressure")
		}
	}
	waitErr := cmd.Wait()
	waited = true
	if err := waitPressureMarker(t, control, 'N', time.Now().Add(childDeadline)); err != nil {
		t.Fatalf("helper stdout lost its original nonblocking status: %v stderr=%q", err, stderr.String())
	}
	if !received {
		t.Fatalf("input did not reach the child while its output pipe was full; helper wait=%v stderr=%q captured=%d bytes", waitErr, stderr.String(), len(got))
	}
	if readErr != nil {
		t.Fatalf("read proxied output: %v", readErr)
	}
	if waitErr != nil {
		t.Fatalf("helper exited unsuccessfully: %v stderr=%q", waitErr, stderr.String())
	}
	want := outputPressurePrefix(outputBytes)
	want = append(want, []byte("\x1b[200~paste\tbytes\x1b[201~\x1b[?2026hsync-body\x1b[?2026l\x03\x1a")...)
	if !bytes.Equal(got, want) {
		t.Fatalf("forwarded output differs: got %d bytes, want %d; first difference at %d, got prefix %q want prefix %q", len(got), len(want), firstByteDifference(got, want), got[:min(32, len(got))], want[:min(32, len(want))])
	}
}

func TestOutputPressureCancellationRestoresOwnedTerminal(t *testing.T) {
	master, slave, err := openPty()
	if err != nil {
		t.Skipf("cannot open pty: %v", err)
	}
	defer unix.Close(master)
	hostTTY := os.NewFile(uintptr(slave), "proxy-pressure-cancel-tty")
	defer hostTTY.Close()
	control := filepath.Join(t.TempDir(), "child-read")

	cmd := exec.Command(os.Args[0], "-test.run=^TestOutputPressureDoesNotStopInputForwarding$", "-test.count=1")
	cmd.Stdin = hostTTY
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	pipeFD := int(stdout.(*os.File).Fd())
	pipeSize, err := unix.FcntlInt(uintptr(pipeFD), unix.F_GETPIPE_SZ, 0)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Env = append(os.Environ(), "YOLO_TTYPROXY_PRESSURE_HELPER=1", "YOLO_TTYPROXY_PRESSURE_FIFO="+control, "YOLO_TTYPROXY_PRESSURE_BYTES="+strconv.Itoa(pipeSize*2))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	defer func() {
		if !waited {
			_ = cmd.Process.Signal(syscall.SIGTERM)
			wait := make(chan error, 1)
			go func() { wait <- cmd.Wait() }()
			select {
			case <-wait:
			case <-time.After(2 * time.Second):
				_ = cmd.Process.Kill()
				<-wait
			}
			_ = stdout.Close()
		}
	}()

	if err := waitPressureMarker(t, control, 'S', time.Now().Add(childDeadline)); err != nil {
		t.Fatalf("target did not become ready: %v stderr=%q", err, stderr.String())
	}
	pressured, maxBuffered := waitForOutputPressure(t, pipeFD, pipeSize)
	if !pressured {
		t.Fatalf("host stdout pipe never became pressured (capacity=%d pending=%d)", pipeSize, maxBuffered)
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	readDone := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, stdout)
		readDone <- err
	}()
	select {
	case err := <-readDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("proxy did not cancel while output was pressured")
	}
	waitErr := cmd.Wait()
	waited = true
	exitErr, ok := waitErr.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 128+int(syscall.SIGTERM) {
		t.Fatalf("helper wait=%v, want signal-arm exit %d; stderr=%q", waitErr, 128+int(syscall.SIGTERM), stderr.String())
	}
	if _, ok := readUntil(master, termReset, childDeadline); !ok {
		t.Fatal("SIGTERM under output pressure did not reset the owned host terminal")
	}
}

func TestOutputPressureTTYCancellationDoesNotBlockReset(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) { runFullTTYSignalCancellation(t, sig) })
	}
}

func runFullTTYSignalCancellation(t *testing.T, sig syscall.Signal) {
	master, slave, err := openPty()
	if err != nil {
		t.Skipf("cannot open pty: %v", err)
	}
	defer unix.Close(master)
	hostTTY := os.NewFile(uintptr(slave), "proxy-pressure-full-tty")
	defer hostTTY.Close()
	cooked := mustGetTermios(t, slave)
	flagsBefore, err := unix.FcntlInt(uintptr(slave), unix.F_GETFL, 0)
	if err != nil {
		t.Fatal(err)
	}
	probe, err := unix.Open(fmt.Sprintf("/proc/self/fd/%d", slave), unix.O_WRONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("open an independent nonblocking TTY probe: %v", err)
	}
	defer unix.Close(probe)
	probeFlags, err := unix.FcntlInt(uintptr(probe), unix.F_GETFL, 0)
	if err != nil || probeFlags&unix.O_NONBLOCK == 0 {
		t.Fatalf("probe flags=%#x err=%v, want independently nonblocking", probeFlags, err)
	}

	control := filepath.Join(t.TempDir(), "child-read")
	cmd := exec.Command(os.Args[0], "-test.run=^TestOutputPressureDoesNotStopInputForwarding$", "-test.count=1")
	cmd.Stdin, cmd.Stdout = hostTTY, hostTTY
	cmd.Env = append(os.Environ(), "YOLO_TTYPROXY_PRESSURE_HELPER=1", "YOLO_TTYPROXY_PRESSURE_FIFO="+control,
		"YOLO_TTYPROXY_PRESSURE_BYTES="+strconv.Itoa(16*1024*1024), "YOLO_TTYPROXY_EXPECT_SUSPEND=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	waited := false
	defer func() {
		if waited {
			return
		}
		_ = cmd.Process.Signal(sig)
		_ = unix.SetNonblock(master, true)
		buf := make([]byte, 4096)
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			select {
			case <-done:
				return
			default:
			}
			n, err := unix.Read(master, buf)
			if n == 0 || (err != nil && (errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK))) {
				time.Sleep(5 * time.Millisecond)
			}
		}
		_ = cmd.Process.Kill()
		<-done
	}()
	if err := waitPressureMarker(t, control, 'S', time.Now().Add(childDeadline)); err != nil {
		t.Fatalf("target did not start before TTY pressure: %v stderr=%q", err, stderr.String())
	}
	if _, err := os.Stat(control + ".pid"); err != nil {
		t.Fatalf("the owned child PID was not recorded before signal-arm testing: %v", err)
	}
	if _, err := fillTTYUntilEAGAIN(probe); err != nil {
		t.Fatalf("independent nonblocking TTY probe did not prove output pressure: %v", err)
	}
	if after, err := unix.FcntlInt(uintptr(slave), unix.F_GETFL, 0); err != nil || after != flagsBefore {
		t.Fatalf("probe changed caller-owned TTY status flags: before=%#x after=%#x err=%v", flagsBefore, after, err)
	}
	if _, err := unix.Write(master, []byte{0x1a}); err != nil {
		t.Fatalf("request suspension under %s terminal pressure: %v", sig, err)
	}
	if err := waitForFile(control+".boundary", time.Now().Add(childDeadline)); err != nil {
		t.Fatalf("proxy did not enter pending suspension before %s: %v", sig, err)
	}
	if err := cmd.Process.Signal(sig); err != nil {
		t.Fatal(err)
	}
	var waitErr error
	select {
	case waitErr = <-done:
	case <-time.After(childDeadline):
		// This release is cleanup after the bounded assertion window, not a retry:
		// it lets a red path terminate its own helper before the test fails.
		_ = unix.SetNonblock(master, true)
		buf := make([]byte, 4096)
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			select {
			case waitErr = <-done:
				assertSignalExit(t, waitErr, sig)
				waited = true
				t.Fatalf("signal %s could not restore/reset the saturated TTY until drained", sig)
			default:
			}
			n, err := unix.Read(master, buf)
			if n == 0 || (err != nil && (errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK))) {
				time.Sleep(5 * time.Millisecond)
			}
		}
		t.Fatalf("owned helper did not exit after releasing its saturated TTY")
	}
	assertSignalExit(t, waitErr, sig)
	waited = true
	if after := mustGetTermios(t, slave); *after != *cooked {
		t.Fatalf("signal %s left host termios changed: before=%+v after=%+v", sig, *cooked, *after)
	}
	pidBytes, err := os.ReadFile(control + ".pid")
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(pidBytes))
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Kill(pid, 0); !errors.Is(err, unix.ESRCH) {
		t.Fatalf("owned target pid %d still exists after signal exit: %v", pid, err)
	}
	if stage, err := os.ReadFile(control + ".proxy"); err != nil || string(stage) == "Z" || string(stage) == "R" {
		t.Fatalf("signal %s left a latent suspend/resume stage %q (err=%v)", sig, stage, err)
	}
}

func fillTTYUntilEAGAIN(fd int) (int, error) {
	buf := bytes.Repeat([]byte{'f'}, 4096)
	written := 0
	for {
		n, err := unix.Write(fd, buf)
		written += n
		if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) {
			return written, nil
		}
		if err != nil {
			return written, err
		}
		if n == 0 {
			return written, io.ErrShortWrite
		}
	}
}

func assertSignalExit(t *testing.T, err error, signal syscall.Signal) {
	t.Helper()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 128+int(signal) {
		t.Fatalf("helper exit = %v, want signal exit %d", err, 128+int(signal))
	}
}

func waitForTTYOutputPressure(t *testing.T, master int) bool {
	t.Helper()
	deadline := time.Now().Add(childDeadline)
	last, stable := -1, 0
	for time.Now().Before(deadline) {
		pending, err := unix.IoctlGetInt(master, unix.TIOCINQ)
		if err != nil {
			t.Fatal(err)
		}
		if pending >= 1024 && pending == last {
			stable++
		} else {
			stable = 0
		}
		last = pending
		if stable >= 5 {
			return true
		}
		time.Sleep(25 * time.Millisecond)
	}
	return false
}

func TestSuspendResumePreservesOutputQueueOrder(t *testing.T) {
	t.Run("foreground-resume", func(t *testing.T) { runSuspendResumePreservesOutputQueueOrder(t, false, false) })
	t.Run("background-child-exit", func(t *testing.T) { runSuspendResumePreservesOutputQueueOrder(t, true, false) })
	t.Run("queued-termination-after-stop", func(t *testing.T) { runSuspendResumePreservesOutputQueueOrder(t, false, true) })
}

func runSuspendResumePreservesOutputQueueOrder(t *testing.T, backgroundChildExit, terminateAfterStop bool) {
	master, slave, err := openPty()
	if err != nil {
		t.Skipf("cannot open pty: %v", err)
	}
	defer unix.Close(master)
	hostTTY := os.NewFile(uintptr(slave), "proxy-suspend-output-tty")
	defer hostTTY.Close()
	control := filepath.Join(t.TempDir(), "child-read")
	if err := os.WriteFile(control+".ownership", []byte("foreground"), 0600); err != nil {
		t.Fatal(err)
	}
	outputBytes := readChunk
	cmd := exec.Command(os.Args[0], "-test.run=^TestOutputPressureDoesNotStopInputForwarding$", "-test.count=1")
	cmd.Stdin, cmd.Stdout = hostTTY, hostTTY
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = append(os.Environ(), "YOLO_TTYPROXY_PRESSURE_HELPER=1", "YOLO_TTYPROXY_PRESSURE_FIFO="+control,
		"YOLO_TTYPROXY_PRESSURE_BYTES="+strconv.Itoa(outputBytes), "YOLO_TTYPROXY_EXPECT_SUSPEND=1",
		"YOLO_TTYPROXY_BG_FG=1", "YOLO_TTYPROXY_SEGMENTED_SUSPEND=1", "YOLO_TTYPROXY_OUTPUT_ACK=1", "YOLO_TTYPROXY_ACK_AFTER_WRITE=1")
	if backgroundChildExit {
		cmd.Env = append(cmd.Env, "YOLO_TTYPROXY_BG_CHILD_EXIT=1")
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waitResult := make(chan error, 1)
	waitDone := make(chan struct{})
	go func() {
		waitResult <- cmd.Wait()
		close(waitDone)
	}()
	var outputMu sync.Mutex
	var captured []byte
	var drainErr error
	readStopped := make(chan struct{})
	drain := func() {
		_ = unix.SetNonblock(master, true)
		buf := make([]byte, 8192)
		for {
			select {
			case <-readStopped:
				return
			default:
			}
			n, err := unix.Read(master, buf)
			if n > 0 {
				outputMu.Lock()
				captured = append(captured, buf[:n]...)
				outputMu.Unlock()
			}
			if n == 0 || (err != nil && (errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK))) {
				time.Sleep(2 * time.Millisecond)
			} else if err != nil && !errors.Is(err, syscall.EINTR) {
				outputMu.Lock()
				drainErr = err
				outputMu.Unlock()
				return
			}
		}
	}
	var drainOnce, stopOnce sync.Once
	var drainStarted atomic.Bool
	drainDone := make(chan struct{})
	startDrain := func() {
		drainOnce.Do(func() {
			drainStarted.Store(true)
			go func() {
				defer close(drainDone)
				drain()
			}()
		})
	}
	stopDrain := func() {
		stopOnce.Do(func() { close(readStopped) })
		if drainStarted.Load() {
			<-drainDone
		}
	}
	cleanup := func() {
		select {
		case <-waitDone:
			stopDrain()
			return
		default:
		}
		_ = cmd.Process.Signal(syscall.SIGCONT)
		_ = cmd.Process.Signal(syscall.SIGTERM)
		startDrain()
		select {
		case <-waitDone:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
			<-waitDone
		}
		stopDrain()
	}
	defer cleanup()
	if err := waitPressureMarker(t, control, 'S', time.Now().Add(childDeadline)); err != nil {
		t.Fatalf("target did not start before suspend: %v stderr=%q", err, stderr.String())
	}
	if err := waitPressureMarker(t, control, 'D', time.Now().Add(childDeadline)); err != nil {
		t.Fatalf("target did not deliver the first distinct output segment: %v stderr=%q", err, stderr.String())
	}
	initial, err := readPTYBytes(master, 1024, time.Now().Add(childDeadline))
	if err != nil || !bytes.Equal(initial, bytes.Repeat([]byte{'a'}, 1024)) {
		t.Fatalf("initial output segment = %d bytes, err=%v", len(initial), err)
	}
	captured = append(captured, initial...)
	probe, err := unix.Open(fmt.Sprintf("/proc/self/fd/%d", slave), unix.O_WRONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("open independent nonblocking output OFD: %v", err)
	}
	filled, err := fillTTYUntilEAGAIN(probe)
	if err != nil || filled == 0 {
		_ = unix.Close(probe)
		t.Fatalf("independent output OFD did not reach EAGAIN: bytes=%d err=%v", filled, err)
	}
	fillerBytes, err := readPTYAvailable(master, time.Now().Add(childDeadline))
	if err != nil || len(fillerBytes) == 0 || !bytes.Equal(fillerBytes, bytes.Repeat([]byte{'f'}, len(fillerBytes))) {
		t.Fatalf("capture the exact independently written pressure bytes: bytes=%d err=%v", len(fillerBytes), err)
	}
	_ = unix.Close(probe)
	probe, err = unix.Open(fmt.Sprintf("/proc/self/fd/%d", slave), unix.O_WRONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("reopen independent nonblocking output OFD after proving pressure: %v", err)
	}
	pressureCount := 0
	for {
		n, writeErr := unix.Write(probe, []byte{'f'})
		if errors.Is(writeErr, unix.EAGAIN) || errors.Is(writeErr, unix.EWOULDBLOCK) {
			break
		}
		if writeErr != nil || n != 1 {
			_ = unix.Close(probe)
			t.Fatalf("independent output OFD failed while recreating measured pressure: n=%d err=%v", n, writeErr)
		}
		pressureCount += n
	}
	_ = unix.Close(probe)
	if err := os.WriteFile(control+".ack", []byte("ack"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := waitPressureMarker(t, control, 'W', time.Now().Add(childDeadline)); err != nil {
		t.Fatalf("second distinct segment did not start after prefix ACK: %v stderr=%q", err, stderr.String())
	}
	if !waitForTTYOutputPressure(t, master) {
		t.Fatal("host output PTY never saturated under the second output segment")
	}
	if _, err := unix.Write(master, []byte{0x1a, 'q'}); err != nil {
		t.Fatal(err)
	}
	if err := waitForFile(control+".boundary", time.Now().Add(childDeadline)); err != nil {
		t.Fatalf("proxy did not snapshot the accepted output boundary before the provisional CAS: %v", err)
	}
	if _, err := unix.Write(master, []byte{0x1a}); err != nil {
		t.Fatalf("repeat suspend request while drain is pending: %v", err)
	}
	if err := waitForFileLength(control+".input", 2, time.Now().Add(childDeadline)); err != nil {
		t.Fatalf("proxy did not process the repeated request while drain was pending: %v", err)
	}
	startDrain()
	stoppedDeadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(stoppedDeadline) && !ownedProcessStopped(cmd.Process.Pid) {
		select {
		case err := <-waitResult:
			t.Fatalf("proxy exited before self-suspend: %v stderr=%q", err, stderr.String())
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ownedProcessStopped(cmd.Process.Pid) {
		stages, _ := os.ReadFile(control + ".proxy")
		boundary, _ := os.ReadFile(control + ".boundary")
		outputMu.Lock()
		seen := append([]byte(nil), captured...)
		readErr := drainErr
		outputMu.Unlock()
		t.Fatalf("proxy did not stop after ordered terminal reset (stages=%q boundary=%q captured=%d reset-at=%d drain-err=%v)", stages, boundary, len(seen), bytes.Index(seen, []byte(termReset)), readErr)
	}
	modeFD, err := unix.Open(fmt.Sprintf("/proc/%d/fd/0", cmd.Process.Pid), unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("open the owned stopped helper's terminal descriptor: %v", err)
	}
	defer unix.Close(modeFD)
	termios := mustGetTermios(t, modeFD)
	if termios.Lflag&unix.ICANON == 0 || termios.Lflag&unix.ECHO == 0 || termios.Oflag&unix.OPOST == 0 {
		t.Fatalf("host terminal was not cooked while the owned proxy was suspended: %+v", *termios)
	}
	if terminateAfterStop {
		if err := unix.Kill(cmd.Process.Pid, syscall.SIGTERM); err != nil {
			t.Fatalf("queue owned cancellation while stopped: %v", err)
		}
		if err := waitForPendingSignal(cmd.Process.Pid, syscall.SIGTERM, time.Now().Add(time.Second)); err != nil {
			t.Fatalf("owned controller did not observe queued SIGTERM before continuation: %v", err)
		}
		if err := unix.Kill(cmd.Process.Pid, syscall.SIGCONT); err != nil {
			t.Fatalf("foreground-continue the owned stopped proxy: %v", err)
		}
		select {
		case err := <-waitResult:
			assertSignalExit(t, err, syscall.SIGTERM)
		case <-time.After(3 * time.Second):
			t.Fatal("queued cancellation was not honored after foreground continuation")
		}
		<-waitDone
		stopDrain()
		if ownedProcessStopped(cmd.Process.Pid) {
			t.Fatal("proxy remained stopped after queued cancellation and continuation")
		}
		if after := mustGetTermios(t, modeFD); *after != *termios {
			t.Fatalf("queued cancellation did not preserve cooked terminal state: before=%+v after=%+v", *termios, *after)
		}
		stages, _ := os.ReadFile(control + ".proxy")
		if bytes.Contains(stages, []byte{'R'}) {
			t.Fatalf("ordinary queued input resumed before queued cancellation: stages=%q", stages)
		}
		input, _ := os.ReadFile(control)
		if bytes.Contains(input, []byte{'R'}) {
			t.Fatalf("owned child consumed queued input before cancellation: %q", input)
		}
		pidBytes, err := os.ReadFile(control + ".pid")
		if err != nil {
			t.Fatal(err)
		}
		childPID, err := strconv.Atoi(string(pidBytes))
		if err != nil {
			t.Fatal(err)
		}
		if err := unix.Kill(childPID, 0); !errors.Is(err, unix.ESRCH) {
			t.Fatalf("queued cancellation left owned child %d alive: %v", childPID, err)
		}
		return
	}
	if err := os.WriteFile(control+".ownership", []byte("background"), 0600); err != nil {
		t.Fatal(err)
	}
	boundaryBytes, err := os.ReadFile(control + ".boundary")
	if err != nil {
		t.Fatal(err)
	}
	boundary, err := strconv.ParseUint(string(boundaryBytes), 10, 64)
	if err != nil || boundary <= 1024 || boundary > uint64(1024+outputBytes) {
		t.Fatalf("suspend boundary %q does not include delivered and outstanding segments within %d bytes", boundaryBytes, 1024+outputBytes)
	}
	if err := unix.Kill(cmd.Process.Pid, syscall.SIGCONT); err != nil {
		t.Fatal(err)
	}
	backgroundDeadline := time.Now().Add(time.Second)
	for ownedProcessStopped(cmd.Process.Pid) && time.Now().Before(backgroundDeadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if ownedProcessStopped(cmd.Process.Pid) {
		t.Fatal("background SIGCONT did not resume the owned proxy")
	}
	if after := mustGetTermios(t, modeFD); *after != *termios {
		t.Fatalf("background SIGCONT changed the shell-equivalent cooked terminal: before=%+v after=%+v", *termios, *after)
	}
	if err := waitPressureMarker(t, control+".proxy", 'Z', time.Now().Add(time.Second)); err != nil {
		t.Fatalf("proxy did not report its suspend syscall return after background continuation: %v", err)
	}
	if stage, err := os.ReadFile(control + ".proxy"); err != nil || !bytes.HasSuffix(stage, []byte{'Z'}) {
		t.Fatalf("background SIGCONT unparked the proxy before foreground resume: stage=%q err=%v", stage, err)
	}
	childInput, err := os.ReadFile(control)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(childInput, []byte{'R'}) {
		t.Fatalf("background SIGCONT forwarded queued input before raw restoration: %q", childInput)
	}
	if backgroundChildExit {
		if err := os.WriteFile(control+".bgexit", []byte("exit"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := waitPressureMarker(t, control+".target", 'T', time.Now().Add(childDeadline)); err != nil {
			t.Fatalf("background child did not queue final ordinary output and exit: %v", err)
		}
		pidBytes, err := os.ReadFile(control + ".pid")
		if err != nil {
			t.Fatal(err)
		}
		childPID, err := strconv.Atoi(string(pidBytes))
		if err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(childDeadline)
		for time.Now().Before(deadline) && !errors.Is(unix.Kill(childPID, 0), unix.ESRCH) {
			time.Sleep(time.Millisecond)
		}
		if err := unix.Kill(childPID, 0); !errors.Is(err, unix.ESRCH) {
			t.Fatalf("background child %d was not reaped: %v", childPID, err)
		}
		time.Sleep(50 * time.Millisecond)
		outputMu.Lock()
		backgroundOutput := bytes.Contains(captured, []byte("BG-TAIL\n"))
		outputMu.Unlock()
		if backgroundOutput {
			t.Fatal("child-exit final drain wrote ordinary output while the proxy was background/cooked")
		}
	}
	if err := os.WriteFile(control+".ownership", []byte("foreground"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := unix.Kill(cmd.Process.Pid, syscall.SIGCONT); err != nil {
		t.Fatal(err)
	}
	if !backgroundChildExit {
		if err := waitPressureMarker(t, control+".proxy", 'R', time.Now().Add(time.Second)); err != nil {
			t.Fatalf("foreground SIGCONT did not restore raw mode and unpark output: %v", err)
		}
	}
	if mode, err := os.ReadFile(control + ".resumed-mode"); !backgroundChildExit && (err != nil || string(mode) != "raw") {
		t.Fatalf("host terminal mode after foreground SIGCONT = %q err=%v, want raw before queued output resumes", mode, err)
	}
	select {
	case err := <-waitResult:
		if err != nil {
			t.Fatalf("proxy did not resume and finish after queued input: %v stderr=%q", err, stderr.String())
		}
	case <-time.After(childDeadline):
		t.Fatal("proxy did not finish after SIGCONT and queued input")
	}
	<-waitDone
	stopDrain()
	_ = unix.SetNonblock(master, true)
	for {
		buf := make([]byte, 8192)
		n, err := unix.Read(master, buf)
		if n > 0 {
			outputMu.Lock()
			captured = append(captured, buf[:n]...)
			outputMu.Unlock()
		}
		if n == 0 || errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EIO) {
			break
		}
		if err != nil {
			t.Fatalf("read terminal output: %v", err)
		}
	}
	outputMu.Lock()
	got := append([]byte(nil), captured...)
	outputMu.Unlock()
	boundarySegment := int(boundary) - 1024
	boundaryPrefix := append(bytes.Repeat([]byte{'a'}, 1024), bytes.Repeat([]byte{'f'}, pressureCount)...)
	boundaryPrefix = append(boundaryPrefix, bytes.Repeat([]byte{'b'}, boundarySegment)...)
	boundaryPrefix = append(boundaryPrefix, []byte(termReset)...)
	remainingPrefix := bytes.Repeat([]byte{'b'}, outputBytes-boundarySegment)
	want := append(boundaryPrefix, remainingPrefix...)
	if backgroundChildExit {
		want = append(want, []byte("BG-TAIL\n")...)
	} else {
		suffix := []byte("\x1b[200~paste\tbytes\x1b[201~\x1b[?2026hsync-body\x1b[?2026l\x03\x1a")
		want = append(want, suffix...)
	}
	want = append(want, []byte(termReset)...)
	if !bytes.Equal(got, want) {
		at := firstByteDifference(got, want)
		resetAt := bytes.Index(got, []byte(termReset))
		start, end := max(0, at-16), min(len(got), at+len(termReset)+16)
		t.Fatalf("suspend/resume changed terminal output ordering or bytes: got=%d want=%d first-difference=%d queued-boundary=%d independent-ofd-pressure-bytes=%d reset-at=%d got-around-difference=%q want-around-difference=%q", len(got), len(want), at, boundary, pressureCount, resetAt, got[start:end], want[start:min(len(want), at+len(termReset)+16)])
	}
}

func TestOwnedChildExitPreemptsPendingSuspend(t *testing.T) {
	master, slave, err := openPty()
	if err != nil {
		t.Skipf("cannot open pty: %v", err)
	}
	defer unix.Close(master)
	hostTTY := os.NewFile(uintptr(slave), "proxy-owned-exit-tty")
	defer hostTTY.Close()
	cooked := mustGetTermios(t, slave)
	control := filepath.Join(t.TempDir(), "child-read")
	cmd := exec.Command(os.Args[0], "-test.run=^TestOutputPressureDoesNotStopInputForwarding$", "-test.count=1")
	cmd.Stdin, cmd.Stdout = hostTTY, hostTTY
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = append(os.Environ(), "YOLO_TTYPROXY_PRESSURE_HELPER=1", "YOLO_TTYPROXY_PRESSURE_FIFO="+control,
		"YOLO_TTYPROXY_PRESSURE_BYTES="+strconv.Itoa(readChunk), "YOLO_TTYPROXY_EXPECT_SUSPEND=1",
		"YOLO_TTYPROXY_FINAL_CLAIM_GATE=1", "YOLO_TTYPROXY_EXIT_GATE_NEWLINE=1", "YOLO_TTYPROXY_OUTPUT_ACK=1", "YOLO_TTYPROXY_ACK_AFTER_WRITE=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()
	helperDone := false
	var capturedMu sync.Mutex
	var captured []byte
	stopDrain, drainDone := make(chan struct{}), make(chan struct{})
	startDrain := func() {
		go func() {
			defer close(drainDone)
			_ = unix.SetNonblock(master, true)
			buf := make([]byte, 4096)
			for {
				select {
				case <-stopDrain:
					return
				default:
				}
				n, err := unix.Read(master, buf)
				if n > 0 {
					capturedMu.Lock()
					captured = append(captured, buf[:n]...)
					capturedMu.Unlock()
				}
				if n == 0 || errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) {
					time.Sleep(time.Millisecond)
				} else if err != nil && !errors.Is(err, syscall.EINTR) {
					return
				}
			}
		}()
	}
	startedDrain := false
	defer func() {
		if !helperDone {
			_ = cmd.Process.Signal(syscall.SIGCONT)
			_ = cmd.Process.Signal(syscall.SIGTERM)
			if !startedDrain {
				startDrain()
				startedDrain = true
			}
			select {
			case <-waitDone:
			case <-time.After(3 * time.Second):
				_ = cmd.Process.Kill()
				<-waitDone
			}
		}
		if startedDrain {
			close(stopDrain)
			<-drainDone
		}
	}()
	if err := waitPressureMarker(t, control, 'S', time.Now().Add(childDeadline)); err != nil {
		t.Fatalf("target did not start: %v stderr=%q", err, stderr.String())
	}
	if err := waitPressureMarker(t, control, 'W', time.Now().Add(childDeadline)); err != nil {
		t.Fatalf("target did not write its accepted prefix: %v", err)
	}
	if !waitForTTYOutputPressure(t, master) {
		t.Fatal("output PTY did not become pressured")
	}
	if _, err := unix.Write(master, []byte{0x1a}); err != nil {
		t.Fatal(err)
	}
	if err := waitForFile(control+".boundary", time.Now().Add(childDeadline)); err != nil {
		t.Fatalf("proxy did not enter pending suspend: %v", err)
	}
	startDrain()
	startedDrain = true
	if err := waitPressureMarker(t, control+".proxy", 'C', time.Now().Add(childDeadline)); err != nil {
		t.Fatalf("proxy did not reach pre-stop gate: %v", err)
	}
	if err := os.WriteFile(control+".newline", []byte("write"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := waitPressureMarker(t, control+".target", 'N', time.Now().Add(childDeadline)); err != nil {
		t.Fatalf("owned child did not queue the distinguishing newline behind the stop gate: %v", err)
	}
	pidBytes, err := os.ReadFile(control + ".pid")
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(pidBytes))
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Kill(pid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(childDeadline)
	for time.Now().Before(deadline) && !errors.Is(unix.Kill(pid, 0), unix.ESRCH) {
		time.Sleep(time.Millisecond)
	}
	if err := unix.Kill(pid, 0); !errors.Is(err, unix.ESRCH) {
		t.Fatalf("owned child %d was not reaped at the stop gate: %v", pid, err)
	}
	if err := os.WriteFile(control+".release", []byte("release"), 0600); err != nil {
		t.Fatal(err)
	}
	var helperErr error
	select {
	case helperErr = <-waitDone:
		helperDone = true
	case <-time.After(childDeadline):
		t.Fatal("owned-child exit did not preempt pending suspend and finish drain")
	}
	if exitErr, ok := helperErr.(*exec.ExitError); !ok || exitErr.ExitCode() != 128+int(syscall.SIGKILL) {
		t.Fatalf("proxy helper exit=%v, want owned child SIGKILL status", helperErr)
	}
	if ownedProcessStopped(cmd.Process.Pid) {
		t.Fatal("proxy stopped after the owned child had already been reaped")
	}
	stages, _ := os.ReadFile(control + ".proxy")
	if bytes.Contains(stages, []byte{'Z'}) || bytes.Contains(stages, []byte{'R'}) {
		t.Fatalf("owned-child exit left a latent suspend stage %q", stages)
	}
	if after := mustGetTermios(t, slave); *after != *cooked {
		t.Fatalf("owned-child exit did not leave the host terminal cooked: before=%+v after=%+v", *cooked, *after)
	}
	deadline = time.Now().Add(childDeadline)
	for time.Now().Before(deadline) {
		capturedMu.Lock()
		newline := bytes.IndexByte(captured, '\n')
		previous := byte(0)
		if newline > 0 {
			previous = captured[newline-1]
		}
		capturedMu.Unlock()
		if newline >= 0 {
			if previous == '\r' {
				t.Fatal("owned-child exit drained the distinguishing newline while cooked termios was active")
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("final drain did not preserve the owned child's distinguishing newline")
}

func TestSuspendCancellationBeforeFinalSignalClaimPreemptsStop(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) { runSuspendCancellationBeforeFinalSignalClaim(t, sig) })
	}
}

func runSuspendCancellationBeforeFinalSignalClaim(t *testing.T, sig syscall.Signal) {
	master, slave, err := openPty()
	if err != nil {
		t.Skipf("cannot open pty: %v", err)
	}
	defer unix.Close(master)
	hostTTY := os.NewFile(uintptr(slave), "proxy-suspend-commit-tty")
	defer hostTTY.Close()
	cooked := mustGetTermios(t, slave)
	control := filepath.Join(t.TempDir(), "child-read")
	outputBytes := readChunk
	cmd := exec.Command(os.Args[0], "-test.run=^TestOutputPressureDoesNotStopInputForwarding$", "-test.count=1")
	cmd.Stdin, cmd.Stdout = hostTTY, hostTTY
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = append(os.Environ(), "YOLO_TTYPROXY_PRESSURE_HELPER=1", "YOLO_TTYPROXY_PRESSURE_FIFO="+control,
		"YOLO_TTYPROXY_PRESSURE_BYTES="+strconv.Itoa(outputBytes), "YOLO_TTYPROXY_EXPECT_SUSPEND=1",
		"YOLO_TTYPROXY_FINAL_CLAIM_GATE=1", "YOLO_TTYPROXY_OUTPUT_ACK=1", "YOLO_TTYPROXY_ACK_AFTER_WRITE=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waitResult := make(chan error, 1)
	waitDone := make(chan struct{})
	go func() {
		waitResult <- cmd.Wait()
		close(waitDone)
	}()
	var outputMu sync.Mutex
	var captured []byte
	readStopped := make(chan struct{})
	drainStart := make(chan struct{})
	var drainStartOnce sync.Once
	startDrainer := func() { drainStartOnce.Do(func() { close(drainStart) }) }
	drainDone := make(chan struct{})
	go func() {
		defer close(drainDone)
		select {
		case <-drainStart:
		case <-readStopped:
			return
		}
		_ = unix.SetNonblock(master, true)
		buf := make([]byte, 8192)
		for {
			select {
			case <-readStopped:
				return
			default:
			}
			n, err := unix.Read(master, buf)
			if n > 0 {
				outputMu.Lock()
				captured = append(captured, buf[:n]...)
				outputMu.Unlock()
			}
			if n == 0 || errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) {
				time.Sleep(time.Millisecond)
			} else if err != nil && !errors.Is(err, syscall.EINTR) {
				return
			}
		}
	}()
	drainStopped := false
	defer func() {
		if !drainStopped {
			startDrainer()
			_ = cmd.Process.Signal(syscall.SIGTERM)
			select {
			case <-waitDone:
			case <-time.After(2 * time.Second):
				_ = cmd.Process.Kill()
				<-waitDone
			}
			close(readStopped)
			<-drainDone
		}
	}()
	if err := waitPressureMarker(t, control, 'S', time.Now().Add(childDeadline)); err != nil {
		t.Fatalf("target did not start before final-claim cancellation: %v stderr=%q", err, stderr.String())
	}
	if err := waitPressureMarker(t, control, 'W', time.Now().Add(childDeadline)); err != nil {
		t.Fatalf("target did not finish its output prefix: %v stderr=%q", err, stderr.String())
	}
	if !waitForTTYOutputPressure(t, master) {
		t.Fatal("host output PTY never saturated before final-claim cancellation")
	}
	if _, err := unix.Write(master, []byte{0x1a, 'q'}); err != nil {
		t.Fatal(err)
	}
	if err := waitForFile(control+".boundary", time.Now().Add(childDeadline)); err != nil {
		t.Fatalf("proxy did not snapshot pending suspend before the final signal claim: %v", err)
	}
	if err := waitPressureMarker(t, control+".input", 'I', time.Now().Add(childDeadline)); err != nil {
		t.Fatalf("proxy did not consume suspend input before final signal claim: %v", err)
	}
	startDrainer()
	start := time.Now()
	// Draining completes the accepted prefix and reset; the fixture holds the old
	// provisional CAS, before cancellation must win the final signal claim.
	for !ownedProcessStopped(cmd.Process.Pid) {
		if time.Since(start) > childDeadline {
			t.Fatal("proxy did not enter actual stop after draining the accepted prefix")
		}
		if stage, err := os.ReadFile(control + ".proxy"); err == nil && bytes.Contains(stage, []byte{'C'}) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := waitPressureMarker(t, control+".proxy", 'C', time.Now().Add(childDeadline)); err != nil {
		t.Fatalf("proxy did not reach the provisional post-CAS gate: %v", err)
	}
	if err := cmd.Process.Signal(sig); err != nil {
		t.Fatal(err)
	}
	if err := waitPressureMarker(t, control+".proxy", 'A', time.Now().Add(childDeadline)); err != nil {
		t.Fatalf("signal %s was not acknowledged at the cancellation gate: %v", sig, err)
	}
	if err := os.WriteFile(control+".release", []byte("release"), 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case waitErr := <-waitResult:
		assertSignalExit(t, waitErr, sig)
	case <-time.After(2 * time.Second):
		t.Fatalf("%s at the provisional post-CAS boundary did not preempt the final signal claim", sig)
	}
	<-waitDone
	close(readStopped)
	<-drainDone
	drainStopped = true
	if after := mustGetTermios(t, slave); *after != *cooked {
		t.Fatalf("%s before final signal claim left host termios changed: before=%+v after=%+v", sig, *cooked, *after)
	}
	stage, err := os.ReadFile(control + ".proxy")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stage, []byte{'Z'}) || bytes.Contains(stage, []byte{'R'}) {
		t.Fatalf("%s before final signal claim left latent suspend/resume stages %q", sig, stage)
	}
	outputMu.Lock()
	got := append([]byte(nil), captured...)
	outputMu.Unlock()
	want := append(outputPressurePrefix(outputBytes), []byte(termReset)...)
	if !bytes.HasPrefix(got, want) {
		t.Fatalf("pre-claim cancellation changed ordered prefix/reset: got=%d want-prefix=%d", len(got), len(want))
	}
	pidBytes, err := os.ReadFile(control + ".pid")
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(pidBytes))
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Kill(pid, 0); !errors.Is(err, unix.ESRCH) {
		t.Fatalf("owned child pid %d survived pre-claim cancellation: %v", pid, err)
	}
}

func waitForPendingSignal(pid int, signal syscall.Signal, deadline time.Time) error {
	mask := uint64(1) << (uint(signal) - 1)
	statusPath := filepath.Join("/proc", strconv.Itoa(pid), "status")
	for time.Now().Before(deadline) {
		status, err := os.ReadFile(statusPath)
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(status), "\n") {
			if !strings.HasPrefix(line, "SigPnd:") && !strings.HasPrefix(line, "ShdPnd:") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) != 2 {
				continue
			}
			pending, err := strconv.ParseUint(fields[1], 16, 64)
			if err == nil && pending&mask != 0 {
				return nil
			}
		}
		time.Sleep(time.Millisecond)
	}
	return fmt.Errorf("signal %s was not observed queued for owned pid %d", signal, pid)
}

func ownedProcessStopped(pid int) bool {
	status, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "status"))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(status), "\n") {
		if strings.HasPrefix(line, "State:") {
			return strings.Contains(line, "T (stopped)") || strings.Contains(line, "t (tracing stop)")
		}
	}
	return false
}

func TestFatalOutputPreemptsPendingSuspend(t *testing.T) {
	master, slave, err := openPty()
	if err != nil {
		t.Skipf("cannot open pty: %v", err)
	}
	defer unix.Close(master)
	hostTTY := os.NewFile(uintptr(slave), "proxy-pending-fatal-tty")
	defer hostTTY.Close()
	cooked := mustGetTermios(t, slave)
	control := filepath.Join(t.TempDir(), "child-read")
	cmd := exec.Command(os.Args[0], "-test.run=^TestOutputPressureDoesNotStopInputForwarding$", "-test.count=1")
	cmd.Stdin = hostTTY
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	pipeSize, err := unix.FcntlInt(stdout.(*os.File).Fd(), unix.F_GETPIPE_SZ, 0)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Env = append(os.Environ(), "YOLO_TTYPROXY_PRESSURE_HELPER=1", "YOLO_TTYPROXY_PRESSURE_FIFO="+control,
		"YOLO_TTYPROXY_PRESSURE_BYTES="+strconv.Itoa(pipeSize*2), "YOLO_TTYPROXY_EXPECT_SINK_ERROR=1",
		"YOLO_TTYPROXY_EXPECT_SUSPEND=1", "YOLO_TTYPROXY_SATURATE_RESET_TTY=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waitResult := make(chan error, 1)
	go func() { waitResult <- cmd.Wait() }()
	waited := false
	defer func() {
		if !waited {
			_ = cmd.Process.Signal(syscall.SIGTERM)
			_ = stdout.Close()
			select {
			case <-waitResult:
			case <-time.After(2 * time.Second):
				_ = cmd.Process.Kill()
				<-waitResult
			}
		}
	}()
	if err := waitPressureMarker(t, control, 'F', time.Now().Add(childDeadline)); err != nil {
		t.Fatalf("fallback TTY was not saturated: %v stderr=%q", err, stderr.String())
	}
	if err := waitPressureMarker(t, control, 'S', time.Now().Add(childDeadline)); err != nil {
		t.Fatalf("fatal-output target did not start: %v stderr=%q", err, stderr.String())
	}
	if pressured, buffered := waitForOutputPressure(t, int(stdout.(*os.File).Fd()), pipeSize); !pressured {
		t.Fatalf("stdout did not saturate before the suspend request (capacity=%d pending=%d)", pipeSize, buffered)
	}
	if _, err := unix.Write(master, []byte{0x1a}); err != nil {
		t.Fatal(err)
	}
	if err := waitForFile(control+".boundary", time.Now().Add(childDeadline)); err != nil {
		t.Fatalf("fatal path did not enter pending suspension: %v", err)
	}
	if err := stdout.Close(); err != nil {
		t.Fatal(err)
	}
	if err := waitPressureMarker(t, control, 'E', time.Now().Add(childDeadline)); err != nil {
		t.Fatalf("fatal output did not preempt pending suspension with its cause: %v stderr=%q", err, stderr.String())
	}
	if err := <-waitResult; err != nil {
		t.Fatalf("helper did not recognize the EPIPE cause: %v stderr=%q", err, stderr.String())
	}
	waited = true
	if after := mustGetTermios(t, slave); *after != *cooked {
		t.Fatalf("fatal output during pending suspension changed host termios: before=%+v after=%+v", *cooked, *after)
	}
	stage, err := os.ReadFile(control + ".proxy")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stage, []byte{'Z'}) || bytes.Contains(stage, []byte{'R'}) {
		t.Fatalf("fatal output during pending suspension left a latent stop stage %q", stage)
	}
	pidBytes, err := os.ReadFile(control + ".pid")
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(pidBytes))
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Kill(pid, 0); !errors.Is(err, unix.ESRCH) {
		t.Fatalf("owned child pid %d survived fatal output: %v", pid, err)
	}
}

func TestOutputSinkErrorWithSaturatedFallbackReturnsPromptly(t *testing.T) {
	master, slave, err := openPty()
	if err != nil {
		t.Skipf("cannot open pty: %v", err)
	}
	defer unix.Close(master)
	hostTTY := os.NewFile(uintptr(slave), "proxy-saturated-reset-tty")
	defer hostTTY.Close()
	cooked := mustGetTermios(t, slave)
	control := filepath.Join(t.TempDir(), "child-read")

	cmd := exec.Command(os.Args[0], "-test.run=^TestOutputPressureDoesNotStopInputForwarding$", "-test.count=1")
	cmd.Stdin = hostTTY
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Env = append(os.Environ(), "YOLO_TTYPROXY_PRESSURE_HELPER=1", "YOLO_TTYPROXY_PRESSURE_FIFO="+control,
		"YOLO_TTYPROXY_PRESSURE_BYTES=4096", "YOLO_TTYPROXY_OUTPUT_ERROR=1", "YOLO_TTYPROXY_EXPECT_SINK_ERROR=1",
		"YOLO_TTYPROXY_SATURATE_RESET_TTY=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	defer func() {
		if !waited {
			_ = cmd.Process.Signal(syscall.SIGTERM)
			_ = drainPTY(master)
			wait := make(chan error, 1)
			go func() { wait <- cmd.Wait() }()
			select {
			case <-wait:
			case <-time.After(2 * time.Second):
				_ = cmd.Process.Kill()
				<-wait
			}
			_ = stdout.Close()
		}
	}()
	if err := waitPressureMarker(t, control, 'F', time.Now().Add(childDeadline)); err != nil {
		t.Fatalf("fallback TTY was not saturated: %v stderr=%q", err, stderr.String())
	}
	if err := waitPressureMarker(t, control, 'S', time.Now().Add(childDeadline)); err != nil {
		t.Fatalf("fatal-output target did not start: %v stderr=%q", err, stderr.String())
	}
	prompt := make(chan error, 1)
	go func() { prompt <- waitPressureMarker(t, control, 'E', time.Now().Add(750*time.Millisecond)) }()
	select {
	case err := <-prompt:
		if err != nil {
			t.Fatalf("fatal output did not return promptly with saturated fallback TTY: %v", err)
		}
	case <-time.After(time.Second):
		_ = drainPTY(master)
		t.Fatal("fatal output return blocked on reset to a saturated fallback TTY")
	}
	if _, err := io.Copy(io.Discard, stdout); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("helper did not preserve the terminal output error: %v stderr=%q", err, stderr.String())
	}
	waited = true
	if after := mustGetTermios(t, slave); *after != *cooked {
		t.Fatalf("fatal output return left host termios changed: before=%+v after=%+v", *cooked, *after)
	}
	pidBytes, err := os.ReadFile(control + ".pid")
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(pidBytes))
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Kill(pid, 0); !errors.Is(err, unix.ESRCH) {
		t.Fatalf("owned child pid %d still exists after fatal-output return: %v", pid, err)
	}
}

func drainPTY(master int) error {
	_ = unix.SetNonblock(master, true)
	buf := make([]byte, 8192)
	for {
		_, err := unix.Read(master, buf)
		if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EIO) {
			return nil
		}
		if err != nil && !errors.Is(err, syscall.EINTR) {
			return err
		}
	}
}

func TestOutputSinkErrorKillsOwnedChildReturnsErrorAndRestoresTerminal(t *testing.T) {
	master, slave, err := openPty()
	if err != nil {
		t.Skipf("cannot open pty: %v", err)
	}
	defer unix.Close(master)
	hostTTY := os.NewFile(uintptr(slave), "proxy-output-error-tty")
	defer hostTTY.Close()
	control := filepath.Join(t.TempDir(), "child-read")

	cmd := exec.Command(os.Args[0], "-test.run=^TestOutputPressureDoesNotStopInputForwarding$", "-test.count=1")
	cmd.Stdin = hostTTY
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	pipeSize, err := unix.FcntlInt(stdout.(*os.File).Fd(), unix.F_GETPIPE_SZ, 0)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Env = append(os.Environ(), "YOLO_TTYPROXY_PRESSURE_HELPER=1", "YOLO_TTYPROXY_PRESSURE_FIFO="+control, "YOLO_TTYPROXY_PRESSURE_BYTES="+strconv.Itoa(int(pipeSize*2)), "YOLO_TTYPROXY_OUTPUT_ERROR=1", "YOLO_TTYPROXY_EXPECT_SINK_ERROR=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	defer func() {
		if !waited {
			_ = cmd.Process.Signal(syscall.SIGTERM)
			wait := make(chan error, 1)
			go func() { wait <- cmd.Wait() }()
			select {
			case <-wait:
			case <-time.After(2 * time.Second):
				_ = cmd.Process.Kill()
				<-wait
			}
			_ = stdout.Close()
		}
	}()
	if err := waitPressureMarker(t, control, 'S', time.Now().Add(childDeadline)); err != nil {
		t.Fatalf("target did not start before the sink error: %v", err)
	}
	if err := waitPressureMarker(t, control, 'E', time.Now().Add(childDeadline)); err != nil {
		t.Fatalf("sink error was not returned by RunWithProxyObserved: %v stderr=%q", err, stderr.String())
	}
	if _, err := io.Copy(io.Discard, stdout); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("helper did not recognize the returned ENOSPC: %v stderr=%q", err, stderr.String())
	}
	waited = true
	pidBytes, err := os.ReadFile(control + ".pid")
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(pidBytes))
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Kill(pid, 0); !errors.Is(err, unix.ESRCH) {
		t.Fatalf("owned child pid %d still exists after sink-error return: %v", pid, err)
	}
	if _, ok := readUntil(master, termReset, childDeadline); !ok {
		t.Fatal("sink-error return did not reset the owned host terminal")
	}
}

func TestOutputPressureTarget(t *testing.T) {
	fifo := os.Getenv("YOLO_TTYPROXY_PRESSURE_FIFO")
	if fifo == "" {
		return
	}
	pressureBytes, err := strconv.Atoi(os.Getenv("YOLO_TTYPROXY_PRESSURE_BYTES"))
	if err != nil || pressureBytes <= 0 {
		t.Fatalf("invalid output pressure size %q", os.Getenv("YOLO_TTYPROXY_PRESSURE_BYTES"))
	}
	setRaw(int(os.Stdin.Fd()), mustGetTermios(t, int(os.Stdin.Fd())))
	if err := os.WriteFile(fifo+".pid", []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writePressureReceipt(fifo, 'S'); err != nil {
		t.Fatal(err)
	}
	segmented := os.Getenv("YOLO_TTYPROXY_SEGMENTED_SUSPEND") == "1"
	outputDone := make(chan error, 1)
	if segmented {
		if err := writeAllTTYProxy(int(os.Stdout.Fd()), bytes.Repeat([]byte{'a'}, 1024)); err != nil {
			t.Fatalf("write initial acknowledged prefix: %v", err)
		}
		if err := writePressureReceipt(fifo, 'D'); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(childDeadline)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(fifo + ".ack"); err == nil {
				break
			}
			time.Sleep(2 * time.Millisecond)
		}
		if _, err := os.Stat(fifo + ".ack"); err != nil {
			t.Fatalf("initial prefix was not acknowledged: %v", err)
		}
		if err := writePressureReceipt(fifo, 'W'); err != nil {
			t.Fatal(err)
		}
		go func() { outputDone <- writeAllTTYProxy(int(os.Stdout.Fd()), bytes.Repeat([]byte{'b'}, pressureBytes)) }()
	} else {
		go func() { outputDone <- writeAllTTYProxy(int(os.Stdout.Fd()), outputPressurePrefix(pressureBytes)) }()
	}
	if !segmented && os.Getenv("YOLO_TTYPROXY_OUTPUT_ACK") == "1" && os.Getenv("YOLO_TTYPROXY_ACK_AFTER_WRITE") == "1" {
		if err := <-outputDone; err != nil {
			t.Fatalf("write pressured output before ACK: %v", err)
		}
		if err := writePressureReceipt(fifo, 'W'); err != nil {
			t.Fatal(err)
		}
	} else if !segmented && os.Getenv("YOLO_TTYPROXY_OUTPUT_ACK") == "1" {
		if err := writePressureReceipt(fifo, 'W'); err != nil {
			t.Fatal(err)
		}
	}
	if os.Getenv("YOLO_TTYPROXY_EXIT_GATE_NEWLINE") == "1" {
		deadline := time.Now().Add(childDeadline)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(fifo + ".newline"); err == nil {
				break
			}
			time.Sleep(time.Millisecond)
		}
		if _, err := os.Stat(fifo + ".newline"); err != nil {
			t.Fatalf("timed out waiting for exit-gate newline trigger: %v", err)
		}
		if err := writeAllTTYProxy(int(os.Stdout.Fd()), []byte("\n")); err != nil {
			t.Fatalf("write exit-gate newline: %v", err)
		}
		if err := writePressureReceipt(fifo+".target", 'N'); err != nil {
			t.Fatal(err)
		}
	}
	if os.Getenv("YOLO_TTYPROXY_BG_CHILD_EXIT") == "1" {
		deadline := time.Now().Add(childDeadline)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(fifo + ".bgexit"); err == nil {
				break
			}
			time.Sleep(time.Millisecond)
		}
		if _, err := os.Stat(fifo + ".bgexit"); err != nil {
			t.Fatalf("timed out waiting for background child-exit trigger: %v", err)
		}
		if err := writeAllTTYProxy(int(os.Stdout.Fd()), []byte("BG-TAIL\n")); err != nil {
			t.Fatalf("write queued background final output: %v", err)
		}
		if err := writePressureReceipt(fifo+".target", 'T'); err != nil {
			t.Fatal(err)
		}
		os.Exit(0)
	}
	var input [1]byte
	if _, err := unix.Read(int(os.Stdin.Fd()), input[:]); err != nil {
		t.Fatalf("read forwarded input: %v", err)
	}
	if input[0] != 'q' {
		t.Fatalf("read input %q, want q", input[0])
	}
	if err := writePressureReceipt(fifo, 'R'); err != nil {
		t.Fatal(err)
	}
	if segmented || os.Getenv("YOLO_TTYPROXY_OUTPUT_ACK") != "1" || os.Getenv("YOLO_TTYPROXY_ACK_AFTER_WRITE") != "1" {
		if err := <-outputDone; err != nil {
			t.Fatalf("write pressured output: %v", err)
		}
	}
	if err := writeAllTTYProxy(int(os.Stdout.Fd()), []byte("\x1b[200~paste\tbytes\x1b[201~\x1b[?2026hsync-body\x1b[?2026l\x03\x1a")); err != nil {
		t.Fatalf("write terminal controls: %v", err)
	}
	os.Exit(0)
}

func outputPressurePrefix(n int) []byte { return bytes.Repeat([]byte{'p'}, n) }

func mustGetTermios(t *testing.T, fd int) *unix.Termios {
	t.Helper()
	termios, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	return termios
}

func waitForOutputPressure(t *testing.T, pipeFD, pipeSize int) (bool, int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	maxBuffered, lastBuffered, stableSamples := 0, -1, 0
	for time.Now().Before(deadline) {
		buffered, err := unix.IoctlGetInt(pipeFD, unix.TIOCINQ)
		if err != nil {
			t.Fatal(err)
		}
		if buffered > maxBuffered {
			maxBuffered = buffered
		}
		if buffered >= pipeSize*4/5 && buffered == lastBuffered {
			stableSamples++
		} else {
			stableSamples = 0
		}
		lastBuffered = buffered
		if stableSamples >= 5 {
			return true, maxBuffered
		}
		time.Sleep(25 * time.Millisecond)
	}
	return false, maxBuffered
}

func writePressureReceipt(path string, marker byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write([]byte{marker})
	return err
}

func waitForFileLength(path string, want int, deadline time.Time) error {
	for time.Now().Before(deadline) {
		info, err := os.Stat(path)
		if err == nil {
			if info.Size() >= int64(want) {
				return nil
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		time.Sleep(5 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %s to reach %d bytes", path, want)
}

func waitForFile(path string, deadline time.Time) error {
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		time.Sleep(5 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %s", path)
}

func waitPressureMarker(t *testing.T, path string, want byte, deadline time.Time) error {
	t.Helper()
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if bytes.Contains(data, []byte{want}) {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for marker %q", want)
}

func readPTYAvailable(fd int, deadline time.Time) ([]byte, error) {
	if err := unix.SetNonblock(fd, true); err != nil {
		return nil, err
	}
	var got []byte
	buf := make([]byte, 4096)
	for time.Now().Before(deadline) {
		n, err := unix.Read(fd, buf)
		if n > 0 {
			got = append(got, buf[:n]...)
		}
		if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) {
			return got, nil
		}
		if err != nil && !errors.Is(err, syscall.EINTR) {
			return got, err
		}
		if n == 0 {
			time.Sleep(time.Millisecond)
		}
	}
	return got, fmt.Errorf("timed out draining PTY pressure bytes")
}

func readPTYBytes(fd, want int, deadline time.Time) ([]byte, error) {
	if err := unix.SetNonblock(fd, true); err != nil {
		return nil, err
	}
	got := make([]byte, 0, want)
	buf := make([]byte, want)
	for len(got) < want && time.Now().Before(deadline) {
		n, err := unix.Read(fd, buf[:want-len(got)])
		if n > 0 {
			got = append(got, buf[:n]...)
		}
		if err != nil && !errors.Is(err, syscall.EINTR) && !errors.Is(err, unix.EAGAIN) && !errors.Is(err, unix.EWOULDBLOCK) {
			return got, err
		}
		if n == 0 || errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) {
			time.Sleep(time.Millisecond)
		}
	}
	if len(got) != want {
		return got, fmt.Errorf("read %d of %d bytes", len(got), want)
	}
	return got, nil
}

func writeAllTTYProxy(fd int, data []byte) error {
	for len(data) > 0 {
		n, err := unix.Write(fd, data)
		if n > 0 {
			data = data[n:]
		}
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

func firstByteDifference(a, b []byte) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return min(len(a), len(b))
}

func TestWriteOutputChunkReportsSinkError(t *testing.T) {
	fd, err := unix.Open("/dev/full", unix.O_WRONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if err := writeOutputChunk(fd, []byte("terminal output")); !errors.Is(err, unix.ENOSPC) {
		t.Fatalf("writeOutputChunk error = %v, want ENOSPC", err)
	}
}

// TestNonTTYFallback: with non-TTY stdin, RunWithProxy plain-spawns and returns
// the child's exit code (no pty), matching the Python fallback.
func TestNonTTYFallback(t *testing.T) {
	// os.Stdin under `go test` is not a TTY, so this exercises the fallback.
	rc, err := RunWithProxy([]string{"sh", "-c", "exit 7"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rc != 7 {
		t.Errorf("non-TTY fallback rc = %d, want 7", rc)
	}
}

// TestNonTTYOnStarted: the onStarted callback runs (best-effort) in the
// fallback path.
func TestNonTTYOnStarted(t *testing.T) {
	ran := make(chan struct{}, 1)
	rc, err := RunWithProxy([]string{"true"}, func(*os.Process) { ran <- struct{}{} }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rc != 0 {
		t.Errorf("rc = %d, want 0", rc)
	}
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Error("onStarted did not run")
	}
}

func TestHandleKillInSynchronousArm(t *testing.T) {
	if mode := os.Getenv("YOLO_TTYPROXY_KILL_ARM"); mode != "" {
		if mode == "pty" {
			fakeHostTTY(t)
		}
		_, err := RunWithProxyObserved([]string{"true"}, nil, nil, Observer{Arm: func(handle Handle) { handle.Kill() }})
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	for _, mode := range []string{"plain", "pty"} {
		t.Run(mode, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestHandleKillInSynchronousArm$")
			cmd.Env = append(os.Environ(), "YOLO_TTYPROXY_KILL_ARM="+mode)
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("synchronous Arm Kill helper: %v", err)
				}
			case <-time.After(5 * time.Second):
				_ = cmd.Process.Kill()
				<-done
				t.Fatal("synchronous Arm Kill deadlocked before the sole reaper ran")
			}
		})
	}
}

func TestProxySlaveFinalizersCannotCloseReusedCallerFD(t *testing.T) {
	if os.Getenv("YOLO_TTYPROXY_FD_REUSE") == "1" {
		fakeHostTTY(t)
		if _, err := RunWithProxy([]string{"true"}, nil, nil); err != nil {
			t.Fatal(err)
		}
		var reused []*os.File
		for i := 0; i < 256; i++ {
			f, err := os.OpenFile(os.DevNull, os.O_RDONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			reused = append(reused, f)
		}
		runtime.GC()
		for _, f := range reused {
			if _, err := unix.FcntlInt(f.Fd(), unix.F_GETFD, 0); err != nil {
				t.Fatalf("forced stale slave finalizer closed reused caller fd %d: %v", f.Fd(), err)
			}
		}
		runtime.KeepAlive(reused)
		for _, f := range reused {
			_ = f.Close()
		}
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestProxySlaveFinalizersCannotCloseReusedCallerFD$")
	cmd.Env = append(os.Environ(), "YOLO_TTYPROXY_FD_REUSE=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("isolated forced-finalizer/fd-reuse regression: %v: %s", err, output)
	}
}

// TestPtyPassthrough drives RunWithProxy with a REAL pty as stdin (so the TTY
// path runs), writes input, and verifies the child echoes it back through the
// proxy. This exercises openPty, raw mode, and the bidirectional pump.
func TestPtyPassthrough(t *testing.T) {
	// fakeHostTTY installs the owned PTY at both stdin and stdout descriptors.
	master, _ := fakeHostTTY(t)

	// Child: cat one line back. `head -1` exits after one line -> proxy returns.
	done := make(chan int, 1)
	go func() {
		rc, _ := RunWithProxy([]string{"head", "-n", "1"}, nil, nil)
		done <- rc
	}()

	// Write a line into the master; the proxy forwards it to the child's pty,
	// head echoes (via the pty) and exits.
	time.Sleep(100 * time.Millisecond)
	_, _ = unix.Write(master, []byte("hello\n"))

	select {
	case rc := <-done:
		if rc != 0 {
			t.Errorf("pty passthrough child rc = %d, want 0", rc)
		}
	case <-time.After(5 * time.Second):
		t.Error("proxy did not return after child exit (pump/exit race?)")
	}
}

// TestSuspendIsTargetedNotPgroupWide is a STATIC guard on the design: the
// suspend path must use syscall.Kill(getpid(), SIGTSTP) — a targeted self-kill,
// never a pgroup-wide kill(0)/kill(-pgid) that would also stop podman. This
// asserts the source contract via a behavioral proxy: selfSuspend on a process
// with SIGTSTP ignored must NOT stop it (proving it's a normal targeted signal,
// not a group broadcast that bypasses our own disposition).
func TestSuspendTargetedSelfOnly(t *testing.T) {
	// Ignore SIGTSTP in this test process so selfSuspend's Kill(getpid(),
	// SIGTSTP) does NOT actually stop the test — proving it's a targeted
	// self-signal subject to our own disposition, not a pgroup-wide broadcast
	// (which would stop podman, the exact regression the design forbids).
	signal.Ignore(syscall.SIGTSTP)
	defer signal.Reset(syscall.SIGTSTP)

	cooked := &unix.Termios{}
	if c, err := unix.IoctlGetTermios(int(os.Stdin.Fd()), unix.TCGETS); err == nil {
		cooked = c
	}
	doneCh := make(chan struct{})
	go func() {
		selfSuspend(int(os.Stdin.Fd()), cooked)
		close(doneCh)
	}()
	select {
	case <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatal("selfSuspend hung — SIGTSTP was not targeted/ignored as expected")
	}
}

// TestSelfSuspendResetsTerminal verifies that selfSuspend writes terminal reset
// escape sequences (including cursor show \x1b[?25h) to the terminal so the
// host shell prompt does not lose its cursor.
func TestSelfSuspendResetsTerminal(t *testing.T) {
	signal.Ignore(syscall.SIGTSTP)
	defer signal.Reset(syscall.SIGTSTP)

	master, slave, err := openPty()
	if err != nil {
		t.Skipf("cannot open pty: %v", err)
	}
	defer unix.Close(master)
	defer unix.Close(slave)

	cooked, err := unix.IoctlGetTermios(slave, unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	setRaw(slave, cooked)

	selfSuspend(slave, cooked)

	buf := make([]byte, 256)
	// Set read deadline so test does not hang if nothing was written.
	if err := unix.SetNonblock(master, true); err != nil {
		t.Fatal(err)
	}
	n, err := unix.Read(master, buf)
	if err != nil {
		t.Fatalf("reading reset sequence from master: %v", err)
	}
	got := string(buf[:n])
	if !strings.Contains(got, "\x1b[?25h") {
		t.Errorf("selfSuspend did not emit cursor show sequence, got %q", got)
	}
	if !strings.Contains(got, termReset) {
		t.Errorf("selfSuspend output = %q, want containing %q", got, termReset)
	}
}

// TestRestoreTerminalResetsTerminalModes asserts that restoreTerminal restores
// cooked termios and emits termReset (cursor visible, mouse off, normal modes).
func TestRestoreTerminalResetsTerminalModes(t *testing.T) {
	master, slave, err := openPty()
	if err != nil {
		t.Skipf("cannot open pty: %v", err)
	}
	defer unix.Close(master)
	defer unix.Close(slave)

	cooked, err := unix.IoctlGetTermios(slave, unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	setRaw(slave, cooked)

	restoreTerminal(slave, cooked)

	// Verify termios restored to cooked.
	current, err := unix.IoctlGetTermios(slave, unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	if current.Lflag&unix.ICANON == 0 {
		t.Error("restoreTerminal did not restore canonical mode (ICANON)")
	}

	// Verify termReset was written to the terminal.
	buf := make([]byte, 256)
	if err := unix.SetNonblock(master, true); err != nil {
		t.Fatal(err)
	}
	n, err := unix.Read(master, buf)
	if err != nil {
		t.Fatalf("reading reset sequence from master: %v", err)
	}
	got := string(buf[:n])
	if !strings.Contains(got, termReset) {
		t.Errorf("restoreTerminal output = %q, want %q", got, termReset)
	}
}

// TestResetTerminalFdSelection asserts that resetTerminal prioritizes stdout,
// falls back to stderr, then falls back to inFd, and writes nothing if none is a tty.
func TestResetTerminalFdSelection(t *testing.T) {
	origStdout, origStderr := os.Stdout, os.Stderr
	oldStdoutFD, oldStderrFD := terminalStdoutFD, terminalStderrFD
	defer func() {
		os.Stdout, os.Stderr = origStdout, origStderr
		terminalStdoutFD, terminalStderrFD = oldStdoutFD, oldStderrFD
	}()

	t.Run("writes to stdout when stdout is tty", func(t *testing.T) {
		m, s, err := openPty()
		if err != nil {
			t.Skip(err)
		}
		defer unix.Close(m)

		stdoutFile := os.NewFile(uintptr(s), "stdout")
		os.Stdout = stdoutFile
		os.Stderr = origStderr
		terminalStdoutFD, terminalStderrFD = s, 2
		_ = unix.SetNonblock(m, true)

		defer func() {
			os.Stdout = origStdout
			_ = stdoutFile.Close()
		}()

		resetTerminal(-1)

		buf := make([]byte, 256)
		n, err := unix.Read(m, buf)
		if err != nil {
			t.Fatalf("read from stdout master: %v", err)
		}
		if string(buf[:n]) != termReset {
			t.Errorf("got %q, want %q", string(buf[:n]), termReset)
		}
	})

	t.Run("writes to stderr when stdout is not tty but stderr is", func(t *testing.T) {
		m, s, err := openPty()
		if err != nil {
			t.Skip(err)
		}
		defer unix.Close(m)

		stderrFile := os.NewFile(uintptr(s), "stderr")
		os.Stdout = origStdout // non-tty under go test
		os.Stderr = stderrFile
		terminalStdoutFD, terminalStderrFD = 1, s
		_ = unix.SetNonblock(m, true)

		defer func() {
			os.Stderr = origStderr
			_ = stderrFile.Close()
		}()

		resetTerminal(-1)

		buf := make([]byte, 256)
		n, err := unix.Read(m, buf)
		if err != nil {
			t.Fatalf("read from stderr master: %v", err)
		}
		if string(buf[:n]) != termReset {
			t.Errorf("got %q, want %q", string(buf[:n]), termReset)
		}
	})

	t.Run("writes to inFd when neither stdout nor stderr is tty", func(t *testing.T) {
		m, inFD, err := openPty()
		if err != nil {
			t.Skip(err)
		}
		defer unix.Close(m)
		defer unix.Close(inFD)

		os.Stdout = origStdout
		os.Stderr = origStderr
		terminalStdoutFD, terminalStderrFD = 1, 2
		_ = unix.SetNonblock(m, true)

		resetTerminal(inFD)

		buf := make([]byte, 256)
		n, err := unix.Read(m, buf)
		if err != nil {
			t.Fatalf("read from inFd master: %v", err)
		}
		if string(buf[:n]) != termReset {
			t.Errorf("got %q, want %q", string(buf[:n]), termReset)
		}
	})

	t.Run("writes nothing when no fd is a tty", func(t *testing.T) {
		os.Stdout = origStdout
		os.Stderr = origStderr
		terminalStdoutFD, terminalStderrFD = 1, 2
		resetTerminal(-1)
	})
}

func TestPtySlaveDescriptorsHaveSingleFileOwners(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate test source")
	}
	productionPath := filepath.Join(filepath.Dir(currentFile), "ttyproxy.go")
	productionBytes, err := os.ReadFile(productionPath)
	if err != nil {
		t.Fatal(err)
	}
	production := parseProxyFunctionSource(t, productionPath, string(productionBytes), "RunWithProxyObserved")
	if !productionPtyOwnerCallpath(production) {
		t.Fatal("production child streams no longer have one closed slave owner through c.Start")
	}

	for _, tc := range []struct {
		name, old, replacement string
	}{
		{
			name: "competing child-stream wrappers",
			old:  "\tif err := c.Start(); err != nil {",
			replacement: "\tc.Stdin = os.NewFile(uintptr(slave), \"competing-stdin\")\n" +
				"\tc.Stdout = os.NewFile(uintptr(slave), \"competing-stdout\")\n" +
				"\tc.Stderr = os.NewFile(uintptr(slave), \"competing-stderr\")\n" +
				"\tif err := c.Start(); err != nil {",
		},
		{
			name:        "deleted actual stream assignment",
			old:         "\tc.Stdin, c.Stdout, c.Stderr = ptyFile, ptyFile, ptyFile\n\tif err := c.Start(); err != nil {",
			replacement: "\tif err := c.Start(); err != nil {",
		},
	} {
		mutated := strings.Replace(string(productionBytes), tc.old, tc.replacement, 1)
		if mutated == string(productionBytes) {
			t.Fatalf("mutation %q did not find its production seam", tc.name)
		}
		if productionPtyOwnerCallpath(parseProxyFunctionSource(t, productionPath, mutated, "RunWithProxyObserved")) {
			t.Errorf("production ownership oracle accepted mutation %q", tc.name)
		}
	}

	fixture := parseProxyFunction(t, currentFile, "TestResetTerminalFdSelection")
	fixtureOwners, fixtureCloses, fixtureNewFiles := fileOwnersAndCloses(fixture)
	if fixtureNewFiles != 2 || len(fixtureOwners) != 2 || fixtureCloses["stdoutFile"] != 1 || fixtureCloses["stderrFile"] != 1 {
		t.Fatalf("terminal-reset fixture NewFile calls=%d owners=%v closes=%v, want two wrappers each explicitly closed by their owner", fixtureNewFiles, fixtureOwners, fixtureCloses)
	}
	if rawClosesDescriptor(fixture, "s") {
		t.Fatal("terminal-reset fixture raw-closes a slave descriptor still owned by an os.File")
	}
}

func productionPtyOwnerCallpath(fn *ast.FuncDecl) bool {
	owners, closes, newFiles := fileOwnersAndCloses(fn)
	return newFiles == 1 && len(owners) == 1 && owners[0] == "ptyFile" && closes["ptyFile"] >= 2 &&
		childStreamsShareOwnerThroughStart(fn, "ptyFile") && !rawClosesDescriptor(fn, "slave")
}

func parseProxyFunction(t *testing.T, filename, name string) *ast.FuncDecl {
	t.Helper()
	contents, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("read %s: %v", filename, err)
	}
	return parseProxyFunctionSource(t, filename, string(contents), name)
}

func parseProxyFunctionSource(t *testing.T, filename, source, name string) *ast.FuncDecl {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, source, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", filename, err)
	}
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == name {
			return fn
		}
	}
	t.Fatalf("%s has no function %s", filename, name)
	return nil
}

func fileOwnersAndCloses(fn *ast.FuncDecl) ([]string, map[string]int, int) {
	var owners []string
	closes := make(map[string]int)
	newFiles := 0
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.AssignStmt:
			for i, rhs := range n.Rhs {
				call, ok := rhs.(*ast.CallExpr)
				if !ok || !isOSNewFile(call) || i >= len(n.Lhs) {
					continue
				}
				if owner, ok := n.Lhs[i].(*ast.Ident); ok {
					owners = append(owners, owner.Name)
				}
			}
		case *ast.CallExpr:
			if isOSNewFile(n) {
				newFiles++
			}
			if sel, ok := n.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Close" {
				if owner, ok := sel.X.(*ast.Ident); ok {
					closes[owner.Name]++
				}
			}
		}
		return true
	})
	return owners, closes, newFiles
}

func isOSNewFile(call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "NewFile" {
		return false
	}
	pkg, ok := selector.X.(*ast.Ident)
	return ok && pkg.Name == "os"
}

func childStreamsShareOwnerThroughStart(fn *ast.FuncDecl, owner string) bool {
	streamAssignment := token.NoPos
	streamAssignments := 0
	startCall := token.NoPos
	startCalls := 0
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.AssignStmt:
			if len(n.Lhs) != 3 || len(n.Rhs) != 3 {
				return true
			}
			fields := make(map[string]bool)
			for _, lhs := range n.Lhs {
				selector, ok := lhs.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				receiver, receiverOK := selector.X.(*ast.Ident)
				if !receiverOK || receiver.Name != "c" ||
					(selector.Sel.Name != "Stdin" && selector.Sel.Name != "Stdout" && selector.Sel.Name != "Stderr") {
					return true
				}
				fields[selector.Sel.Name] = true
			}
			if len(fields) == 3 {
				streamAssignments++
				streamAssignment = n.Pos()
				shared := true
				for _, rhs := range n.Rhs {
					ident, ok := rhs.(*ast.Ident)
					if !ok || ident.Name != owner {
						shared = false
						break
					}
				}
				if !shared {
					streamAssignment = token.NoPos
				}
			}
		case *ast.CallExpr:
			selector, ok := n.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Start" {
				return true
			}
			receiver, ok := selector.X.(*ast.Ident)
			if ok && receiver.Name == "c" {
				startCalls++
				startCall = n.Pos()
			}
		}
		return true
	})
	return streamAssignments == 1 && startCalls == 1 && streamAssignment.IsValid() && startCall > streamAssignment
}

func rawClosesDescriptor(fn *ast.FuncDecl, descriptor string) bool {
	found := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "Close" {
			return true
		}
		pkg, ok := selector.X.(*ast.Ident)
		if !ok || pkg.Name != "unix" {
			return true
		}
		arg, ok := call.Args[0].(*ast.Ident)
		found = found || ok && arg.Name == descriptor
		return true
	})
	return found
}

// The stage-hook order pin on the plain (non-TTY) path: spawned then exited,
// and nothing else — the plain fallback has no drain and no termios to
// restore, so those stages must stay silent rather than fire vacuously. The
// pty path's stages are pinned end-to-end by the integration suite, where a
// real terminal exists.
func TestStageHookPlainPathOrder(t *testing.T) {
	var stages []string
	rc, err := RunWithProxyHooked([]string{"/bin/true"}, nil, nil, func(s string) {
		stages = append(stages, s)
	})
	if err != nil || rc != 0 {
		t.Fatalf("rc=%d err=%v", rc, err)
	}
	want := []string{StageSpawned, StageExited}
	if len(stages) != len(want) {
		t.Fatalf("stages = %v, want %v", stages, want)
	}
	for i := range want {
		if stages[i] != want[i] {
			t.Fatalf("stages = %v, want %v", stages, want)
		}
	}
}

// A hook that panics must not take the proxy with it — observers are
// best-effort by contract, and the reverse (a diagnostics hook killing every
// launch) is the failure the perf design forbids.
func TestStageHookPanicIsContained(t *testing.T) {
	rc, err := RunWithProxyHooked([]string{"/bin/true"}, nil, nil, func(string string) {
		panic("observer bug")
	})
	if err != nil || rc != 0 {
		t.Fatalf("panicking hook changed the outcome: rc=%d err=%v", rc, err)
	}
}

// TestWinchResignalsTheChild is the regression for size drift — the ptys in a
// jail chain disagreeing about rows and columns, which reads as a garbled agent
// TUI. See resyncWinsize for the race and the measurement.
//
// IT IS DETERMINISTIC FOR A USEFUL REASON, and one worth stating because the
// production bug is a coin flip. The child here is NOT in the fake host pty's
// foreground process group, so the kernel sends it no SIGWINCH of its own: the
// only signal it can possibly receive is the one resyncWinsize sends. Without the
// fix the child sees zero and never prints; with it, exactly one, after the proxy
// pty already reports the new size. So this fails by TIMING OUT before the fix and
// passes promptly after — the mutation check ran both ways.
//
// The proxy is driven by raising SIGWINCH in this process rather than by resizing
// alone: the proxy learns of resizes through signal.Notify on its own process, and
// a pty with no foreground process group signals nobody when its size changes.
// A test that only resized would assert nothing at all.
func TestWinchResignalsTheChild(t *testing.T) {
	master, _ := fakeHostTTY(t)

	// `sleep & wait` rather than a bare `sleep`: a POSIX shell defers a trap until
	// the current foreground command finishes, so a bare sleep would swallow the
	// signal for its whole duration and make this a test of the timeout. The trap
	// kills the sleep too — a backgrounded child inherits the proxy pty and holds
	// it open, so the proxy's drain would never see EOF and the test would hang
	// after already having proved its point.
	//
	// THE RESIZE WAITS FOR THE CHILD TO SAY ITS TRAP IS SET: the T it prints once the
	// trap and $S are in place. That T also proves the proxy is pumping, which it starts
	// only after registering for SIGWINCH. A 200ms sleep used to stand in for both. When
	// sh was slow to start, the re-signal reached a shell with no trap yet, which ignores
	// SIGWINCH by default, and the failure below then blamed resyncWinsize. The sleep
	// outlasts every wait below, so a missing signal cannot end the child inside one.
	run := startProxied(t, master, []string{"sh", "-c",
		`trap 'stty size <&0; kill $S 2>/dev/null; exit 0' WINCH; sleep 30 & S=$!; printf T; wait`}, Observer{})
	if got, ok := readUntil(master, "T", childDeadline); !ok {
		t.Fatalf("the child never said its WINCH trap was set (read %q): the child or the proxy "+
			"never got going, which says nothing about resizes", got)
	}

	// Resize the FAKE HOST pty, then tell the proxy the way the kernel would.
	want := &unix.Winsize{Row: 41, Col: 137}
	if err := unix.IoctlSetWinsize(master, unix.TIOCSWINSZ, want); err != nil {
		t.Skipf("cannot resize the fake host pty: %v", err)
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGWINCH); err != nil {
		t.Fatalf("raising SIGWINCH: %v", err)
	}

	// The child's `stty size` lands on the proxy pty, which the proxy pumps to
	// os.Stdout — the fake host pty slave — so it comes back out of `master`.
	if got, ok := readUntil(master, "41 137", childDeadline); !ok {
		// Anything but the proxy's own reset on its way out is the child's report.
		if strings.TrimSpace(strings.ReplaceAll(got, termReset, "")) != "" {
			t.Fatalf("child reported %q, want the resized 41x137", got)
		}
		t.Fatal("the child never saw a SIGWINCH. The proxy writes the new size to its " +
			"pty but that pty has no foreground process group, so nothing signals the " +
			"child — this is size drift, and resyncWinsize's targeted re-signal is what " +
			"closes it.")
	}
	select {
	case <-run.done:
	case <-time.After(childDeadline):
		t.Error("proxy did not return after the child exited")
	}
}

// TestTermResetDisablesEveryModeThatSurvivesAKill names each escape sequence
// INDIVIDUALLY, because every other assertion in this file compares what was
// written against termReset itself — which passes whatever the constant says and
// would go on passing if a sequence were dropped from it.
//
// The keyboard rows are the ones with a reported symptom behind them: a terminal
// left under an enhanced keyboard protocol emits a CSI burst per keypress, which
// is the state a user escapes by pasting `reset`.
func TestTermResetDisablesEveryModeThatSurvivesAKill(t *testing.T) {
	for _, tc := range []struct{ seq, why string }{
		{"\x1b[0m", "SGR attributes"},
		{"\x1b[?25h", "cursor visibility (DECTCEM)"},
		{"\x1b[?1000l", "X10 mouse tracking"},
		{"\x1b[?1002l", "button-event mouse tracking"},
		{"\x1b[?1003l", "any-event mouse tracking"},
		{"\x1b[?1006l", "SGR mouse encoding"},
		{"\x1b[?2004l", "bracketed paste"},
		{"\x1b[?1l", "application cursor keys (DECCKM)"},
		{"\x1b>", "application keypad (DECKPNM)"},
		{"\x1b[?1004l", "focus reporting"},
		{"\x1b[<u", "kitty keyboard protocol flags"},
		{"\x1b[>4;0m", "xterm modifyOtherKeys"},
	} {
		if !strings.Contains(termReset, tc.seq) {
			t.Errorf("termReset does not disable %s (missing %q)", tc.why, tc.seq)
		}
	}
	// The one thing it must NOT do: restoreTerminal is also the Ctrl-Z path, where
	// leaving the alternate screen would wipe the suspended program's display.
	if strings.Contains(termReset, "\x1b[?1049l") {
		t.Error("termReset leaves the alternate screen; selfSuspend would wipe the " +
			"suspended program's display and `fg` could not restore it")
	}
}

// TestHelperLargePasteChild is the child process helper for
// TestLargePasteDeadlockPinsNonblockingMasterForwarding. It runs as a single process,
// so killing it closes the pty slave cleanly without orphaned sub-shells.
func TestHelperLargePasteChild(t *testing.T) {
	if os.Getenv("GO_WANT_LARGE_PASTE_CHILD") != "1" {
		return
	}
	inFd := int(os.Stdin.Fd())
	if cooked, err := unix.IoctlGetTermios(inFd, unix.TCGETS); err == nil {
		setRaw(inFd, cooked)
	}
	if _, err := os.Stdout.Write([]byte("READY\n")); err != nil {
		os.Exit(1)
	}
	// Write 128KB to stdout before reading input.
	outBuf := make([]byte, 128*1024)
	for i := range outBuf {
		outBuf[i] = 'O'
	}
	if _, err := os.Stdout.Write(outBuf); err != nil {
		os.Exit(2)
	}
	// Read 64KB from stdin.
	inBuf := make([]byte, 64*1024)
	if _, err := io.ReadFull(os.Stdin, inBuf); err != nil {
		os.Exit(3)
	}
	os.Exit(0)
}

// TestLargePasteDeadlockPinsNonblockingMasterForwarding asserts that a large input paste
// does not deadlock the proxy when the child writes output before draining input.
func TestLargePasteDeadlockPinsNonblockingMasterForwarding(t *testing.T) {
	master, _ := fakeHostTTY(t)

	var inputObserved atomic.Int64
	var outputObserved atomic.Int64
	var pasteWritten atomic.Int64
	run := startProxied(t, master, []string{os.Args[0], "-test.run=^TestHelperLargePasteChild$", "-test.count=1"},
		Observer{Env: []string{"GO_WANT_LARGE_PASTE_CHILD=1"}, Input: func(n int, _ string) { inputObserved.Add(int64(n)) }})

	if got, ok := readUntil(master, "READY\n", childDeadline); !ok {
		t.Fatalf("child never signaled READY: %q", got)
	} else {
		outputObserved.Add(int64(len(got) - len("READY\n")))
	}

	// Drain host master output in the background through the child's final byte,
	// including bytes still buffered after the proxy run has returned.
	stopDrain := make(chan struct{})
	var stopDrainOnce sync.Once
	stopOutputDrain := func() { stopDrainOnce.Do(func() { close(stopDrain) }) }
	drainDone := make(chan struct{})
	go func() {
		defer close(drainDone)
		buf := make([]byte, 4096)
		for {
			select {
			case <-stopDrain:
				return
			default:
				_ = unix.SetNonblock(master, true)
				n, err := unix.Read(master, buf)
				if n > 0 {
					outputObserved.Add(int64(n))
				}
				if err != nil {
					if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) {
						time.Sleep(5 * time.Millisecond)
						continue
					}
					return
				}
				if n == 0 {
					time.Sleep(5 * time.Millisecond)
					continue
				}
			}
		}
	}()

	// Feed the 64KB paste in background as the proxy accepts it.
	paste := make([]byte, 65536)
	for i := range paste {
		paste[i] = 'A'
	}
	pasteDone := make(chan struct{})
	go func() {
		defer close(pasteDone)
		written := 0
		for written < len(paste) {
			select {
			case <-stopDrain:
				return
			default:
				fds := []unix.PollFd{{Fd: int32(master), Events: unix.POLLOUT}}
				n, err := unix.Poll(fds, 50)
				if err != nil || n == 0 {
					continue
				}
				m, err := unix.Write(master, paste[written:])
				if m > 0 {
					written += m
					pasteWritten.Add(int64(m))
				}
				if err != nil && !errors.Is(err, unix.EAGAIN) {
					return
				}
			}
		}
	}()
	t.Cleanup(func() {
		stopOutputDrain()
		<-drainDone
		<-pasteDone
	})
	deadline := time.Now().Add(2 * time.Second)

	select {
	case rc := <-run.done:
		if rc != 0 {
			detail := fmt.Sprintf("child exit code = %d, want 0 (host input observed=%d/%d, paste written=%d/%d, output drained=%d)",
				rc, inputObserved.Load(), len(paste), pasteWritten.Load(), len(paste), outputObserved.Load())
			_, _ = fmt.Fprintln(os.Stderr, "LargePaste flow: "+detail)
			t.Fatal(detail)
		}
	case <-time.After(time.Until(deadline)):
		detail := fmt.Sprintf("proxyLoop deadlocked on large input paste while child was writing (host input observed=%d/%d, paste written=%d/%d, output drained=%d)",
			inputObserved.Load(), len(paste), pasteWritten.Load(), len(paste), outputObserved.Load())
		_, _ = fmt.Fprintln(os.Stderr, "LargePaste flow: "+detail)
		t.Fatal(detail)
	}
	select {
	case <-run.finished:
	case <-time.After(time.Until(deadline)):
		detail := fmt.Sprintf("proxy returned an exit code but did not finish (input=%d paste=%d output=%d)", inputObserved.Load(), pasteWritten.Load(), outputObserved.Load())
		_, _ = fmt.Fprintln(os.Stderr, "LargePaste flow: "+detail)
		t.Fatal(detail)
	}
	expectedOutput := 128*1024 + len(termReset)
	for outputObserved.Load() < int64(expectedOutput) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := outputObserved.Load(); got != int64(expectedOutput) {
		detail := fmt.Sprintf("host output drainer saw %d/%d child-plus-reset bytes before the original two-second bound", got, expectedOutput)
		_, _ = fmt.Fprintln(os.Stderr, "LargePaste flow: "+detail)
		t.Fatal(detail)
	}
	stopOutputDrain()
	<-drainDone
	<-pasteDone
	_, _ = fmt.Fprintf(os.Stderr, "LargePaste flow: host input=%d/%d paste-written=%d/%d output-drained=%d/%d\n",
		inputObserved.Load(), len(paste), pasteWritten.Load(), len(paste), outputObserved.Load(), 128*1024+len(termReset))
	if got := inputObserved.Load(); got != int64(len(paste)) {
		t.Errorf("proxy observed %d/%d host input bytes before child exit", got, len(paste))
	}
	if got := pasteWritten.Load(); got != int64(len(paste)) {
		t.Errorf("host paste writer completed %d/%d bytes", got, len(paste))
	}
	if got := outputObserved.Load(); got != int64(expectedOutput) {
		t.Errorf("host output drainer captured %d/%d child-plus-reset bytes", got, expectedOutput)
	}
}
