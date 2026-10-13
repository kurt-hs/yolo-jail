package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/mschulkind-oss/yolo-jail/internal/paths"
	"github.com/mschulkind-oss/yolo-jail/internal/pidlock"
	"github.com/mschulkind-oss/yolo-jail/internal/richtext"
)

func printPreparationStatus(out, errw io.Writer, color bool, key string) int {
	state, err := loadPreparationState()
	if err != nil {
		fmt.Fprintf(errw, "preparation status: %v\n", err)
		return 1
	}
	pr := richtext.Printer{W: out, Color: color}
	if len(state.Subscriptions) == 0 {
		if key == "" {
			pr.Printf("[dim]No built-tree preparations are registered.[/dim]")
		}
		return 0
	}
	if pidlock.Held(preparationWorkerLockPath()) {
		pr.Printf("Tree preparation worker: running (lease held; pid %d)", pidlock.Holder(preparationWorkerLockPath()))
	} else if state.Worker.Identity != "" {
		pr.Printf("Tree preparation worker: stopped or unknown (state retained; run `yolo pack prepare start` to reauthorize and resume)")
	} else {
		pr.Printf("Tree preparation worker: not running")
	}
	records := append([]preparationRecord(nil), state.Subscriptions...)
	sortPreparationRecords(records)
	found := false
	for _, r := range records {
		if key != "" && r.Key != key {
			continue
		}
		found = true
		phase := r.Phase
		if phase == "" {
			phase = "pending"
		}
		pr.Printf("%s [%s/%s] generation %d: %s%s", r.Key, r.Runtime, r.Platform, r.Generation, phase,
			map[bool]string{true: " (inactive)", false: ""}[!r.Active])
		if r.ServingEntry != "" {
			pr.Printf("  admitted entry: %s", r.ServingEntry)
		}
		if !r.At.IsZero() {
			pr.Printf("  updated: %s", r.At.Format("2006-01-02 15:04:05 MST"))
		}
		if !r.RetryAt.IsZero() {
			pr.Printf("  retry after: %s", r.RetryAt.Format("2006-01-02 15:04:05 MST"))
		}
		if r.Reason != "" {
			pr.Printf("  reason: %s", richtext.Escape(r.Reason))
		}
		if r.Log != "" {
			pr.Printf("  log: %s", r.Log)
		}
	}
	if key != "" && !found {
		pr.Printf("[dim]No preparation subscription is recorded for %s.[/dim]", richtext.Escape(key))
	}
	return 0
}

func packPrepare(args []string, out, errw io.Writer, color bool) int {
	if len(args) != 1 {
		fmt.Fprintln(errw, "yolo pack prepare: use `yolo pack prepare start`, `stop` or `status`")
		return 2
	}
	switch args[0] {
	case "status":
		if len(args) != 1 {
			return 2
		}
		return printPreparationStatus(out, errw, color, "")
	case "start":
		state, err := preparationSubscriptions()
		if err != nil {
			fmt.Fprintf(errw, "yolo pack prepare start: %v\n", err)
			return 1
		}
		current, err := currentPreparationTrees(state)
		if err != nil {
			fmt.Fprintf(errw, "yolo pack prepare start: %v — repair %s, then run `yolo pack prepare start` again\n", err, paths.UserConfigPath())
			return 1
		}
		state = refreshPreparationSubscriptions(state, current)
		eligible := false
		for _, r := range state {
			eligible = eligible || r.Active
		}
		if !eligible {
			fmt.Fprintln(out, "No selected next-launch tree preparations are registered; nothing was started.")
			return 0
		}
		if err := ensurePreparationScheduler(); err != nil {
			fmt.Fprintf(errw, "yolo pack prepare start: could not start the owned host worker (%v) — retry `yolo pack prepare start`\n", err)
			return 1
		}
		fmt.Fprintln(out, "The host tree-preparation worker is running; inspect progress with `yolo pack prepare status`.")
		return 0
	case "stop":
		state, err := loadPreparationState()
		if err != nil {
			fmt.Fprintf(errw, "yolo pack prepare stop: %v\n", err)
			return 1
		}
		lockPath := preparationWorkerLockPath()
		if !pidlock.Held(lockPath) {
			fmt.Fprintln(out, "No proven live preparation worker owns the scheduler lease; recorded state was retained.")
			return 0
		}
		pid := pidlock.Holder(lockPath)
		if state.Worker.Identity == "" || state.Worker.PID != pid {
			fmt.Fprintf(errw, "yolo pack prepare stop: scheduler lease is held by pid %d, but its recorded worker identity is unknown — state was retained and no process was signalled\n", pid)
			return 1
		}
		if err := withPreparationState(func(s *preparationState) error {
			if s.Worker.Identity != state.Worker.Identity {
				return fmt.Errorf("worker identity changed during stop")
			}
			s.Worker.StopRequested = true
			return nil
		}); err != nil {
			fmt.Fprintf(errw, "yolo pack prepare stop: %v\n", err)
			return 1
		}
		if err := os.WriteFile(preparationStopPath(), []byte(state.Worker.Identity+"\n"), 0o600); err != nil {
			fmt.Fprintf(errw, "yolo pack prepare stop: could not signal the proven worker (%v)\n", err)
			return 1
		}
		fmt.Fprintln(out, "Stop requested for the proven preparation worker; its owned child work will drain and state will be retained.")
		return 0
	default:
		fmt.Fprintf(errw, "yolo pack prepare: unknown action %q — use start, stop or status\n", args[0])
		return 2
	}
}
