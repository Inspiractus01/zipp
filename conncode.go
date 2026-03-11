package main

import (
	"fmt"
	"strconv"
	"strings"
)

const defaultNestPort = 9090

// decodeNestCode decodes a 10-digit code (XXXXX-XXXXX) back to "IP:port".
// If input already contains a dot it is treated as a full IP:port address.
// Example: "16834-22532" → "100.86.253.68:9090"
//          "32322-35781" → "192.168.1.5:9090"
func decodeNestCode(code string) (string, error) {
	code = strings.TrimSpace(code)

	// full address passed directly
	if strings.Contains(code, ".") {
		return code, nil
	}

	clean := strings.ReplaceAll(code, "-", "")
	if len(clean) != 10 {
		return "", fmt.Errorf("expected a 10-digit code like 16834-22532")
	}
	val, err := strconv.ParseUint(clean, 10, 32)
	if err != nil {
		return "", fmt.Errorf("invalid code")
	}
	o1 := (val >> 24) & 0xFF
	o2 := (val >> 16) & 0xFF
	o3 := (val >> 8) & 0xFF
	o4 := val & 0xFF
	return fmt.Sprintf("%d.%d.%d.%d:%d", o1, o2, o3, o4, defaultNestPort), nil
}
