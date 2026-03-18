package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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

	nestAvailable := nest != nil && !nest.Disabled
	mode := job.mode()
	doLocal := mode == "local" || mode == "both"
	doNest := (mode == "nest" || mode == "both") && nestAvailable

	if mode == "nest" && !nestAvailable {
		output <- styleDim.Render("  nest not available, skipping")
		return nil
	}

	if doLocal {
		if err := os.MkdirAll(baseDir, 0755); err != nil {
			return fmt.Errorf("could not create destination dir: %w", err)
		}
		if job.Compress {
			if err := runJobCompressed(job, src, baseDir, snapshot, output); err != nil {
				return err
			}
		} else {
			if err := runJobRsync(job, src, baseDir, snapshot, output); err != nil {
				return err
			}
		}
	}

	if doNest {
		if !doLocal {
			job.LastRun = time.Now()
		}
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
	infos, err := listSnapshotInfos(job, nil)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(infos))
	for i, s := range infos {
		names[i] = s.Name
	}
	return names, nil
}

// SnapshotInfo holds a snapshot name, disk size and where it lives.
// Source is "local", "nest", or "both".
type SnapshotInfo struct {
	Name   string
	Size   int64
	Source string
}

// listSnapshotInfos returns snapshots for a job, newest first.
// It merges local snapshots with nest snapshots when a nest is configured
// and the job uses nest mode.
func listSnapshotInfos(job *Job, nest *NestConfig) ([]SnapshotInfo, error) {
	// --- local snapshots ---
	localMap := map[string]int64{}
	baseDir := expandPath(job.Destination)
	if entries, err := os.ReadDir(baseDir); err == nil {
		for _, e := range entries {
			name := e.Name()
			if !e.IsDir() && !strings.HasSuffix(name, ".tar.gz") && !strings.HasSuffix(name, ".tar.zst") {
				continue
			}
			localMap[name] = 0
		}
	}

	// --- nest snapshots ---
	var nestEntries []nestSnapshotEntry
	mode := job.mode()
	if nest != nil && !nest.Disabled && (mode == "nest" || mode == "both") {
		nestEntries, _ = listNestSnapshots(job.Name, nest.Address)
	}
	nestMap := map[string]int64{}
	for _, e := range nestEntries {
		nestMap[e.Name] = e.Size
	}

	// --- merge ---
	seen := map[string]bool{}
	var snaps []SnapshotInfo

	// add all local
	for name, size := range localMap {
		source := "local"
		if _, onNest := nestMap[name]; onNest {
			source = "both"
		}
		snaps = append(snaps, SnapshotInfo{Name: name, Size: size, Source: source})
		seen[name] = true
	}
	// add nest-only
	for _, e := range nestEntries {
		if !seen[e.Name] {
			snaps = append(snaps, SnapshotInfo{Name: e.Name, Size: e.Size, Source: "nest"})
		}
	}

	sort.Slice(snaps, func(i, j int) bool { return snaps[i].Name > snaps[j].Name })
	return snaps, nil
}

type nestSnapshotEntry struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// listNestSnapshots fetches snapshots from zipp-nest for a job.
// Supports both new [{name,size}] and old [string] response formats.
func listNestSnapshots(jobName, address string) ([]nestSnapshotEntry, error) {
	url := fmt.Sprintf("http://%s/backups/%s", address, jobName)
	client := http.Client{Timeout: 4 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("nest returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	// try new format [{name, size}]
	var entries []nestSnapshotEntry
	if err := json.Unmarshal(body, &entries); err == nil && len(entries) > 0 {
		if entries[0].Name != "" {
			return entries, nil
		}
	}
	// fallback: old format ["name1", "name2"]
	var names []string
	if err := json.Unmarshal(body, &names); err != nil {
		return nil, err
	}
	entries = make([]nestSnapshotEntry, len(names))
	for i, n := range names {
		entries[i] = nestSnapshotEntry{Name: n}
	}
	return entries, nil
}

// runRestoreFromNest downloads a snapshot from zipp-nest and extracts it.
func runRestoreFromNest(job *Job, snapshot string, nest *NestConfig, output chan<- string) error {
	dst := expandPath(job.Source)
	output <- fmt.Sprintf("→ restoring %s from nest", job.Name)
	output <- fmt.Sprintf("  snapshot: %s", snapshot)
	output <- fmt.Sprintf("  to:       %s", dst)
	output <- "  downloading from nest..."

	url := fmt.Sprintf("http://%s/backups/%s/%s", nest.Address, job.Name, snapshot)
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("nest download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("nest returned %d", resp.StatusCode)
	}

	// write to temp file
	tmp, err := os.CreateTemp("", "zipp-nest-*.tar.gz")
	if err != nil {
		return fmt.Errorf("temp file: %w", err)
	}
	defer os.Remove(tmp.Name())

	n, err := io.Copy(tmp, resp.Body)
	if err != nil {
		tmp.Close()
		return fmt.Errorf("download failed: %w", err)
	}
	tmp.Close()
	output <- fmt.Sprintf("  downloaded %s", formatBytes(n))
	output <- "  extracting..."

	if err := os.MkdirAll(dst, 0755); err != nil {
		return fmt.Errorf("could not create destination: %w", err)
	}
	cmd := exec.Command("tar", "-xzf", tmp.Name(), "-C", dst)
	if runtime.GOOS == "darwin" {
		cmd.Env = append(os.Environ(), "COPYFILE_DISABLE=1")
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("extraction failed: %w\n%s", err, string(out))
	}
	output <- "  ✓ restored from nest"
	return nil
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
