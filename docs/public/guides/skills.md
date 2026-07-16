---
title: Skills
weight: 40
description: Install, approve, and manage Agent Skills with the joe skills subcommand.
---

# Skills

**Agent Skills** are installable bundles of operational know-how that Joe loads from
`~/.joe/skills/`. You manage them with the `joe skills` subcommand. New installs land in
**quarantine** unless your skills policy auto-approves them, so adding a skill and
*activating* it are deliberately two steps.

## The subcommand set

`joe skills` has these subcommands — there is no `status` subcommand:

```
joe skills install <repo-url> [--ref <branch|tag>] [--subdir <path>]
joe skills list
joe skills remove <skill-name> [--force]
joe skills update [<skill-name>]
joe skills approve <skill-name>
joe skills reject <skill-name>
joe skills reload
```

## Install a skill

Install clones a **git repository**; you pass the repository URL as a positional
argument, not a local path. Pin a branch or tag with `--ref`, and install a single skill
out of a multi-skill repo with `--subdir` (a sparse checkout of that subdirectory):

```sh
joe skills install https://github.com/example/joe-sre-skills --ref v1 --subdir runbooks
```

A fresh install lands **quarantined** unless your policy auto-approves its source. The
command tells you which state it landed in.

## Approve or reject

Quarantine is a gate: a quarantined skill is on disk but not active. Promote it into the
active registry, or discard it:

```sh
joe skills approve <skill-name>   # activate a quarantined skill
joe skills reject  <skill-name>   # delete a quarantined skill from disk
```

Auto-approval is governed by a policy file, `~/.joe/skills-policy.yaml`. If the file is
absent, the policy is deny-by-default — everything lands quarantined until you approve it
by hand. (A malformed policy file is treated as fatal rather than silently trusting
everything.)

## List, update, remove

```sh
joe skills list                   # installed skills and their status (active / quarantined)
joe skills update [<skill-name>]  # fetch + reset every install, or just the one named
joe skills remove <skill-name> [--force]   # uninstall; --force if the install holds others
```

## Refresh the running daemon

Installing, approving, or removing changes the filesystem. To make the running daemon
rescan `~/.joe/skills/` without a restart:

```sh
joe skills reload
```

It reports how many skills were registered before and after, and what was added, removed,
or updated.

## Where to go next

- How skills feed Joe's autonomy → [The agent loop and autonomy](../../concepts/agent-loop-and-autonomy/)
