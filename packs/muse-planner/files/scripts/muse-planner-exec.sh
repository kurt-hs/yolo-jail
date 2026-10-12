#!/usr/bin/env bash
#
# muse-planner-exec.sh — the Meta Muse Code CLI (`muse`) as a pi-subagents
# `external-cli` runner.
#
# WHY A WRAPPER, AND NOT `command: muse` DIRECTLY. Three things the definition
# cannot say: the prompt arrives on stdin and `muse exec` takes it as a flag
# (`--prompt-file`, so no argv length limit and no shell quoting), the run needs
# a hard deadline of its own, and the planner's read-only posture has to be
# pinned per run rather than read from a settings file the user also edits.
#
# STDIN IS THE PROMPT. pi-subagents writes the child's assembled prompt to this
# process's stdin and closes it —
# `<System instructions>\n<system prompt>\n\n<Task>\n<task>` — and reads nothing
# but this process's stdout and stderr (pi-subagents
# src/runs/shared/external-cli-runner.ts: `child.stdin.end(input.prompt)`, with
# `promptDelivery: "stdin"` in the agent definition naming that contract).
#
# NO CREDENTIAL LIVES HERE, and none may be added. `muse` authenticates from
# `META_API_KEY` in its environment if it is set, else the credential its own
# login stored under `~/.config/muse` — both ambient, neither this script's
# business. A value written here would be a secret in a file the pack ships to
# everyone.
#
# NO `--yolo` AND NO `--disable-sandbox`: each removes Muse's own OS sandbox.
# `--disable-approval` only skips the interactive approval prompt and keeps the
# sandbox, which is what an unattended run in an already-confined jail wants.
# The read-only permission profile is the other half of the posture, pinned per
# run with `-c permissions.default_profile=":read-only"`, which Meta documents as
# a per-run override of any existing setting and which therefore holds for the
# planner whatever the settings file says. (The `muse-code` pack deliberately
# ships NO default profile: the interactive CLI keeps Meta's own default, and
# this flag is what makes the planner read-only.)
#
# BOTH HALVES OF THAT `-c` VALUE ARE LOAD-BEARING, and both were once wrong.
# `-c` VALUES ARE JSON (`muse exec --help`: "values are JSON"), so the whole
# value is quoted — the bare `read-only` is refused before the run starts with
# `-c permissions.default_profile: refusing run: not valid JSON (quote strings)`
# and exit 2. And `:read-only` is the native-catalog id itself; the bare
# `read-only` name resolves to nothing (`Permission profile 'read-only' is
# unavailable: profile does not exist`, exit 1). Measured against Muse Code
# 1.4.6, free, with `--provider echo`: this exact argv reaches exit 0.
#
# EXIT CODES are the child's own, propagated: 0 completed, 1 failed, 2 usage,
# 124 when the deadline above fired, 127 when `muse` is not installed. pi-subagents
# shows stderr and its own timeout message beside a non-zero exit, so a message
# on stderr is how a failure reaches the parent session.

set -uo pipefail

muse_bin="${MUSE_BIN:-muse}"
timeout_secs="${MUSE_PLANNER_TIMEOUT_SECS:-900}"
max_model_steps="${MUSE_PLANNER_MAX_STEPS:-60}"

if ! command -v "$muse_bin" >/dev/null 2>&1; then
	printf 'muse-planner-exec: %s is not on PATH, so the Muse Code CLI is not installed here.\n' "$muse_bin" >&2
	printf 'Install it with: curl -fsSL https://dev.meta.ai/install.sh | sh\n' >&2
	exit 127
fi

prompt_file="$(mktemp "${TMPDIR:-/tmp}/muse-planner-prompt.XXXXXX")" || {
	printf 'muse-planner-exec: could not create a prompt file.\n' >&2
	exit 1
}
trap 'rm -f "$prompt_file"' EXIT

if ! cat >"$prompt_file"; then
	printf 'muse-planner-exec: could not read the prompt from stdin.\n' >&2
	exit 1
fi
if [ ! -s "$prompt_file" ]; then
	printf 'muse-planner-exec: the prompt on stdin was empty; nothing to plan.\n' >&2
	exit 1
fi

muse_argv=(
	exec
	--prompt-file "$prompt_file"
	--disable-approval
	--max-model-steps "$max_model_steps"
	-c 'permissions.default_profile=":read-only"'
)

# `< /dev/null` is the non-interactive half: with no terminal on its stdin the
# CLI cannot fall back to prompting for an approval, a login or anything else.
if command -v timeout >/dev/null 2>&1; then
	timeout -k 30 "$timeout_secs" "$muse_bin" "${muse_argv[@]}" </dev/null
	status=$?
	if [ "$status" -eq 124 ]; then
		printf 'muse-planner-exec: the planner exceeded its %ss deadline and was terminated.\n' "$timeout_secs" >&2
	fi
	exit "$status"
fi

# No `timeout(1)`: the deadline is pi-subagents' own `timeoutMs` then, and this
# script says so rather than pretending it bounded anything.
printf 'muse-planner-exec: no `timeout(1)` on PATH, so only the agent deadline bounds this run.\n' >&2
"$muse_bin" "${muse_argv[@]}" </dev/null
exit $?
