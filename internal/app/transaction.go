package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/sherlock-wong/vps-net-manager/internal/model"
	"github.com/sherlock-wong/vps-net-manager/internal/platform"
	"github.com/sherlock-wong/vps-net-manager/internal/protocol"
	"github.com/sherlock-wong/vps-net-manager/internal/render"
)

type CoreSet struct {
	SingBox bool
	Xray    bool
}

type PortChecker interface {
	CheckPorts(context.Context, model.State) error
}
type CoreChecker interface {
	CheckCores(context.Context, render.Output) error
}
type FirewallController interface {
	Prepare(context.Context, []protocol.FirewallRule, []protocol.FirewallRule) (Rollback, error)
	Finalize(context.Context, []protocol.FirewallRule, []protocol.FirewallRule) error
}
type ServiceController interface {
	Activate(context.Context, CoreSet) (Rollback, error)
}
type NATController interface {
	Prepare(context.Context, model.State, model.State) (Rollback, error)
	Finalize(context.Context, model.State, model.State) error
}

type NoopPortChecker struct{}

func (NoopPortChecker) CheckPorts(context.Context, model.State) error { return nil }

type NoopFirewallController struct{}

func (NoopFirewallController) Prepare(context.Context, []protocol.FirewallRule, []protocol.FirewallRule) (Rollback, error) {
	return func(context.Context) error { return nil }, nil
}
func (NoopFirewallController) Finalize(context.Context, []protocol.FirewallRule, []protocol.FirewallRule) error {
	return nil
}

type NoopServiceController struct{}

func (NoopServiceController) Activate(context.Context, CoreSet) (Rollback, error) {
	return func(context.Context) error { return nil }, nil
}

type NoopNATController struct{}

func (NoopNATController) Prepare(context.Context, model.State, model.State) (Rollback, error) {
	return func(context.Context) error { return nil }, nil
}
func (NoopNATController) Finalize(context.Context, model.State, model.State) error { return nil }

// ApplyOptions carries every host-touching dependency explicitly so faults can
// be injected in tests and production adapters stay isolated from protocols.
type ApplyOptions struct {
	StateDirectory string
	Previous       *model.State
	Ports          PortChecker
	Cores          CoreChecker
	Firewall       FirewallController
	Artifacts      ArtifactStore
	Services       ServiceController
	NAT            NATController
	ExtraArtifacts []Artifact
}

type Applied struct {
	Rendered RenderedState
	Cores    CoreSet
	Rules    []protocol.FirewallRule
}

