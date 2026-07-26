package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sherlock-wong/vps-net-manager/internal/certificate"
	"github.com/sherlock-wong/vps-net-manager/internal/model"
)

func TestCertificateSyncCandidateValidatesAndStagesSources(t *testing.T) {
	directory := t.TempDir()
	now := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	certPEM, keyPEM, _, err := certificate.CreatePinned("node.example.com", now)
	if err != nil {
		t.Fatal(err)
	}
	sourceCert, sourceKey := filepath.Join(directory, "source-cert.pem"), filepath.Join(directory, "source-key.pem")
	if err := os.WriteFile(sourceCert, certPEM, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourceKey, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	state := model.NewState()
	state.Certificates["default"] = model.Certificate{Name: "source", Cert: filepath.Join(directory, "managed-cert.pem"), Key: filepath.Join(directory, "managed-key.pem"), SourceCert: sourceCert, SourceKey: sourceKey, Mode: model.CertificateModePinned}
	candidate, artifacts, changed, err := CertificateSyncCandidate(state, now)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || len(artifacts) != 2 || candidate.Certificates["default"].DER_SHA256 == "" {
		t.Fatalf("changed=%v artifacts=%d certificate=%+v", changed, len(artifacts), candidate.Certificates["default"])
	}
}

func TestCertificateSyncCandidateDoesNotStageOnInvalidSource(t *testing.T) {
	state := model.NewState()
	state.Certificates["default"] = model.Certificate{Name: "source", Cert: "/managed/cert.pem", Key: "/managed/key.pem", SourceCert: "/missing/cert.pem", SourceKey: "/missing/key.pem"}
	if _, _, _, err := CertificateSyncCandidate(state, time.Now()); err == nil {
		t.Fatal("CertificateSyncCandidate accepted missing source")
	}
}

func TestSyncCertificatesUsesApplyTransaction(t *testing.T) {
	state := model.NewState()
	state.Certificates["default"] = model.Certificate{Name: "no source", Cert: "/managed/cert.pem", Key: "/managed/key.pem"}
	changed, err := SyncCertificates(context.Background(), state, time.Now(), ApplyOptions{})
	if err != nil || changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
}
