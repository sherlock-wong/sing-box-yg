package ui

import (
	"bufio"
	"context"
	"strings"
	"testing"

	"github.com/sherlock-wong/vps-net-manager/internal/model"
)

func TestInitialSetupAllowsZeroProtocolState(t *testing.T) {
	var output strings.Builder
	state, err := InitialSetup(context.Background(), strings.NewReader("0\n\n"), &output, t.TempDir(), model.NewState())
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	if state.Protocols.VLESSReality != nil || state.Protocols.Hysteria2 != nil || state.Protocols.AnyTLS != nil {
		t.Fatalf("state = %+v", state.Protocols)
	}
}

func TestParseSelectionRejectsDuplicatesAndUnknownValues(t *testing.T) {
	for _, value := range []string{"1,1", "4", "vless"} {
		if _, err := parseSelection(value); err == nil {
			t.Fatalf("parseSelection accepted %q", value)
		}
	}
}

func TestEditProtocolsTogglesExistingProtocolWithoutWritingState(t *testing.T) {
	configuration, err := newVLESS(443, "www.example.com")
	if err != nil {
		t.Fatal(err)
	}
	state := model.NewState()
	state.Protocols.VLESSReality = configuration
	var output strings.Builder
	candidate, changed, err := EditProtocols(context.Background(), bufio.NewScanner(strings.NewReader("1\n1\n0\n")), &output, t.TempDir(), state)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || candidate.Protocols.VLESSReality.Enabled {
		t.Fatalf("candidate = %+v, changed = %v", candidate.Protocols.VLESSReality, changed)
	}
	if !state.Protocols.VLESSReality.Enabled {
		t.Fatal("editor changed original state")
	}
}

func TestSelectRealitySNIUsesManualValue(t *testing.T) {
	var output strings.Builder
	value, err := selectRealitySNI(context.Background(), &prompt{scanner: bufio.NewScanner(strings.NewReader("1\nwww.cloudflare.com\n")), output: &output}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if value != "www.cloudflare.com" {
		t.Fatalf("SNI = %q", value)
	}
}

func TestNewRealityKeyMaterialMatchesModelValidation(t *testing.T) {
	privateKey, publicKey, shortID, err := newRealityKeyMaterial()
	if err != nil {
		t.Fatal(err)
	}
	state := model.NewState()
	state.Protocols.VLESSReality = &model.VLESSReality{Enabled: true, Port: 443, Engine: model.RealityEngineSingBox, UUID: "123e4567-e89b-42d3-a456-426614174000", SNI: "www.example.com", PrivateKey: privateKey, PublicKey: publicKey, ShortID: shortID}
	if err := state.Validate(); err != nil {
		t.Fatalf("generated material is invalid: %v", err)
	}
}
