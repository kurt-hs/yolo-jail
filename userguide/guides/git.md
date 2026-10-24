# Git Without a Credential in the Jail

The `git` pack lets an agent in a jail fetch from this project's git remotes with **your own SSH
keys or SSH agent on your machine**. The jail never holds a key or a token. Instead, a small
service on your machine, the **git-broker**, connects to each remote for the jail and streams the
result back. The jail runs the real `git`, unchanged.

This version **fetches only**. `git fetch`, `git pull` and `git ls-remote` work. Every push is
refused before anything reaches the remote, in git's own words, so you push from your machine.
Every call is recorded on your machine, and `yolo audit --service git` lists them.

## Turning it on

Your machine needs `ssh`, and whatever it already uses to reach your remotes: a key in `~/.ssh`
or a running SSH agent. `ssh-add -l` on your machine shows the agent's keys.

Select the pack in your user config (`~/.config/yolo-jail/config.jsonc`):

```jsonc
{
  "packs": ["claude", "git"]
}
```

And in each project that should have the broker, run this on your machine:

```bash
yolo loopholes enable git-broker
```

The command turns the broker on for that one project, from its next fresh launch. It never edits
your config: it writes a small file for the project in `~/.config/yolo-jail/workspaces/` and
prints its name. There is no switch that turns the broker on for every project, and `yolo check`
refuses a `git-broker` switch in your user config or in a project's own config.

Selecting the pack alone changes nothing about git. With the broker off, the jail's git behaves
exactly as it would without the pack.

### Approving the remotes

The next fresh launch of the project asks once, in its config-change prompt, to approve its
**remotes**: every `url` and `pushurl` in the project's `.git/config`, on any host, shown exactly
as written. The broker connects only to a remote you approved, and only to the address you
approved. With no terminal, pass `--accept-config-changes`, or run
`yolo check --accept-config-changes` on your machine first.

A remote whose `https://` address carries a password is left out, and the prompt says why.

A new remote joins the same way: add it with `git remote add`, then run `yolo config apply` on
your machine, in the project, and answer y.

## What happens

| | |
| :--- | :--- |
| `git fetch`, `git pull`, `git ls-remote` | Run through your machine's `ssh` against an approved remote, streamed back as git expects |
| `git push` | Refused per branch, nothing sent: `! [remote rejected] <branch> (needs approval, which this version cannot ask for)`. Push from your machine |
| A push to anything but a branch or a tag | Refused always, naming `refs/heads/` and `refs/tags/` |
| A remote you did not approve | Refused at once, naming the approved remotes and how to add one; nothing runs |
| No key or agent on your machine | git shows `ssh`'s own error, and the broker adds a line naming `ssh-add -l` on your machine |

An `https://` remote is refused in this version with a message to use the remote's SSH address.

## How it works

The pack sets `GIT_CONFIG_COUNT` and the `GIT_CONFIG_KEY_<n>` and `GIT_CONFIG_VALUE_<n>`
variables in the jail, which tell git to send `ssh://`, `https://` and `git@` remotes to a remote
helper, `git-remote-yolo`. The helper forwards git's own protocol to the broker on your machine,
which runs your `ssh`, so the key never leaves your machine. The variables are set only in a
project whose broker is on.

Not sent to the broker, and working as they would with no broker: `http://`, `git://`, `file://`
and local-path remotes, and an `scp`-style remote whose user is not `git` (`me@host:repo.git`).

An agent that exports its own `GIT_CONFIG_COUNT` replaces the pack's, and its fetches then fail
for want of a key. The briefing the pack gives each agent says not to.

## On each setup

The broker runs on your machine beside the jail, on podman on Linux and macOS and with
`macos-user`. Apple Container carries no traffic from a container back to the Mac, so the broker
is not available there. See
[the loopholes table](../reference/settings-per-setup.md#the-loopholes-host-services-a-jail-can-use).
