package bbr

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sherlock-wong/vps-net-manager/internal/platform"
)

func TestEnableWritesOnlyProjectFiles(t *testing.T) {
	directory := t.TempDir()
	manager := Manager{SysctlPath: filepath.Join(directory, "sysctl.conf"), ModulePath: filepath.Join(directory, "modules.conf"), Runner: platform.RunnerFunc(func(context.Context, platform.Command) (platform.CommandResult, error) {
		return platform.CommandResult{}, nil
	})}
	if err := manager.Enable(context.Background()); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(manager.SysctlPath)
	if err != nil || !strings.Contains(string(contents), "tcp_congestion_control = bbr") {
		t.Fatalf("sysctl = %q, err = %v", contents, err)
	}
}

func TestEnableRestoresFilesWhenSysctlReloadFails(t *testing.T) {
	directory := t.TempDir()
	sysctlPath := filepath.Join(directory, "sysctl.conf")
	if err := os.WriteFile(sysctlPath, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manager := Manager{SysctlPath: sysctlPath, ModulePath: filepath.Join(directory, "modules.conf"), Runner: platform.RunnerFunc(func(context.Context, platform.Command) (platform.CommandResult, error) {
		return platform.CommandResult{}, errors.New("sysctl failed")
	})}
	if err := manager.Enable(context.Background()); err == nil {
		t.Fatal("Enable unexpectedly succeeded")
	}
	contents, err := os.ReadFile(sysctlPath)
	if err != nil || string(contents) != "old\n" {
		t.Fatalf("sysctl = %q, err = %v", contents, err)
	}
	if _, err := os.Stat(manager.ModulePath); !os.IsNotExist(err) {
		t.Fatalf("module file remains: %v", err)
	}
}

func TestStatusRequiresBothBBRAndFQ(t *testing.T) {
	responses := []string{"bbr\n", "fq\n"}
	manager := Manager{Runner: platform.RunnerFunc(func(context.Context, platform.Command) (platform.CommandResult, error) {
		value := responses[0]
		responses = responses[1:]
		return platform.CommandResult{Output: value}, nil
	})}
	status, err := manager.Status(context.Background())
	if err != nil || !status.Enabled {
		t.Fatalf("status = %+v, err = %v", status, err)
	}
}
