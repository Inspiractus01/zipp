package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type updateResult struct {
	latest  string
	hasUpdate bool
}

func checkForUpdate() updateResult {
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("https://api.github.com/repos/Inspiractus01/zipp/releases/latest")
	if err != nil {
		return updateResult{}
	}
	defer resp.Body.Close()

	var data struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return updateResult{}
	}

	latest := strings.TrimPrefix(data.TagName, "v")
	if latest == "" || latest == version {
		return updateResult{latest: latest}
	}

	return updateResult{
		latest:    latest,
		hasUpdate: newerThan(latest, version),
	}
}

// simple semver compare — good enough for x.y.z
func newerThan(a, b string) bool {
	return fmt.Sprintf("%010s", a) > fmt.Sprintf("%010s", b)
}
