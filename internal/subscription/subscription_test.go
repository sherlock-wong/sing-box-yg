package subscription

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

func registry(t *testing.T) *protocol.Registry {
	t.Helper()
	value, err := protocol.NewRegistry(vlessreality.Module{}, hysteria2.Module{}, anytls.Module{})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func subscriptionState() model.State {
	return model.State{Schema: model.CurrentSchema, Certificates: map[string]model.Certificate{"default": {Name: "fixed", Cert: "/cert.pem", Key: "/key.pem", Insecure: true}}, Protocols: model.Protocols{
		VLESSReality: &model.VLESSReality{Enabled: true, Name: "Reality node", PublicAddress: "2001:db8::1", Port: 443, Engine: model.RealityEngineSingBox, UUID: "7f5fa27f-ec78-4c2d-a5e9-b7375a2968d6", SNI: "www.example.com", PrivateKey: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", PublicKey: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", ShortID: "a1b2", Xray: model.XrayReality{Fingerprint: "chrome", SpiderX: "/"}},
		Hysteria2:    &model.Hysteria2{Enabled: true, Name: "Hy2 node", PublicAddress: "hy2.example.com", Port: 8443, Password: "s e c r e t", Domain: "node.example.com", CertificateID: "default", UpMbps: 100, DownMbps: 100, UDPHop: "20000:20100"},
		AnyTLS:       &model.AnyTLS{Enabled: false, PublicAddress: "anytls.example.com", Port: 9443, Password: "secret", Domain: "node.example.com", CertificateID: "default", Padding: model.Padding{Mode: model.PaddingDefault}},
	}}
}

func TestRenderIncludesEnabledLinksAndOutbounds(t *testing.T) {
	output, err := Render(subscriptionState(), registry(t))
	if err != nil {
		t.Fatal(err)
	}
	if !output.AddressAvailable || len(output.Links) != 2 {
		t.Fatalf("output = %+v", output)
	}
	if !strings.HasPrefix(output.Links[0], "hysteria2://s%20e%20c%20r%20e%20t@hy2.example.com:8443?") {
		t.Fatalf("hy2 link = %q", output.Links[0])
	}
	if !strings.Contains(output.Links[0], "insecure=1") || !strings.Contains(output.Links[0], "mport=20000%3A20100") {
		t.Fatalf("hy2 link = %q", output.Links[0])
	}
	if !strings.HasPrefix(output.Links[1], "vless://7f5fa27f-ec78-4c2d-a5e9-b7375a2968d6@[2001:db8::1]:443?") {
		t.Fatalf("vless link = %q", output.Links[1])
	}
	var client struct {
		Outbounds []struct {
			Type string `json:"type"`
		} `json:"outbounds"`
	}
	if err := json.Unmarshal(output.SingBox, &client); err != nil {
		t.Fatal(err)
	}
	if len(client.Outbounds) != 2 || client.Outbounds[0].Type != "hysteria2" || client.Outbounds[1].Type != "vless" {
		t.Fatalf("outbounds = %+v", client.Outbounds)
	}
	if strings.Count(output.LinksText(), "\n") != 2 {
		t.Fatalf("links text = %q", output.LinksText())
	}
	if !strings.Contains(string(output.Mihomo), "proxy-groups:") || !strings.Contains(string(output.Mihomo), "type: vless") || strings.Contains(string(output.Mihomo), "type: anytls") {
		t.Fatalf("mihomo = %s", output.Mihomo)
	}
}

func TestRenderAllowsMissingPublicAddress(t *testing.T) {
	state := subscriptionState()
	state.Protocols.VLESSReality.PublicAddress = ""
	state.Protocols.Hysteria2.PublicAddress = ""
	output, err := Render(state, registry(t))
	if err != nil {
		t.Fatal(err)
	}
	if output.AddressAvailable || len(output.Links) != 0 || output.LinksText() != "" || len(output.MissingAddresses) != 2 {
		t.Fatalf("output = %+v", output)
	}
}

func TestRenderMigratesLegacySharedAddress(t *testing.T) {
	state := subscriptionState()
	state.PublicAddress = "legacy.example.com"
	state.Protocols.VLESSReality.PublicAddress = ""
	state.Protocols.Hysteria2.PublicAddress = ""
	output, err := Render(state, registry(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(output.Links) != 2 || !strings.Contains(output.Links[0], "@legacy.example.com:8443") || !strings.Contains(output.Links[1], "@legacy.example.com:443") {
		t.Fatalf("links = %v", output.Links)
	}
}

func TestRenderIncludesPinnedAnyTLSCertificateDigest(t *testing.T) {
	state := subscriptionState()
	state.Protocols.VLESSReality.Enabled = false
	state.Protocols.Hysteria2.Enabled = false
	state.Protocols.AnyTLS.Enabled = true
	certificate := state.Certificates["default"]
	certificate.Mode = model.CertificateModePinned
	certificate.DER_SHA256 = strings.Repeat("a", 64)
	state.Certificates["default"] = certificate
	output, err := Render(state, registry(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(output.Links) != 1 || !strings.Contains(output.Links[0], "pcs="+strings.Repeat("a", 64)) {
		t.Fatalf("links = %v", output.Links)
	}
}
