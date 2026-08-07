package web

import (
	"github.com/sherlock-wong/vps-net-manager/internal/model"
	"strings"
	"testing"
)

func certificates() map[string]model.Certificate {
	return map[string]model.Certificate{"cert": {Name: "cert", Cert: "/cert/fullchain.pem", Key: "/cert/privkey.pem"}}
}
func TestConfigRendersWebSocketProxy(t *testing.T) {
	state := State{Schema: Schema, Proxies: []Proxy{{ID: "web_a1b2c3d4", Domain: "monitor.example.com", ListenPort: 8443, TargetHost: "127.0.0.1", TargetPort: 25774, CertificateID: "cert"}}}
	config, err := Config(state, certificates())
	if err != nil {
		t.Fatal(err)
	}
	for _, wanted := range []string{"listen 8443 ssl", "server_name monitor.example.com", "proxy_pass http://127.0.0.1:25774", "Connection \"upgrade\""} {
		if !strings.Contains(string(config), wanted) {
			t.Fatalf("config missing %q: %s", wanted, config)
		}
	}
}

func TestValidateRejectsDuplicateDomainAcrossPorts(t *testing.T) {
	state := State{Schema: Schema, Proxies: []Proxy{
		{ID: "web_a1b2c3d4", Domain: "monitor.example.com", ListenPort: 443, TargetHost: "127.0.0.1", TargetPort: 25774, CertificateID: "cert"},
		{ID: "web_b1b2c3d4", Domain: "monitor.example.com", ListenPort: 8443, TargetHost: "127.0.0.1", TargetPort: 25775, CertificateID: "cert"},
	}}
	if err := state.Validate(certificates()); err == nil || !strings.Contains(err.Error(), "already managed") {
		t.Fatalf("Validate error = %v", err)
	}
}
