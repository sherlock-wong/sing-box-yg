package komari

import "testing"

func TestStateValidation(t *testing.T) {
	if err := (State{Schema: Schema, Enabled: true, Mode: ModeDomain, ListenHost: "127.0.0.1", ListenPort: 25774, Domain: "status.example.com", ProxyID: "web_a1b2c3d4"}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (State{Schema: Schema, Enabled: true, Mode: ModeDirect, ListenHost: "0.0.0.0", ListenPort: 25774}).Validate(); err != nil {
		t.Fatal(err)
	}
}
