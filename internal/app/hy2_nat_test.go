package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sherlock-wong/vps-net-manager/internal/model"
	"github.com/sherlock-wong/vps-net-manager/internal/platform"
)

func TestHy2NATPreopensBothFamiliesAndRollsBack(t *testing.T) {
	commands := []string{}
	runner := platform.RunnerFunc(func(_ context.Context, command platform.Command) (platform.CommandResult, error) {
		commands = append(commands, command.Path+" "+strings.Join(command.Args, " "))
		if strings.Contains(strings.Join(command.Args, " "), " -C ") {
			return platform.CommandResult{ExitCode: 1}, errors.New("absent")
		}
		return platform.CommandResult{}, nil
	})
	previous, candidate := model.NewState(), model.NewState()
	candidate.Protocols.Hysteria2 = &model.Hysteria2{Enabled: true, Port: 8443, UDPHop: "20000:20100"}
	rollback, err := (Hy2NATController{Runner: runner}).Prepare(context.Background(), previous, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if err := rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(commands, "\n")
	for _, expected := range []string{"iptables -t nat -A PREROUTING", "ip6tables -t nat -A PREROUTING", "iptables -t nat -D PREROUTING", "ip6tables -t nat -D PREROUTING", "--dport 20000:20100", "--to-ports 8443"} {
		if !strings.Contains(joined, expected) {
			t.Errorf("commands lack %q:\n%s", expected, joined)
		}
	}
}
