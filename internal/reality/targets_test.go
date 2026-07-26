package reality

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadTargetsIgnoresCommentsAndDuplicates(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "targets.txt")
	content := "# comment\nwww.cloudflare.com # preferred\n\nwww.cloudflare.com\nwww.microsoft.com\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	targets, err := LoadTargets(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"www.cloudflare.com", "www.microsoft.com"}
	if !reflect.DeepEqual(targets, want) {
		t.Fatalf("targets = %v, want %v", targets, want)
	}
}

func TestLoadTargetsRejectsNonHostnames(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "targets.txt")
	if err := os.WriteFile(path, []byte("https://example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTargets(path); err == nil {
		t.Fatal("LoadTargets accepted a URL")
	}
}
