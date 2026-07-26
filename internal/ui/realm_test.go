package ui

import (
	"bufio"
	"context"
	"strings"
	"testing"

	"github.com/sherlock-wong/vps-net-manager/internal/realm"
)

func TestEditRealmAddsThenDeletesCandidateRule(t *testing.T) {
	input := bufio.NewScanner(strings.NewReader("1\n0.0.0.0\n40123\n203.0.113.10\n443\n0\n"))
	var output strings.Builder
	candidate, changed, err := EditRealm(context.Background(), input, &output, realm.NewState())
	if err != nil {
		t.Fatal(err)
	}
	if !changed || len(candidate.Rules) != 1 {
		t.Fatalf("changed=%v rules=%+v", changed, candidate.Rules)
	}
}
