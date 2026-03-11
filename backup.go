package main

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

func runJob(job *Job, nest *NestConfig, output chan<- string) error {
	src := expandPath(job.Source)
	baseDir := expandPath(job.Destination)

	snapshot := time.Now().Format("2006-01-02_15-04-05")

	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return fmt.Errorf("could not create destination dir: %w", err)
	}

	var err error
	if job.Compress {
		err = runJobCompressed(job, src, baseDir, snapshot, output)
	} else {
		err = runJobRsync(job, src, baseDir, snapshot, output)
	}
	if err != nil {
		return err
	}
	if job.NestEnabled && nest != nil && !nest.Disabled {
		uploadToNest(job, src, snapshot, nest, output)
		if job.MaxSnapshots > 0 {
			pruneNestSnapshots(job.Name, nest.Address, job.MaxSnapshots, output)
		}
	}
	return nil
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
		output <- fmt.Sprintf("  warning: pruning failed: %v", err)
	} else if deleted > 0 {
		output <- fmt.Sprintf("  ✓ pruned %d old snapshot(s)", deleted)
	}

	output <- "  ✓ done"
	return nil
}

func runJobCompressed(job *Job, src, baseDir, snapshot string, output chan<- string) error {
	srcDir := strings.TrimSuffix(src, "/")
	archivePath := filepath.Join(baseDir, snapshot+".tar.gz")

	output <- fmt.Sprintf("→ %s", job.Name)
	output <- fmt.Sprintf("  from  %s/", srcDir)
	output <- fmt.Sprintf("  to    %s", archivePath)
	output <- "  compressing files..."

	cmd := exec.Command("tar", "-czf", archivePath, "-C", srcDir, ".")
	// on macOS, disable copying of extended attributes (avoids xattr permission errors)
	if runtime.GOOS == "darwin" {
		cmd.Env = append(os.Environ(), "COPYFILE_DISABLE=1")
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("compression failed: %w\n%s", err, string(out))
	}

	if stat, err := os.Stat(archivePath); err == nil {
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

// listSnapshots returns snapshot names for a job, newest first.
func listSnapshots(job *Job) ([]string, error) {
	baseDir := expandPath(job.Destination)
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return nil, err
	}
	var snaps []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasSuffix(name, ".tar.gz") || strings.HasSuffix(name, ".tar.zst") {
			snaps = append(snaps, name)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(snaps)))
	return snaps, nil
}

func runRestore(job *Job, snapshot string, output chan<- string) error {
	baseDir := expandPath(job.Destination)
	dst := expandPath(job.Source)

	output <- fmt.Sprintf("→ restoring %s", job.Name)
	output <- fmt.Sprintf("  snapshot: %s", snapshot)
	output <- fmt.Sprintf("  to:       %s", dst)

	if strings.HasSuffix(snapshot, ".tar.gz") || strings.HasSuffix(snapshot, ".tar.zst") {
		archivePath := filepath.Join(baseDir, snapshot)
		output <- "  extracting archive..."
		cmd := exec.Command("tar", "-xzf", archivePath, "-C", dst)
		if runtime.GOOS == "darwin" {
			cmd.Env = append(os.Environ(), "COPYFILE_DISABLE=1")
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("extraction failed: %w\n%s", err, string(out))
		}
	} else {
		src := filepath.Join(baseDir, snapshot) + "/"
		if !strings.HasSuffix(dst, "/") {
			dst += "/"
		}
		output <- "  syncing files..."
		cmd := exec.Command("rsync", "-a", "--delete", "--stats", src, dst)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("restore failed: %w\n%s", err, string(out))
		}
		for _, line := range strings.Split(string(out), "\n") {
			if strings.HasPrefix(line, "Number of files:") ||
				strings.HasPrefix(line, "Total transferred") {
				output <- "  " + strings.TrimSpace(line)
			}
		}
	}

	output <- "  ✓ restored"
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

func uploadToNest(job *Job, src, snapshot string, nest *NestConfig, output chan<- string) {
	output <- "  uploading to nest..."

	// compress source to memory
	srcDir := strings.TrimSuffix(src, "/")
	cmd := exec.Command("tar", "-czf", "-", "-C", srcDir, ".")
	if runtime.GOOS == "darwin" {
		cmd.Env = append(os.Environ(), "COPYFILE_DISABLE=1")
	}
	data, err := cmd.Output()
	if err != nil {
		output <- fmt.Sprintf("  ✗ nest upload failed (compress): %v", err)
		return
	}

	url := "http://" + nest.Address + "/backups/" + job.Name
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		output <- fmt.Sprintf("  ✗ nest upload failed: %v", err)
		return
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		output <- fmt.Sprintf("  ✗ nest upload failed: %v", err)
		return
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		output <- fmt.Sprintf("  ✗ nest upload failed: status %d", resp.StatusCode)
		return
	}
	output <- fmt.Sprintf("  ✓ uploaded to nest (%s)", formatBytes(int64(len(data))))
}

func pruneNestSnapshots(jobName, address string, keep int, output chan<- string) {
	url := fmt.Sprintf("http://%s/backups/%s?keep=%d", address, jobName, keep)
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		output <- fmt.Sprintf("  ✓ nest snapshots pruned (keeping %d)", keep)
	}
}

func expandPath(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}
