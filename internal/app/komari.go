package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/sherlock-wong/vps-net-manager/internal/dependency"
	"github.com/sherlock-wong/vps-net-manager/internal/komari"
	"github.com/sherlock-wong/vps-net-manager/internal/platform"
	"github.com/sherlock-wong/vps-net-manager/internal/protocol"
)

const (
	DefaultKomariService   = "vps-net-manager-komari.service"
	DefaultKomariStateFile = "komari.json"
)

// KomariInstaller fetches a pinned, checksum-verified official binary.
type KomariInstaller struct {
	StateDirectory string
	UnitDirectory  string
	Locks          dependency.Locks
	Client         *http.Client
	Runner         platform.CommandRunner
}

func (installer KomariInstaller) Install(ctx context.Context, architecture string) (string, error) {
	if installer.StateDirectory == "" || installer.UnitDirectory == "" {
		return "", fmt.Errorf("state and unit directories are required")
	}
	locks := installer.Locks
	if locks.Komari.Version == "" {
		var err error
		locks, err = dependency.Embedded()
		if err != nil {
			return "", err
		}
	}
	if err := locks.Validate(); err != nil {
		return "", err
	}
	_, asset, err := locks.Asset("komari", architecture)
	if err != nil {
		return "", err
	}
	if asset.Archive != "raw" {
		return "", fmt.Errorf("Komari asset must be a raw binary")
	}
	stage, err := os.MkdirTemp("", "vpnm-komari-install-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	binary := filepath.Join(stage, "komari")
	if err := platform.DownloadVerified(ctx, installer.Client, platform.Download{URL: asset.URL, SHA256: asset.SHA256, Mode: 0o700}, binary); err != nil {
		return "", fmt.Errorf("download Komari: %w", err)
	}
	contents, err := os.ReadFile(binary)
	if err != nil {
		return "", err
	}
	metadata, err := json.Marshal(struct {
		Version string `json:"version"`
		SHA256  string `json:"sha256"`
	}{Version: locks.Komari.Version, SHA256: asset.SHA256})
	if err != nil {
		return "", err
	}
	unitState := komari.NewState()
	artifacts := make([]Artifact, 0, 4)
	statePath := filepath.Join(installer.StateDirectory, DefaultKomariStateFile)
	if _, err := os.Stat(statePath); os.IsNotExist(err) {
		initial, encodeErr := json.MarshalIndent(unitState, "", "  ")
		if encodeErr != nil {
			return "", encodeErr
		}
		artifacts = append(artifacts, Artifact{Path: statePath, Contents: initial, Mode: 0o600})
	} else if err != nil {
		return "", fmt.Errorf("inspect Komari state: %w", err)
	} else {
		unitState, err = LoadKomariStateOrEmpty(statePath)
		if err != nil {
			return "", err
		}
	}
	artifacts = append(artifacts, []Artifact{
		{Path: filepath.Join(installer.StateDirectory, "bin", "komari"), Contents: contents, Mode: 0o755},
		{Path: filepath.Join(installer.StateDirectory, "bin", "komari.json"), Contents: metadata, Mode: 0o600},
		// Systemd requires the configured working directory to exist before it
		// starts Komari. The file is harmless and stays inside VPNM ownership.
		{Path: filepath.Join(installer.StateDirectory, "komari", ".keep"), Contents: nil, Mode: 0o600},
	}...)
	rollback, err := (FilesystemStore{}).Commit(ctx, artifacts)
	if err != nil {
		return "", err
	}
	if err := installKomariUnit(ctx, installer.StateDirectory, installer.UnitDirectory, unitState, installer.Runner); err != nil {
		return "", joinFailure(err, rollback(ctx))
	}
	return locks.Komari.Version, nil
}

type KomariApplyOptions struct {
	StateDirectory string
	UnitDirectory  string
	Firewall       FirewallController
	Artifacts      ArtifactStore
	Runner         platform.CommandRunner
}

func DefaultKomariApplyOptions(stateDirectory string) KomariApplyOptions {
	return KomariApplyOptions{StateDirectory: stateDirectory, UnitDirectory: "/etc/systemd/system", Firewall: UFWController{}, Artifacts: FilesystemStore{}}
}

func LoadKomariStateOrEmpty(path string) (komari.State, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return komari.NewState(), nil
	}
	if err != nil {
		return komari.State{}, fmt.Errorf("read Komari state: %w", err)
	}
	return komari.Parse(data)
}

