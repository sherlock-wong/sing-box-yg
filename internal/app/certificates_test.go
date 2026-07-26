package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

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
	info, err := os.Stat(filepath.Join(directory, "certificates", "default", "key.pem"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("key mode = %v, err = %v", info.Mode(), err)
	}
}
