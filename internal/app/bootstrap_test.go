package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRejectLegacyInstall(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "protocols.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RejectLegacyInstall(directory); err == nil {
		t.Fatal("RejectLegacyInstall accepted legacy state")
	}
}

func TestInitializeEmptyStateWritesSchemaOneWithoutOverwriting(t *testing.T) {
	directory := t.TempDir()
	state, err := InitializeEmptyState(context.Background(), directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := InitializeEmptyState(context.Background(), directory); err == nil {
		t.Fatal("InitializeEmptyState overwrote state")
	}
}
