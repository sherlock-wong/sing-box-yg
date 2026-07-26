package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sherlock-wong/vps-net-manager/internal/model"
	"github.com/sherlock-wong/vps-net-manager/internal/platform"
)

const hy2NATMarker = "vps-net-manager-hy2"

// Hy2NATController manages only explicit REDIRECT rules carrying VPNM's
// marker. Both iptables families are configured because the Hy2 inbound
// listens on IPv4 and IPv6.
type Hy2NATController struct {
	Runner  platform.CommandRunner
	Timeout time.Duration
}

func (controller Hy2NATController) Prepare(ctx context.Context, previous, candidate model.State) (Rollback, error) {
	previousRules, candidateRules := hy2Rules(previous), hy2Rules(candidate)
	added := make([]hy2Rule, 0)
	for _, rule := range candidateRules {
		if containsHy2Rule(previousRules, rule) {
			continue
		}
		if err := controller.add(ctx, rule); err != nil {
			_ = controller.deleteAll(ctx, added)
			return nil, err
		}
		added = append(added, rule)
	}
	return func(rollbackContext context.Context) error { return controller.deleteAll(rollbackContext, added) }, nil
}

func (controller Hy2NATController) Finalize(ctx context.Context, previous, candidate model.State) error {
	previousRules, candidateRules := hy2Rules(previous), hy2Rules(candidate)
	obsolete := make([]hy2Rule, 0)
	for _, rule := range previousRules {
		if !containsHy2Rule(candidateRules, rule) {
			obsolete = append(obsolete, rule)
		}
	}
	return controller.deleteAll(ctx, obsolete)
}

type hy2Rule struct {
	Hop  string
	Port uint16
}

func hy2Rules(state model.State) []hy2Rule {
	configuration := state.Protocols.Hysteria2
	if configuration == nil || !configuration.Enabled || configuration.UDPHop == "" {
		return nil
	}
	return []hy2Rule{{Hop: configuration.UDPHop, Port: configuration.Port}}
}
func containsHy2Rule(rules []hy2Rule, wanted hy2Rule) bool {
	for _, rule := range rules {
		if rule == wanted {
			return true
		}
	}
	return false
}

func (controller Hy2NATController) add(ctx context.Context, rule hy2Rule) error {
	for _, binary := range []string{"iptables", "ip6tables"} {
		if controller.exists(ctx, binary, rule) {
			continue
		}
		if err := controller.run(ctx, binary, append([]string{"-t", "nat", "-A", "PREROUTING"}, rule.args()...)); err != nil {
			return fmt.Errorf("add %s Hy2 hop: %w", binary, err)
		}
	}
	return nil
}

func (controller Hy2NATController) deleteAll(ctx context.Context, rules []hy2Rule) error {
	var failures []error
	for _, rule := range rules {
		for _, binary := range []string{"iptables", "ip6tables"} {
			if err := controller.run(ctx, binary, append([]string{"-t", "nat", "-D", "PREROUTING"}, rule.args()...)); err != nil {
				failures = append(failures, fmt.Errorf("remove %s Hy2 hop: %w", binary, err))
			}
		}
	}
	return joinErrors(failures)
}

func (controller Hy2NATController) exists(ctx context.Context, binary string, rule hy2Rule) bool {
	return controller.run(ctx, binary, append([]string{"-t", "nat", "-C", "PREROUTING"}, rule.args()...)) == nil
}
func (controller Hy2NATController) run(ctx context.Context, binary string, args []string) error {
	runner := controller.Runner
	if runner == nil {
		runner = platform.SystemRunner{}
	}
	timeout := controller.Timeout
	if timeout == 0 {
		timeout = 20 * time.Second
	}
	_, err := runner.Run(ctx, platform.Command{Path: binary, Args: args, Timeout: timeout})
	return err
}
func (rule hy2Rule) args() []string {
	return []string{"-p", "udp", "--dport", rule.Hop, "-m", "comment", "--comment", hy2NATMarker, "-j", "REDIRECT", "--to-ports", fmt.Sprintf("%d", rule.Port)}
}
func joinErrors(failures []error) error {
	if len(failures) == 0 {
		return nil
	}
	return fmt.Errorf("%s", strings.Join(errorStrings(failures), "; "))
}
func errorStrings(failures []error) []string {
	values := make([]string, 0, len(failures))
	for _, failure := range failures {
		values = append(values, failure.Error())
	}
	return values
}
