package app

import (
	"fmt"

	"github.com/sherlock-wong/vps-net-manager/internal/model"
	"github.com/sherlock-wong/vps-net-manager/internal/protocol"
	"github.com/sherlock-wong/vps-net-manager/internal/protocol/anytls"
	"github.com/sherlock-wong/vps-net-manager/internal/protocol/hysteria2"
	"github.com/sherlock-wong/vps-net-manager/internal/protocol/vlessreality"
	"github.com/sherlock-wong/vps-net-manager/internal/render"
	"github.com/sherlock-wong/vps-net-manager/internal/subscription"
)

// DefaultRegistry is the single registration point for protocols included in
// this build. Menus, renderers, and transactions will all use this registry.
func DefaultRegistry() (*protocol.Registry, error) {
	return protocol.NewRegistry(vlessreality.Module{}, hysteria2.Module{}, anytls.Module{})
}

type RenderedState struct {
	Server       render.Output
	Subscription subscription.Output
}

// RenderState performs all pure state validation and output rendering. It is
// intentionally safe to call during preflight because it writes nothing.
func RenderState(state model.State) (RenderedState, error) {
	registry, err := DefaultRegistry()
	if err != nil {
		return RenderedState{}, fmt.Errorf("build protocol registry: %w", err)
	}
	server, err := render.Configs(state, registry)
	if err != nil {
		return RenderedState{}, fmt.Errorf("render server config: %w", err)
	}
	client, err := subscription.Render(state, registry)
	if err != nil {
		return RenderedState{}, fmt.Errorf("render client config: %w", err)
	}
	return RenderedState{Server: server, Subscription: client}, nil
}
