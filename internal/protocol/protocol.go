// Package protocol defines the narrow contract shared by all server protocols.
package protocol

import (
	"fmt"
	"sort"

	"github.com/sherlock-wong/vps-net-manager/internal/model"
)

type FirewallRule struct {
	Network string
	Port    uint16
}

// Protocol implementations are pure: they validate and describe config, but
// never write files, change a firewall, or start a service.
type Protocol interface {
	Key() string
	Validate(model.StateView) error
	SingBoxInbound(model.StateView) (any, bool, error)
	XrayInbound(model.StateView) (any, bool, error)
	ClientOutbound(model.StateView, string) (any, error)
	ShareLink(model.StateView, string) (string, error)
	FirewallRules(model.StateView) []FirewallRule
}

// MihomoProvider is optional because it is a client format, not part of the
// server protocol contract. Protocols without a Mihomo representation simply
// do not implement it.
type MihomoProvider interface {
	MihomoProxy(model.StateView, string) (MihomoProxy, bool, error)
}

type MihomoProxy struct {
	Name   string
	Config any
}

type Registry struct{ protocols map[string]Protocol }

func NewRegistry(protocols ...Protocol) (*Registry, error) {
	registry := &Registry{protocols: make(map[string]Protocol, len(protocols))}
	for _, item := range protocols {
		if item == nil || item.Key() == "" {
			return nil, fmt.Errorf("protocol registry contains an empty protocol")
		}
		if _, exists := registry.protocols[item.Key()]; exists {
			return nil, fmt.Errorf("protocol %q is registered twice", item.Key())
		}
		registry.protocols[item.Key()] = item
	}
	return registry, nil
}

func (registry *Registry) Get(key string) (Protocol, bool) {
	item, ok := registry.protocols[key]
	return item, ok
}

func (registry *Registry) All() []Protocol {
	keys := make([]string, 0, len(registry.protocols))
	for key := range registry.protocols {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	items := make([]Protocol, 0, len(keys))
	for _, key := range keys {
		items = append(items, registry.protocols[key])
	}
	return items
}

func (registry *Registry) Validate(view model.StateView) error {
	for _, item := range registry.All() {
		if err := item.Validate(view); err != nil {
			return fmt.Errorf("%s: %w", item.Key(), err)
		}
	}
	return nil
}
