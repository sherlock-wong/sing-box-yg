package app

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/sherlock-wong/vps-net-manager/internal/certificate"
	"github.com/sherlock-wong/vps-net-manager/internal/model"
)

// CertificateSyncCandidate validates every source pair before producing any
// artifact. The returned files are committed by Apply in the same rollback
// scope as state, generated configuration, firewall, and service activation.
func CertificateSyncCandidate(state model.State, now time.Time) (model.State, []Artifact, bool, error) {
	candidate := model.NewSnapshot(state).Snapshot()
	ids := make([]string, 0, len(candidate.Certificates))
	for id := range candidate.Certificates {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	artifacts := make([]Artifact, 0, len(ids)*2)
	changed := false
	for _, id := range ids {
		item := candidate.Certificates[id]
		if item.SourceCert == "" {
			continue
		}
		certificatePEM, err := os.ReadFile(item.SourceCert)
		if err != nil {
			return model.State{}, nil, false, fmt.Errorf("read certificate source %s: %w", id, err)
		}
		keyPEM, err := os.ReadFile(item.SourceKey)
		if err != nil {
			return model.State{}, nil, false, fmt.Errorf("read certificate key source %s: %w", id, err)
		}
		info, err := certificate.Inspect(certificatePEM, keyPEM, "", now)
		if err != nil {
			return model.State{}, nil, false, fmt.Errorf("validate certificate source %s: %w", id, err)
		}
		certificateChanged, err := contentsDiffer(item.Cert, certificatePEM)
		if err != nil {
			return model.State{}, nil, false, fmt.Errorf("read managed certificate %s: %w", id, err)
		}
		keyChanged, err := contentsDiffer(item.Key, keyPEM)
		if err != nil {
			return model.State{}, nil, false, fmt.Errorf("read managed certificate key %s: %w", id, err)
		}
		if certificateChanged {
			artifacts = append(artifacts, Artifact{Path: item.Cert, Contents: certificatePEM, Mode: 0o644})
		}
		if keyChanged {
			artifacts = append(artifacts, Artifact{Path: item.Key, Contents: keyPEM, Mode: 0o600})
		}
		if certificateChanged || keyChanged || item.DER_SHA256 != info.DER_SHA256 || item.SPKI_SHA256 != info.SPKI_SHA256 || item.Insecure != (item.Mode == model.CertificateModePinned) {
			changed = true
		}
		item.DER_SHA256, item.SPKI_SHA256 = info.DER_SHA256, info.SPKI_SHA256
		item.Insecure = item.Mode == model.CertificateModePinned
		candidate.Certificates[id] = item
	}
	if !changed {
		return state, nil, false, nil
	}
	if err := candidate.Validate(); err != nil {
		return model.State{}, nil, false, err
	}
	if _, err := RenderState(candidate); err != nil {
		return model.State{}, nil, false, fmt.Errorf("render candidate certificate configuration: %w", err)
	}
	return candidate, artifacts, true, nil
}

func contentsDiffer(path string, wanted []byte) (bool, error) {
	existing, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return !bytes.Equal(existing, wanted), nil
}

func SyncCertificates(ctx context.Context, state model.State, now time.Time, options ApplyOptions) (bool, error) {
	candidate, artifacts, changed, err := CertificateSyncCandidate(state, now)
	if err != nil || !changed {
		return false, err
	}
	options.Previous = &state
	options.ExtraArtifacts = artifacts
	if _, err := options.Apply(ctx, candidate); err != nil {
		return false, err
	}
	return true, nil
}
