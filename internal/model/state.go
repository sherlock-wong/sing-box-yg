// Package model contains the persisted, schema-versioned manager state.
package model

// CurrentSchema is deliberately a single supported schema. Older shell state
// must be uninstalled instead of being silently translated.
const CurrentSchema = 1

// NewState creates a valid zero-protocol state. It is used during bootstrap;
// the interactive installer later adds only explicitly selected protocols.
func NewState() State {
	return State{Schema: CurrentSchema, Certificates: make(map[string]Certificate)}
}

// State is the complete typed state stored at /etc/vps-net-manager/state.json.
// A nil protocol pointer means that protocol has not been added.
type State struct {
	Schema int `json:"schema"`
	// PublicAddress is retained only to read states written before share
	// addresses became protocol-specific. New state never writes it.
	PublicAddress string                 `json:"public_address,omitempty"`
	Protocols     Protocols              `json:"protocols"`
	Certificates  map[string]Certificate `json:"certificates,omitempty"`
}

type Protocols struct {
	VLESSReality *VLESSReality `json:"vless_reality,omitempty"`
	Hysteria2    *Hysteria2    `json:"hysteria2,omitempty"`
	AnyTLS       *AnyTLS       `json:"anytls,omitempty"`
}

// VLESSReality holds both Sing-box and Xray Reality options. Engine selects
// the only server core allowed to own this inbound.
type VLESSReality struct {
	Enabled       bool          `json:"enabled"`
	Name          string        `json:"name,omitempty"`
	PublicAddress string        `json:"public_address,omitempty"`
	Port          uint16        `json:"port"`
	Engine        RealityEngine `json:"engine"`
	UUID          string        `json:"uuid"`
	SNI           string        `json:"sni"`
	PrivateKey    string        `json:"private_key"`
	PublicKey     string        `json:"public_key"`
	ShortID       string        `json:"short_id"`
	Xray          XrayReality   `json:"xray,omitempty"`
}

type RealityEngine string

const (
	RealityEngineSingBox RealityEngine = "sing-box"
	RealityEngineXray    RealityEngine = "xray"
)

type XrayReality struct {
	Target          string   `json:"target,omitempty"`
	ServerNames     []string `json:"server_names,omitempty"`
	Fingerprint     string   `json:"fingerprint,omitempty"`
	SpiderX         string   `json:"spider_x,omitempty"`
	MaxTimeDiff     int      `json:"max_time_diff,omitempty"`
	MinClientVer    string   `json:"min_client_ver,omitempty"`
	MaxClientVer    string   `json:"max_client_ver,omitempty"`
	MLDSA65Seed     string   `json:"mldsa65_seed,omitempty"`
	MLDSA65Verify   string   `json:"mldsa65_verify,omitempty"`
	FallbackProfile string   `json:"fallback_profile,omitempty"`
}

type Hysteria2 struct {
	Enabled       bool   `json:"enabled"`
	Name          string `json:"name,omitempty"`
	PublicAddress string `json:"public_address,omitempty"`
	Port          uint16 `json:"port"`
	Password      string `json:"password"`
	Domain        string `json:"domain"`
	CertificateID string `json:"certificate_id"`
	UpMbps        int    `json:"up_mbps"`
	DownMbps      int    `json:"down_mbps"`
	UDPHop        string `json:"udp_hop,omitempty"`
}

type AnyTLS struct {
	Enabled       bool    `json:"enabled"`
	Name          string  `json:"name,omitempty"`
	PublicAddress string  `json:"public_address,omitempty"`
	Port          uint16  `json:"port"`
	Password      string  `json:"password"`
	Domain        string  `json:"domain"`
	CertificateID string  `json:"certificate_id"`
	Padding       Padding `json:"padding"`
}

type Padding struct {
	Mode  PaddingMode `json:"mode"`
	Lines []string    `json:"lines,omitempty"`
}

type PaddingMode string

const (
	PaddingDefault PaddingMode = "default"
	PaddingCustom  PaddingMode = "custom"
)

// Certificate is intentionally metadata-only for now. Certificate file
// validation and source tracking are added by the certificate package.
type Certificate struct {
	Name        string          `json:"name"`
	Cert        string          `json:"cert"`
	Key         string          `json:"key"`
	SourceCert  string          `json:"source_cert,omitempty"`
	SourceKey   string          `json:"source_key,omitempty"`
	Insecure    bool            `json:"insecure,omitempty"`
	Mode        CertificateMode `json:"mode,omitempty"`
	DER_SHA256  string          `json:"der_sha256,omitempty"`
	SPKI_SHA256 string          `json:"spki_sha256,omitempty"`
}

type CertificateMode string

const (
	CertificateModePinned  CertificateMode = "pinned"
	CertificateModeTrusted CertificateMode = "trusted"
)

// StateView is the read-only boundary passed to protocol implementations.
// Snapshot returns a value copy, preventing a protocol from changing caller
// state while rendering or validating it.
type StateView interface {
	Snapshot() State
}

type Snapshot State

func NewSnapshot(state State) Snapshot { return Snapshot(cloneState(state)) }

func (state Snapshot) Snapshot() State { return cloneState(State(state)) }

func cloneState(state State) State {
	copy := state
	if state.Certificates != nil {
		copy.Certificates = make(map[string]Certificate, len(state.Certificates))
		for id, certificate := range state.Certificates {
			copy.Certificates[id] = certificate
		}
	}
	if state.Protocols.VLESSReality != nil {
		value := *state.Protocols.VLESSReality
		value.Xray.ServerNames = append([]string(nil), value.Xray.ServerNames...)
		copy.Protocols.VLESSReality = &value
	}
	if state.Protocols.Hysteria2 != nil {
		value := *state.Protocols.Hysteria2
		copy.Protocols.Hysteria2 = &value
	}
	if state.Protocols.AnyTLS != nil {
		value := *state.Protocols.AnyTLS
		value.Padding.Lines = append([]string(nil), value.Padding.Lines...)
		copy.Protocols.AnyTLS = &value
	}
	return copy
}

// MigrateLegacyPublicAddress moves the old shared address into every
// configured protocol that has not chosen its own address yet. It is safe to
// call repeatedly and clears the legacy field so the next persisted state is
// fully protocol-specific.
func (state *State) MigrateLegacyPublicAddress() bool {
	if state.PublicAddress == "" {
		return false
	}
	legacy := state.PublicAddress
	if item := state.Protocols.VLESSReality; item != nil && item.PublicAddress == "" {
		item.PublicAddress = legacy
	}
	if item := state.Protocols.Hysteria2; item != nil && item.PublicAddress == "" {
		item.PublicAddress = legacy
	}
	if item := state.Protocols.AnyTLS; item != nil && item.PublicAddress == "" {
		item.PublicAddress = legacy
	}
	state.PublicAddress = ""
	return true
}
