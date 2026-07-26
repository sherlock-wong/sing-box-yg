package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDigestUsesFileBaseNameAndSHA256(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vpnm-linux-amd64")
	if err := os.WriteFile(path, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	value, err := digest(path)
	if err != nil {
		t.Fatal(err)
	}
	if value.Name != "vpnm-linux-amd64" || value.SHA256 != "9a3a45d01531a20e89ac6ae10b0b0beb0492acd7216a368aa062d1a5fecaf9cd" {
		t.Fatalf("value = %+v", value)
	}
}
