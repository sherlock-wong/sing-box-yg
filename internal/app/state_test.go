package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sherlock-wong/vps-net-manager/internal/model"
)

func TestLoadStateRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	contents := `{"schema":1,"protocols":{},"accidental":true}`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadState(path); err == nil {
		t.Fatal("LoadState accepted an unknown field")
	}
}

func TestRenderStateProducesServerAndClientArtifacts(t *testing.T) {
	state := appState()
	artifacts, err := RenderState(state)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts.Server.SingBox) == 0 || len(artifacts.Subscription.SingBox) == 0 || len(artifacts.Subscription.Mihomo) == 0 {
		t.Fatalf("incomplete artifacts: %+v", artifacts)
	}
	var config struct {
		Inbounds []any `json:"inbounds"`
	}
	if err := json.Unmarshal(artifacts.Server.SingBox, &config); err != nil {
		t.Fatal(err)
	}
	if len(config.Inbounds) != 1 {
		t.Fatalf("inbounds = %v", config.Inbounds)
	}
}

func appState() model.State {
	return model.State{Schema: model.CurrentSchema, PublicAddress: "node.example.com", Protocols: model.Protocols{VLESSReality: &model.VLESSReality{Enabled: true, Port: 443, Engine: model.RealityEngineSingBox, UUID: "7f5fa27f-ec78-4c2d-a5e9-b7375a2968d6", SNI: "www.example.com", PrivateKey: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", PublicKey: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", ShortID: "a1b2"}}}
}

func TestLoadStateAcceptsEmptySchemaOneState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"schema":1,"protocols":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadState(path); err != nil {
		t.Fatal(err)
	}
}

func TestLoadStateMigratesLegacySharedAddress(t *testing.T) {
	directory := t.TempDir()
	contents, err := json.Marshal(appState())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "state.json")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	state, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if state.PublicAddress != "" || state.Protocols.VLESSReality.PublicAddress != "node.example.com" {
		t.Fatalf("state = %+v", state)
	}
}

func TestLoadStateRejectsLegacyCertificateDirectory(t *testing.T) {
	directory := t.TempDir()
	state := model.NewState()
	state.Certificates["demo"] = model.Certificate{Name: "demo", Cert: filepath.Join(directory, "certificates", "demo", "fullchain.pem"), Key: filepath.Join(directory, "certificates", "demo", "privkey.pem"), Mode: model.CertificateModePinned}
	contents, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "state.json")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadState(path); err == nil {
		t.Fatal("LoadState accepted a legacy certificate directory")
	}
}
