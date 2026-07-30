// Package app contains non-interactive application use cases.
package app

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/sherlock-wong/vps-net-manager/internal/model"
)

const DefaultStatePath = "/etc/vps-net-manager/state.json"

// LoadState parses and validates state without changing the host. Unknown JSON
// fields are rejected so a typo cannot silently change a future deployment.
func LoadState(path string) (model.State, error) {
	file, err := os.Open(path)
	if err != nil {
		return model.State{}, fmt.Errorf("open state: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var state model.State
	if err := decoder.Decode(&state); err != nil {
		return model.State{}, fmt.Errorf("parse state: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return model.State{}, fmt.Errorf("parse state: multiple JSON values")
	}
	if err := state.Validate(); err != nil {
		return model.State{}, fmt.Errorf("validate state: %w", err)
	}
	if err := validateManagedCertificatePaths(state, filepath.Dir(path)); err != nil {
		return model.State{}, err
	}
	return state, nil
}

// validateManagedCertificatePaths intentionally accepts only the current
// project-owned layout. Certificate source paths can point outside the state
// directory, but the files consumed by services must be private copies under
// certs/<id>/; old layouts are rejected rather than silently supported.
func validateManagedCertificatePaths(state model.State, stateDirectory string) error {
	for id, item := range state.Certificates {
		expectedCert := filepath.Join(stateDirectory, "certs", id, "fullchain.pem")
		expectedKey := filepath.Join(stateDirectory, "certs", id, "privkey.pem")
		if filepath.Clean(item.Cert) != expectedCert || filepath.Clean(item.Key) != expectedKey {
			return fmt.Errorf("certificate %s uses an unsupported path; expected %s and %s", id, expectedCert, expectedKey)
		}
	}
	return nil
}
