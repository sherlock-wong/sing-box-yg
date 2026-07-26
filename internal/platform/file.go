package platform

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// AtomicWriteFile never exposes a partial state or generated configuration.
func AtomicWriteFile(path string, contents []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".vpnm-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return fmt.Errorf("set file permissions: %w", err)
	}
	if _, err := temporary.Write(contents); err != nil {
		temporary.Close()
		return fmt.Errorf("write temporary file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync temporary file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary file: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace file: %w", err)
	}
	return syncDirectory(directory)
}

func CopyFile(destination string, source io.Reader, mode os.FileMode) error {
	contents, err := io.ReadAll(source)
	if err != nil {
		return fmt.Errorf("read source: %w", err)
	}
	return AtomicWriteFile(destination, contents, mode)
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
