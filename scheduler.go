package main

import (
	"fmt"
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
	// systemd (linux)
	if runtime.GOOS == "linux" {
		out, err := exec.Command("systemctl", "is-active", "zipp.timer").Output()
		if err == nil && strings.TrimSpace(string(out)) == "active" {
			return schedulerStatus{active: true, method: "systemd"}
		}
	}

	// launchd (macOS)
	if runtime.GOOS == "darwin" {
		home, _ := os.UserHomeDir()
		plist := home + "/Library/LaunchAgents/com.zipp.runner.plist"
		if _, err := os.Stat(plist); err == nil {
			return schedulerStatus{active: true, method: "launchd"}
		}
	}

	// cron fallback
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

	// linux — systemd
	if runtime.GOOS == "linux" {
		if _, err := exec.LookPath("systemctl"); err == nil {
			lines = append(lines, "detected: Linux + systemd")
			if err := installSystemd(self); err == nil {
				lines = append(lines, styleSuccess.Render("✓ systemd timer installed (runs every hour)"))
				lines = append(lines, styleDim.Render("  systemctl status zipp.timer"))
				return lines, nil
			} else {
				lines = append(lines, styleWarning.Render("! systemd failed: "+err.Error()))
				lines = append(lines, styleDim.Render("  falling back to cron..."))
			}
		}
	}

	// macOS — launchd (no FDA needed, works out of the box)
	if runtime.GOOS == "darwin" {
		lines = append(lines, "detected: macOS")
		if err := installLaunchd(self); err == nil {
			lines = append(lines, styleSuccess.Render("✓ launchd agent installed (runs every hour)"))
			lines = append(lines, styleDim.Render("  launchctl list | grep zipp"))
			return lines, nil
		} else {
			lines = append(lines, styleWarning.Render("! launchd failed: "+err.Error()))
			lines = append(lines, styleDim.Render("  falling back to cron..."))
		}
	}

	// fallback: cron
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		lines = append(lines, "detected: "+runtime.GOOS)
	}
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

func installLaunchd(bin string) error {
	home, _ := os.UserHomeDir()
	agentsDir := home + "/Library/LaunchAgents"
	plist := agentsDir + "/com.zipp.runner.plist"

	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		return err
	}

	content := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>com.zipp.runner</string>
	<key>ProgramArguments</key>
	<array>
		<string>` + bin + `</string>
		<string>run</string>
	</array>
	<key>StartInterval</key>
	<integer>3600</integer>
	<key>RunAtLoad</key>
	<true/>
</dict>
</plist>
`
	if err := os.WriteFile(plist, []byte(content), 0644); err != nil {
		return err
	}

	// load it (unload first in case it exists)
	exec.Command("launchctl", "unload", plist).Run()
	return exec.Command("launchctl", "load", plist).Run()
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
	cmd.Stdout = nil // suppress tee output
	return cmd.Run()
}

func uninstall() {
	fmt.Println("uninstalling Zipp...")

	// systemd
	if runtime.GOOS == "linux" {
		exec.Command("sudo", "systemctl", "disable", "--now", "zipp.timer").Run()
		exec.Command("sudo", "rm", "-f",
			"/etc/systemd/system/zipp.service",
			"/etc/systemd/system/zipp.timer",
		).Run()
		exec.Command("sudo", "systemctl", "daemon-reload").Run()
		fmt.Println("✓ removed systemd timer")
	}

	// cron
	existing, err := exec.Command("crontab", "-l").Output()
	if err == nil {
		cleaned := ""
		for _, line := range strings.Split(string(existing), "\n") {
			if !strings.Contains(line, "zipp run") {
				cleaned += line + "\n"
			}
		}
		cmd := exec.Command("crontab", "-")
		cmd.Stdin = strings.NewReader(cleaned)
		if cmd.Run() == nil {
			fmt.Println("✓ removed cron job")
		}
	}

	// binary
	exec.Command("sudo", "rm", "-f", "/usr/local/bin/zipp").Run()
	fmt.Println("✓ removed binary")

	// config — ask
	home, _ := os.UserHomeDir()
	cfgDir := home + "/.zipp"
	fmt.Print("remove config and jobs (~/.zipp)? [y/N] ")
	var answer string
	fmt.Scanln(&answer)
	if strings.ToLower(answer) == "y" {
		os.RemoveAll(cfgDir)
		fmt.Println("✓ removed ~/.zipp")
	}

	fmt.Println("\nzipp uninstalled. bye 🪰")
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
