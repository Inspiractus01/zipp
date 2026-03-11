# 🪰 Zipp

Simple backup manager with scheduling and snapshots. Add your backup jobs, set an interval, forget about it.

## Install

**Linux (amd64):**
```bash
curl -sL https://github.com/Inspiractus01/zipp/releases/latest/download/zipp-linux-amd64 -o /usr/local/bin/zipp && chmod +x /usr/local/bin/zipp
```

**Linux (arm64 / Raspberry Pi):**
```bash
curl -sL https://github.com/Inspiractus01/zipp/releases/latest/download/zipp-linux-arm64 -o /usr/local/bin/zipp && chmod +x /usr/local/bin/zipp
```

**macOS (Apple Silicon):**
```bash
curl -sL https://github.com/Inspiractus01/zipp/releases/latest/download/zipp-darwin-arm64 -o /usr/local/bin/zipp && chmod +x /usr/local/bin/zipp
```

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
