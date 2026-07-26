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
)

type InstalledCores struct {
	SingBox string `json:"sing_box"`
	Xray    string `json:"xray"`
}

func LoadInstalledCores(stateDirectory string) (InstalledCores, error) {
	contents, err := os.ReadFile(filepath.Join(stateDirectory, "bin", "cores.json"))
	if err != nil {
		return InstalledCores{}, fmt.Errorf("read installed cores: %w", err)
	}
	var installed InstalledCores
	if err := json.Unmarshal(contents, &installed); err != nil {
		return InstalledCores{}, fmt.Errorf("parse installed cores: %w", err)
	}
	if installed.SingBox == "" || installed.Xray == "" {
		return InstalledCores{}, fmt.Errorf("installed core metadata is incomplete")
	}
	return installed, nil
}

// CoreInstaller downloads both locked server cores into a private staging
// directory, verifies and extracts them, then atomically replaces both live
// binaries together.
type CoreInstaller struct {
	StateDirectory string
	Locks          dependency.Locks
	Client         *http.Client
}

func (installer CoreInstaller) Install(ctx context.Context, architecture string) (InstalledCores, error) {
	artifacts, installed, err := installer.Stage(ctx, architecture)
	if err != nil {
		return InstalledCores{}, err
	}
	rollback, err := (FilesystemStore{}).Commit(ctx, artifacts)
	if err != nil {
		return InstalledCores{}, fmt.Errorf("install core binaries: %w", err)
	}
	_ = rollback // kept available for a future larger install transaction.
	return installed, nil
}

// Stage downloads, verifies, and extracts both cores without changing a live
// path. Updaters can include the returned artifacts in a larger rollback.
func (installer CoreInstaller) Stage(ctx context.Context, architecture string) ([]Artifact, InstalledCores, error) {
	if installer.StateDirectory == "" {
		return nil, InstalledCores{}, fmt.Errorf("state directory is required")
	}
	locks, err := installer.locks()
	if err != nil {
		return nil, InstalledCores{}, err
	}
	stage, err := os.MkdirTemp("", "vpnm-core-install-")
	if err != nil {
		return nil, InstalledCores{}, fmt.Errorf("create core staging directory: %w", err)
	}
	defer os.RemoveAll(stage)
	singBox, err := installer.stageCore(ctx, stage, locks, "sing-box", architecture)
	if err != nil {
		return nil, InstalledCores{}, err
	}
	xray, err := installer.stageCore(ctx, stage, locks, "xray", architecture)
	if err != nil {
		return nil, InstalledCores{}, err
	}
	metadata, err := json.Marshal(InstalledCores{SingBox: locks.SingBox.Version, Xray: locks.Xray.Version})
	if err != nil {
		return nil, InstalledCores{}, fmt.Errorf("encode core metadata: %w", err)
	}
	binaries := filepath.Join(installer.StateDirectory, "bin")
	artifacts := []Artifact{
		{Path: filepath.Join(binaries, "sing-box"), Contents: singBox, Mode: 0o755},
		{Path: filepath.Join(binaries, "xray"), Contents: xray, Mode: 0o755},
		{Path: filepath.Join(binaries, "cores.json"), Contents: metadata, Mode: 0o600},
	}
	return artifacts, InstalledCores{SingBox: locks.SingBox.Version, Xray: locks.Xray.Version}, nil
}

func (installer CoreInstaller) locks() (dependency.Locks, error) {
	locks := installer.Locks
	if locks.SingBox.Version == "" && locks.Xray.Version == "" {
		var err error
		locks, err = dependency.Embedded()
		if err != nil {
			return dependency.Locks{}, err
		}
	}
	if err := locks.Validate(); err != nil {
		return dependency.Locks{}, err
	}
	return locks, nil
}

func (installer CoreInstaller) stageCore(ctx context.Context, stage string, locks dependency.Locks, name, architecture string) ([]byte, error) {
	_, asset, err := locks.Asset(name, architecture)
	if err != nil {
		return nil, err
	}
	archive := filepath.Join(stage, name+"."+asset.Archive)
	if err := platform.DownloadVerified(ctx, installer.Client, platform.Download{URL: asset.URL, SHA256: asset.SHA256, Mode: 0o600}, archive); err != nil {
		return nil, fmt.Errorf("download %s: %w", name, err)
	}
	binary := filepath.Join(stage, name)
	if err := platform.ExtractArchiveMember(archive, asset.Archive, asset.Member, binary, 0o700); err != nil {
		return nil, fmt.Errorf("extract %s: %w", name, err)
	}
	contents, err := os.ReadFile(binary)
	if err != nil {
		return nil, fmt.Errorf("read staged %s: %w", name, err)
	}
	return contents, nil
}
