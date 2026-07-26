package model

import (
	"errors"
	"testing"
)

func validState() State {
	return State{
		Schema:       CurrentSchema,
		Certificates: map[string]Certificate{"default": {Name: "test", Cert: "/cert.pem", Key: "/key.pem"}},
		Protocols: Protocols{
			VLESSReality: &VLESSReality{Enabled: true, Port: 443, Engine: RealityEngineSingBox, UUID: "7f5fa27f-ec78-4c2d-a5e9-b7375a2968d6", SNI: "www.example.com", PrivateKey: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", PublicKey: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", ShortID: "a1b2"},
			Hysteria2:    &Hysteria2{Enabled: false, Port: 8443, Password: "password", Domain: "node.example.com", CertificateID: "default", UpMbps: 100, DownMbps: 100, UDPHop: "20000:20100"},
			AnyTLS:       &AnyTLS{Enabled: true, Port: 9443, Password: "password", Domain: "node.example.com", CertificateID: "default", Padding: Padding{Mode: PaddingDefault}},
		},
	}
}

func TestStateValidateAcceptsTypedState(t *testing.T) {
	if err := validState().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestStateValidateRejectsDuplicatePorts(t *testing.T) {
	state := validState()
	state.Protocols.AnyTLS.Port = state.Protocols.Hysteria2.Port
	var validation *ValidationError
	if !errors.As(state.Validate(), &validation) || validation.Field != "protocols.anytls.port" {
		t.Fatalf("unexpected error: %v", state.Validate())
	}
}

func TestStateValidateRejectsUnknownSchema(t *testing.T) {
	state := validState()
	state.Schema = 4
	if err := state.Validate(); err == nil {
		t.Fatal("Validate accepted an old shell schema")
	}
}

func TestStateValidateRequiresXraySettings(t *testing.T) {
	state := validState()
	state.Protocols.VLESSReality.Engine = RealityEngineXray
	if err := state.Validate(); err == nil {
		t.Fatal("Validate accepted xray without target")
	}
}

func TestStateValidateRejectsUnknownXrayFallbackProfile(t *testing.T) {
	state := validState()
	state.Protocols.VLESSReality.Xray.FallbackProfile = "unlimited"
	if err := state.Validate(); err == nil {
		t.Fatal("Validate accepted an unknown fallback profile")
	}
}

func TestSnapshotDoesNotExposeStateReferences(t *testing.T) {
	state := validState()
	view := NewSnapshot(state)
	copy := view.Snapshot()
	copy.Certificates["default"] = Certificate{Name: "changed"}
	copy.Protocols.AnyTLS.Padding.Lines = append(copy.Protocols.AnyTLS.Padding.Lines, "changed")
	if state.Certificates["default"].Name != "test" {
		t.Fatal("snapshot changed certificate map")
	}
	if len(state.Protocols.AnyTLS.Padding.Lines) != 0 {
		t.Fatal("snapshot changed padding lines")
	}
}
