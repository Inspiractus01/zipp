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
	if runtime.GOOS == "linux" {
		out, err := exec.Command("systemctl", "is-active", "zipp.timer").Output()
		if err == nil && strings.TrimSpace(string(out)) == "active" {
			return schedulerStatus{active: true, method: "systemd"}
		}
	}

	if runtime.GOOS == "darwin" {
		home, _ := os.UserHomeDir()
		plist := home + "/Library/LaunchAgents/com.zipp.runner.plist"
		if _, err := os.Stat(plist); err == nil {
			return schedulerStatus{active: true, method: "launchd"}
		}
	}

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

	// macOS — launchd (no Full Disk Access needed, writes to ~/Library/LaunchAgents)
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

	// unload first in case an older version is already loaded
	exec.Command("launchctl", "unload", plist).Run()
	return exec.Command("launchctl", "load", plist).Run()
}

func installCron(bin string) error {
	entry := "0 * * * * " + bin + " run\n"
	existing, _ := exec.Command("crontab", "-l").Output()
	if strings.Contains(string(existing), "zipp run") {
		return nil
	}
	cmd := exec.Command("crontab", "-")
	cmd.Stdin = strings.NewReader(string(existing) + entry)
	return cmd.Run()
}

func writeFileRoot(path, content string) error {
	cmd := exec.Command("sudo", "tee", path)
	cmd.Stdin = strings.NewReader(content)
	cmd.Stdout = nil
	return cmd.Run()
}

func uninstall() {
	fmt.Println("uninstalling Zipp...")

	if runtime.GOOS == "linux" {
		exec.Command("sudo", "systemctl", "disable", "--now", "zipp.timer").Run()
		exec.Command("sudo", "rm", "-f",
			"/etc/systemd/system/zipp.service",
			"/etc/systemd/system/zipp.timer",
		).Run()
		exec.Command("sudo", "systemctl", "daemon-reload").Run()
		fmt.Println("✓ removed systemd timer")
	}

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

	exec.Command("sudo", "rm", "-f", "/usr/local/bin/zipp").Run()
	fmt.Println("✓ removed binary")

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

// setupSchedulerCmd exits alt-screen on Linux so sudo prompts work.
// On macOS launchd writes to ~/Library/LaunchAgents — no sudo needed.
func setupSchedulerCmd() tea.Cmd {
	if runtime.GOOS == "linux" {
		return setupSchedulerLinuxCmd()
	}
	return func() tea.Msg {
		lines, err := setupScheduler()
		return runResultMsg{lines: lines, err: err}
	}
}

func setupSchedulerLinuxCmd() tea.Cmd {
	self, _ := os.Executable()
	if self == "" {
		self = "/usr/local/bin/zipp"
	}
	script := fmt.Sprintf(`set -e
echo "detected: Linux + systemd"
sudo tee /etc/systemd/system/zipp.service > /dev/null << 'UNIT'
[Unit]
Description=Zipp backup runner

[Service]
Type=oneshot
ExecStart=%s run
UNIT

sudo tee /etc/systemd/system/zipp.timer > /dev/null << 'UNIT'
[Unit]
Description=Zipp backup timer

[Timer]
OnBootSec=5min
OnUnitActiveSec=1h
Persistent=true

[Install]
WantedBy=timers.target
UNIT

sudo systemctl daemon-reload
sudo systemctl enable --now zipp.timer
echo "✓ systemd timer installed (runs every hour)"
`, self)

	cmd := exec.Command("bash", "-c", script)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		var lines []string
		if err != nil {
			return runResultMsg{lines: lines, err: fmt.Errorf("setup failed: %w", err)}
		}
		lines = append(lines, styleSuccess.Render("✓ systemd timer installed (runs every hour)"))
		lines = append(lines, styleDim.Render("  systemctl status zipp.timer"))
		return runResultMsg{lines: lines}
	})
}

type schedulerCheckMsg schedulerStatus

func checkSchedulerCmd() tea.Cmd {
	return func() tea.Msg {
		return schedulerCheckMsg(checkScheduler())
	}
}
