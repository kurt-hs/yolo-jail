# Git Without a Credential in the Jail

The `git` pack lets an agent in a jail fetch from this project's git remotes with **your own
credentials on your machine**: your SSH keys or SSH agent for an SSH remote, and your git
credential helper for an HTTPS one. The jail never holds a key or a token. Instead, a small
service on your machine, the **git-broker**, connects to each remote for the jail and streams the
result back. The jail runs the real `git`, unchanged.

`git fetch`, `git pull` and `git ls-remote` work. A push goes through only when you have
granted it ahead of time: with a **create-branch grant** the agent may push a new branch, and
then keep pushing to the branches it created that way. Those branches are its **working set**,
and a **branch grant** in the project config adds a branch you name, such as `main`, to it. Every
other push is
refused before anything reaches the remote, in git's own words, and you push it from your machine.
Every call is recorded on your machine, and `yolo audit --service git` lists them.

## Turning it on

Your machine needs whatever it already uses to reach your remotes:

- For an SSH remote, `ssh` and a key in `~/.ssh` or a running SSH agent. `ssh-add -l` on your
  machine shows the agent's keys.
- For an HTTPS remote, `git` and a credential helper that already holds a credential for the
  host, such as the one `git config credential.helper` names, a system keychain, or Git
  Credential Manager. The broker asks it with `git credential fill` and never prompts you.

Select the pack, and grant read access, in your user config (`~/.config/yolo-jail/config.jsonc`):

```jsonc
{
  "packs": ["claude", "git"],
  "brokered": { "git": { "grants": [{ "kind": "read", "repo": "*" }] } }
}
```

Selecting the pack starts the broker in every project, and on its own it **grants nothing**: an SSH
fetch or any push without the read grant is refused, naming the grant to add. A public HTTPS fetch
is tried anonymously and never uses a host credential; if that server asks for one, the fetch is
refused until the read grant and the project's remote are approved. The read grant lets each
project use its own approved remotes, and nothing else; `"*"` means each project's own remotes,
never every repository you can reach.

The grant never takes effect unseen. Each project's next config-change prompt, or
`yolo config apply` run on your machine in the project, shows it as a line of the
repository-scope block, marked as coming from your user config, beside the remotes it covers,
and asks you to approve it. A running jail's broker takes it at its next call, with no restart.
Without the read grant, the prompt asks nothing about the broker at all; withdrawing the grant
shows the remotes and the grant removed once, and after that the prompt is silent again.

The broker has no on/off switch: `yolo loopholes enable git-broker` refuses, and `yolo check`
refuses a `git-broker` switch in any config file. To stop the broker, take `git` out of `packs`.
An older yolo's per-project switch is ignored with a warning; `yolo loopholes disable git-broker`
removes it.

### Approving the remotes

Once the read grant applies, the next fresh launch of the project asks once, in its
config-change prompt, to approve its **remotes**: every `url` and `pushurl` in the project's `.git/config`, on any host, shown exactly
as written. The broker uses host credentials only for a remote you approved, and only to the address you
approved. A public HTTPS fetch may be tried anonymously without adding that remote to the approved
scope; that exception grants no credential access and cannot be used to push. With no terminal, pass
`--accept-config-changes`, or run `yolo check --accept-config-changes` on your machine first.

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
are still refused, unless a [branch grant](#declaring-a-working-set-branch-in-config) in the
project config declares the branch.

On your machine, `yolo approve` lists each project's working set:

```text
w-0c7b1e813e940452e59c53612436f6df  working set  git  [/home/me/code/app]  repo github.com/me/app  branch refs/heads/agent/x  by create-branch  since 2026-10-24T04:43:12Z
```

```bash
yolo approve --revoke w-0c7b1e813e940452e59c53612436f6df   # one branch
yolo approve --revoke all --workspace ~/code/app            # every recorded branch of that project; branches declared in config stay
```

Revoking ends what the broker recorded: the working-set entries, and any grant you allowed with
`yolo approve`. It does not end the create-branch grant in the project's config, so the agent can
still push a new branch afterward. To stop that, remove the grant from the project's
`yolo-jail.jsonc` (`brokered.git.grants`), or remove `create-branch` from `brokered.git.grant_kinds`
in your user config; the broker checks your user config at every push. A `--workspace` path that
matches nothing says so and exits with an error; the path in brackets in `yolo approve` is the one
to give.

## Declaring a working-set branch in config

A **branch grant** puts a branch you name, such as `main` of the agent's own fork, in the
project's working set, declared in config rather than created by the agent. It is granted the same
two ways as create-branch:

1. **Your user config allows the kind**, beside any other kind you allow:

   ```jsonc
   // ~/.config/yolo-jail/config.jsonc
   {
     "packs": ["claude", "git"],
     "brokered": { "git": { "grant_kinds": ["create-branch", "branch"] } }
   }
   ```

2. **The project config names the grant, its repository and its branch.** The branch is one
   branch's short name, `main` for `refs/heads/main`. There is no pattern: list each branch as its
   own grant.

   ```jsonc
   // yolo-jail.jsonc in the project
   {
     "brokered": { "git": { "grants": [
       { "kind": "branch", "repo": "github.com/me/app", "branch": "main" }
     ] } }
   }
   ```

The grant joins at the next config-change prompt, or at `yolo config apply` for a running jail,
as create-branch does, and `yolo check` refuses it while your user config does not list
`branch`. A `branch` that is not a branch name git accepts, one written as `refs/heads/main`, or a
pattern such as `"*"` is refused when the config loads, saying how to write it. So is `"repo": "*"`:
it would put the branch of every approved remote in the working set, an upstream project's
`main` and any remote you approve later included. Name the repository, and list one grant per
repository.

The branch is then in the working set like one the agent created: the agent may push it, create
it if it does not exist yet, force-push it and delete it. The remote's own branch protection
still applies. A tag is never covered. The config holds the branch, not the broker's record, so a
delete through the broker leaves it declared, and the agent may push it again. While a jail of
the project runs, `yolo approve` lists the branch beside the recorded ones, marked `from config`
with the file that declares it:

```text
c-5d0f3c2a9b8e71d4a6c0e9f2b1d3a7c5  working set  git  [/home/me/code/app]  repo github.com/me/app  branch refs/heads/main  from config /home/me/code/app/yolo-jail.jsonc
```

`yolo approve --revoke` refuses such a line and names that file, and
`yolo approve --revoke all --workspace` leaves it in place. To take the branch out, remove
the grant from the project's `yolo-jail.jsonc` and run `yolo config apply`, or remove `branch`
from your user config. The broker reads both at every push, so either reaches the next one.

## What happens

| | |
| :--- | :--- |
| `git fetch`, `git pull`, `git ls-remote` over SSH | Run through your machine's `ssh` against an approved remote, streamed back as git expects |
| `git fetch`, `git pull`, `git ls-remote` over HTTPS | Sent to an approved remote over HTTPS, checked against your machine's trusted certificates, with the credential your machine's `git credential fill` gives when the server asks for one |
| A push of a new branch, with a create-branch grant | Sent to the remote; when the remote accepts it, the branch joins the working set |
| A push to a branch in the working set, recorded or declared in config | Sent to the remote, force-pushes and deletes included |
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
