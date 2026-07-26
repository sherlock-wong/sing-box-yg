package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sherlock-wong/vps-net-manager/internal/platform"
	"github.com/sherlock-wong/vps-net-manager/internal/render"
)

// CommandCoreChecker asks the installed cores to validate staged candidate
// configs. Configs are never placed at their live paths during preflight.
type CommandCoreChecker struct {
	SingBoxPath string
	XrayPath    string
	Timeout     time.Duration
}

func (checker CommandCoreChecker) CheckCores(ctx context.Context, output render.Output) error {
	directory, err := os.MkdirTemp("", "vpnm-core-check-")
	if err != nil {
		return fmt.Errorf("create core check directory: %w", err)
	}
	defer os.RemoveAll(directory)
	if output.NeedsSingBox {
		if checker.SingBoxPath == "" {
			return fmt.Errorf("sing-box is required but no binary path is configured")
		}
		if err := checker.check(ctx, checker.SingBoxPath, []string{"check", "-c"}, filepath.Join(directory, "sing-box.json"), output.SingBox); err != nil {
			return fmt.Errorf("sing-box check: %w", err)
		}
	}
	if output.NeedsXray {
		if checker.XrayPath == "" {
			return fmt.Errorf("xray is required but no binary path is configured")
		}
		if err := checker.check(ctx, checker.XrayPath, []string{"run", "-test", "-c"}, filepath.Join(directory, "xray.json"), output.Xray); err != nil {
			return fmt.Errorf("xray check: %w", err)
		}
	}
	return nil
}

func (checker CommandCoreChecker) check(ctx context.Context, binary string, arguments []string, path string, contents []byte) error {
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		return fmt.Errorf("stage candidate config: %w", err)
	}
	arguments = append(arguments, path)
	_, err := platform.Run(ctx, platform.Command{Path: binary, Args: arguments, Timeout: checker.Timeout})
	return err
}
