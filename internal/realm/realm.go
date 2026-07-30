// Package realm owns the typed, independent state used for Realm forwarding.
package realm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"regexp"
	"sort"
	"strings"
)

const Schema = 1

var ruleID = regexp.MustCompile(`^realm_[a-f0-9]{8}$`)

type State struct {
	Schema int    `json:"schema"`
	Rules  []Rule `json:"rules"`
}

type Rule struct {
	ID         string `json:"id"`
	ListenHost string `json:"listen_host"`
	ListenPort uint16 `json:"listen_port"`
	RemoteHost string `json:"remote_host"`
	RemotePort uint16 `json:"remote_port"`
}

func NewState() State { return State{Schema: Schema, Rules: []Rule{}} }

func (state State) Validate() error {
	if state.Schema != Schema {
		return fmt.Errorf("unsupported realm schema %d", state.Schema)
	}
	seen := make(map[string]struct{}, len(state.Rules))
	for index, rule := range state.Rules {
		if !ruleID.MatchString(rule.ID) {
			return fmt.Errorf("rules[%d]: invalid id", index)
		}
		if !validHost(rule.ListenHost) || !validHost(rule.RemoteHost) {
			return fmt.Errorf("rules[%d]: invalid listen or remote host", index)
		}
		if rule.ListenPort == 0 || rule.RemotePort == 0 {
			return fmt.Errorf("rules[%d]: ports must be between 1 and 65535", index)
		}
		key := endpoint(rule.ListenHost, rule.ListenPort)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("rules[%d]: duplicate listener %s", index, key)
		}
		seen[key] = struct{}{}
	}
	return nil
}

// Config renders stable TOML; Realm forwards both TCP and UDP for every rule.
func Config(state State) ([]byte, error) {
	if err := state.Validate(); err != nil {
		return nil, err
	}
	rules := append([]Rule(nil), state.Rules...)
	sort.SliceStable(rules, func(left, right int) bool { return rules[left].ID < rules[right].ID })
	var output bytes.Buffer
	output.WriteString("# Managed by VPS Net Manager. Edit via vm to preserve state.\n[log]\nlevel = \"warn\"\n\n[network]\nno_tcp = false\nuse_udp = true\nipv6_only = false\n")
	for _, rule := range rules {
		fmt.Fprintf(&output, "\n[[endpoints]]\nlisten = %q\nremote = %q\n", endpoint(rule.ListenHost, rule.ListenPort), endpoint(rule.RemoteHost, rule.RemotePort))
	}
	return output.Bytes(), nil
}

func Rules(state State) []PortRule {
	rules := make([]PortRule, 0, len(state.Rules)*2)
	for _, rule := range state.Rules {
		rules = append(rules, PortRule{ID: rule.ID, Port: rule.ListenPort, Network: "tcp"}, PortRule{ID: rule.ID, Port: rule.ListenPort, Network: "udp"})
	}
	return rules
}

type PortRule struct {
	ID      string
	Port    uint16
	Network string
}

func Parse(data []byte) (State, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var state State
	if err := decoder.Decode(&state); err != nil {
		return State{}, fmt.Errorf("parse realm state: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return State{}, fmt.Errorf("parse realm state: multiple JSON values")
	}
	if err := state.Validate(); err != nil {
		return State{}, err
	}
	return state, nil
}

func endpoint(host string, port uint16) string {
	return net.JoinHostPort(host, fmt.Sprintf("%d", port))
}

func validHost(host string) bool {
	if net.ParseIP(host) != nil {
		return true
	}
	if len(host) == 0 || len(host) > 253 || strings.HasSuffix(host, ".") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-') {
				return false
			}
		}
	}
	return true
}
