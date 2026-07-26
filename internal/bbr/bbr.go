// Package bbr manages only VPNM-owned BBR settings.
package bbr

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sherlock-wong/vps-net-manager/internal/platform"
)

const (
	DefaultSysctlPath = "/etc/sysctl.d/99-vps-net-manager-bbr.conf"
	DefaultModulePath = "/etc/modules-load.d/vps-net-manager-bbr.conf"
)

type Status struct {
	CongestionControl string
	Qdisc             string
	Enabled           bool
}
type Manager struct {
	SysctlPath string
	ModulePath string
	Runner     platform.CommandRunner
}

func (manager Manager) Enable(ctx context.Context) error {
	sysctlPath, modulePath := manager.paths()
	backups, err := capture(sysctlPath, modulePath)
	if err != nil {
		return err
	}
	if err := write(sysctlPath, []byte("net.core.default_qdisc = fq\nnet.ipv4.tcp_congestion_control = bbr\n")); err != nil {
		return err
	}
	if err := write(modulePath, []byte("tcp_bbr\n")); err != nil {
		_ = restore(backups)
		return err
	}
	if err := manager.reload(ctx); err != nil {
		return join(err, restore(backups))
	}
	return nil
}

func (manager Manager) Restore(ctx context.Context) error {
	sysctlPath, modulePath := manager.paths()
	backups, err := capture(sysctlPath, modulePath)
	if err != nil {
		return err
	}
	if err := remove(sysctlPath); err != nil {
		return err
	}
	if err := remove(modulePath); err != nil {
		_ = restore(backups)
		return err
	}
	if err := manager.reload(ctx); err != nil {
		return join(err, restore(backups))
	}
	return nil
}

func (manager Manager) Status(ctx context.Context) (Status, error) {
	runner := manager.runner()
	control, err := runner.Run(ctx, platform.Command{Path: "sysctl", Args: []string{"-n", "net.ipv4.tcp_congestion_control"}, Timeout: 15 * time.Second})
	if err != nil {
		return Status{}, fmt.Errorf("read congestion control: %w", err)
	}
	qdisc, err := runner.Run(ctx, platform.Command{Path: "sysctl", Args: []string{"-n", "net.core.default_qdisc"}, Timeout: 15 * time.Second})
	if err != nil {
		return Status{}, fmt.Errorf("read qdisc: %w", err)
	}
	status := Status{CongestionControl: strings.TrimSpace(control.Output), Qdisc: strings.TrimSpace(qdisc.Output)}
	status.Enabled = status.CongestionControl == "bbr" && status.Qdisc == "fq"
	return status, nil
}

func (manager Manager) reload(ctx context.Context) error {
	_, err := manager.runner().Run(ctx, platform.Command{Path: "sysctl", Args: []string{"--system"}, Timeout: 30 * time.Second})
	if err != nil {
		return fmt.Errorf("reload sysctl: %w", err)
	}
	return nil
}
func (manager Manager) runner() platform.CommandRunner {
	if manager.Runner != nil {
		return manager.Runner
	}
	return platform.SystemRunner{}
}
func (manager Manager) paths() (string, string) {
	sysctlPath, modulePath := manager.SysctlPath, manager.ModulePath
	if sysctlPath == "" {
		sysctlPath = DefaultSysctlPath
	}
	if modulePath == "" {
		modulePath = DefaultModulePath
	}
	return sysctlPath, modulePath
}

type backup struct {
	path     string
	exists   bool
	contents []byte
	mode     os.FileMode
}

func capture(paths ...string) ([]backup, error) {
	backups := make([]backup, 0, len(paths))
	for _, path := range paths {
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			backups = append(backups, backup{path: path})
			continue
		}
		if err != nil {
			return nil, err
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		backups = append(backups, backup{path: path, exists: true, contents: contents, mode: info.Mode().Perm()})
	}
	return backups, nil
}
func write(path string, contents []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return platform.AtomicWriteFile(path, contents, 0o644)
}
func remove(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
func restore(backups []backup) error {
	var failures []error
	for _, backup := range backups {
		if backup.exists {
			if err := platform.AtomicWriteFile(backup.path, backup.contents, backup.mode); err != nil {
				failures = append(failures, err)
			}
		} else if err := remove(backup.path); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
func join(primary, rollback error) error {
	if rollback == nil {
		return primary
	}
	return errors.Join(primary, fmt.Errorf("rollback: %w", rollback))
}
