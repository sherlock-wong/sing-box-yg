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

func TestAddPinnedCertificateCreatesProtectedKeyAndCandidateState(t *testing.T) {
	directory := t.TempDir()
	candidate, err := AddPinnedCertificate(context.Background(), directory, model.NewState(), "default", "初始固定证书", "node.example.com", time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	certificate := candidate.Certificates["default"]
	if certificate.Mode != model.CertificateModePinned || certificate.DER_SHA256 == "" {
		t.Fatalf("certificate = %+v", certificate)
	}
	info, err := os.Stat(filepath.Join(directory, "certificates", "default", "privkey.pem"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("key mode = %v, err = %v", info.Mode(), err)
	}
}

func TestStageImportedCertificateKeepsSourceAndDefersWrites(t *testing.T) {
	directory := t.TempDir()
	certificatePEM, keyPEM, _, err := certificate.CreatePinned("node.example.com", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	sourceCert, sourceKey := filepath.Join(directory, "source-cert.pem"), filepath.Join(directory, "source-key.pem")
	if err := os.WriteFile(sourceCert, certificatePEM, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourceKey, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	candidate, artifacts, err := StageImportedCertificate(context.Background(), filepath.Join(directory, "state"), model.NewState(), "acme", "example ACME", sourceCert, sourceKey, model.CertificateModeTrusted, true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	item := candidate.Certificates["acme"]
	if item.SourceCert != sourceCert || item.SourceKey != sourceKey || item.Mode != model.CertificateModeTrusted {
		t.Fatalf("certificate = %+v", item)
	}
	if len(artifacts) != 2 {
		t.Fatalf("artifacts = %d", len(artifacts))
	}
	if _, err := os.Stat(item.Cert); !os.IsNotExist(err) {
		t.Fatalf("staged certificate was written before Apply: %v", err)
	}
}
