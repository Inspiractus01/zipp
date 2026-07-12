package main

import (
	"testing"
	"time"
)

func TestIsDue(t *testing.T) {
	j := &Job{Enabled: true, IntervalHours: 24}
	if !j.isDue() {
		t.Error("never-run job should be due")
	}
	j.LastRun = time.Now().Add(-1 * time.Hour)
	if j.isDue() {
		t.Error("job run 1h ago with 24h interval should not be due")
	}
	j.LastRun = time.Now().Add(-25 * time.Hour)
	if !j.isDue() {
		t.Error("job run 25h ago with 24h interval should be due")
	}
	j.Enabled = false
	if j.isDue() {
		t.Error("disabled job should never be due")
	}
	j.Enabled = true
	j.IntervalHours = 0
	if j.isDue() {
		t.Error("manual job (interval 0) should never be due")
	}
}

func TestJobModeMigration(t *testing.T) {
	j := &Job{}
	if j.mode() != "local" {
		t.Errorf("default mode = %q, want local", j.mode())
	}
	j.NestEnabled = true // legacy field
	if j.mode() != "both" {
		t.Errorf("legacy NestEnabled mode = %q, want both", j.mode())
	}
	j.NestMode = "nest"
	if j.mode() != "nest" {
		t.Errorf("explicit mode = %q, want nest", j.mode())
	}
}
