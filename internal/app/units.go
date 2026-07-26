package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/sherlock-wong/vps-net-manager/internal/platform"
)

// InstallCoreUnits writes only the two project-owned systemd unit files and
// reloads systemd. It does not enable either service; Apply selects active
// cores based on protocol state.
func InstallCoreUnits(ctx context.Context, stateDirectory, unitDirectory string, runner platform.CommandRunner) error {
	if stateDirectory == "" || unitDirectory == "" {
		return fmt.Errorf("state and unit directories are required")
	}
	if strings.ContainsAny(stateDirectory, "\n\r") {
		return fmt.Errorf("state directory contains a newline")
	}
	if runner == nil {
		runner = platform.SystemRunner{}
	}
	rollback, err := (FilesystemStore{}).Commit(ctx, []Artifact{
		{Path: filepath.Join(unitDirectory, DefaultSingBoxService), Contents: []byte(singBoxUnit(stateDirectory)), Mode: 0o644},
		{Path: filepath.Join(unitDirectory, DefaultXrayService), Contents: []byte(xrayUnit(stateDirectory)), Mode: 0o644},
	})
	if err != nil {
		return fmt.Errorf("write core units: %w", err)
	}
	if _, err := runner.Run(ctx, platform.Command{Path: "systemctl", Args: []string{"daemon-reload"}, Timeout: 30 * time.Second}); err != nil {
		return joinFailure(fmt.Errorf("reload systemd: %w", err), rollback(ctx))
	}
	return nil
}

func singBoxUnit(stateDirectory string) string {
	return "[Unit]\nDescription=VPS Net Manager Sing-box\nAfter=network-online.target\nWants=network-online.target\n\n[Service]\nType=simple\nExecStart=" + filepath.Join(stateDirectory, "bin", "sing-box") + " run -c " + filepath.Join(stateDirectory, "generated", "sing-box.json") + "\nRestart=on-failure\nRestartSec=3\nNoNewPrivileges=true\n\n[Install]\nWantedBy=multi-user.target\n"
}
func xrayUnit(stateDirectory string) string {
	return "[Unit]\nDescription=VPS Net Manager Xray\nAfter=network-online.target\nWants=network-online.target\n\n[Service]\nType=simple\nExecStart=" + filepath.Join(stateDirectory, "bin", "xray") + " run -c " + filepath.Join(stateDirectory, "generated", "xray.json") + "\nRestart=on-failure\nRestartSec=3\nNoNewPrivileges=true\n\n[Install]\nWantedBy=multi-user.target\n"
}
