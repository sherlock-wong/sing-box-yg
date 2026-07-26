package reality

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadTargetsOrDefaultUsesEmbeddedListWhenOverrideIsMissing(t *testing.T) {
	targets, err := LoadTargetsOrDefault(filepath.Join(t.TempDir(), "missing.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) == 0 {
		t.Fatal("embedded target list is empty")
	}
}

func TestLoadTargetsOrDefaultPrefersOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "targets.txt")
	if err := os.WriteFile(path, []byte("override.example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	targets, err := LoadTargetsOrDefault(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0] != "override.example.com" {
		t.Fatalf("targets = %v", targets)
	}
}
