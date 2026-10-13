package entrypoint

// programPhasePerfShellFn appends coarse shell observations to the existing boot
// perf block, like appendProvisionPerf. It never creates a sink: no boot log (host
// launchers included) means no recording. Boot owns block retention. Each row is
// self-contained because a detached refresh may finish after a later boot block.
// PID and foreground/detached mode identify the act, not a claimed launch generation.
const programPhasePerfShellFn = `
_yolo_phase_perf() {
    local _phase="$1" _bin="$2" _kind="$3" _mode="$4" _elapsed="$5" _rc="$6"
    [ -n "${HOME:-}" ] && [ -f "$HOME/.yolo-perf.log" ] || return 0
    {
        printf '  program phase: %.32s %.128s %.8s (pid=%s mode=%.16s; coarse elapsed seconds=%s; exit=%s)\n' \
            "$_phase" "$_bin" "$_kind" "$$" "$_mode" "$_elapsed" "$_rc"
    } 2>/dev/null >> "$HOME/.yolo-perf.log" || true
}
`
