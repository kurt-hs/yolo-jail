## `git` remotes in this jail

**First check whether the broker runs for this jail:** `echo "${GIT_CONFIG_COUNT:-unset}"`.

- **`unset`: the git broker did not start for this launch.** It runs whenever the git pack is
  selected, so this is a backend that runs no host services (Apple Container) or a broker that
  failed to start. Plain git behaves exactly as it would without the git pack: no remote is
  rewritten, and a remote that needs a credential fails for want of one, since the jail holds
  none. Ask the user to run `yolo check` on the host, in this workspace.
- **A number: the broker runs.** This jail's `git` is the real one, but its `ssh://`,
  `https://` and `git@` remotes go through the git pack's broker on the host: `GIT_CONFIG_COUNT`
  and the `GIT_CONFIG_KEY_<n>` variables rewrite them to `yolo::<url>`, and git runs
  `git-remote-yolo` (`~/.yolo/bin/block/git-remote-yolo`, a forwarder to `yolo git-remote`). The
  host connects with its own credentials, its SSH keys or agent for an SSH remote and its git
  credential helper for an HTTPS one; the jail holds no git credential.

With the broker running:

- **It grants nothing until the user's config does.** SSH fetches and pushes refused with `no read
  grant` mean the user has not granted read access: ask them to add
  `{"kind": "read", "repo": "*"}` to `brokered.git.grants` in their user config on the host, then
  run `yolo config apply` on the host, in this workspace, and approve the grant. The running
  broker takes it at its next call. You cannot grant it from the jail, and a workspace config
  edit does not either. A public HTTPS fetch is the exception: it is tried anonymously, and never
  asks the host credential helper.
- With the read grant, `git fetch`, `git pull` and `git ls-remote` work against this
  workspace's approved remotes, over SSH or HTTPS. An HTTPS credential is used only after that
  exact remote and read grant are approved.
- **A push goes through only when the user granted it ahead of time.** With a `create-branch`
  grant on the repository, you may push a branch that does not exist on the remote yet; once the
  remote accepts it, it joins this workspace's working set, and later pushes to it (updates,
  force-pushes, deletes) go through too, in this jail and in later ones. A `branch` grant in
  the workspace config declares a branch (such as `main`) in the working set too: you may
  create, push, force-push and delete it like one you created. Unless the working set holds it,
  push your work to a new branch of your own, not to `main` or a branch someone else created.
  Every other push is refused whole, per ref, in git's own report:
  `! [remote rejected] <ref> (<reason>)`, and nothing is pushed. Read the reason: `needs
  approval, which this version cannot ask for` means the user has to push it from the host (or
  grant `create-branch` for a new branch, or a `branch` grant for that branch, in the workspace
  config and approve it with `yolo config apply`); a ref outside `refs/heads/` and `refs/tags/` is never pushed through
  the broker. Do not retry a refused push.
- A remote outside the approved scope is refused for SSH and push. A public HTTPS fetch may be
  tried anonymously outside that scope; if it asks for credentials, it is refused naming the
  scope. A new remote joins only when the user approves it in the config-change prompt on the
  host: add it with `git remote add`, then ask the user to run `yolo config apply` on the host, in
  this workspace, and answer y.
- An HTTPS fetch the host has no credential for is refused naming the `git credential fill` the
  user can run on the host, but only after its URL and read grant are approved; public fetches
  never ask the helper. The broker never prompts.
- `http://`, `git://`, `file://`, local paths, and an scp-style remote whose user is not `git`
  are not rewritten, so they behave as they would with no broker.
- Never set or export `GIT_CONFIG_COUNT` or `GIT_CONFIG_KEY_<n>` yourself: yours replaces
  the pack's, so the rewrite is gone and every fetch then fails for want of a credential.
- If git says the broker's endpoint is missing, the broker is not running for this jail: ask the
  user to run `yolo check` on the host.
