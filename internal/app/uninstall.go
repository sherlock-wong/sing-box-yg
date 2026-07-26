package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/sherlock-wong/vps-net-manager/internal/platform"
	"github.com/sherlock-wong/vps-net-manager/internal/protocol"
)

type Uninstaller struct {
	StateDirectory string
	UnitDirectory  string
	BinaryPath     string
	Runner         platform.CommandRunner
	Firewall       FirewallController
}

// Uninstall removes only VPNM-owned resources. It deliberately requires the
// caller to make the destructive confirmation decision before invocation.
func (uninstaller Uninstaller) Uninstall(ctx context.Context) error {
	if !safeOwnedDirectory(uninstaller.StateDirectory) || uninstaller.UnitDirectory == "" || uninstaller.BinaryPath == "" {
		return fmt.Errorf("unsafe uninstall paths")
	}
	runner := uninstaller.Runner
	if runner == nil {
		runner = platform.SystemRunner{}
	}
	firewall := uninstaller.Firewall
	if firewall == nil {
		firewall = UFWController{}
	}
	lock, err := platform.AcquireLock(ctx, filepath.Join(uninstaller.StateDirectory, "state.lock"))
	if err != nil {
		return err
	}
	defer lock.Release()
	rules, err := uninstaller.rules()
	if err != nil {
		return err
	}
	if err := firewall.Finalize(ctx, rules, nil); err != nil {
		return fmt.Errorf("remove VPNM firewall rules: %w", err)
	}
	for _, unit := range []string{DefaultSingBoxService, DefaultXrayService, DefaultRealmService, DefaultCertSyncTimer, DefaultCertSyncService} {
		if err := stopDisable(ctx, runner, unit); err != nil {
			return err
		}
	}
	for _, unit := range []string{DefaultSingBoxService, DefaultXrayService, DefaultRealmService, DefaultCertSyncTimer, DefaultCertSyncService} {
		if err := os.Remove(filepath.Join(uninstaller.UnitDirectory, unit)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove unit %s: %w", unit, err)
		}
	}
	if _, err := runner.Run(ctx, platform.Command{Path: "systemctl", Args: []string{"daemon-reload"}, Timeout: 30 * time.Second}); err != nil {
		return fmt.Errorf("reload systemd after uninstall: %w", err)
	}
	if err := os.RemoveAll(uninstaller.StateDirectory); err != nil {
		return fmt.Errorf("remove VPNM state directory: %w", err)
	}
	if err := os.Remove(uninstaller.BinaryPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove VPNM executable: %w", err)
	}
	return nil
}

func (uninstaller Uninstaller) rules() ([]protocol.FirewallRule, error) {
	unique := make(map[string]protocol.FirewallRule)
	statePath := filepath.Join(uninstaller.StateDirectory, "state.json")
	if _, err := os.Stat(statePath); err == nil {
		state, err := LoadState(statePath)
		if err != nil {
			return nil, err
		}
		rules, err := FirewallRules(state)
		if err != nil {
			return nil, err
		}
		for _, rule := range rules {
			unique[fmt.Sprintf("%s/%d", rule.Network, rule.Port)] = rule
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	realmPath := filepath.Join(uninstaller.StateDirectory, "realm.json")
	if _, err := os.Stat(realmPath); err == nil {
		state, err := LoadRealmState(realmPath)
		if err != nil {
			return nil, err
		}
		for _, rule := range realmFirewallRules(state) {
			unique[fmt.Sprintf("%s/%d", rule.Network, rule.Port)] = rule
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	rules := make([]protocol.FirewallRule, 0, len(unique))
	for _, rule := range unique {
		rules = append(rules, rule)
	}
	sort.Slice(rules, func(left, right int) bool {
		return rules[left].Network < rules[right].Network || rules[left].Network == rules[right].Network && rules[left].Port < rules[right].Port
	})
	return rules, nil
}

func stopDisable(ctx context.Context, runner platform.CommandRunner, unit string) error {
	result, err := runner.Run(ctx, platform.Command{Path: "systemctl", Args: []string{"disable", "--now", unit}, Timeout: 45 * time.Second})
	if err == nil || result.ExitCode == 1 || result.ExitCode == 3 || result.ExitCode == 4 {
		return nil
	}
	return fmt.Errorf("stop %s: %w", unit, err)
}

func safeOwnedDirectory(directory string) bool {
	cleaned := filepath.Clean(directory)
	return filepath.IsAbs(cleaned) && cleaned != "/" && cleaned != "." && len(cleaned) > 4
}
