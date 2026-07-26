package app

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/sherlock-wong/vps-net-manager/internal/platform"
)

const (
	DefaultCertSyncService = "vps-net-manager-cert-sync.service"
	DefaultCertSyncTimer   = "vps-net-manager-cert-sync.timer"
)

// InstallCertificateTimer creates the only automatic task. It calls a
// non-interactive subcommand, so systemd never enters the numbered menu.
func InstallCertificateTimer(ctx context.Context, unitDirectory string, runner platform.CommandRunner) error {
	if unitDirectory == "" {
		return fmt.Errorf("unit directory is required")
	}
	if runner == nil {
		runner = platform.SystemRunner{}
	}
	rollback, err := (FilesystemStore{}).Commit(ctx, []Artifact{
		{Path: filepath.Join(unitDirectory, DefaultCertSyncService), Contents: []byte(certSyncServiceUnit()), Mode: 0o644},
		{Path: filepath.Join(unitDirectory, DefaultCertSyncTimer), Contents: []byte(certSyncTimerUnit()), Mode: 0o644},
	})
	if err != nil {
		return fmt.Errorf("write certificate timer units: %w", err)
	}
	if _, err := runner.Run(ctx, platform.Command{Path: "systemctl", Args: []string{"daemon-reload"}, Timeout: 30 * time.Second}); err != nil {
		return joinFailure(fmt.Errorf("reload systemd: %w", err), rollback(ctx))
	}
	if _, err := runner.Run(ctx, platform.Command{Path: "systemctl", Args: []string{"enable", "--now", DefaultCertSyncTimer}, Timeout: 30 * time.Second}); err != nil {
		return joinFailure(fmt.Errorf("enable certificate timer: %w", err), rollback(ctx))
	}
	return nil
}

func certSyncServiceUnit() string {
	return "[Unit]\nDescription=VPS Net Manager certificate synchronization\n\n[Service]\nType=oneshot\nExecStart=/usr/local/bin/vpnm cert sync --quiet\n"
}

func certSyncTimerUnit() string {
	return "[Unit]\nDescription=Run VPS Net Manager certificate synchronization every 6 hours\n\n[Timer]\nOnBootSec=10m\nOnUnitActiveSec=6h\nPersistent=true\n\n[Install]\nWantedBy=timers.target\n"
}
