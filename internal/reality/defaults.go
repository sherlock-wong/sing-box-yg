package reality

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/sherlock-wong/vps-net-manager/assets"
)

// LoadTargetsOrDefault prefers a user-maintained candidate list. If it is not
// present on a new machine, it falls back to the binary's embedded list.
func LoadTargetsOrDefault(path string) ([]string, error) {
	file, err := os.Open(path)
	if err == nil {
		defer file.Close()
		return parseTargets(file)
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("open targets file: %w", err)
	}
	return parseTargets(strings.NewReader(assets.RealityTargets))
}

func parseTargets(scannerSource interface{ Read([]byte) (int, error) }) ([]string, error) {
	seen := make(map[string]struct{})
	targets := make([]string, 0)
	scanner := bufio.NewScanner(scannerSource)
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
