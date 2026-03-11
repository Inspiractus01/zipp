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

func runCLI(args []string) {
	switch args[0] {

	case "run":
		cfg, err := loadConfig()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		out := make(chan string, 128)
		go func() {
			for line := range out {
				fmt.Println(line)
			}
		}()
		ran := 0
		for _, job := range cfg.Jobs {
			if job.isDue() {
				if err := runJob(job, out); err != nil {
					fmt.Fprintf(os.Stderr, "job %q failed: %v\n", job.Name, err)
				}
				ran++
			}
		}
		close(out)
		if ran == 0 {
			fmt.Println("no jobs due")
		}
		cfg.save()

	case "run-all":
		cfg, err := loadConfig()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		out := make(chan string, 128)
		go func() {
			for line := range out {
				fmt.Println(line)
			}
		}()
		for _, job := range cfg.Jobs {
			if !job.Enabled {
				continue
			}
			if err := runJob(job, out); err != nil {
				fmt.Fprintf(os.Stderr, "job %q failed: %v\n", job.Name, err)
			}
		}
		close(out)
		cfg.save()

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

	case "uninstall", "--uninstall":
		uninstall()

	case "version", "-v", "--version":
		fmt.Printf("zipp v%s\n", version)

	case "help", "-h", "--help":
		fmt.Print(`🪰 Zipp v` + version + `

Usage:
  zipp              open interactive UI
  zipp run          run jobs that are due (for cron/systemd)
  zipp run-all      run all enabled jobs
  zipp list         list all jobs
  zipp version      show version
  zipp uninstall    remove zipp from this system

`)

	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", args[0])
		os.Exit(1)
	}
}
