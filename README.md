# zipp

[![zipp](https://zipp.rest/social-card.png)](https://zipp.rest)

**[zipp.rest](https://zipp.rest)** — simple, fast backups for your terminal.

Add jobs, set a schedule, forget about it.

## Install

```bash
curl -sL https://raw.githubusercontent.com/Inspiractus01/zipp/main/install.sh | bash
```

macOS and Linux · amd64 and arm64 · release binaries are checksum-verified on install

## What it does

- **Snapshot backups** — every run creates a timestamped copy, old ones pruned automatically
- **Hardlink dedup** — unchanged files are hardlinked against the previous snapshot (rsync `--link-dest`), so 10 snapshots of a 5 GB folder don't cost 50 GB
- **Scheduler** — installs a launchd agent (macOS) or systemd timer (Linux), runs in the background; failed backups fire a desktop notification
- **Three backup modes** — `[local]` stays on this machine, `[nest]` goes to your server, `[nest+local]` does both
- **End-to-end encryption** — nest uploads are encrypted on your machine with [age](https://age-encryption.org) before they leave; the server only ever sees ciphertext
- **One-key restore** — browse snapshots and restore from the TUI

## Commands

```
zipp              open the TUI
zipp run          run jobs that are due (called by the scheduler)
zipp run-all      run all enabled jobs
zipp list         list all jobs
zipp update       update to the latest version
```

## Remote backups

Pair with [zipp-nest](https://github.com/Inspiractus01/zipp-nest) to back up to your own server — end-to-end encrypted, token-authenticated, no third-party storage.

1. Start zipp-nest on your server and copy the connection code from **Connection info**
2. In zipp, open **Nest** and paste the code (it contains the address and the auth token)
3. Press enter on a job → **Backup mode** → `nest` or `both`

> **Back up your encryption key!** Nest backups are encrypted with the key in
> `~/.zipp/nest.key`. Copy that file to any machine that should be able to
> restore them — without it the backups cannot be decrypted.

Config: `~/.zipp/config.json`
