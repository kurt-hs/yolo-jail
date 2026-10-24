## `git` remotes in this jail

This jail's `git` is the real one, but its `ssh://`, `https://` and `git@` remotes go through the
git pack's broker on the host: `GIT_CONFIG_COUNT` and the `GIT_CONFIG_KEY_<n>` variables rewrite
them to `yolo::<url>`, and git runs `git-remote-yolo` (`~/.yolo/bin/block/git-remote-yolo`, a
forwarder to `yolo git-remote`). The host connects with its own SSH keys or agent; the jail holds
no git credential.

- `git fetch`, `git pull` and `git ls-remote` work against this workspace's remotes as the user
  approved them at launch.
- Every push is refused in this version, per ref, in git's own report:
  `! [remote rejected] <ref> (<reason>)`. Read the reason: `needs approval, which this version
  cannot ask for` means nothing was pushed and the user has to push from the host; a ref outside
  `refs/heads/` and `refs/tags/` is never pushed through the broker.
- A remote outside the approved scope is refused naming the scope. A new remote joins only when
  the user approves it in the config-change prompt on the host: add it with `git remote add`,
  then ask the user to run `yolo config apply` on the host, in this workspace, and answer y.
- `http://`, `git://`, `file://`, local paths, and an scp-style remote whose user is not `git`
  are not rewritten, so they behave as they would with no broker.
- Never set or export `GIT_CONFIG_COUNT` or `GIT_CONFIG_KEY_<n>` yourself: yours replaces
  the pack's, so the rewrite is gone and every fetch then fails for want of a credential.
- If git says the broker's endpoint is missing, the broker is not running for this jail: ask the
  user to run `yolo check` on the host.
