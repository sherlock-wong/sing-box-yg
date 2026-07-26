package platform

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractArchiveMemberExtractsLockedTarMember(t *testing.T) {
	directory := t.TempDir()
	archive := filepath.Join(directory, "core.tar.gz")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	writer := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(writer)
	if err := tarWriter.WriteHeader(&tar.Header{Name: "release/core", Mode: 0o755, Size: int64(len("binary"))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write([]byte("binary")); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(directory, "bin", "core")
	if err := ExtractArchiveMember(archive, "tar.gz", "release/core", destination, 0o755); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(destination)
	if err != nil || string(contents) != "binary" {
		t.Fatalf("contents = %q, err = %v", contents, err)
	}
}

func TestExtractArchiveMemberRejectsNonRegularZipMember(t *testing.T) {
	directory := t.TempDir()
	archive := filepath.Join(directory, "core.zip")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	header := &zip.FileHeader{Name: "xray"}
	header.SetMode(os.ModeSymlink | 0o777)
	entry, err := writer.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("target")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ExtractArchiveMember(archive, "zip", "xray", filepath.Join(directory, "xray"), 0o755); err == nil {
		t.Fatal("ExtractArchiveMember accepted a symlink")
	}
}
