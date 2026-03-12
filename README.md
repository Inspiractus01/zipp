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

## Website & SEO

- The marketing page in `docs/` now includes structured metadata and Open Graph tags that point to `https://zipp.rest/`. Update those values if the canonical domain changes.
- `docs/sitemap.xml` and `docs/robots.txt` are shipped inside the Docker image so search engines can index the page. Adjust the `lastmod` field or add entries if you create additional URLs.
- `docs/social-card.png` is referenced by OG/Twitter tags; replace it with another 1200×630 image if you want a different preview.
