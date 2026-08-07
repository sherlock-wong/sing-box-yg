// Package web owns the typed state and deterministic Nginx configuration for
// VPNM-managed HTTPS reverse proxies.
package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"regexp"
	"sort"
	"strings"

	"github.com/sherlock-wong/vps-net-manager/internal/model"
)

const Schema = 1

var ruleID = regexp.MustCompile(`^web_[a-f0-9]{8}$`)

type State struct {
	Schema  int     `json:"schema"`
	Proxies []Proxy `json:"proxies"`
}

type Proxy struct {
	ID            string `json:"id"`
	Domain        string `json:"domain"`
	ListenPort    uint16 `json:"listen_port"`
	TargetHost    string `json:"target_host"`
	TargetPort    uint16 `json:"target_port"`
	CertificateID string `json:"certificate_id"`
}

func NewState() State { return State{Schema: Schema, Proxies: []Proxy{}} }

func (state State) Validate(certificates map[string]model.Certificate) error {
	if state.Schema != Schema {
		return fmt.Errorf("unsupported web schema %d", state.Schema)
	}
	seen := map[string]struct{}{}
	for index, item := range state.Proxies {
		if !ruleID.MatchString(item.ID) {
			return fmt.Errorf("proxies[%d]: invalid id", index)
		}
		if !hostname(item.Domain) || !host(item.TargetHost) || item.ListenPort == 0 || item.TargetPort == 0 {
			return fmt.Errorf("proxies[%d]: invalid domain, target, or port", index)
		}
		certificate, found := certificates[item.CertificateID]
		if !found {
			return fmt.Errorf("proxies[%d]: certificate does not exist", index)
		}
		if certificate.Cert == "" || certificate.Key == "" {
			return fmt.Errorf("proxies[%d]: certificate is incomplete", index)
		}
		// A VPNM-managed hostname has one owner. Nginx technically permits the
		// same name on several ports, but that makes service ownership ambiguous.
		key := strings.ToLower(item.Domain)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("proxies[%d]: domain is already managed by another proxy", index)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func Config(state State, certificates map[string]model.Certificate) ([]byte, error) {
	if err := state.Validate(certificates); err != nil {
		return nil, err
	}
	items := append([]Proxy(nil), state.Proxies...)
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	var output bytes.Buffer
	output.WriteString("# Managed by VPS Net Manager. Do not edit.\n")
	for _, item := range items {
		certificate := certificates[item.CertificateID]
		fmt.Fprintf(&output, "\nserver {\n    listen %d ssl;\n    server_name %s;\n\n    ssl_certificate %s;\n    ssl_certificate_key %s;\n    ssl_protocols TLSv1.2 TLSv1.3;\n\n    location / {\n        proxy_pass http://%s;\n        proxy_http_version 1.1;\n        proxy_set_header Host $host;\n        proxy_set_header X-Real-IP $remote_addr;\n        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;\n        proxy_set_header X-Forwarded-Proto $scheme;\n        proxy_set_header Upgrade $http_upgrade;\n        proxy_set_header Connection \"upgrade\";\n        proxy_buffering off;\n    }\n}\n", item.ListenPort, item.Domain, certificate.Cert, certificate.Key, endpoint(item.TargetHost, item.TargetPort))
	}
	return output.Bytes(), nil
}

func Rules(state State) []PortRule {
	rules := make([]PortRule, 0, len(state.Proxies))
	seen := map[uint16]struct{}{}
	for _, item := range state.Proxies {
		if _, ok := seen[item.ListenPort]; !ok {
			rules, seen[item.ListenPort] = append(rules, PortRule{Port: item.ListenPort}), struct{}{}
		}
	}
	return rules
}

type PortRule struct{ Port uint16 }

func Parse(data []byte, certificates map[string]model.Certificate) (State, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var state State
	if err := decoder.Decode(&state); err != nil {
		return State{}, fmt.Errorf("parse web state: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return State{}, fmt.Errorf("parse web state: multiple JSON values")
	}
	if err := state.Validate(certificates); err != nil {
		return State{}, err
	}
	return state, nil
}

func endpoint(host string, port uint16) string {
	return net.JoinHostPort(host, fmt.Sprintf("%d", port))
}
func host(value string) bool { return net.ParseIP(value) != nil || hostname(value) }
func hostname(value string) bool {
	if len(value) == 0 || len(value) > 253 || strings.ContainsAny(value, ":/ \t") {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if !(r == '-' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
				return false
			}
		}
	}
	return true
}
