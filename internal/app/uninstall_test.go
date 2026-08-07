package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sherlock-wong/vps-net-manager/internal/model"
	"github.com/sherlock-wong/vps-net-manager/internal/platform"
)

func TestUninstallerRemovesOnlyProvidedVPNMPaths(t *testing.T) {
	stateDirectory, units, binaryDirectory := filepath.Join(t.TempDir(), "vps-net-manager"), t.TempDir(), t.TempDir()
	if err := os.MkdirAll(stateDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	state, err := json.Marshal(model.NewState())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDirectory, "state.json"), state, 0o600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(binaryDirectory, "vpnm")
	if err := os.WriteFile(binary, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	commands := []string{}
	runner := platform.RunnerFunc(func(_ context.Context, command platform.Command) (platform.CommandResult, error) {
		commands = append(commands, strings.Join(command.Args, " "))
		return platform.CommandResult{}, nil
	})
	if err := (Uninstaller{StateDirectory: stateDirectory, UnitDirectory: units, BinaryPath: binary, Runner: runner, Firewall: NoopFirewallController{}}).Uninstall(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stateDirectory); !os.IsNotExist(err) {
		t.Fatalf("state remains: %v", err)
	}
	if _, err := os.Stat(binary); !os.IsNotExist(err) {
		t.Fatalf("binary remains: %v", err)
	}
	if !containsCommand(commands, "daemon-reload") || !containsCommand(commands, "reload nginx") {
		t.Fatalf("commands = %v", commands)
	}
}

func containsCommand(commands []string, wanted string) bool {
	for _, command := range commands {
		if command == wanted {
			return true
		}
	}
	return false
}
