// Package assets exposes build-time assets used by the VPS binary.
package assets

import _ "embed"

// RealityTargets is the built-in candidate list. A user-maintained state-dir
// copy overrides it; a missing override never requires network access.
//
//go:embed reality-targets.txt
var RealityTargets string

// DependencyLocks is the one machine-readable source for core download
// locations, archive members, and SHA-256 values embedded in vpnm.
//
//go:embed dependencies.json
var DependencyLocks []byte
