package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sherlock-wong/vps-net-manager/internal/platform"
)

type Artifact struct {
	Path     string
	Contents []byte
	Mode     os.FileMode
}

type Rollback func(context.Context) error

// ArtifactStore groups the state/configuration replacement portion of an
// apply transaction. Commit must return a rollback valid until it is called.
type ArtifactStore interface {
	Commit(context.Context, []Artifact) (Rollback, error)
}

type FilesystemStore struct{}

type fileBackup struct {
	path     string
	exists   bool
	contents []byte
	mode     os.FileMode
}

func (FilesystemStore) Commit(ctx context.Context, artifacts []Artifact) (Rollback, error) {
	backups := make([]fileBackup, 0, len(artifacts))
	for _, artifact := range artifacts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if artifact.Path == "" {
			return nil, fmt.Errorf("artifact path is required")
		}
		info, err := os.Stat(artifact.Path)
		if err == nil {
			if info.IsDir() {
				return nil, fmt.Errorf("artifact path is a directory")
			}
			contents, readErr := os.ReadFile(artifact.Path)
			if readErr != nil {
				return nil, fmt.Errorf("back up artifact: %w", readErr)
			}
			backups = append(backups, fileBackup{path: artifact.Path, exists: true, contents: contents, mode: info.Mode().Perm()})
			continue
		}
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("inspect artifact: %w", err)
		}
		backups = append(backups, fileBackup{path: artifact.Path})
	}

	rollback := func(context.Context) error { return restoreBackups(backups) }
	for _, artifact := range artifacts {
		if err := os.MkdirAll(filepath.Dir(artifact.Path), 0o700); err != nil {
			_ = rollback(ctx)
			return nil, fmt.Errorf("create artifact directory: %w", err)
		}
		if err := platform.AtomicWriteFile(artifact.Path, artifact.Contents, artifact.Mode); err != nil {
			_ = rollback(ctx)
			return nil, fmt.Errorf("commit artifact: %w", err)
		}
	}
	return rollback, nil
}

func restoreBackups(backups []fileBackup) error {
	var failures []error
	for index := len(backups) - 1; index >= 0; index-- {
		backup := backups[index]
		if !backup.exists {
			if err := os.Remove(backup.path); err != nil && !os.IsNotExist(err) {
				failures = append(failures, err)
			}
			continue
		}
		if err := platform.AtomicWriteFile(backup.path, backup.contents, backup.mode); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
