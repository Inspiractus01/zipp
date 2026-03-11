package main

import (
	"os"
	"os/exec"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type schedulerStatus struct {
	active bool
	method string // "systemd", "cron", "none"
}

func checkScheduler() schedulerStatus {
	// check systemd first
	if runtime.GOOS == "linux" {
		out, err := exec.Command("systemctl", "is-active", "zipp.timer").Output()
		if err == nil && strings.TrimSpace(string(out)) == "active" {
			return schedulerStatus{active: true, method: "systemd"}
		}
	}

	// check cron
	out, err := exec.Command("crontab", "-l").Output()
	if err == nil && strings.Contains(string(out), "zipp run") {
		return schedulerStatus{active: true, method: "cron"}
	}

	return schedulerStatus{active: false, method: "none"}
}

func setupScheduler() ([]string, error) {
	var lines []string
	self, err := os.Executable()
	if err != nil {
		self = "zipp"
	}

	// try systemd on linux
	if runtime.GOOS == "linux" {
		if _, err := exec.LookPath("systemctl"); err == nil {
			lines = append(lines, "detected: Linux with systemd")
			err := installSystemd(self)
			if err == nil {
				lines = append(lines, styleSuccess.Render("✓ systemd timer installed (runs every hour)"))
				lines = append(lines, styleDim.Render("  systemctl status zipp.timer"))
				return lines, nil
			}
			lines = append(lines, styleWarning.Render("! systemd failed: "+err.Error()))
			lines = append(lines, styleDim.Render("  falling back to cron..."))
		}
	}

	// fallback: cron
	lines = append(lines, "detected: "+runtime.GOOS)
	if err := installCron(self); err != nil {
		return lines, err
	}
	lines = append(lines, styleSuccess.Render("✓ cron job installed (runs every hour)"))
	lines = append(lines, styleDim.Render("  crontab -l to verify"))
	return lines, nil
}

func installSystemd(bin string) error {
	svc := `[Unit]
Description=Zipp backup runner

[Service]
Type=oneshot
ExecStart=` + bin + ` run
`
	timer := `[Unit]
Description=Zipp backup timer

[Timer]
OnBootSec=5min
OnUnitActiveSec=1h
Persistent=true

[Install]
WantedBy=timers.target
`
	if err := writeFileRoot("/etc/systemd/system/zipp.service", svc); err != nil {
		return err
	}
	if err := writeFileRoot("/etc/systemd/system/zipp.timer", timer); err != nil {
		return err
	}
	if err := exec.Command("sudo", "systemctl", "daemon-reload").Run(); err != nil {
		return err
	}
	return exec.Command("sudo", "systemctl", "enable", "--now", "zipp.timer").Run()
}

func installCron(bin string) error {
	entry := "0 * * * * " + bin + " run\n"

	// get existing crontab
	existing, _ := exec.Command("crontab", "-l").Output()
	if strings.Contains(string(existing), "zipp run") {
		return nil // already there
	}

	combined := string(existing) + entry
	cmd := exec.Command("crontab", "-")
	cmd.Stdin = strings.NewReader(combined)
	return cmd.Run()
}

func writeFileRoot(path, content string) error {
	cmd := exec.Command("sudo", "tee", path)
	cmd.Stdin = strings.NewReader(content)
	return cmd.Run()
}

// tea.Cmd
func setupSchedulerCmd() tea.Cmd {
	return func() tea.Msg {
		lines, err := setupScheduler()
		return runResultMsg{lines: lines, err: err}
	}
}

type schedulerCheckMsg schedulerStatus

func checkSchedulerCmd() tea.Cmd {
	return func() tea.Msg {
		return schedulerCheckMsg(checkScheduler())
	}
}
