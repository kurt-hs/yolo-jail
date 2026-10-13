package cli

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mschulkind-oss/yolo-jail/internal/pidlock"
	"github.com/mschulkind-oss/yolo-jail/internal/svcendpoint"
)

type preparationControlCall struct {
	command preparationCommand
	reply   chan preparationResponse
}

func preparationExecutableDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// runPreparationWorker owns only lifecycle. dispatch must reauthorize inputs and
// guard admission; it receives the cancellation context used by existing owned
// build-child/jail cleanup. No autonomous adapter may bypass that prerequisite.
// Replace drains; stop cancels. The lease remains held through either teardown.
func runPreparationWorker(ctx context.Context, root, selectedExecutable string, ticks <-chan time.Time, dispatch func(context.Context) error) error {
	if dispatch == nil {
		return errors.New("preparation dispatcher missing; wire generation-guarded admission before starting")
	}
	if err := ensurePreparationRoot(root); err != nil {
		return err
	}
	lock, err := pidlock.Acquire(filepath.Join(root, "worker.lock"), pidlock.NoWait, nil)
	if err != nil {
		return err
	}
	defer lock.Release()
	digest, err := preparationExecutableDigest(selectedExecutable)
	if err != nil {
		return err
	}
	token := make([]byte, 32)
	if _, err = rand.Read(token); err != nil {
		return err
	}
	record := preparationWorkerRecord{Identity: hex.EncodeToString(token), ExecutablePath: selectedExecutable, ExecutableDigest: digest, PID: os.Getpid()}
	listener, err := svcendpoint.Listen(filepath.Join(root, "control.endpoint"), "127.0.0.1")
	if err != nil {
		return err
	}
	defer listener.Close()
	if err = writePreparationJSON(filepath.Join(root, "worker.json"), record); err != nil {
		return err
	}
	// Do not delete stale records: a crash/replacement retains diagnostic evidence.
	ownerCtx, ownerCancel := context.WithCancel(ctx)
	defer ownerCancel()
	calls := make(chan preparationControlCall)
	go servePreparationControl(ownerCtx, listener, calls)
	phase := "idle"
	var active <-chan error
	var result error
	pending := false
	ctxDone := ctx.Done()
	begin := func() {
		current, e := preparationExecutableDigest(selectedExecutable)
		if e != nil || current != digest {
			phase = "draining"
			result = fmt.Errorf("preparation executable identity changed or unreadable; start again with the selected binary: %v", e)
			return
		}
		done := make(chan error, 1)
		active = done
		phase = "running"
		go func() { done <- dispatch(ownerCtx) }()
	}
	for {
		if (phase == "stopping" || phase == "draining") && active == nil {
			return result
		}
		select {
		case <-ctxDone:
			phase = "stopping"
			ownerCancel()
			ctxDone = nil
		case call := <-calls:
			response := preparationResponse{Identity: record.Identity, Phase: phase}
			if call.command.Identity != record.Identity {
				response.Error = "preparation incarnation changed; reread status before control"
			} else {
				switch call.command.Operation {
				case "status":
				case "wake":
					if phase == "stopping" || phase == "draining" {
						response.Error = "preparation is draining/stopping; wait for lease release before starting"
					} else if active == nil {
						begin()
					} else {
						pending = true
					}
				case "stop":
					phase = "stopping"
					pending = false
					ownerCancel()
				case "replace":
					if phase != "stopping" {
						phase = "draining"
					}
					pending = false
				default:
					response.Error = "unknown preparation control; use wake, status, stop or replace"
				}
			}
			response.Phase = phase
			call.reply <- response
		case _, ok := <-ticks:
			if !ok {
				ticks = nil
				continue
			}
			if phase == "stopping" || phase == "draining" {
				continue
			}
			// Check identity even while busy; replacement cannot start another act.
			current, e := preparationExecutableDigest(selectedExecutable)
			if e != nil || current != digest {
				phase = "draining"
				pending = false
				result = fmt.Errorf("preparation executable identity changed or unreadable; start again with the selected binary: %v", e)
				continue
			}
			if active == nil {
				begin()
			} else {
				pending = true
			}
		case <-active:
			// Adapter owns outcome persistence. A failed key never becomes scheduler success.
			active = nil
			if phase == "running" {
				phase = "idle"
				if pending {
					pending = false
					begin()
				}
			}
		}
	}
}

func servePreparationControl(ctx context.Context, listener *svcendpoint.Listener, calls chan<- preparationControlCall) {
	// A slow client consumes one slot, not the control accept loop. Excess clients
	// are closed rather than spawning unbounded authenticated/handshake waiters.
	slots := make(chan struct{}, 8)
	var handlers sync.WaitGroup
	defer handlers.Wait()
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		select {
		case slots <- struct{}{}:
		default:
			conn.Close()
			continue
		}
		handlers.Add(1)
		go func() {
			defer handlers.Done()
			defer func() { <-slots }()
			defer conn.Close()
			deadline := time.Now().Add(preparationControlBound)
			conn.SetDeadline(deadline)
			var cmd preparationCommand
			_, err := svcendpoint.ReadPreamble(conn)
			conn.SetDeadline(deadline)
			if err == nil {
				err = json.NewDecoder(io.LimitReader(conn, 4096)).Decode(&cmd)
			}
			if err != nil {
				return
			}
			call := preparationControlCall{cmd, make(chan preparationResponse, 1)}
			timer := time.NewTimer(time.Until(deadline))
			defer timer.Stop()
			select {
			case calls <- call:
			case <-ctx.Done():
				return
			case <-timer.C:
				return
			}
			select {
			case response := <-call.reply:
				_ = json.NewEncoder(conn).Encode(response)
			case <-timer.C:
			}
		}()
	}
}
