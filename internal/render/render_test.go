package render

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sherlock-wong/vps-net-manager/internal/model"
	"github.com/sherlock-wong/vps-net-manager/internal/protocol"
	"github.com/sherlock-wong/vps-net-manager/internal/protocol/anytls"
	"github.com/sherlock-wong/vps-net-manager/internal/protocol/hysteria2"
	"github.com/sherlock-wong/vps-net-manager/internal/protocol/vlessreality"
)

func TestConfigsRendersEnabledProtocolsOnly(t *testing.T) {
	state := model.State{Schema: model.CurrentSchema, Certificates: map[string]model.Certificate{"default": {Name: "test", Cert: "/cert.pem", Key: "/key.pem"}}, Protocols: model.Protocols{
		VLESSReality: &model.VLESSReality{Enabled: true, Port: 443, Engine: model.RealityEngineSingBox, UUID: "7f5fa27f-ec78-4c2d-a5e9-b7375a2968d6", SNI: "www.example.com", PrivateKey: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", PublicKey: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", ShortID: "a1b2"},
		Hysteria2:    &model.Hysteria2{Enabled: false, Port: 8443, Password: "secret", Domain: "node.example.com", CertificateID: "default", UpMbps: 100, DownMbps: 100},
		AnyTLS:       &model.AnyTLS{Enabled: true, Port: 9443, Password: "secret", Domain: "node.example.com", CertificateID: "default", Padding: model.Padding{Mode: model.PaddingDefault}},
	}}
	registry, err := protocol.NewRegistry(vlessreality.Module{}, hysteria2.Module{}, anytls.Module{})
	if err != nil {
		t.Fatal(err)
	}
	output, err := Configs(state, registry)
	if err != nil {
		t.Fatal(err)
	}
	var singBox struct {
		Inbounds []struct {
			Type string `json:"type"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(output.SingBox, &singBox); err != nil {
		t.Fatal(err)
	}
	if len(singBox.Inbounds) != 2 || singBox.Inbounds[0].Type != "anytls" || singBox.Inbounds[1].Type != "vless" {
		t.Fatalf("inbounds = %+v", singBox.Inbounds)
	}
	var xray struct {
		Inbounds []any `json:"inbounds"`
	}
	if err := json.Unmarshal(output.Xray, &xray); err != nil {
		t.Fatal(err)
	}
	if len(xray.Inbounds) != 0 {
		t.Fatalf("xray inbounds = %v", xray.Inbounds)
	}
}

func TestConfigsRendersXrayVLESS(t *testing.T) {
	state := model.State{Schema: model.CurrentSchema, Protocols: model.Protocols{VLESSReality: &model.VLESSReality{Enabled: true, Port: 443, Engine: model.RealityEngineXray, UUID: "7f5fa27f-ec78-4c2d-a5e9-b7375a2968d6", SNI: "www.example.com", PrivateKey: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", PublicKey: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", ShortID: "a1b2", Xray: model.XrayReality{Target: "www.example.com:443", ServerNames: []string{"www.example.com"}, MinClientVer: "1.8.0", MaxClientVer: "1.9.0", MaxTimeDiff: 1000, MLDSA65Seed: "seed", FallbackProfile: "balanced"}}}}
	registry, _ := protocol.NewRegistry(vlessreality.Module{})
	output, err := Configs(state, registry)
	if err != nil {
		t.Fatal(err)
	}
	var xray struct {
		Inbounds []any `json:"inbounds"`
	}
	if err := json.Unmarshal(output.Xray, &xray); err != nil {
		t.Fatal(err)
	}
	if len(xray.Inbounds) != 1 {
		t.Fatalf("xray inbounds = %v", xray.Inbounds)
	}
	if !strings.Contains(string(output.Xray), `"target": "www.example.com:443"`) || !strings.Contains(string(output.Xray), `"limitFallbackUpload"`) || !strings.Contains(string(output.Xray), `"tcpSettings"`) {
		t.Fatalf("xray config = %s", output.Xray)
	}
}
