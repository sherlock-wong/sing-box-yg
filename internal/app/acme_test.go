package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sherlock-wong/vps-net-manager/internal/certificate"
	"github.com/sherlock-wong/vps-net-manager/internal/dependency"
	"github.com/sherlock-wong/vps-net-manager/internal/platform"
)

func TestACMEAdapterValidatesMaterialAfterScript(t *testing.T) {
	now := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	certificatePEM, keyPEM, _, err := certificate.CreatePinned("node.example.com", now)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	certPath, keyPath := filepath.Join(directory, "cert.pem"), filepath.Join(directory, "key.pem")
	if err := os.WriteFile(certPath, certificatePEM, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	script := []byte("#!/usr/bin/env bash\nexit 0\n")
	digest := sha256.Sum256(script)
	locks := testLocksWithACME("https://downloads.example.test/acme.sh", hex.EncodeToString(digest[:]))
	adapter := ACMEAdapter{Locks: locks, Client: &http.Client{Transport: archiveTransport{"https://downloads.example.test/acme.sh": script}}, Runner: platform.RunnerFunc(func(context.Context, platform.Command) (platform.CommandResult, error) {
		return platform.CommandResult{}, nil
	})}
	if _, err := adapter.Run(context.Background(), nil, certPath, keyPath, "node.example.com", now); err != nil {
		t.Fatal(err)
	}
}

func testLocksWithACME(address, checksum string) dependency.Locks {
	asset := dependency.Asset{URL: "https://downloads.example.test/core", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Archive: "zip", Member: "core"}
	assets := map[string]dependency.Asset{"amd64": asset, "arm64": asset}
	return dependency.Locks{SingBox: dependency.Core{Version: "1", Assets: assets}, Xray: dependency.Core{Version: "1", Assets: assets}, Realm: dependency.Core{Version: "1", Assets: assets}, ACME: dependency.Script{Commit: "0123456789abcdef0123456789abcdef01234567", URL: address, SHA256: checksum}}
}
