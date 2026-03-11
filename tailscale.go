package main

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type tailscaleStatus struct {
	installed bool
	running   bool
	ip        string
}

func checkTailscale() tailscaleStatus {
	if _, err := exec.LookPath("tailscale"); err != nil {
		return tailscaleStatus{}
	}
	out, err := exec.Command("tailscale", "ip", "-4").Output()
	if err != nil {
		return tailscaleStatus{installed: true, running: false}
	}
	ip := strings.TrimSpace(string(out))
	return tailscaleStatus{installed: true, running: ip != "", ip: ip}
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
		cmd = exec.Command("bash", "-c", "curl -fsSL https://tailscale.com/install.sh | sh && tailscale up")
	} else {
		cmd = exec.Command("bash", "-c", "brew install tailscale && brew services start tailscale && tailscale up")
	}
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return nestTSDoneMsg{err: err}
	})
}

func tailscaleUpCmd() tea.Cmd {
	return func() tea.Msg {
		out, err := exec.Command("tailscale", "up").CombinedOutput()
		if err != nil {
			return nestTSDoneMsg{err: fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)}
		}
		return nestTSDoneMsg{}
	}
}

func tailscaleDownCmd() tea.Cmd {
	return func() tea.Msg {
		out, err := exec.Command("tailscale", "down").CombinedOutput()
		if err != nil {
			return nestTSDoneMsg{err: fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)}
		}
		return nestTSDoneMsg{}
	}
}
