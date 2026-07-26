// manifest creates the immutable install metadata published to main-build.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/sherlock-wong/vps-net-manager/internal/dependency"
)

type manifest struct {
	SourceCommit string            `json:"source_commit"`
	BuiltAt      time.Time         `json:"built_at"`
	Binaries     map[string]binary `json:"binaries"`
	Dependencies dependency.Locks  `json:"dependencies"`
}
type binary struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

func main() {
	commit := flag.String("commit", "", "source commit")
	builtAt := flag.String("built-at", "", "RFC3339 build timestamp")
	amd64 := flag.String("amd64", "", "amd64 vpnm binary")
	arm64 := flag.String("arm64", "", "arm64 vpnm binary")
	output := flag.String("output", "manifest.json", "manifest path")
	checksums := flag.String("checksums", "checksums.txt", "checksums path")
	flag.Parse()
	if *commit == "" || *builtAt == "" || *amd64 == "" || *arm64 == "" {
		fatal("commit, built-at, amd64, and arm64 are required")
	}
	timestamp, err := time.Parse(time.RFC3339, *builtAt)
	if err != nil {
		fatal("invalid built-at: " + err.Error())
	}
	locks, err := dependency.Embedded()
	if err != nil {
		fatal(err.Error())
	}
	amd64Binary, err := digest(*amd64)
	if err != nil {
		fatal(err.Error())
	}
	arm64Binary, err := digest(*arm64)
	if err != nil {
		fatal(err.Error())
	}
	value := manifest{SourceCommit: *commit, BuiltAt: timestamp, Binaries: map[string]binary{"amd64": amd64Binary, "arm64": arm64Binary}, Dependencies: locks}
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		fatal(err.Error())
	}
	if err := os.WriteFile(*output, append(encoded, '\n'), 0o644); err != nil {
		fatal(err.Error())
	}
	entries := []binary{amd64Binary, arm64Binary}
	sort.Slice(entries, func(left, right int) bool { return entries[left].Name < entries[right].Name })
	contents := ""
	for _, entry := range entries {
		contents += entry.SHA256 + "  " + entry.Name + "\n"
	}
	if err := os.WriteFile(*checksums, []byte(contents), 0o644); err != nil {
		fatal(err.Error())
	}
}

func digest(path string) (binary, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return binary{}, fmt.Errorf("read %s: %w", path, err)
	}
	value := sha256.Sum256(contents)
	return binary{Name: filepath.Base(path), SHA256: hex.EncodeToString(value[:])}, nil
}
func fatal(message string) { fmt.Fprintln(os.Stderr, "manifest:", message); os.Exit(1) }
