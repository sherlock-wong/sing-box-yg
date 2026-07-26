package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sherlock-wong/vps-net-manager/internal/model"
	"github.com/sherlock-wong/vps-net-manager/internal/platform"
)

// RejectLegacyInstall refuses the only supported legacy state layout. It never
// attempts migration, removal, or credential interpretation.
func RejectLegacyInstall(stateDirectory string) error {
	legacy := filepath.Join(stateDirectory, "protocols.json")
	if _, err := os.Stat(legacy); err == nil {
		return fmt.Errorf("检测到旧版 Bash 安装（%s）；请先在旧 vpnm 菜单执行卸载", legacy)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect legacy installation: %w", err)
	}
	return nil
}

// InitializeEmptyState creates schema 1 only if no current state exists. It is
// intentionally safe for a fresh zero-protocol install and never overwrites
// an existing node.
func InitializeEmptyState(ctx context.Context, stateDirectory string) (model.State, error) {
	path := filepath.Join(stateDirectory, "state.json")
	if _, err := os.Stat(path); err == nil {
		return model.State{}, fmt.Errorf("state already exists")
	} else if !os.IsNotExist(err) {
		return model.State{}, fmt.Errorf("inspect existing state: %w", err)
	}
	state := model.NewState()
	contents, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return model.State{}, fmt.Errorf("encode initial state: %w", err)
	}
	if _, err := (FilesystemStore{}).Commit(ctx, []Artifact{{Path: path, Contents: contents, Mode: 0o600}}); err != nil {
		return model.State{}, fmt.Errorf("create initial state: %w", err)
	}
	return state, nil
}

// Bootstrap validates the target host and old-install boundary, installs
// verified cores, writes core units, and creates a legal empty state. The UI
// owns protocol selection afterwards.
func Bootstrap(ctx context.Context, stateDirectory, unitDirectory string, runner platform.CommandRunner) (platform.Host, error) {
	host, err := platform.InspectSupportedHost()
	if err != nil {
		return platform.Host{}, err
	}
	if err := RejectLegacyInstall(stateDirectory); err != nil {
		return platform.Host{}, err
	}
	if _, err := (CoreInstaller{StateDirectory: stateDirectory}).Install(ctx, host.Architecture); err != nil {
		return platform.Host{}, err
	}
	if err := InstallCoreUnits(ctx, stateDirectory, unitDirectory, runner); err != nil {
		return platform.Host{}, err
	}
	if err := InstallCertificateTimer(ctx, unitDirectory, runner); err != nil {
		return platform.Host{}, err
	}
	if _, err := InitializeEmptyState(ctx, stateDirectory); err != nil {
		return platform.Host{}, err
	}
	return host, nil
}
