package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sherlock-wong/vps-net-manager/internal/platform"
)

const (
	DefaultSingBoxService = "vps-net-manager-sing-box.service"
	DefaultXrayService    = "vps-net-manager-xray.service"
)

// SystemdServiceController changes only VPNM's two core units. It captures
// the prior running set and restores it if candidate activation fails.
type SystemdServiceController struct {
	Runner      platform.CommandRunner
	SingBoxUnit string
	XrayUnit    string
	Timeout     time.Duration
}

func (controller SystemdServiceController) Activate(ctx context.Context, desired CoreSet) (Rollback, error) {
	runner := controller.Runner
	if runner == nil {
		runner = platform.SystemRunner{}
	}
	singBox, xray := controller.units()
	previousSingBox, err := systemdActive(ctx, runner, singBox, controller.Timeout)
	if err != nil {
		return nil, err
	}
	previousXray, err := systemdActive(ctx, runner, xray, controller.Timeout)
	if err != nil {
		return nil, err
	}
	restore := func(restoreContext context.Context) error {
		if desired.SingBox || previousSingBox {
			if err := systemdStop(restoreContext, runner, singBox, controller.Timeout); err != nil {
				return err
			}
		}
		if desired.Xray || previousXray {
			if err := systemdStop(restoreContext, runner, xray, controller.Timeout); err != nil {
				return err
			}
		}
		if previousSingBox {
			if err := systemdStart(restoreContext, runner, singBox, controller.Timeout); err != nil {
				return err
			}
		}
		if previousXray {
			if err := systemdStart(restoreContext, runner, xray, controller.Timeout); err != nil {
				return err
			}
		}
		return nil
	}
	if previousSingBox {
		if err := systemdStop(ctx, runner, singBox, controller.Timeout); err != nil {
			return nil, err
		}
	}
	if previousXray {
		if err := systemdStop(ctx, runner, xray, controller.Timeout); err != nil {
			_ = restore(ctx)
			return nil, err
		}
	}
	if desired.SingBox {
		if err := systemdStart(ctx, runner, singBox, controller.Timeout); err != nil {
			_ = restore(ctx)
			return nil, err
		}
	}
	if desired.Xray {
		if err := systemdStart(ctx, runner, xray, controller.Timeout); err != nil {
			_ = restore(ctx)
			return nil, err
		}
	}
	return restore, nil
}

func (controller SystemdServiceController) units() (string, string) {
	singBox, xray := controller.SingBoxUnit, controller.XrayUnit
	if singBox == "" {
		singBox = DefaultSingBoxService
	}
	if xray == "" {
		xray = DefaultXrayService
	}
	return singBox, xray
}
func systemdStop(ctx context.Context, runner platform.CommandRunner, unit string, timeout time.Duration) error {
	_, err := runner.Run(ctx, platform.Command{Path: "systemctl", Args: []string{"stop", unit}, Timeout: timeout})
	return err
}
func systemdStart(ctx context.Context, runner platform.CommandRunner, unit string, timeout time.Duration) error {
	if _, err := runner.Run(ctx, platform.Command{Path: "systemctl", Args: []string{"start", unit}, Timeout: timeout}); err != nil {
		return err
	}
	active, err := systemdActive(ctx, runner, unit, timeout)
	if err != nil {
		return err
	}
	if !active {
		return fmt.Errorf("%s did not become active", unit)
	}
	return nil
}
func systemdActive(ctx context.Context, runner platform.CommandRunner, unit string, timeout time.Duration) (bool, error) {
	result, err := runner.Run(ctx, platform.Command{Path: "systemctl", Args: []string{"is-active", "--quiet", unit}, Timeout: timeout})
	if err == nil {
		return true, nil
	}
	if result.ExitCode == 3 || result.ExitCode == 4 {
		return false, nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return false, fmt.Errorf("check %s: timed out", unit)
	}
	return false, fmt.Errorf("check %s: %w", unit, err)
}
