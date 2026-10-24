# Git Without a Credential in the Jail

The `git` pack lets an agent in a jail fetch from this project's git remotes with **your own
credentials on your machine**: your SSH keys or SSH agent for an SSH remote, and your git
credential helper for an HTTPS one. The jail never holds a key or a token. Instead, a small
service on your machine, the **git-broker**, connects to each remote for the jail and streams the
result back. The jail runs the real `git`, unchanged.

`git fetch`, `git pull` and `git ls-remote` work. A push goes through only when you have
granted it ahead of time: with a **create-branch grant** the agent may push a new branch, and
then keep pushing to the branches it created that way (its **working set**). Every other push is
refused before anything reaches the remote, in git's own words, and you push it from your machine.
Every call is recorded on your machine, and `yolo audit --service git` lists them.

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

## Letting the agent push its own branches

Two settings, one in each config, let the agent create branches:

1. **Your user config allows the kind of grant.** Without this, no project can hold it, and
   `yolo check` refuses a project that asks, naming this key:

   ```jsonc
   // ~/.config/yolo-jail/config.jsonc
   {
     "packs": ["claude", "git"],
     "brokered": { "git": { "grant_kinds": ["create-branch"] } }
   }
   ```

2. **The project config names the grant and its repository**, as `host/path` (the remote's
   address without the user, the scheme or `.git`), or `"*"` for every approved remote:

   ```jsonc
   // yolo-jail.jsonc in the project
   {
     "brokered": { "git": { "grants": [{ "kind": "create-branch", "repo": "github.com/me/app" }] } }
   }
   ```

The grant joins at the next config-change prompt, where you approve it beside the remotes, or at
`yolo config apply` for a running jail. Remove `create-branch` from your user config and the next
push stops creating branches, in every project.

With the grant, the agent may push a branch that does not exist yet on that repository. When the
remote accepts it, the branch joins the project's **working set**: the branches the agent may
push to again without asking, update, force-push or delete. The working set belongs to the
project, not to one jail: two jails of the project share it, and it lasts across launches until
the branch is deleted (by a push through the broker, or found gone on the remote at the next
push) or you revoke it. A branch you pushed from your machine, or one another project created,
is never in it, and neither is `main` or any other branch the agent did not create: those pushes
are still refused.

On your machine, `yolo approve` lists each project's working set:

```text
w-0c7b1e813e940452e59c53612436f6df  working set  git  [/home/me/code/app]  repo github.com/me/app  branch refs/heads/agent/x  by create-branch  since 2026-10-24T04:43:12Z
```

```bash
yolo approve --revoke w-0c7b1e813e940452e59c53612436f6df   # one branch
yolo approve --revoke all --workspace ~/code/app            # every grant and branch of that project
```

## What happens

| | |
| :--- | :--- |
| `git fetch`, `git pull`, `git ls-remote` over SSH | Run through your machine's `ssh` against an approved remote, streamed back as git expects |
| `git fetch`, `git pull`, `git ls-remote` over HTTPS | Sent to an approved remote over HTTPS, checked against your machine's trusted certificates, with the credential your machine's `git credential fill` gives when the server asks for one |
| A push of a new branch, with a create-branch grant | Sent to the remote; when the remote accepts it, the branch joins the working set |
| A push to a branch in the working set | Sent to the remote, force-pushes and deletes included |
| Any other `git push` | Refused per branch, nothing sent: `! [remote rejected] <branch> (needs approval, which this version cannot ask for)`. Push from your machine |
| A push of several branches, one of them refused | Refused whole: a push through the broker goes through entirely or not at all |
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
