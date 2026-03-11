# 🪰 Zipp

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

## Scheduling

On Linux, set up a systemd timer or cron job to run `zipp run` periodically:

```bash
# cron — check every hour
echo "0 * * * * /usr/local/bin/zipp run" | crontab -
```
