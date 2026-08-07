// Package komari owns the small, typed state required to run Komari as a
// VPNM-managed service. HTTPS exposure belongs to the generic web package.
package komari

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

const Schema = 1

type Mode string

const (
	ModeDirect Mode = "direct"
	ModeDomain Mode = "domain"
)

type State struct {
	Schema     int    `json:"schema"`
	Enabled    bool   `json:"enabled"`
	Mode       Mode   `json:"mode,omitempty"`
	ListenHost string `json:"listen_host,omitempty"`
	ListenPort uint16 `json:"listen_port,omitempty"`
	Domain     string `json:"domain,omitempty"`
	ProxyID    string `json:"proxy_id,omitempty"`
}

func NewState() State { return State{Schema: Schema} }

func (state State) Validate() error {
	if state.Schema != Schema {
		return fmt.Errorf("unsupported Komari schema %d", state.Schema)
	}
	if !state.Enabled {
		return nil
	}
	if state.ListenPort == 0 {
		return fmt.Errorf("Komari listen port is required")
	}
	switch state.Mode {
	case ModeDirect:
		if state.ListenHost != "0.0.0.0" || state.Domain != "" || state.ProxyID != "" {
			return fmt.Errorf("invalid direct Komari configuration")
		}
	case ModeDomain:
		if state.ListenHost != "127.0.0.1" || state.Domain == "" || state.ProxyID == "" {
			return fmt.Errorf("invalid domain Komari configuration")
		}
	default:
		return fmt.Errorf("unsupported Komari mode %q", state.Mode)
	}
	return nil
}

func Parse(data []byte) (State, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var state State
	if err := decoder.Decode(&state); err != nil {
		return State{}, fmt.Errorf("parse Komari state: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return State{}, fmt.Errorf("parse Komari state: multiple JSON values")
	}
	if err := state.Validate(); err != nil {
		return State{}, err
	}
	return state, nil
}
