package app

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/sherlock-wong/vps-net-manager/internal/komari"
	"github.com/sherlock-wong/vps-net-manager/internal/platform"
)

func TestNginxInstallerUninstallRemovesOnlyManagedConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "vps-net-manager.conf")
	if err := os.WriteFile(configPath, []byte("managed"), 0o644); err != nil {
		t.Fatal(err)
	}
	var commands []string
	runner := platform.RunnerFunc(func(_ context.Context, command platform.Command) (platform.CommandResult, error) {
		commands = append(commands, command.Path+" "+strings.Join(command.Args, " "))
		return platform.CommandResult{}, nil
	})
	installer := NginxInstaller{Runner: runner, ConfigPath: configPath}
	if err := installer.Uninstall(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("managed config remains: %v", err)
	}
	want := []string{"nginx -v", "systemctl disable --now nginx", "apt-get purge -y nginx nginx-common"}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands=%v want=%v", commands, want)
	}
}

func TestUninstallKomariRemovesOwnedResourcesAndData(t *testing.T) {
	stateDirectory, unitDirectory := filepath.Join(t.TempDir(), "vps-net-manager"), t.TempDir()
	for _, path := range []string{
		filepath.Join(stateDirectory, "bin", "komari"),
		filepath.Join(stateDirectory, "bin", "komari.json"),
		filepath.Join(stateDirectory, DefaultKomariStateFile),
		filepath.Join(stateDirectory, "komari", "database.db"),
		filepath.Join(unitDirectory, DefaultKomariService),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("owned"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var commands []string
	runner := platform.RunnerFunc(func(_ context.Context, command platform.Command) (platform.CommandResult, error) {
		commands = append(commands, command.Path+" "+strings.Join(command.Args, " "))
		return platform.CommandResult{}, nil
	})
	state := komari.State{Schema: komari.Schema, Enabled: true, Mode: komari.ModeDirect, ListenHost: "0.0.0.0", ListenPort: 25774}
	if err := UninstallKomari(context.Background(), stateDirectory, unitDirectory, state, true, runner, NoopFirewallController{}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(stateDirectory, "bin", "komari"), filepath.Join(stateDirectory, DefaultKomariStateFile), filepath.Join(stateDirectory, "komari"), filepath.Join(unitDirectory, DefaultKomariService)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("resource remains %s: %v", path, err)
		}
	}
	if !containsCommand(commands, "systemctl disable --now "+DefaultKomariService) || !containsCommand(commands, "systemctl daemon-reload") {
		t.Fatalf("commands=%v", commands)
	}
}
