package main

import (
	"fmt"
	"os/exec"
	"runtime"
)

// notifyFailure fires a best-effort desktop notification so scheduled
// background runs don't fail silently.
func notifyFailure(jobName string, err error) {
	msg := fmt.Sprintf("backup %q failed: %v", jobName, err)
	if len(msg) > 200 {
		msg = msg[:200] + "…"
	}
	switch runtime.GOOS {
	case "darwin":
		script := fmt.Sprintf(`display notification %q with title "zipp"`, msg)
		exec.Command("osascript", "-e", script).Run()
	case "linux":
		if _, lookErr := exec.LookPath("notify-send"); lookErr == nil {
			exec.Command("notify-send", "-u", "critical", "zipp", msg).Run()
		}
	}
}
