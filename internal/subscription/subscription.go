// Package subscription renders client-facing artifacts from enabled protocols.
package subscription

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sherlock-wong/vps-net-manager/internal/model"
	"github.com/sherlock-wong/vps-net-manager/internal/protocol"
	"gopkg.in/yaml.v3"
)

type Output struct {
	AddressAvailable bool
	Links            []string
	SingBox          []byte
	Mihomo           []byte
}

// Render only includes enabled protocols. An omitted public address is a
// normal incomplete-install state: no link is emitted and no secret is lost.
func Render(state model.State, registry *protocol.Registry) (Output, error) {
	if registry == nil {
		return Output{}, fmt.Errorf("protocol registry is required")
	}
	if err := state.Validate(); err != nil {
		return Output{}, err
	}
	view := model.NewSnapshot(state)
	if err := registry.Validate(view); err != nil {
		return Output{}, err
	}
	configuration := singBoxClient{Log: clientLog{Level: "warn"}, Outbounds: make([]any, 0)}
	mihomo := mihomoConfig{Proxies: make([]any, 0), ProxyGroups: []proxyGroup{{Name: "PROXY", Type: "select", Proxies: make([]string, 0)}}}
	output := Output{AddressAvailable: state.PublicAddress != "", Links: make([]string, 0)}
	if !output.AddressAvailable {
		encoded, err := json.MarshalIndent(configuration, "", "  ")
		if err != nil {
			return Output{}, fmt.Errorf("encode empty client config: %w", err)
		}
		output.SingBox = encoded
		output.Mihomo, err = yaml.Marshal(mihomo)
		if err != nil {
			return Output{}, fmt.Errorf("encode empty mihomo config: %w", err)
		}
		return output, nil
	}
	for _, item := range registry.All() {
		link, err := item.ShareLink(view, state.PublicAddress)
		if err != nil {
			return Output{}, fmt.Errorf("render share link %s: %w", item.Key(), err)
		}
		if link != "" {
			output.Links = append(output.Links, link)
		}
		outbound, err := item.ClientOutbound(view, state.PublicAddress)
		if err != nil {
			return Output{}, fmt.Errorf("render sing-box client %s: %w", item.Key(), err)
		}
		if outbound != nil {
			configuration.Outbounds = append(configuration.Outbounds, outbound)
		}
		provider, supported := item.(protocol.MihomoProvider)
		if !supported {
			continue
		}
		proxy, enabled, err := provider.MihomoProxy(view, state.PublicAddress)
		if err != nil {
			return Output{}, fmt.Errorf("render mihomo %s: %w", item.Key(), err)
		}
		if enabled {
			mihomo.Proxies = append(mihomo.Proxies, proxy.Config)
			mihomo.ProxyGroups[0].Proxies = append(mihomo.ProxyGroups[0].Proxies, proxy.Name)
		}
	}
	encoded, err := json.MarshalIndent(configuration, "", "  ")
	if err != nil {
		return Output{}, fmt.Errorf("encode sing-box client config: %w", err)
	}
	output.SingBox = encoded
	output.Mihomo, err = yaml.Marshal(mihomo)
	if err != nil {
		return Output{}, fmt.Errorf("encode mihomo config: %w", err)
	}
	return output, nil
}

func (output Output) LinksText() string {
	if len(output.Links) == 0 {
		return ""
	}
	return strings.Join(output.Links, "\n") + "\n"
}

type clientLog struct {
	Level string `json:"level"`
}
type singBoxClient struct {
	Log       clientLog `json:"log"`
	Outbounds []any     `json:"outbounds"`
}
type mihomoConfig struct {
	Proxies     []any        `yaml:"proxies"`
	ProxyGroups []proxyGroup `yaml:"proxy-groups"`
}
type proxyGroup struct {
	Name    string   `yaml:"name"`
	Type    string   `yaml:"type"`
	Proxies []string `yaml:"proxies"`
}
