package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/sherlock-wong/vps-net-manager/internal/platform"
)

func TestInstallCoreUnitsWritesOwnedUnitsAndReloadsSystemd(t *testing.T) {
	units := t.TempDir()
	commands := []string{}
	runner := platform.RunnerFunc(func(_ context.Context, command platform.Command) (platform.CommandResult, error) {
		commands = append(commands, strings.Join(command.Args, " "))
		return platform.CommandResult{}, nil
	})
	if err := InstallCoreUnits(context.Background(), "/etc/vps-net-manager", units, runner); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join(units, DefaultSingBoxService))
	if err != nil || !strings.Contains(string(contents), "ExecStart=/etc/vps-net-manager/bin/sing-box run -c /etc/vps-net-manager/generated/sing-box.json") {
		t.Fatalf("unit = %q, err = %v", contents, err)
	}
	if len(commands) != 1 || commands[0] != "daemon-reload" {
		t.Fatalf("commands = %v", commands)
	}
}

func TestInstallCoreUnitsRollsBackWhenReloadFails(t *testing.T) {
	units := t.TempDir()
	path := filepath.Join(units, DefaultSingBoxService)
	if err := os.WriteFile(path, []byte("old unit"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := platform.RunnerFunc(func(context.Context, platform.Command) (platform.CommandResult, error) {
		return platform.CommandResult{}, errors.New("reload failed")
	})
	if err := InstallCoreUnits(context.Background(), "/etc/vps-net-manager", units, runner); err == nil {
		t.Fatal("InstallCoreUnits unexpectedly succeeded")
	}
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != "old unit" {
		t.Fatalf("unit = %q, err = %v", contents, err)
	}
}

func TestInstallRealmUnitWritesOwnedUnit(t *testing.T) {
	units := t.TempDir()
	runner := platform.RunnerFunc(func(context.Context, platform.Command) (platform.CommandResult, error) {
		return platform.CommandResult{}, nil
	})
	if err := InstallRealmUnit(context.Background(), "/etc/vps-net-manager", units, runner); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join(units, DefaultRealmService))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), "ExecStart=/etc/vps-net-manager/bin/realm -c /etc/vps-net-manager/generated/realm.toml") {
		t.Fatalf("unexpected realm unit:\n%s", contents)
	}
}

func TestInstallCertificateTimerUsesNonInteractiveCommand(t *testing.T) {
	units := t.TempDir()
	commands := []string{}
	runner := platform.RunnerFunc(func(_ context.Context, command platform.Command) (platform.CommandResult, error) {
		commands = append(commands, strings.Join(command.Args, " "))
		return platform.CommandResult{}, nil
	})
	if err := InstallCertificateTimer(context.Background(), units, runner); err != nil {
		t.Fatal(err)
	}
	service, err := os.ReadFile(filepath.Join(units, DefaultCertSyncService))
	if err != nil || !strings.Contains(string(service), "ExecStart=/usr/local/bin/vm cert renew --quiet") {
		t.Fatalf("service = %q, err = %v", service, err)
	}
	if got, want := commands, []string{"daemon-reload", "enable --now " + DefaultCertSyncTimer}; !reflect.DeepEqual(got, want) {
		t.Fatalf("commands = %v, want %v", got, want)
	}
}
