package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sherlock-wong/vps-net-manager/internal/platform"
	"github.com/sherlock-wong/vps-net-manager/internal/protocol"
)

func TestUFWPrepareAddsOnlyMissingRulesAndRollsThemBack(t *testing.T) {
	commands := []string{}
	runner := platform.RunnerFunc(func(_ context.Context, command platform.Command) (platform.CommandResult, error) {
		commands = append(commands, strings.Join(command.Args, " "))
		if command.Args[0] == "status" {
			return platform.CommandResult{Output: "Status: active\n443/tcp\n"}, nil
		}
		return platform.CommandResult{}, nil
	})
	controller := UFWController{Runner: runner}
	rollback, err := controller.Prepare(context.Background(), nil, []protocol.FirewallRule{{Network: "tcp", Port: 443}, {Network: "udp", Port: 8443}})
	if err != nil {
		t.Fatal(err)
	}
	if err := rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(commands, "\n")
	if strings.Contains(joined, "allow 443/tcp") || !strings.Contains(joined, "allow 8443/udp") || !strings.Contains(joined, "delete allow 8443/udp") {
		t.Fatalf("commands = %s", joined)
	}
}

func TestUWFFinalizeRemovesOnlyObsoleteManagedRule(t *testing.T) {
	commands := []string{}
	runner := platform.RunnerFunc(func(_ context.Context, command platform.Command) (platform.CommandResult, error) {
		commands = append(commands, strings.Join(command.Args, " "))
		if command.Args[0] == "status" {
			return platform.CommandResult{Output: "Status: active\n443/tcp # vps-net-manager:tcp-443\n"}, nil
		}
		return platform.CommandResult{}, nil
	})
	controller := UFWController{Runner: runner}
	if err := controller.Finalize(context.Background(), []protocol.FirewallRule{{Network: "tcp", Port: 443}}, []protocol.FirewallRule{{Network: "udp", Port: 8443}}); err != nil {
		t.Fatal(err)
	}
	if joined := strings.Join(commands, "\n"); !strings.Contains(joined, "delete allow 443/tcp comment vps-net-manager:tcp-443") {
		t.Fatalf("commands = %s", joined)
	}
}
