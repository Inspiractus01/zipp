# Zipp

```
    \    /\    /
     \  /  \  /
     (●      ●)
      \______/
        ||||
       /||||\
```

Simple backup manager with scheduling and snapshots. Add your backup jobs, set an interval, forget about it.

## Install

```bash
curl -sL https://raw.githubusercontent.com/Inspiractus01/zipp/main/install.sh | bash
```

Auto-detects your OS and architecture (Linux/macOS, amd64/arm64).

## Usage

```
zipp              open the UI
zipp run          run jobs that are due  (for cron/systemd)
zipp list         list all jobs
```

Jobs and config are stored in `~/.zipp/config.json`.

## Features

- **Snapshot backups** — each run creates a timestamped copy via rsync
- **Auto-pruning** — keeps only N most recent snapshots per job
- **Scheduler** — sets up systemd timer (Linux) or launchd (macOS) automatically
- **Update check** — notifies you when a new version is available
- **Nice TUI** — animated fly, purple/blue theme

## Scheduling

Open `zipp` and select **Setup** — it detects your OS and installs the right scheduler automatically.
