# Git Without a Credential in the Jail

The `git` pack lets an agent in a jail fetch from this project's git remotes with **your own
credentials on your machine**: your SSH keys or SSH agent for an SSH remote, and your git
credential helper for an HTTPS one. The jail never holds a key or a token. Instead, a small
service on your machine, the **git-broker**, connects to each remote for the jail and streams the
result back. The jail runs the real `git`, unchanged.

This version **fetches only**. `git fetch`, `git pull` and `git ls-remote` work. Every push needs
a grant this version cannot ask for, so every push is refused before anything reaches the remote,
in git's own words, and you push from your machine. Every call is recorded on your machine, and
`yolo audit --service git` lists them.

## Turning it on

Your machine needs whatever it already uses to reach your remotes:

- For an SSH remote, `ssh` and a key in `~/.ssh` or a running SSH agent. `ssh-add -l` on your
  machine shows the agent's keys.
- For an HTTPS remote, `git` and a credential helper that already holds a credential for the
  host, such as the one `git config credential.helper` names, a system keychain, or Git
  Credential Manager. The broker asks it with `git credential fill` and never prompts you.

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
exactly as it would without the pack, and the launch's "Not set" line names the command above.

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
| `git fetch`, `git pull`, `git ls-remote` over SSH | Run through your machine's `ssh` against an approved remote, streamed back as git expects |
| `git fetch`, `git pull`, `git ls-remote` over HTTPS | Sent to an approved remote over HTTPS, checked against your machine's trusted certificates, with the credential your machine's `git credential fill` gives when the server asks for one |
| `git push` | Refused per branch, nothing sent: `! [remote rejected] <branch> (needs approval, which this version cannot ask for)`. Push from your machine |
| A push to anything but a branch or a tag | Refused always, naming `refs/heads/` and `refs/tags/` |
| A remote you did not approve | Refused at once, naming the approved remotes and how to add one; nothing runs |
| No SSH key or agent on your machine | git shows `ssh`'s own error, and the broker adds a line naming `ssh-add -l` on your machine |
| No HTTPS credential on your machine | Refused, nothing sent, naming the `git credential fill` to run on your machine to see what your helper answers |
| An HTTPS server that does not speak git's protocol version 2 | Refused, nothing sent, naming the remote's SSH address as the way through |

## How it works

The pack sets `GIT_CONFIG_COUNT` and the `GIT_CONFIG_KEY_<n>` and `GIT_CONFIG_VALUE_<n>`
variables in the jail, which tell git to send `ssh://`, `https://` and `git@` remotes to a remote
helper, `git-remote-yolo`. The helper forwards git's own protocol to the broker on your machine,
which runs your `ssh`, or makes the HTTPS requests with your credential helper's answer, so the
credential never leaves your machine. The variables are set only in a project whose broker is on.

Not sent to the broker, and working as they would with no broker: `http://`, `git://`, `file://`
and local-path remotes, and an `scp`-style remote whose user is not `git` (`me@host:repo.git`).

An agent that exports its own `GIT_CONFIG_COUNT` replaces the pack's, and its fetches then fail
for want of a credential. The briefing the pack gives each agent says not to.

## On each setup

The broker runs on your machine beside the jail. It runs on podman on Linux, and should work on
podman on macOS and with `macos-user`, where it has not yet been run. Apple Container carries no
traffic from a container back to the Mac, so the broker is not available there. See
[the loopholes table](../reference/settings-per-setup.md#the-loopholes-host-services-a-jail-can-use).
