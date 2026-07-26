package reality

import (
	"fmt"
	"os"
	"strings"
)

// LoadTargets reads one DNS hostname per line. Empty lines and comments are
// intentionally ignored so maintainers can document why a target is present.
func LoadTargets(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open targets file: %w", err)
	}
	defer file.Close()

	return parseTargets(file)
}

func validHostname(host string) bool {
	if len(host) == 0 || len(host) > 253 || strings.ContainsAny(host, ":/ \t") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, char := range label {
			if !(char == '-' || char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9') {
				return false
			}
		}
	}
	return true
}
