package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
)

func startWatchers(cfg *Config) error {
	var jobs []*Job
	for _, j := range cfg.Jobs {
		if j.WatchMode && j.Enabled {
			jobs = append(jobs, j)
		}
	}
	if len(jobs) == 0 {
		fmt.Println("  no jobs with watch mode enabled")
		fmt.Println("  open zipp and press w on a job to enable live sync")
		return nil
	}

	output := make(chan string, 128)
	go func() {
		for line := range output {
			fmt.Println(line)
		}
	}()

	fmt.Println()
	fmt.Println("  )()(")
	fmt.Println(" ( ●● )  zipp watch")
	fmt.Println("  \\──/")
	fmt.Println("  /||\\")
	fmt.Println()

	var wg sync.WaitGroup
	for _, job := range jobs {
		wg.Add(1)
		go func(j *Job) {
			defer wg.Done()
			watchJob(j, cfg, output)
		}(job)
	}

	// wait for Ctrl+C
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	fmt.Println("\n  stopping watchers...")
	return nil
}

func watchJob(job *Job, cfg *Config, output chan<- string) {
	src := expandPath(job.Source)

	w, err := fsnotify.NewWatcher()
	if err != nil {
		output <- fmt.Sprintf("  ✗ %s: watcher error: %v", job.Name, err)
		return
	}
	defer w.Close()

	if err := addDirsRecursive(w, src); err != nil {
		output <- fmt.Sprintf("  ✗ %s: cannot watch %s: %v", job.Name, src, err)
		return
	}

	output <- fmt.Sprintf("  ~ watching  %-16s  %s", job.Name, src)

	var mu sync.Mutex
	var pending bool

	trigger := func() {
		mu.Lock()
		if pending {
			mu.Unlock()
			return
		}
		pending = true
		mu.Unlock()

		time.AfterFunc(3*time.Second, func() {
			mu.Lock()
			pending = false
			mu.Unlock()

			output <- fmt.Sprintf("  ↺ change · %s", job.Name)
			if err := runJob(job, cfg.Nest, output); err != nil {
				output <- fmt.Sprintf("  ✗ %v", err)
			} else {
				// reload and re-save so LastRun persists
				if fresh, err := loadConfig(); err == nil {
					for _, j := range fresh.Jobs {
						if j.ID == job.ID {
							j.LastRun = job.LastRun
							break
						}
					}
					fresh.save()
				}
			}
		})
	}

	for {
		select {
		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			switch {
			case ev.Has(fsnotify.Create):
				if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
					w.Add(ev.Name)
				}
				trigger()
			case ev.Has(fsnotify.Write), ev.Has(fsnotify.Remove), ev.Has(fsnotify.Rename):
				trigger()
			}
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			output <- fmt.Sprintf("  ! watcher: %v", err)
		}
	}
}

func addDirsRecursive(w *fsnotify.Watcher, root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip inaccessible paths
		}
		if info.IsDir() {
			return w.Add(path)
		}
		return nil
	})
}
