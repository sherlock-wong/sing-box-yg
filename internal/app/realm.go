package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sherlock-wong/vps-net-manager/internal/platform"
	"github.com/sherlock-wong/vps-net-manager/internal/protocol"
	"github.com/sherlock-wong/vps-net-manager/internal/realm"
)

const DefaultRealmService = "vps-net-manager-realm.service"

// InstallRealmUnit installs the isolated Realm unit. Rules and configuration
// remain in the VPNM state directory; this never touches unrelated services.
func InstallRealmUnit(ctx context.Context, stateDirectory, unitDirectory string, runner platform.CommandRunner) error {
	if stateDirectory == "" || unitDirectory == "" {
		return fmt.Errorf("state and unit directories are required")
	}
	if runner == nil {
		runner = platform.SystemRunner{}
	}
	rollback, err := (FilesystemStore{}).Commit(ctx, []Artifact{{
		Path:     filepath.Join(unitDirectory, DefaultRealmService),
		Contents: []byte(realmUnit(stateDirectory)),
		Mode:     0o644,
	}})
	if err != nil {
		return fmt.Errorf("write realm unit: %w", err)
	}
	if _, err := runner.Run(ctx, platform.Command{Path: "systemctl", Args: []string{"daemon-reload"}, Timeout: 30 * time.Second}); err != nil {
		return joinFailure(fmt.Errorf("reload systemd: %w", err), rollback(ctx))
	}
	return nil
}

func realmUnit(stateDirectory string) string {
	return "[Unit]\nDescription=VPS Net Manager Realm forwarding\nAfter=network-online.target\nWants=network-online.target\n\n[Service]\nType=simple\nExecStart=" + filepath.Join(stateDirectory, "bin", "realm") + " -c " + filepath.Join(stateDirectory, "generated", "realm.toml") + "\nRestart=on-failure\nRestartSec=3\nNoNewPrivileges=true\n\n[Install]\nWantedBy=multi-user.target\n"
}

type RealmPortChecker interface {
	CheckRealmPorts(context.Context, realm.State) error
}

type NoopRealmPortChecker struct{}

func (NoopRealmPortChecker) CheckRealmPorts(context.Context, realm.State) error { return nil }

type RealmServiceController interface {
	ActivateRealm(context.Context, bool) (Rollback, error)
}

type SystemdRealmServiceController struct {
	Runner  platform.CommandRunner
	Unit    string
	Timeout time.Duration
}

func (controller SystemdRealmServiceController) ActivateRealm(ctx context.Context, desired bool) (Rollback, error) {
	runner := controller.Runner
	if runner == nil {
		runner = platform.SystemRunner{}
	}
	unit := controller.Unit
	if unit == "" {
		unit = DefaultRealmService
	}
	timeout := controller.Timeout
	if timeout == 0 {
		timeout = 45 * time.Second
	}
	previousActive, err := systemdActive(ctx, runner, unit, timeout)
	if err != nil {
		return nil, err
	}
	previousEnabled, err := systemdEnabled(ctx, runner, unit, timeout)
	if err != nil {
		return nil, err
	}
	restore := func(rollbackContext context.Context) error {
		if err := systemdStop(rollbackContext, runner, unit, timeout); err != nil {
			return err
		}
		if !previousEnabled {
			if _, err := runner.Run(rollbackContext, platform.Command{Path: "systemctl", Args: []string{"disable", unit}, Timeout: timeout}); err != nil {
				return err
			}
		}
		if previousEnabled {
			if _, err := runner.Run(rollbackContext, platform.Command{Path: "systemctl", Args: []string{"enable", unit}, Timeout: timeout}); err != nil {
				return err
			}
		}
		if previousActive {
			return systemdStart(rollbackContext, runner, unit, timeout)
		}
		return nil
	}
	if !desired {
		if err := systemdStop(ctx, runner, unit, timeout); err != nil {
			return nil, err
		}
		if _, err := runner.Run(ctx, platform.Command{Path: "systemctl", Args: []string{"disable", unit}, Timeout: timeout}); err != nil {
			_ = restore(ctx)
			return nil, err
		}
		return restore, nil
	}
	if _, err := runner.Run(ctx, platform.Command{Path: "systemctl", Args: []string{"enable", unit}, Timeout: timeout}); err != nil {
		return nil, err
	}
	if _, err := runner.Run(ctx, platform.Command{Path: "systemctl", Args: []string{"restart", unit}, Timeout: timeout}); err != nil {
		_ = restore(ctx)
		return nil, err
	}
	if active, err := systemdActive(ctx, runner, unit, timeout); err != nil || !active {
		_ = restore(ctx)
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("%s did not become active", unit)
	}
	return restore, nil
}