func (options ApplyOptions) Apply(ctx context.Context, state model.State) (Applied, error) {
	if options.StateDirectory == "" {
		return Applied{}, fmt.Errorf("state directory is required")
	}
	if options.Ports == nil || options.Cores == nil || options.Firewall == nil || options.Artifacts == nil || options.Services == nil {
		return Applied{}, fmt.Errorf("apply dependencies are required")
	}
	lock, err := platform.AcquireLock(ctx, filepath.Join(options.StateDirectory, "state.lock"))
	if err != nil {
		return Applied{}, fmt.Errorf("acquire state lock: %w", err)
	}
	defer lock.Release()
	rendered, err := RenderState(state)
	if err != nil {
		return Applied{}, err
	}
	if err := options.Ports.CheckPorts(ctx, state); err != nil {
		return Applied{}, fmt.Errorf("check candidate ports: %w", err)
	}
	if err := options.Cores.CheckCores(ctx, rendered.Server); err != nil {
		return Applied{}, fmt.Errorf("check candidate core configuration: %w", err)
	}
	rules, err := FirewallRules(state)
	if err != nil {
		return Applied{}, err
	}
	var previousRules []protocol.FirewallRule
	if options.Previous != nil {
		previousRules, err = FirewallRules(*options.Previous)
		if err != nil {
			return Applied{}, fmt.Errorf("render previous firewall rules: %w", err)
		}
	}
	firewallRollback, err := options.Firewall.Prepare(ctx, previousRules, rules)
	if err != nil {
		return Applied{}, fmt.Errorf("pre-open firewall rules: %w", err)
	}
	nat := options.NAT
	if nat == nil {
		nat = NoopNATController{}
	}
	previousState := model.NewState()
	if options.Previous != nil {
		previousState = *options.Previous
	}
	natRollback, err := nat.Prepare(ctx, previousState, state)
	if err != nil {
		return Applied{}, joinFailure(fmt.Errorf("prepare Hy2 NAT: %w", err), firewallRollback(ctx))
	}
	artifacts, err := StateArtifacts(options.StateDirectory, state, rendered)
	if err != nil {
		rollbackErr := firewallRollback(ctx)
		return Applied{}, joinFailure(err, rollbackErr, natRollback(ctx))
	}
	artifacts = append(artifacts, options.ExtraArtifacts...)
	fileRollback, err := options.Artifacts.Commit(ctx, artifacts)
	if err != nil {
		rollbackErr := firewallRollback(ctx)
		return Applied{}, joinFailure(fmt.Errorf("commit candidate artifacts: %w", err), rollbackErr, natRollback(ctx))
	}
	cores := CoreSet{SingBox: rendered.Server.NeedsSingBox, Xray: rendered.Server.NeedsXray}
	serviceRollback, err := options.Services.Activate(ctx, cores)
	if err != nil {
		return Applied{}, joinFailure(fmt.Errorf("activate candidate services: %w", err), fileRollback(ctx), firewallRollback(ctx), natRollback(ctx))
	}
	if err := options.Firewall.Finalize(ctx, previousRules, rules); err != nil {
		return Applied{}, joinFailure(fmt.Errorf("finalize firewall rules: %w", err), fileRollback(ctx), serviceRollback(ctx), firewallRollback(ctx), natRollback(ctx))
	}
	if err := nat.Finalize(ctx, previousState, state); err != nil {
		return Applied{}, joinFailure(fmt.Errorf("finalize Hy2 NAT: %w", err), fileRollback(ctx), serviceRollback(ctx), firewallRollback(ctx), natRollback(ctx))
	}
	return Applied{Rendered: rendered, Cores: cores, Rules: rules}, nil
}

func StateArtifacts(directory string, state model.State, rendered RenderedState) ([]Artifact, error) {
	encodedState, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode state: %w", err)
	}
	generated := filepath.Join(directory, "generated")
	return []Artifact{
		{Path: filepath.Join(directory, "state.json"), Contents: encodedState, Mode: 0o600},
		{Path: filepath.Join(generated, "sing-box.json"), Contents: rendered.Server.SingBox, Mode: 0o600},
		{Path: filepath.Join(generated, "xray.json"), Contents: rendered.Server.Xray, Mode: 0o600},
		{Path: filepath.Join(generated, "subscription.txt"), Contents: []byte(rendered.Subscription.LinksText()), Mode: 0o600},
		{Path: filepath.Join(generated, "sing-box-client.json"), Contents: rendered.Subscription.SingBox, Mode: 0o600},
		{Path: filepath.Join(generated, "mihomo.yaml"), Contents: rendered.Subscription.Mihomo, Mode: 0o600},
	}, nil
}

func FirewallRules(state model.State) ([]protocol.FirewallRule, error) {
	registry, err := DefaultRegistry()
	if err != nil {
		return nil, err
	}
	view := model.NewSnapshot(state)
	unique := make(map[string]protocol.FirewallRule)
	for _, item := range registry.All() {
		for _, rule := range item.FirewallRules(view) {
			unique[fmt.Sprintf("%s/%d", rule.Network, rule.Port)] = rule
		}
	}
	rules := make([]protocol.FirewallRule, 0, len(unique))
	for _, rule := range unique {
		rules = append(rules, rule)
	}
	sort.Slice(rules, func(left, right int) bool {
		if rules[left].Network != rules[right].Network {
			return rules[left].Network < rules[right].Network
		}
		return rules[left].Port < rules[right].Port
	})
	return rules, nil
}

func joinFailure(primary error, rollbacks ...error) error {
	failures := []error{primary}
	for _, err := range rollbacks {
		if err != nil {
			failures = append(failures, fmt.Errorf("rollback: %w", err))
		}
	}
	return errors.Join(failures...)
}
