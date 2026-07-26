package app

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/sherlock-wong/vps-net-manager/internal/platform"
	"github.com/sherlock-wong/vps-net-manager/internal/protocol"
)

const defaultUFWMarker = "vps-net-manager"

// UFWController never deletes an unmarked rule. It pre-opens only candidate
// ports and removes only project-owned rules which became obsolete.
type UFWController struct {
	Runner  platform.CommandRunner
	Marker  string
	Timeout time.Duration
}

func (controller UFWController) Prepare(ctx context.Context, previous, candidate []protocol.FirewallRule) (Rollback, error) {
	active, status, err := controller.status(ctx)
	if err != nil {
		return nil, err
	}
	if !active {
		return func(context.Context) error { return nil }, nil
	}
	added := make([]protocol.FirewallRule, 0)
	for _, rule := range candidate {
		if ufwHasSpec(status, ruleSpec(rule)) {
			continue
		}
		if err := controller.allow(ctx, rule); err != nil {
			_ = controller.deleteRules(ctx, added)
			return nil, err
		}
		added = append(added, rule)
	}
	return func(rollbackContext context.Context) error { return controller.deleteRules(rollbackContext, added) }, nil
}

func (controller UFWController) Finalize(ctx context.Context, previous, candidate []protocol.FirewallRule) error {
	active, status, err := controller.status(ctx)
	if err != nil {
		return err
	}
	if !active {
		return nil
	}
	current := make(map[string]struct{}, len(candidate))
	for _, rule := range candidate {
		current[ruleKey(rule)] = struct{}{}
	}
	obsolete := make([]protocol.FirewallRule, 0)
	for _, rule := range previous {
		if _, retained := current[ruleKey(rule)]; retained || !strings.Contains(status, controller.markerFor(rule)) {
			continue
		}
		obsolete = append(obsolete, rule)
	}
	return controller.deleteRules(ctx, obsolete)
}

func (controller UFWController) status(ctx context.Context) (bool, string, error) {
	runner := controller.Runner
	if runner == nil {
		runner = platform.SystemRunner{}
	}
	result, err := runner.Run(ctx, platform.Command{Path: "ufw", Args: []string{"status"}, Env: []string{"LC_ALL=C"}, Timeout: controller.Timeout})
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return false, "", nil
		}
		return false, "", fmt.Errorf("check UFW status: %w", err)
	}
	return strings.Contains(result.Output, "Status: active"), result.Output, nil
}

func (controller UFWController) allow(ctx context.Context, rule protocol.FirewallRule) error {
	runner := controller.Runner
	if runner == nil {
		runner = platform.SystemRunner{}
	}
	_, err := runner.Run(ctx, platform.Command{Path: "ufw", Args: []string{"allow", ruleSpec(rule), "comment", controller.markerFor(rule)}, Env: []string{"LC_ALL=C"}, Timeout: controller.Timeout})
	if err != nil {
		return fmt.Errorf("allow %s: %w", ruleSpec(rule), err)
	}
	return nil
}

func (controller UFWController) deleteRules(ctx context.Context, rules []protocol.FirewallRule) error {
	runner := controller.Runner
	if runner == nil {
		runner = platform.SystemRunner{}
	}
	sort.Slice(rules, func(left, right int) bool { return ruleKey(rules[left]) > ruleKey(rules[right]) })
	var failures []error
	for _, rule := range rules {
		_, err := runner.Run(ctx, platform.Command{Path: "ufw", Args: []string{"--force", "delete", "allow", ruleSpec(rule), "comment", controller.markerFor(rule)}, Env: []string{"LC_ALL=C"}, Timeout: controller.Timeout})
		if err != nil {
			failures = append(failures, fmt.Errorf("delete %s: %w", ruleSpec(rule), err))
		}
	}
	return errors.Join(failures...)
}

func (controller UFWController) markerFor(rule protocol.FirewallRule) string {
	marker := controller.Marker
	if marker == "" {
		marker = defaultUFWMarker
	}
	return marker + ":" + ruleKey(rule)
}
func ruleSpec(rule protocol.FirewallRule) string {
	return fmt.Sprintf("%d/%s", rule.Port, rule.Network)
}
func ruleKey(rule protocol.FirewallRule) string { return fmt.Sprintf("%s-%d", rule.Network, rule.Port) }
func ufwHasSpec(status, spec string) bool       { return strings.Contains(status, spec) }
