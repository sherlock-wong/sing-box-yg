// Package app contains non-interactive application use cases.
package app

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

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
	return state, nil
}
