package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sherlock-wong/vps-net-manager/internal/render"
)

func TestCommandCoreCheckerSkipsUnusedCores(t *testing.T) {
	if err := (CommandCoreChecker{}).CheckCores(context.Background(), render.Output{}); err != nil {
		t.Fatal(err)
	}
}

func TestCommandCoreCheckerRequiresConfiguredBinary(t *testing.T) {
	err := (CommandCoreChecker{}).CheckCores(context.Background(), render.Output{NeedsSingBox: true, SingBox: []byte(`{}`)})
	if err == nil || !strings.Contains(err.Error(), "no binary path") {
		t.Fatalf("error = %v", err)
	}
}
