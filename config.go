package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Job struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Source        string    `json:"source"`
	Destination   string    `json:"destination"`
	IntervalHours int       `json:"intervalHours"`
	MaxSnapshots  int       `json:"maxSnapshots"`
	LastRun       time.Time `json:"lastRun"`
	Enabled       bool      `json:"enabled"`
	Compress      bool      `json:"compress"`
	NestEnabled   bool      `json:"nestEnabled,omitempty"` // legacy, use NestMode
	NestMode      string    `json:"nestMode,omitempty"`    // "local", "nest", "both"
}

// mode returns the effective backup mode, migrating legacy NestEnabled.
func (j *Job) mode() string {
	if j.NestMode != "" {
		return j.NestMode
	}
	if j.NestEnabled {
		return "both"
	}
	return "local"
}

type NestConfig struct {
	Address  string `json:"address"`
	Disabled bool   `json:"disabled,omitempty"` // when true, skip uploads
}

type Config struct {
	Version string      `json:"version"`
	Jobs    []*Job      `json:"jobs"`
	Nest    *NestConfig `json:"nest,omitempty"`
}

func configPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".zipp", "config.json")
}

func loadConfig() (*Config, error) {
	data, err := os.ReadFile(configPath())
	if os.IsNotExist(err) {
		return &Config{Version: version, Jobs: []*Job{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) save() error {
	path := configPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (c *Config) addJob(j *Job) {
	c.Jobs = append(c.Jobs, j)
}

func (c *Config) removeJob(id string) {
	filtered := c.Jobs[:0]
	for _, j := range c.Jobs {
		if j.ID != id {
			filtered = append(filtered, j)
		}
	}
	c.Jobs = filtered
}

func (j *Job) isDue() bool {
	if !j.Enabled || j.IntervalHours == 0 {
		return false
	}
	if j.LastRun.IsZero() {
		return true
	}
	return time.Since(j.LastRun) >= time.Duration(j.IntervalHours)*time.Hour
}

func (j *Job) nextRun() string {
	if !j.Enabled {
		return "disabled"
	}
	if j.IntervalHours == 0 {
		return "manual"
	}
	if j.LastRun.IsZero() {
		return "never ran"
	}
	next := j.LastRun.Add(time.Duration(j.IntervalHours) * time.Hour)
	until := time.Until(next)
	if until <= 0 {
		return "due now"
	}
	if until < time.Hour {
		return "in " + roundMins(until)
	}
	return "in " + roundHours(until)
}

func roundMins(d time.Duration) string {
	m := int(d.Minutes())
	if m == 1 {
		return "1 min"
	}
	return fmt.Sprintf("%d mins", m)
}

func roundHours(d time.Duration) string {
	h := int(d.Hours())
	if h == 1 {
		return "1 hour"
	}
	return fmt.Sprintf("%dh", h)
}
