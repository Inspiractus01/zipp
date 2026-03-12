# zipp

```
  )()(
 ( ●● )
  \──/
  /||\
```

Simple backup manager for your terminal. Add jobs, set a schedule, forget about it.

## Install

```bash
curl -sL https://raw.githubusercontent.com/Inspiractus01/zipp/main/install.sh | bash
```

macOS and Linux · amd64 and arm64

## What it does

- **Snapshot backups** — every run creates a timestamped copy, old ones pruned automatically
- **Scheduler** — installs a launchd agent (macOS) or systemd timer (Linux), runs in the background
- **Three backup modes** — `[local]` stays on this machine, `[nest]` goes to your server, `[nest+local]` does both
- **Live sync** — watch a folder and sync instantly on any file change
- **One-key restore** — browse snapshots and restore from the TUI

## Commands

```
zipp              open the TUI
zipp run          run jobs that are due (called by the scheduler)
zipp watch        start live sync watchers
zipp list         list all jobs
```

## Remote backups

Pair with [zipp-nest](https://github.com/Inspiractus01/zipp-nest) to back up to your own server — end-to-end encrypted, no third-party storage.

Config: `~/.zipp/config.json`

## Website

The landing page moved to its own repo (`zipp-site`). Clone it separately, keep it private, and trigger deployments with the webhook script in that repo.
