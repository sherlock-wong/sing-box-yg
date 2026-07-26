// Package render aggregates pure protocol contributions into core configs.
package render

import (
	"encoding/json"
	"fmt"

	"github.com/sherlock-wong/vps-net-manager/internal/model"
	"github.com/sherlock-wong/vps-net-manager/internal/protocol"
)

type Output struct {
	SingBox      []byte
	Xray         []byte
	NeedsSingBox bool
	NeedsXray    bool
}

// Configs validates state and produces JSON configurations without writing to
// disk or invoking either core. A transaction performs those host operations.
func Configs(state model.State, registry *protocol.Registry) (Output, error) {
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
	singBox := singBoxConfig{Log: logConfig{Level: "warn"}, Inbounds: make([]any, 0)}
	xray := xrayConfig{Log: xrayLog{LogLevel: "warning"}, Inbounds: make([]any, 0)}
	for _, item := range registry.All() {
		inbound, enabled, err := item.SingBoxInbound(view)
		if err != nil {
			return Output{}, fmt.Errorf("render sing-box %s: %w", item.Key(), err)
		}
		if enabled {
			singBox.Inbounds = append(singBox.Inbounds, inbound)
		}
		inbound, enabled, err = item.XrayInbound(view)
		if err != nil {
			return Output{}, fmt.Errorf("render xray %s: %w", item.Key(), err)
		}
		if enabled {
			xray.Inbounds = append(xray.Inbounds, inbound)
		}
	}
	singBoxJSON, err := json.MarshalIndent(singBox, "", "  ")
	if err != nil {
		return Output{}, fmt.Errorf("encode sing-box config: %w", err)
	}
	xrayJSON, err := json.MarshalIndent(xray, "", "  ")
	if err != nil {
		return Output{}, fmt.Errorf("encode xray config: %w", err)
	}
	return Output{SingBox: singBoxJSON, Xray: xrayJSON, NeedsSingBox: len(singBox.Inbounds) > 0, NeedsXray: len(xray.Inbounds) > 0}, nil
}

type logConfig struct {
	Level string `json:"level"`
}
type singBoxConfig struct {
	Log      logConfig `json:"log"`
	Inbounds []any     `json:"inbounds"`
}
type xrayLog struct {
	LogLevel string `json:"loglevel"`
}
type xrayConfig struct {
	Log      xrayLog `json:"log"`
	Inbounds []any   `json:"inbounds"`
}
