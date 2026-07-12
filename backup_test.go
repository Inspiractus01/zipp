package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsSnapshotName(t *testing.T) {
	valid := []string{
		"2026-07-12_10-30-00",
		"2026-07-12_10-30-00.tar.gz",
		"2026-07-12_10-30-00.tar.gz.age",
		"2026-07-12_10-30-00.tar.zst",
	}
	for _, n := range valid {
		if !isSnapshotName(n) {
			t.Errorf("%q should be a snapshot name", n)
		}
	}
	invalid := []string{
		"my-photos",
		"2026-07-12",
		"notes.tar.gz",
		"2026-07-12_10-30-00.zip",
		".DS_Store",
	}
	for _, n := range invalid {
		if isSnapshotName(n) {
			t.Errorf("%q should NOT be a snapshot name", n)
		}
	}
}

func TestPruneSnapshotsLeavesUnrelatedFiles(t *testing.T) {
	dir := t.TempDir()
	snaps := []string{"2026-01-01_00-00-00", "2026-01-02_00-00-00", "2026-01-03_00-00-00"}
	for _, s := range snaps {
		os.Mkdir(filepath.Join(dir, s), 0755)
	}
	os.Mkdir(filepath.Join(dir, "unrelated-folder"), 0755)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hi"), 0644)

	deleted, err := pruneSnapshots(dir, 2)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
	if _, err := os.Stat(filepath.Join(dir, "2026-01-01_00-00-00")); !os.IsNotExist(err) {
		t.Error("oldest snapshot should have been pruned")
	}
	for _, keep := range []string{"2026-01-02_00-00-00", "2026-01-03_00-00-00", "unrelated-folder", "notes.txt"} {
		if _, err := os.Stat(filepath.Join(dir, keep)); err != nil {
			t.Errorf("%s should still exist: %v", keep, err)
		}
	}
}

func TestPruneSnapshotsZeroKeepsAll(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "2026-01-01_00-00-00"), 0755)
	deleted, err := pruneSnapshots(dir, 0)
	if err != nil || deleted != 0 {
		t.Fatalf("deleted = %d, err = %v; want 0, nil", deleted, err)
	}
}

func TestNewestSnapshotDir(t *testing.T) {
	dir := t.TempDir()
	if got := newestSnapshotDir(dir, ""); got != "" {
		t.Errorf("empty dir: got %q", got)
	}
	os.Mkdir(filepath.Join(dir, "2026-01-01_00-00-00"), 0755)
	os.Mkdir(filepath.Join(dir, "2026-01-05_12-00-00"), 0755)
	os.Mkdir(filepath.Join(dir, "zz-not-a-snapshot"), 0755)
	if got := newestSnapshotDir(dir, ""); got != "2026-01-05_12-00-00" {
		t.Errorf("got %q, want 2026-01-05_12-00-00", got)
	}
}
