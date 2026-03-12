package main

import (
	"fmt"
	"strings"
)

const defaultNestPort = 9090

// decodeNestCode decodes an 8-char hex code (XXXX-XXXX) back to "IP:port".
// If input already contains a dot it is treated as a full IP:port address.
// Example: "6456-fd44" → "100.86.253.68:9090"
//
//	"c0a8-0105" → "192.168.1.5:9090"
func decodeNestCode(code string) (string, error) {
	code = strings.TrimSpace(code)

	// full address passed directly
	if strings.Contains(code, ".") {
		return code, nil
	}

	clean := strings.ToLower(strings.ReplaceAll(code, "-", ""))
	if len(clean) != 8 {
		return "", fmt.Errorf("expected an 8-char code like 6456-fd44")
	}
	var val uint32
	_, err := fmt.Sscanf(clean, "%x", &val)
	if err != nil {
		return "", fmt.Errorf("invalid code")
	}
	o1 := (val >> 24) & 0xFF
	o2 := (val >> 16) & 0xFF
	o3 := (val >> 8) & 0xFF
	o4 := val & 0xFF
	return fmt.Sprintf("%d.%d.%d.%d:%d", o1, o2, o3, o4, defaultNestPort), nil
}
