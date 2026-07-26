package reality

import (
	"bufio"
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

	seen := make(map[string]struct{})
	targets := make([]string, 0)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		target := strings.TrimSpace(strings.SplitN(scanner.Text(), "#", 2)[0])
		if target == "" {
			continue
		}
		if !validHostname(target) {
			return nil, fmt.Errorf("invalid target hostname %q", target)
		}
		if _, exists := seen[target]; exists {
			continue
		}
		seen[target] = struct{}{}
		targets = append(targets, target)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read targets file: %w", err)
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("targets file contains no hostname")
	}
	return targets, nil
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
