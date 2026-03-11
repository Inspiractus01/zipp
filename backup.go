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

	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return fmt.Errorf("could not create destination dir: %w", err)
	}

	if job.Compress {
		return runJobCompressed(job, src, baseDir, snapshot, output)
	}
	return runJobRsync(job, src, baseDir, snapshot, output)
}

func runJobRsync(job *Job, src, baseDir, snapshot string, output chan<- string) error {
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
	output <- fmt.Sprintf("  from  %s", src)
	output <- fmt.Sprintf("  to    %s", dest)
	output <- "  syncing files..."

	cmd := exec.Command("rsync", "-a", "--delete", "--stats", src, dest)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("rsync failed: %w\n%s", err, string(out))
	}

	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "Number of files:") ||
			strings.HasPrefix(line, "Total transferred") ||
			strings.HasPrefix(line, "Number of created") {
			output <- "  " + strings.TrimSpace(line)
		}
	}

	job.LastRun = time.Now()

	if job.MaxSnapshots > 0 {
		output <- "  cleaning up old snapshots..."
	}
	deleted, err := pruneSnapshots(baseDir, job.MaxSnapshots)
	if err != nil {
		output <- fmt.Sprintf("  warning: pruning snapshots failed: %v", err)
	} else if deleted > 0 {
		output <- fmt.Sprintf("  ✓ pruned %d old snapshot(s)", deleted)
	}

	output <- "  ✓ done"
	return nil
}

// findZstd returns the full path to the zstd binary, or "" if not found.
// Checks common locations because PATH inside a GUI app can differ from the shell.
func findZstd() string {
	candidates := []string{
		"zstd",
		"/opt/homebrew/bin/zstd",
		"/usr/local/bin/zstd",
		"/usr/bin/zstd",
		"/opt/local/bin/zstd",
	}
	for _, c := range candidates {
		if p, err := exec.LookPath(c); err == nil {
			return p
		}
	}
	return ""
}

func runJobCompressed(job *Job, src, baseDir, snapshot string, output chan<- string) error {
	srcDir := strings.TrimSuffix(src, "/")

	zstd := findZstd()
	var archivePath string
	var tarArgs []string
	if zstd != "" {
		archivePath = filepath.Join(baseDir, snapshot+".tar.zst")
		tarArgs = []string{"-I", zstd, "-cf", archivePath, "-C", srcDir, "."}
		output <- "  compressing with zstd..."
	} else {
		archivePath = filepath.Join(baseDir, snapshot+".tar.gz")
		tarArgs = []string{"-czf", archivePath, "-C", srcDir, "."}
		output <- "  compressing with gzip (install zstd for better speed)..."
	}

	output <- fmt.Sprintf("→ %s", job.Name)
	output <- fmt.Sprintf("  from  %s/", srcDir)
	output <- fmt.Sprintf("  to    %s", archivePath)

	cmd := exec.Command("tar", tarArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tar failed: %w\n%s", err, string(out))
	}

	stat, err := os.Stat(archivePath)
	if err == nil {
		output <- fmt.Sprintf("  archive size: %s", formatBytes(stat.Size()))
	}

	job.LastRun = time.Now()

	if job.MaxSnapshots > 0 {
		output <- "  cleaning up old archives..."
	}
	deleted, err := pruneSnapshots(baseDir, job.MaxSnapshots)
	if err != nil {
		output <- fmt.Sprintf("  warning: pruning failed: %v", err)
	} else if deleted > 0 {
		output <- fmt.Sprintf("  ✓ pruned %d old archive(s)", deleted)
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
		name := e.Name()
		if e.IsDir() || strings.HasSuffix(name, ".tar.gz") || strings.HasSuffix(name, ".tar.zst") {
			snapshots = append(snapshots, name)
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

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func expandPath(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}
