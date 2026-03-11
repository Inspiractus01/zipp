package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func runJob(job *Job, output chan<- string) error {
	src := expandPath(job.Source)
	baseDir := expandPath(job.Destination)

	snapshot := time.Now().Format("2006-01-02_15-04-05")
	dest := filepath.Join(baseDir, snapshot)

	if err := os.MkdirAll(dest, 0755); err != nil {
		return fmt.Errorf("could not create snapshot dir: %w", err)
	}

	if !strings.HasSuffix(src, "/") {
		src += "/"
	}
	if !strings.HasSuffix(dest, "/") {
		dest += "/"
	}

	output <- fmt.Sprintf("→ %s", job.Name)
	output <- fmt.Sprintf("  src:  %s", src)
	output <- fmt.Sprintf("  dest: %s", dest)

	cmd := exec.Command("rsync", "-a", "--delete", "--stats", src, dest)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("rsync failed: %w\n%s", err, string(out))
	}

	// parse a nice summary from rsync --stats
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "Number of files:") ||
			strings.HasPrefix(line, "Total transferred") ||
			strings.HasPrefix(line, "Number of created") ||
			strings.HasPrefix(line, "Number of deleted") {
			output <- "  " + strings.TrimSpace(line)
		}
	}

	job.LastRun = time.Now()

	deleted, err := pruneSnapshots(baseDir, job.MaxSnapshots)
	if err != nil {
		output <- fmt.Sprintf("  warning: pruning snapshots failed: %v", err)
	} else if deleted > 0 {
		output <- fmt.Sprintf("  ✓ pruned %d old snapshot(s)", deleted)
	}

	output <- "  ✓ done"
	return nil
}

func pruneSnapshots(dest string, max int) (int, error) {
	if max <= 0 {
		return 0, nil
	}
	entries, err := os.ReadDir(dest)
	if err != nil {
		return 0, err
	}
	var snapshots []string
	for _, e := range entries {
		if e.IsDir() {
			snapshots = append(snapshots, e.Name())
		}
	}
	sort.Strings(snapshots)
	deleted := 0
	for len(snapshots) > max {
		target := filepath.Join(dest, snapshots[0])
		if err := os.RemoveAll(target); err != nil {
			return deleted, err
		}
		snapshots = snapshots[1:]
		deleted++
	}
	return deleted, nil
}

func expandPath(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}
