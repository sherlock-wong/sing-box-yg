package app

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"time"

	"github.com/sherlock-wong/vps-net-manager/internal/certificate"
	"github.com/sherlock-wong/vps-net-manager/internal/model"
)

var certificateIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// AddPinnedCertificate stages a project-owned fixed certificate and returns a
// candidate state. The caller applies that state through the normal
// transaction, so no protocol is rebound implicitly.
func AddPinnedCertificate(ctx context.Context, stateDirectory string, state model.State, id, name, hostname string, now time.Time) (model.State, error) {
	if !certificateIDPattern.MatchString(id) {
		return model.State{}, fmt.Errorf("invalid certificate ID")
	}
	if name == "" {
		return model.State{}, fmt.Errorf("certificate name is required")
	}
	if _, exists := state.Certificates[id]; exists {
		return model.State{}, fmt.Errorf("certificate ID already exists")
	}
	certificatePEM, keyPEM, info, err := certificate.CreatePinned(hostname, now)
	if err != nil {
		return model.State{}, err
	}
	directory := filepath.Join(stateDirectory, "certificates", id)
	certPath, keyPath := filepath.Join(directory, "cert.pem"), filepath.Join(directory, "key.pem")
	if _, err := (FilesystemStore{}).Commit(ctx, []Artifact{{Path: certPath, Contents: certificatePEM, Mode: 0o644}, {Path: keyPath, Contents: keyPEM, Mode: 0o600}}); err != nil {
		return model.State{}, fmt.Errorf("store pinned certificate: %w", err)
	}
	candidate := model.NewSnapshot(state).Snapshot()
	if candidate.Certificates == nil {
		candidate.Certificates = make(map[string]model.Certificate)
	}
	candidate.Certificates[id] = model.Certificate{Name: name, Cert: certPath, Key: keyPath, Insecure: true, Mode: model.CertificateModePinned, DER_SHA256: info.DER_SHA256, SPKI_SHA256: info.SPKI_SHA256}
	if err := candidate.Validate(); err != nil {
		return model.State{}, err
	}
	return candidate, nil
}
