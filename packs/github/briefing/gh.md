## `gh` in this jail

`gh` here is `~/.yolo/bin/block/gh`, the github pack's forwarder, not a blocker. Each call goes
to the github-broker on the host, which runs the host's own `gh` login against this workspace's
GitHub repositories. The jail holds no GitHub token; `gh auth status` names the login.

- **The broker grants nothing until the user's config does.** A call refused with `no read
  grant` means the user has not granted read access: ask them to add
  `{"kind": "read", "repo": "*"}` to `brokered.github.grants` in their user config on the host,
  then run `yolo config apply` on the host, in this workspace, and approve the grant. You cannot
  grant it from the jail.
- With the read grant, reads run, and their output and exit codes are `gh`'s own. `--jq` and `--template` work as in
  `gh`, and so does piping `--json` output into `jq`.
- An in-scope write files a pending request and waits briefly for the host. Tell the user to run
  `yolo approve <id>` on the host to allow that exact command once, or `yolo approve <id> --deny`.
  The approval binds command, repository and stdin digest, is one use, and expires after 15 minutes.
  `--for` and `--add` are refused for GitHub writes; a legacy set-wide grant is not spent. If the
  request times out, it remains visible in the host broker log and `yolo approve`, and the same
  command can be retried after approval. A configured `on_request` hook is optional; its output
  is never consent.
- Exit 64 is a refusal, and its message says why and what to do instead. Refused: anything that
  could print the credential or reach the host, a repository outside this workspace's scope, and
  every call across the account. Raw `gh api graphql` is always refused; `gh`'s own commands
  (`gh pr view`, `gh issue list`, …) use GraphQL inside and work.
- A repository this workspace has no remote for stays out of scope until the user approves it.
  Add it to the `repos` list under `brokered.github` in the workspace config file your
  environment briefing names (create the key only if the file has none), or in the local file
  beside it for what the project should not commit, or when the config file is read-only here.
  Then run `yolo check --no-build`, and ask the user to run `yolo config apply` on the host, in
  this workspace, and answer y to the repository-scope prompt. In a jail started by this yolo,
  the running broker takes the repository at its next call; an older one needs a restart.
  Nothing changes before that y.
- A wildcard entry reaches more than one repository: `*/*` is every repository the host login can
  reach, `OWNER/*` is one owner's, `*/REPO` is that name under any owner. A whole segment is the
  only wildcard form. Use one only when the user asks for that reach; it widens only the repository
  scope. Writes still require exact host approval, and commands that act across the account remain
  refused (a search with no `--repo`, `gh repo list`, `gh status`).
- In a `gh api` path, percent-encode a slash in a branch name as usual:
  `repos/OWNER/REPO/branches/MS%2Fmain`.
- Exit 69: no broker in this jail, or no `gh` login on the host.
