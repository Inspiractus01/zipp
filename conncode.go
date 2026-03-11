package main

import (
	"fmt"
	"strconv"
	"strings"
)

const defaultNestPort = 9090

// decodeNestCode decodes a short XXX-XXXX code back to "IP:port".
// Example: "570-0932" → "100.86.253.68:9090"
// If input already looks like an IP:port address, it is returned as-is.
func decodeNestCode(code string) (string, error) {
	code = strings.TrimSpace(code)

	// if it contains a dot it's already a full address — pass through
	if strings.Contains(code, ".") {
		return code, nil
	}

	clean := strings.ReplaceAll(code, "-", "")
	if len(clean) != 7 {
		return "", fmt.Errorf("expected a 7-digit code like 570-0932")
	}
	val, err := strconv.Atoi(clean)
	if err != nil {
		return "", fmt.Errorf("invalid code")
	}
	o2 := val >> 16
	o3 := (val >> 8) & 0xFF
	o4 := val & 0xFF
	return fmt.Sprintf("100.%d.%d.%d:%d", o2, o3, o4, defaultNestPort), nil
}
