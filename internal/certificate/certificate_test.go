package certificate

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"
)

func TestCreatePinnedProducesValidMatchingCertificate(t *testing.T) {
	now := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	certificatePEM, keyPEM, info, err := CreatePinned("node.example.com", now)
	if err != nil {
		t.Fatal(err)
	}
	if info.DER_SHA256 == "" || info.SPKI_SHA256 == "" || len(info.DNSNames) != 1 {
		t.Fatalf("info = %+v", info)
	}
	if _, err := Inspect(certificatePEM, keyPEM, "node.example.com", now); err != nil {
		t.Fatal(err)
	}
	if _, err := Inspect(certificatePEM, keyPEM, "other.example.com", now); err == nil {
		t.Fatal("Inspect accepted an uncovered hostname")
	}
}

func TestInspectRejectsMismatchedKeyAndExpiredCertificate(t *testing.T) {
	now := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	certificatePEM, _, _, err := CreatePinned("node.example.com", now)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if _, err := Inspect(certificatePEM, keyPEM, "node.example.com", now); err == nil {
		t.Fatal("Inspect accepted a mismatched key")
	}
	validCertificate, validKey, _, err := CreatePinned("node.example.com", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Inspect(validCertificate, validKey, "node.example.com", now.AddDate(2, 0, 0)); err == nil {
		t.Fatal("Inspect accepted an expired certificate")
	}
}
