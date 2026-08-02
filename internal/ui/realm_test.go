package ui

import (
	"bufio"
	"context"
	"strings"
	"testing"

	"github.com/sherlock-wong/vps-net-manager/internal/realm"
)

func TestEditRealmAddsCandidateRule(t *testing.T) {
	input := bufio.NewScanner(strings.NewReader("2\n0.0.0.0\n40123\n203.0.113.10\n443\n0\n"))
	var output strings.Builder
	candidate, changed, err := EditRealm(context.Background(), input, &output, realm.NewState())
	if err != nil {
		t.Fatal(err)
	}
	if !changed || len(candidate.Rules) != 1 {
		t.Fatalf("changed=%v rules=%+v", changed, candidate.Rules)
	}
}

func TestEditRealmDeletesRuleByNumber(t *testing.T) {
	state := realm.State{Schema: realm.Schema, Rules: []realm.Rule{
		{ID: "realm_a1b2c3d4", ListenHost: "0.0.0.0", ListenPort: 40123, RemoteHost: "203.0.113.10", RemotePort: 443},
		{ID: "realm_e5f60708", ListenHost: "0.0.0.0", ListenPort: 40124, RemoteHost: "203.0.113.11", RemotePort: 443},
	}}
	input := bufio.NewScanner(strings.NewReader("3\n2\n0\n"))
	var output strings.Builder
	candidate, changed, err := EditRealm(context.Background(), input, &output, state)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || len(candidate.Rules) != 1 || candidate.Rules[0].ID != "realm_a1b2c3d4" {
		t.Fatalf("changed=%v rules=%+v", changed, candidate.Rules)
	}
	if !strings.Contains(output.String(), "2. 0.0.0.0:40124 → 203.0.113.11:443") {
		t.Fatalf("delete menu did not list numbered rules: %s", output.String())
	}
}
