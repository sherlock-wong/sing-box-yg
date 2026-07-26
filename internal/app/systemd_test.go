package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sherlock-wong/vps-net-manager/internal/platform"
)

func TestSystemdServiceControllerActivatesCandidateAndRestoresPreviousSet(t *testing.T) {
	active := map[string]bool{DefaultSingBoxService: true}
	commands := []string{}
	runner := platform.RunnerFunc(func(_ context.Context, command platform.Command) (platform.CommandResult, error) {
		commands = append(commands, strings.Join(append([]string{command.Path}, command.Args...), " "))
		unit := command.Args[len(command.Args)-1]
		switch command.Args[0] {
		case "is-active":
			if active[unit] {
				return platform.CommandResult{}, nil
			}
			return platform.CommandResult{ExitCode: 3}, errors.New("inactive")
		case "stop":
			active[unit] = false
		case "start":
			active[unit] = true
		}
		return platform.CommandResult{}, nil
	})
	controller := SystemdServiceController{Runner: runner}
	rollback, err := controller.Activate(context.Background(), CoreSet{Xray: true})
	if err != nil {
		t.Fatal(err)
	}
	if active[DefaultSingBoxService] || !active[DefaultXrayService] {
		t.Fatalf("active = %v", active)
	}
	if err := rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !active[DefaultSingBoxService] || active[DefaultXrayService] {
		t.Fatalf("restored active = %v", active)
	}
	if len(commands) < 8 {
		t.Fatalf("commands = %v", commands)
	}
}

func TestSystemdServiceControllerAllowsZeroProtocolStateWithoutInstalledUnits(t *testing.T) {
	commands := []string{}
	runner := platform.RunnerFunc(func(_ context.Context, command platform.Command) (platform.CommandResult, error) {
		commands = append(commands, strings.Join(command.Args, " "))
		return platform.CommandResult{ExitCode: 4}, errors.New("unit not found")
	})
	rollback, err := (SystemdServiceController{Runner: runner}).Activate(context.Background(), CoreSet{})
	if err != nil {
		t.Fatal(err)
	}
	if err := rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 2 {
		t.Fatalf("commands = %v", commands)
	}
}
