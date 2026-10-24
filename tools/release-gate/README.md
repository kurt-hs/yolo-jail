# Read-only pre-tag CI proof

`release-gate` waits for a successful regular CI push run for one frozen commit. It never creates
refs/releases, dispatches workflows, or invokes Git. Callers must pass the SHA they already froze; the
helper does not replace it with a newer branch tip.

```console
go run ./tools/release-gate --repo OWNER/REPO --sha FULL_SHA --timeout 60m --poll-interval 15s
```

`--repo` and a full 40-hex `--sha` are required. The default total deadline is 1 hour; the default poll
interval is 15 seconds. Both values must be positive (bounded at 24 hours and 10 minutes respectively).
Individual HTTP requests are capped at 30 seconds. Exit 0 prints a JSON proof to stdout. Invalid input exits
2. Missing/failed/cancelled/incomplete proof, API failure, or timeout exits 1, writes progress/refusal to
stderr, and states that nothing was tagged.

The policy is fixed: `.github/workflows/ci.yml`, `push`, `main`, exact SHA and repository. The workflow
identity is resolved from the Actions API, not trusted by its display name. The newest eligible run by run
number (then run ID) wins; an older green run never substitutes for a newer queued or failed matching run.
The helper waits through queued/running states and missing runs until the deadline, but refuses completed
non-success conclusions immediately. It verifies the latest run attempt and paginates the jobs endpoint,
requiring successful `secrets-scan`, `check-go`, `check-macos`, both `build-image` matrix jobs, all four
`integration` architecture/shard jobs, and `integration-complete`. If these job IDs/matrix cardinalities
change in `ci.yml`, update this fixed gate and its fixtures with the workflow change.

Optional credentials are read only from `GH_TOKEN`, then `GITHUB_TOKEN`; there is no token flag. Redirects
are not followed, response bodies and credentials are never printed, and API retries are limited to 429/5xx
within the overall deadline. Tests use an offline fake HTTP server, injected clock and sleeper; no real API,
credentials, tag or publication is used by the test suite.

## The darwin floor cache gate

`release-gate darwin-floor` waits until both macOS floor-profile store paths of a frozen commit are in the
project's public Cachix cache ([PTG-D4](../../docs/reference/release-process.md#PTG-D4)). It also never
creates refs, dispatches workflows, or invokes Git; the caller,
[`darwin-floor-gate.sh`](../release-wiring/darwin-floor-gate.sh), computes the commit list.

```console
go run ./tools/release-gate darwin-floor --repo OWNER/REPO --cache NAME --sha FULL_SHA \
  [--later-flake-change FULL_SHA] --path STORE_PATH --path STORE_PATH \
  --wait-shas FILE --timeout 100m --poll-interval 30s
```

Each `--path` must be a `/nix/store/<hash>-<name>` path; `--wait-shas` is a file of full commit SHAs, one per
line. A path is cached when `https://NAME.cachix.org/<hash>.narinfo` answers 200 with a `StorePath:` naming
it; a 404 is a miss; 429/5xx retry; anything else refuses. A profile counts only when every path on its
narinfo's `References:` line is cached too. While a `darwin-floor-cache.yml` run for one of the listed
commits (a push on main, or a dispatch on any branch) is queued or running, it polls, even when every path is
already cached. With none pending it reads the cache once more, then passes or refuses with the dispatch to
run: on main, or, when `--later-flake-change` names a later flake change on main, on a branch pushed at
`--sha`. Exit 0 prints `{"cache": ..., "paths": [...]}`; a refusal,
failure or timeout exits 1 with the next step on stderr; invalid input exits 2.
