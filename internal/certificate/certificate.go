// Package certificate validates and creates certificate material without
// invoking openssl or trusting external tool output.
package certificate

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"
)

type Info struct {
	DNSNames    []string
	NotBefore   time.Time
	NotAfter    time.Time
	DER_SHA256  string
	SPKI_SHA256 string
}

// Inspect checks certificate parsing, private/public-key matching, and
// validity time. hostname is optional; when supplied it must match a SAN.
func Inspect(certificatePEM, keyPEM []byte, hostname string, now time.Time) (Info, error) {
	certificate, err := parseCertificate(certificatePEM)
	if err != nil {
		return Info{}, err
	}
	privateKey, err := parsePrivateKey(keyPEM)
	if err != nil {
		return Info{}, err
	}
	if !publicKeysMatch(certificate.PublicKey, privateKey.Public()) {
		return Info{}, fmt.Errorf("certificate and private key do not match")
	}
	if now.Before(certificate.NotBefore) || !now.Before(certificate.NotAfter) {
		return Info{}, fmt.Errorf("certificate is not currently valid")
	}
	if hostname != "" {
		if err := certificate.VerifyHostname(hostname); err != nil {
			return Info{}, fmt.Errorf("certificate SAN does not cover %s: %w", hostname, err)
		}
	}
	derHash := sha256.Sum256(certificate.Raw)
	spki, err := x509.MarshalPKIXPublicKey(certificate.PublicKey)
	if err != nil {
		return Info{}, fmt.Errorf("marshal certificate public key: %w", err)
	}
	spkiHash := sha256.Sum256(spki)
	return Info{DNSNames: append([]string(nil), certificate.DNSNames...), NotBefore: certificate.NotBefore, NotAfter: certificate.NotAfter, DER_SHA256: hex.EncodeToString(derHash[:]), SPKI_SHA256: hex.EncodeToString(spkiHash[:])}, nil
}

// CreatePinned creates a self-signed ECDSA certificate for a fixed/pinned
// client configuration. The caller must protect the returned private key.
func CreatePinned(hostname string, now time.Time) ([]byte, []byte, Info, error) {
	if hostname == "" {
		return nil, nil, Info{}, fmt.Errorf("certificate hostname is required")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, Info{}, fmt.Errorf("generate certificate key: %w", err)
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return nil, nil, Info{}, fmt.Errorf("generate certificate serial: %w", err)
	}
	template := &x509.Certificate{SerialNumber: serial, Subject: pkixName(hostname), DNSNames: []string{hostname}, NotBefore: now.Add(-5 * time.Minute), NotAfter: now.AddDate(1, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, Info{}, fmt.Errorf("create pinned certificate: %w", err)
	}
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	encodedKey, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, Info{}, fmt.Errorf("marshal certificate key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: encodedKey})
	info, err := Inspect(certificatePEM, keyPEM, hostname, now)
	if err != nil {
		return nil, nil, Info{}, err
	}
	return certificatePEM, keyPEM, info, nil
}

// pkixName isolates the subject construction so certificate content stays
// intentionally minimal; SAN is the authoritative hostname source.
func pkixName(commonName string) pkix.Name { return pkix.Name{CommonName: commonName} }

func parseCertificate(contents []byte) (*x509.Certificate, error) {
	for len(contents) > 0 {
		block, rest := pem.Decode(contents)
		if block == nil {
			break
		}
		contents = rest
		if block.Type != "CERTIFICATE" {
			continue
		}
		certificate, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse certificate: %w", err)
		}
		return certificate, nil
	}
	return nil, fmt.Errorf("PEM contains no certificate")
}

func parsePrivateKey(contents []byte) (crypto.Signer, error) {
	block, _ := pem.Decode(contents)
	if block == nil {
		return nil, fmt.Errorf("PEM contains no private key")
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if signer, ok := key.(crypto.Signer); ok {
			return signer, nil
		}
	}
	if key, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	return nil, fmt.Errorf("unsupported private key")
}

func publicKeysMatch(left crypto.PublicKey, right crypto.PublicKey) bool {
	leftDER, leftErr := x509.MarshalPKIXPublicKey(left)
	rightDER, rightErr := x509.MarshalPKIXPublicKey(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftDER, rightDER)
}