func (options KomariApplyOptions) Apply(ctx context.Context, previous, candidate komari.State) error {
	if options.StateDirectory == "" || options.UnitDirectory == "" || options.Firewall == nil || options.Artifacts == nil {
		return fmt.Errorf("Komari apply dependencies are required")
	}
	if err := candidate.Validate(); err != nil {
		return err
	}
	lock, err := platform.AcquireLock(ctx, filepath.Join(options.StateDirectory, "state.lock"))
	if err != nil {
		return fmt.Errorf("acquire state lock: %w", err)
	}
	defer lock.Release()
	previousRules, candidateRules := komariFirewallRules(previous), komariFirewallRules(candidate)
	rollbackFW, err := options.Firewall.Prepare(ctx, previousRules, candidateRules)
	if err != nil {
		return fmt.Errorf("pre-open Komari firewall rules: %w", err)
	}
	encoded, err := json.MarshalIndent(candidate, "", "  ")
	if err != nil {
		return joinFailure(err, rollbackFW(ctx))
	}
	rollbackFiles, err := options.Artifacts.Commit(ctx, []Artifact{
		{Path: filepath.Join(options.StateDirectory, DefaultKomariStateFile), Contents: encoded, Mode: 0o600},
		{Path: filepath.Join(options.UnitDirectory, DefaultKomariService), Contents: []byte(komariUnit(options.StateDirectory, candidate)), Mode: 0o644},
	})
	if err != nil {
		return joinFailure(fmt.Errorf("commit Komari configuration: %w", err), rollbackFW(ctx))
	}
	runner := options.Runner
	if runner == nil {
		runner = platform.SystemRunner{}
	}
	if _, err := runner.Run(ctx, platform.Command{Path: "systemctl", Args: []string{"daemon-reload"}, Timeout: 30 * time.Second}); err != nil {
		return joinFailure(fmt.Errorf("reload systemd: %w", err), rollbackFiles(ctx), rollbackFW(ctx))
	}
	if candidate.Enabled {
		if _, err := runner.Run(ctx, platform.Command{Path: "systemctl", Args: []string{"enable", "--now", DefaultKomariService}, Timeout: 45 * time.Second}); err != nil {
			return joinFailure(fmt.Errorf("start Komari: %w", err), rollbackFiles(ctx), rollbackFW(ctx))
		}
	} else if err := stopDisable(ctx, runner, DefaultKomariService); err != nil {
		return joinFailure(fmt.Errorf("stop Komari: %w", err), rollbackFiles(ctx), rollbackFW(ctx))
	}
	if err := options.Firewall.Finalize(ctx, previousRules, candidateRules); err != nil {
		return joinFailure(fmt.Errorf("finalize Komari firewall rules: %w", err), rollbackFiles(ctx), rollbackFW(ctx))
	}
	return nil
}

func installKomariUnit(ctx context.Context, stateDirectory, unitDirectory string, state komari.State, runner platform.CommandRunner) error {
	if runner == nil {
		runner = platform.SystemRunner{}
	}
	rollback, err := (FilesystemStore{}).Commit(ctx, []Artifact{{Path: filepath.Join(unitDirectory, DefaultKomariService), Contents: []byte(komariUnit(stateDirectory, state)), Mode: 0o644}})
	if err != nil {
		return fmt.Errorf("write Komari unit: %w", err)
	}
	if _, err := runner.Run(ctx, platform.Command{Path: "systemctl", Args: []string{"daemon-reload"}, Timeout: 30 * time.Second}); err != nil {
		return joinFailure(fmt.Errorf("reload systemd: %w", err), rollback(ctx))
	}
	return nil
}

// RestartKomari activates the replacement binary only when Komari is already
// enabled. Its persisted state and linked Nginx configuration are untouched.
func RestartKomari(ctx context.Context, state komari.State, runner platform.CommandRunner) error {
	if !state.Enabled {
		return nil
	}
	if runner == nil {
		runner = platform.SystemRunner{}
	}
	if _, err := runner.Run(ctx, platform.Command{Path: "systemctl", Args: []string{"restart", DefaultKomariService}, Timeout: 45 * time.Second}); err != nil {
		return err
	}
	active, err := systemdActive(ctx, runner, DefaultKomariService, 45*time.Second)
	if err != nil {
		return err
	}
	if !active {
		return fmt.Errorf("%s did not become active", DefaultKomariService)
	}
	return nil
}

func komariUnit(stateDirectory string, state komari.State) string {
	host, port := state.ListenHost, state.ListenPort
	if host == "" {
		host, port = "127.0.0.1", 25774
	}
	return "[Unit]\nDescription=VPS Net Manager Komari\nAfter=network-online.target\nWants=network-online.target\n\n[Service]\nType=simple\nWorkingDirectory=" + filepath.Join(stateDirectory, "komari") + "\nExecStart=" + filepath.Join(stateDirectory, "bin", "komari") + " server -l " + host + ":" + fmt.Sprint(port) + "\nRestart=on-failure\nRestartSec=3\nNoNewPrivileges=true\n\n[Install]\nWantedBy=multi-user.target\n"
}

func komariFirewallRules(state komari.State) []protocol.FirewallRule {
	if !state.Enabled || state.Mode != komari.ModeDirect {
		return nil
	}
	return []protocol.FirewallRule{{Network: "tcp", Port: state.ListenPort}}
}