func systemdEnabled(ctx context.Context, runner platform.CommandRunner, unit string, timeout time.Duration) (bool, error) {
	result, err := runner.Run(ctx, platform.Command{Path: "systemctl", Args: []string{"is-enabled", "--quiet", unit}, Timeout: timeout})
	if err == nil {
		return true, nil
	}
	if result.ExitCode == 1 || result.ExitCode == 3 || result.ExitCode == 4 {
		return false, nil
	}
	return false, fmt.Errorf("check %s enabled: %w", unit, err)
}

// RealmApplyOptions applies the separate Realm state transaction without
// involving protocol cores. Its UFW rules carry the Realm rule id in markers.
type RealmApplyOptions struct {
	StateDirectory string
	UnitDirectory  string
	Ports          RealmPortChecker
	Firewall       FirewallController
	Artifacts      ArtifactStore
	Services       RealmServiceController
	Runner         platform.CommandRunner
}

func (options RealmApplyOptions) Apply(ctx context.Context, state realm.State) error {
	if options.StateDirectory == "" || options.UnitDirectory == "" || options.Ports == nil || options.Firewall == nil || options.Artifacts == nil || options.Services == nil {
		return fmt.Errorf("realm apply dependencies are required")
	}
	lock, err := platform.AcquireLock(ctx, filepath.Join(options.StateDirectory, "state.lock"))
	if err != nil {
		return fmt.Errorf("acquire state lock: %w", err)
	}
	defer lock.Release()
	if err := state.Validate(); err != nil {
		return err
	}
	config, err := realm.Config(state)
	if err != nil {
		return err
	}
	if err := options.Ports.CheckRealmPorts(ctx, state); err != nil {
		return fmt.Errorf("check candidate realm ports: %w", err)
	}
	previous, err := loadRealmOrEmpty(filepath.Join(options.StateDirectory, "realm.json"))
	if err != nil {
		return err
	}
	previousRules, candidateRules := realmFirewallRules(previous), realmFirewallRules(state)
	firewallRollback, err := options.Firewall.Prepare(ctx, previousRules, candidateRules)
	if err != nil {
		return fmt.Errorf("pre-open realm firewall rules: %w", err)
	}
	encoded, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return joinFailure(fmt.Errorf("encode realm state: %w", err), firewallRollback(ctx))
	}
	artifacts := []Artifact{
		{Path: filepath.Join(options.StateDirectory, "realm.json"), Contents: encoded, Mode: 0o600},
		{Path: filepath.Join(options.StateDirectory, "generated", "realm.toml"), Contents: config, Mode: 0o600},
		{Path: filepath.Join(options.UnitDirectory, DefaultRealmService), Contents: []byte(realmUnit(options.StateDirectory)), Mode: 0o644},
	}
	fileRollback, err := options.Artifacts.Commit(ctx, artifacts)
	if err != nil {
		return joinFailure(fmt.Errorf("commit realm artifacts: %w", err), firewallRollback(ctx))
	}
	if err := options.reload(ctx); err != nil {
		return joinFailure(err, fileRollback(ctx), firewallRollback(ctx))
	}
	serviceRollback, err := options.Services.ActivateRealm(ctx, len(state.Rules) > 0)
	if err != nil {
		return joinFailure(fmt.Errorf("activate realm service: %w", err), fileRollback(ctx), firewallRollback(ctx))
	}
	if err := options.Firewall.Finalize(ctx, previousRules, candidateRules); err != nil {
		return joinFailure(fmt.Errorf("finalize realm firewall rules: %w", err), serviceRollback(ctx), fileRollback(ctx), firewallRollback(ctx))
	}
	return nil
}

func (options RealmApplyOptions) reload(ctx context.Context) error {
	runner := options.Runner
	if runner == nil {
		runner = platform.SystemRunner{}
	}
	if _, err := runner.Run(ctx, platform.Command{Path: "systemctl", Args: []string{"daemon-reload"}, Timeout: 30 * time.Second}); err != nil {
		return fmt.Errorf("reload systemd: %w", err)
	}
	return nil
}

func loadRealmOrEmpty(path string) (realm.State, error) {
	contents, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return realm.NewState(), nil
	}
	if err != nil {
		return realm.State{}, fmt.Errorf("read realm state: %w", err)
	}
	return ParseRealmState(contents)
}

// LoadRealmState validates the separate Realm document without changing it.
func LoadRealmState(path string) (realm.State, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return realm.State{}, fmt.Errorf("read realm state: %w", err)
	}
	return ParseRealmState(contents)
}

func ParseRealmState(contents []byte) (realm.State, error) {
	state, err := realm.Parse(contents)
	if err != nil {
		return realm.State{}, fmt.Errorf("parse realm state: %w", err)
	}
	return state, nil
}

func realmFirewallRules(state realm.State) []protocol.FirewallRule {
	portRules := realm.Rules(state)
	rules := make([]protocol.FirewallRule, 0, len(portRules))
	for _, rule := range portRules {
		rules = append(rules, protocol.FirewallRule{Network: rule.Network, Port: rule.Port})
	}
	return rules
}
