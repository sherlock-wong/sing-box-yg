package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/sherlock-wong/vps-net-manager/internal/dependency"
	"github.com/sherlock-wong/vps-net-manager/internal/platform"
	"github.com/sherlock-wong/vps-net-manager/internal/realm"
)

type RealmInstaller struct {
	StateDirectory string
	UnitDirectory  string
	Locks          dependency.Locks
	Client         *http.Client
	Runner         platform.CommandRunner
}

// Install fetches one pinned Realm binary, validates its exact archive member,
// and writes only VPNM-owned paths. Existing Realm rules are never overwritten.
func (installer RealmInstaller) Install(ctx context.Context, architecture string) (string, error) {
	if installer.StateDirectory == "" || installer.UnitDirectory == "" {
		return "", fmt.Errorf("state and unit directories are required")
	}
	locks := installer.Locks
	if locks.Realm.Version == "" {
		var err error
		locks, err = dependency.Embedded()
		if err != nil {
			return "", err
		}
	}
	if err := locks.Validate(); err != nil {
		return "", err
	}
	_, asset, err := locks.Asset("realm", architecture)
	if err != nil {
		return "", err
	}
	stage, err := os.MkdirTemp("", "vpnm-realm-install-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	archive := filepath.Join(stage, "realm."+asset.Archive)
	if err := platform.DownloadVerified(ctx, installer.Client, platform.Download{URL: asset.URL, SHA256: asset.SHA256, Mode: 0o600}, archive); err != nil {
		return "", fmt.Errorf("download realm: %w", err)
	}
	binary := filepath.Join(stage, "realm")
	if err := platform.ExtractArchiveMember(archive, asset.Archive, asset.Member, binary, 0o700); err != nil {
		return "", fmt.Errorf("extract realm: %w", err)
	}
	contents, err := os.ReadFile(binary)
	if err != nil {
		return "", err
	}
	metadata, err := json.Marshal(struct {
		Version string `json:"version"`
		SHA256  string `json:"sha256"`
	}{Version: locks.Realm.Version, SHA256: asset.SHA256})
	if err != nil {
		return "", err
	}
	artifacts := []Artifact{
		{Path: filepath.Join(installer.StateDirectory, "bin", "realm"), Contents: contents, Mode: 0o755},
		{Path: filepath.Join(installer.StateDirectory, "bin", "realm.json"), Contents: metadata, Mode: 0o600},
	}
	if _, err := os.Stat(filepath.Join(installer.StateDirectory, "realm.json")); os.IsNotExist(err) {
		initial, encodeErr := json.MarshalIndent(realm.NewState(), "", "  ")
		if encodeErr != nil {
			return "", encodeErr
		}
		artifacts = append(artifacts, Artifact{Path: filepath.Join(installer.StateDirectory, "realm.json"), Contents: initial, Mode: 0o600})
	} else if err != nil {
		return "", fmt.Errorf("inspect realm state: %w", err)
	}
	rollback, err := (FilesystemStore{}).Commit(ctx, artifacts)
	if err != nil {
		return "", err
	}
	if err := InstallRealmUnit(ctx, installer.StateDirectory, installer.UnitDirectory, installer.Runner); err != nil {
		return "", joinFailure(err, rollback(ctx))
	}
	return locks.Realm.Version, nil
}
