package main

import (
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type tailscaleStatus struct {
	installed bool
	loggedIn  bool
	running   bool
	ip        string
}

func checkTailscale() tailscaleStatus {
	if _, err := exec.LookPath("tailscale"); err != nil {
		return tailscaleStatus{}
	}
	out, err := exec.Command("tailscale", "ip", "-4").Output()
	if err == nil {
		ip := strings.TrimSpace(string(out))
		if ip != "" {
			return tailscaleStatus{installed: true, loggedIn: true, running: true, ip: ip}
		}
	}
	statusOut, _ := exec.Command("tailscale", "status").CombinedOutput()
	loggedIn := !strings.Contains(string(statusOut), "Logged out")
	return tailscaleStatus{installed: true, loggedIn: loggedIn, running: false}
}

type nestTSCheckMsg tailscaleStatus
type nestTSDoneMsg struct{ err error }

func checkNestTSCmd() tea.Cmd {
	return func() tea.Msg {
		return nestTSCheckMsg(checkTailscale())
	}
}

func installTailscaleCmd() tea.Cmd {
	var cmd *exec.Cmd
	if runtime.GOOS == "linux" {
		cmd = exec.Command("bash", "-c", "curl -fsSL https://tailscale.com/install.sh | sh")
	} else {
		cmd = exec.Command("bash", "-c", "brew install tailscale && brew services start tailscale")
	}
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return nestTSDoneMsg{err: err}
	})
}

func tailscaleLoginCmd() tea.Cmd {
	return tea.ExecProcess(exec.Command("tailscale", "login"), func(err error) tea.Msg {
		return nestTSDoneMsg{err: err}
	})
}

func tailscaleLogoutCmd() tea.Cmd {
	return tea.ExecProcess(exec.Command("sudo", "tailscale", "logout"), func(err error) tea.Msg {
		return nestTSDoneMsg{err: err}
	})
}

func tailscaleUpCmd() tea.Cmd {
	return tea.ExecProcess(exec.Command("sudo", "tailscale", "up"), func(err error) tea.Msg {
		return nestTSDoneMsg{err: err}
	})
}

func tailscaleDownCmd() tea.Cmd {
	return tea.ExecProcess(exec.Command("sudo", "tailscale", "down"), func(err error) tea.Msg {
		return nestTSDoneMsg{err: err}
	})
}

// checkNestTSCmdDelayed waits briefly before checking, so tailscale has time to update.
func checkNestTSCmdDelayed() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(1 * time.Second)
		return nestTSCheckMsg(checkTailscale())
	}
}
