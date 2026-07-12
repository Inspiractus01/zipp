package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

var version = "dev" // set at build time via -ldflags

func main() {
	if len(os.Args) > 1 {
		runCLI(os.Args[1:])
		return
	}

	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading config: %v\n", err)
		os.Exit(1)
	}

	p := tea.NewProgram(newModel(cfg), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// runJobsCLI runs the given jobs sequentially, streaming output to stdout.
// Returns the number of failed jobs; failures also fire a desktop
// notification so scheduled runs don't fail silently.
func runJobsCLI(cfg *Config, jobs []*Job) int {
	failed := 0
	for _, job := range jobs {
		out := make(chan string, 128)
		done := make(chan struct{})
		go func() {
			for line := range out {
				fmt.Println(line)
			}
			close(done)
		}()
		err := runJob(job, cfg.Nest, out)
		close(out)
		<-done
		if err != nil {
			fmt.Fprintf(os.Stderr, "job %q failed: %v\n", job.Name, err)
			notifyFailure(job.Name, err)
			failed++
		}
	}
	return failed
}

func runCLI(args []string) {
	switch args[0] {

	case "run":
		cfg, err := loadConfig()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		var due []*Job
		for _, job := range cfg.Jobs {
			if job.isDue() {
				due = append(due, job)
			}
		}
		if len(due) == 0 {
			fmt.Println("no jobs due")
			return
		}
		failed := runJobsCLI(cfg, due)
		cfg.save()
		if failed > 0 {
			os.Exit(1)
		}

	case "run-all":
		cfg, err := loadConfig()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		var enabled []*Job
		for _, job := range cfg.Jobs {
			if job.Enabled {
				enabled = append(enabled, job)
			}
		}
		failed := runJobsCLI(cfg, enabled)
		cfg.save()
		if failed > 0 {
			os.Exit(1)
		}

	case "list":
		cfg, err := loadConfig()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if len(cfg.Jobs) == 0 {
			fmt.Println("no jobs configured")
			return
		}
		for _, j := range cfg.Jobs {
			status := "✓"
			if !j.Enabled {
				status = "✗"
			} else if j.isDue() {
				status = "⚡"
			}
			fmt.Printf("%s  %-24s  %s\n", status, j.Name, j.nextRun())
		}

	case "update":
		result := checkForUpdate()
		if !result.hasUpdate {
			fmt.Printf("zipp v%s is up to date\n", version)
			return
		}
		fmt.Printf("updating zipp v%s → v%s\n", version, result.latest)
		if err := runUpdate(); err != nil {
			fmt.Fprintf(os.Stderr, "update failed: %v\n", err)
			os.Exit(1)
		}

	case "uninstall", "--uninstall":
		uninstall()

	case "version", "-v", "--version":
		fmt.Printf("zipp v%s\n", version)

	case "help", "-h", "--help":
		fmt.Print(`zipp v` + version + `

Usage:
  zipp              open interactive UI
  zipp run          run jobs that are due (for cron/systemd)
  zipp run-all      run all enabled jobs
  zipp list         list all jobs
  zipp update       update zipp to the latest version
  zipp version      show version
  zipp uninstall    remove zipp from this system

`)

	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", args[0])
		os.Exit(1)
	}
}
