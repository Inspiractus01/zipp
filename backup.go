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
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

const snapshotTimeFormat = "2006-01-02_15-04-05"

// snapshotNameRe matches names zipp itself created — a timestamp directory
// or archive. Pruning and listing only ever touch matching entries, so a
// destination shared with other files is safe.
var snapshotNameRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}_\d{2}-\d{2}-\d{2}(\.tar\.gz(\.age)?|\.tar\.zst)?$`)

func isSnapshotName(name string) bool {
	return snapshotNameRe.MatchString(name)
}

func runJob(job *Job, nest *NestConfig, output chan<- string) error {
	src := expandPath(job.Source)
	baseDir := expandPath(job.Destination)
	snapshot := time.Now().Format(snapshotTimeFormat)

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
		if err := uploadToNest(job, src, nest, output); err != nil {
			return err
		}
		if job.MaxSnapshots > 0 {
			pruneNestSnapshots(job.Name, nest, job.MaxSnapshots, output)
		}
	}

	// only mark the job done when every requested target succeeded,
	// so the scheduler retries failed runs on the next tick
	job.LastRun = time.Now()
	return nil
}

// newestSnapshotDir returns the most recent uncompressed snapshot in baseDir
// other than exclude, or "" if there is none. Used as rsync --link-dest so
// unchanged files are hardlinked instead of copied again.
func newestSnapshotDir(baseDir, exclude string) string {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return ""
	}
	newest := ""
	for _, e := range entries {
		if e.IsDir() && isSnapshotName(e.Name()) && e.Name() != exclude && e.Name() > newest {
			newest = e.Name()
		}
	}
	return newest
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

	args := []string{"-a", "--delete", "--stats"}
	if prev := newestSnapshotDir(baseDir, snapshot); prev != "" {
		if abs, err := filepath.Abs(filepath.Join(baseDir, prev)); err == nil {
			args = append(args, "--link-dest="+abs)
			output <- styleDim.Render("  unchanged files hardlinked against " + prev)
		}
	}
	args = append(args, src, dest)
	output <- "  syncing files..."

	cmd := exec.Command("rsync", args...)
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
			if isSnapshotName(e.Name()) {
				localMap[e.Name()] = 0
			}
		}
	}

	// --- nest snapshots ---
	var nestEntries []nestSnapshotEntry
	mode := job.mode()
	if nest != nil && !nest.Disabled && (mode == "nest" || mode == "both") {
		nestEntries, _ = listNestSnapshots(job.Name, nest)
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

// nestRequest builds a request with the auth token attached.
func nestRequest(method, url string, nest *NestConfig, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	if nest.Token != "" {
		req.Header.Set("Authorization", "Bearer "+nest.Token)
	}
	return req, nil
}

// listNestSnapshots fetches snapshots from zipp-nest for a job.
// Supports both new [{name,size}] and old [string] response formats.
func listNestSnapshots(jobName string, nest *NestConfig) ([]nestSnapshotEntry, error) {
	url := fmt.Sprintf("http://%s/backups/%s", nest.Address, jobName)
	req, err := nestRequest(http.MethodGet, url, nest, nil)
	if err != nil {
		return nil, err
	}
	client := http.Client{Timeout: 4 * time.Second}
	resp, err := client.Do(req)
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

// runRestoreFromNest downloads a snapshot from zipp-nest, decrypts it if
// needed, and extracts it.
func runRestoreFromNest(job *Job, snapshot string, nest *NestConfig, output chan<- string) error {
	dst := expandPath(job.Source)
	output <- fmt.Sprintf("→ restoring %s from nest", job.Name)
	output <- fmt.Sprintf("  snapshot: %s", snapshot)
	output <- fmt.Sprintf("  to:       %s", dst)
	output <- "  downloading from nest..."

	url := fmt.Sprintf("http://%s/backups/%s/%s", nest.Address, job.Name, snapshot)
	req, err := nestRequest(http.MethodGet, url, nest, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("nest download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("nest returned %d", resp.StatusCode)
	}

	var reader io.Reader = resp.Body
	if strings.HasSuffix(snapshot, ".age") {
		id, err := loadOrCreateIdentity()
		if err != nil {
			return fmt.Errorf("could not load encryption key: %w", err)
		}
		reader, err = decryptFrom(resp.Body, id)
		if err != nil {
			return fmt.Errorf("decrypt failed — is %s the key this backup was made with? %w", keyPath(), err)
		}
	}

	// write to temp file
	tmp, err := os.CreateTemp("", "zipp-nest-*.tar.gz")
	if err != nil {
		return fmt.Errorf("temp file: %w", err)
	}
	defer os.Remove(tmp.Name())

	n, err := io.Copy(tmp, reader)
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
		if isSnapshotName(e.Name()) {
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

// uploadToNest streams an encrypted tar.gz of src to the nest server.
// Nothing is buffered in memory and nothing unencrypted leaves the machine.
func uploadToNest(job *Job, src string, nest *NestConfig, output chan<- string) error {
	output <- "  encrypting + uploading to nest..."

	id, err := loadOrCreateIdentity()
	if err != nil {
		return fmt.Errorf("nest upload failed (key): %w", err)
	}

	srcDir := strings.TrimSuffix(src, "/")
	cmd := exec.Command("tar", "-czf", "-", "-C", srcDir, ".")
	if runtime.GOOS == "darwin" {
		cmd.Env = append(os.Environ(), "COPYFILE_DISABLE=1")
	}
	tarOut, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("nest upload failed: %w", err)
	}
	var tarErr bytes.Buffer
	cmd.Stderr = &tarErr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("nest upload failed (tar): %w", err)
	}

	pr, pw := io.Pipe()
	go func() {
		ew, err := encryptTo(pw, id)
		if err != nil {
			pw.CloseWithError(err)
			return
		}
		if _, err := io.Copy(ew, tarOut); err != nil {
			pw.CloseWithError(err)
			return
		}
		if err := ew.Close(); err != nil {
			pw.CloseWithError(err)
			return
		}
		pw.Close()
	}()

	url := "http://" + nest.Address + "/backups/" + job.Name
	req, err := nestRequest(http.MethodPost, url, nest, pr)
	if err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return fmt.Errorf("nest upload failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Zipp-Encrypted", "age")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return fmt.Errorf("nest upload failed: %w", err)
	}
	defer resp.Body.Close()

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("nest upload failed (tar): %v\n%s", err, tarErr.String())
	}

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return fmt.Errorf("nest rejected the upload (unauthorized) — re-enter the connection code from the nest")
	default:
		return fmt.Errorf("nest upload failed: status %d", resp.StatusCode)
	}

	var result struct {
		Snapshot string `json:"snapshot"`
		Size     string `json:"size"`
	}
	if json.NewDecoder(resp.Body).Decode(&result) == nil && result.Size != "" {
		output <- fmt.Sprintf("  ✓ uploaded to nest (%s, %s)", result.Snapshot, result.Size)
	} else {
		output <- "  ✓ uploaded to nest"
	}
	return nil
}

func pruneNestSnapshots(jobName string, nest *NestConfig, keep int, output chan<- string) {
	url := fmt.Sprintf("http://%s/backups/%s?keep=%d", nest.Address, jobName, keep)
	req, err := nestRequest(http.MethodDelete, url, nest, nil)
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
