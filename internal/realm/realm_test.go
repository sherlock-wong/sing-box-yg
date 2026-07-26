package realm

import (
	"strings"
	"testing"
)

func TestValidateAndRenderDualStackRules(t *testing.T) {
	state := State{Schema: Schema, Rules: []Rule{
		{ID: "realm_a1b2c3d4", ListenHost: "0.0.0.0", ListenPort: 40123, RemoteHost: "203.0.113.10", RemotePort: 443},
		{ID: "realm_e5f60708", ListenHost: "2001:db8::1", ListenPort: 40124, RemoteHost: "2001:db8::2", RemotePort: 8443},
	}}
	config, err := Config(state)
	if err != nil {
		t.Fatal(err)
	}
	text := string(config)
	for _, expected := range []string{`listen = "0.0.0.0:40123"`, `remote = "[2001:db8::2]:8443"`, "use_udp = true"} {
		if !strings.Contains(text, expected) {
			t.Errorf("config lacks %q:\n%s", expected, text)
		}
	}
	if got := len(Rules(state)); got != 4 {
		t.Fatalf("rule count = %d, want 4", got)
	}
}

func TestRejectDuplicateListenerAndInvalidID(t *testing.T) {
	state := State{Schema: Schema, Rules: []Rule{
		{ID: "realm_a1b2c3d4", ListenHost: "0.0.0.0", ListenPort: 443, RemoteHost: "example.com", RemotePort: 443},
		{ID: "realm_bad0000", ListenHost: "0.0.0.0", ListenPort: 443, RemoteHost: "example.com", RemotePort: 443},
	}}
	if err := state.Validate(); err == nil {
		t.Fatal("Validate accepted duplicate listener")
	}
}

func TestParseRejectsUnknownFields(t *testing.T) {
	_, err := Parse([]byte(`{"schema":1,"rules":[],"unexpected":true}`))
	if err == nil {
		t.Fatal("Parse accepted unknown field")
	}
}
