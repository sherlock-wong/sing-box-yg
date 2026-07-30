package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/sherlock-wong/vps-net-manager/internal/certificate"
	"github.com/sherlock-wong/vps-net-manager/internal/model"
)

var certificateIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// StagePinnedCertificate creates a project-owned fixed certificate without
// changing the host. Its artifacts are supplied to Apply so the certificate,
// state and service configuration share one rollback boundary.
func StagePinnedCertificate(ctx context.Context, stateDirectory string, state model.State, id, name, hostname string, now time.Time) (model.State, []Artifact, error) {
	if err := ctx.Err(); err != nil {
		return model.State{}, nil, err
	}
	if !certificateIDPattern.MatchString(id) {
		return model.State{}, nil, fmt.Errorf("invalid certificate ID")
	}
	if name == "" {
		return model.State{}, nil, fmt.Errorf("certificate name is required")
	}
	if _, exists := state.Certificates[id]; exists {
		return model.State{}, nil, fmt.Errorf("certificate ID already exists")
	}
	certificatePEM, keyPEM, info, err := certificate.CreatePinned(hostname, now)
	if err != nil {
		return model.State{}, nil, err
	}
	directory := filepath.Join(stateDirectory, "certificates", id)
	certPath, keyPath := filepath.Join(directory, "cert.pem"), filepath.Join(directory, "key.pem")
	candidate := model.NewSnapshot(state).Snapshot()
	if candidate.Certificates == nil {
		candidate.Certificates = make(map[string]model.Certificate)
	}
	candidate.Certificates[id] = model.Certificate{Name: name, Cert: certPath, Key: keyPath, Insecure: true, Mode: model.CertificateModePinned, DER_SHA256: info.DER_SHA256, SPKI_SHA256: info.SPKI_SHA256}
	if err := candidate.Validate(); err != nil {
		return model.State{}, nil, err
	}
	return candidate, []Artifact{{Path: certPath, Contents: certificatePEM, Mode: 0o644}, {Path: keyPath, Contents: keyPEM, Mode: 0o600}}, nil
}

// StageImportedCertificate validates an existing PEM pair and stages a
// private manager-owned copy. When followSource is true, later cert sync
// checks the original paths before replacing the managed copy.
func StageImportedCertificate(ctx context.Context, stateDirectory string, state model.State, id, name, sourceCert, sourceKey string, mode model.CertificateMode, followSource bool, now time.Time) (model.State, []Artifact, error) {
	if err := ctx.Err(); err != nil {
		return model.State{}, nil, err
	}
	if !certificateIDPattern.MatchString(id) {
		return model.State{}, nil, fmt.Errorf("invalid certificate ID")
	}
	if name == "" {
		return model.State{}, nil, fmt.Errorf("certificate name is required")
	}
	if mode != model.CertificateModePinned && mode != model.CertificateModeTrusted {
		return model.State{}, nil, fmt.Errorf("invalid certificate mode")
	}
	if _, exists := state.Certificates[id]; exists {
		return model.State{}, nil, fmt.Errorf("certificate ID already exists")
	}
	certificatePEM, err := os.ReadFile(sourceCert)
	if err != nil {
		return model.State{}, nil, fmt.Errorf("read certificate source: %w", err)
	}
	keyPEM, err := os.ReadFile(sourceKey)
	if err != nil {
		return model.State{}, nil, fmt.Errorf("read certificate key source: %w", err)
	}
	info, err := certificate.Inspect(certificatePEM, keyPEM, "", now)
	if err != nil {
		return model.State{}, nil, fmt.Errorf("validate imported certificate: %w", err)
	}
	directory := filepath.Join(stateDirectory, "certificates", id)
	certPath, keyPath := filepath.Join(directory, "cert.pem"), filepath.Join(directory, "key.pem")
	candidate := model.NewSnapshot(state).Snapshot()
	if candidate.Certificates == nil {
		candidate.Certificates = make(map[string]model.Certificate)
	}
	item := model.Certificate{Name: name, Cert: certPath, Key: keyPath, Insecure: mode == model.CertificateModePinned, Mode: mode, DER_SHA256: info.DER_SHA256, SPKI_SHA256: info.SPKI_SHA256}
	if followSource {
		item.SourceCert, item.SourceKey = sourceCert, sourceKey
	}
	candidate.Certificates[id] = item
	if err := candidate.Validate(); err != nil {
		return model.State{}, nil, err
	}
	return candidate, []Artifact{{Path: certPath, Contents: certificatePEM, Mode: 0o644}, {Path: keyPath, Contents: keyPEM, Mode: 0o600}}, nil
}

// AddPinnedCertificate stages a project-owned fixed certificate and returns a
// candidate state. The caller applies that state through the normal
// transaction, so no protocol is rebound implicitly.
func AddPinnedCertificate(ctx context.Context, stateDirectory string, state model.State, id, name, hostname string, now time.Time) (model.State, error) {
	candidate, artifacts, err := StagePinnedCertificate(ctx, stateDirectory, state, id, name, hostname, now)
	if err != nil {
		return model.State{}, err
	}
	if _, err := (FilesystemStore{}).Commit(ctx, artifacts); err != nil {
		return model.State{}, fmt.Errorf("store pinned certificate: %w", err)
	}
	return candidate, nil
}
