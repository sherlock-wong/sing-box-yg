package model

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
)

var (
	uuidPattern       = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)
	realityKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
	shortIDPattern    = regexp.MustCompile(`^(?:[0-9a-fA-F]{2}){1,8}$`)
	sha256Pattern     = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
)

// ValidationError points at an invalid user-controlled state field without
// printing any secret held by that field.
type ValidationError struct {
	Field  string
	Reason string
}

func (err *ValidationError) Error() string { return err.Field + ": " + err.Reason }

func invalid(field, reason string) error { return &ValidationError{Field: field, Reason: reason} }

func (state State) Validate() error {
	if state.Schema != CurrentSchema {
		return invalid("schema", fmt.Sprintf("unsupported schema %d (expected %d)", state.Schema, CurrentSchema))
	}
	if state.PublicAddress != "" && !validHost(state.PublicAddress) {
		return invalid("public_address", "must be a hostname or IP address without a port")
	}
	for id, certificate := range state.Certificates {
		if id == "" || certificate.Name == "" || certificate.Cert == "" || certificate.Key == "" {
			return invalid("certificates", "contains an incomplete certificate")
		}
		if (certificate.SourceCert == "") != (certificate.SourceKey == "") {
			return invalid("certificates."+id+".source", "certificate and key source must be set together")
		}
		if certificate.Mode != "" && certificate.Mode != CertificateModePinned && certificate.Mode != CertificateModeTrusted {
			return invalid("certificates."+id+".mode", "must be pinned or trusted")
		}
		if certificate.DER_SHA256 != "" && !sha256Pattern.MatchString(certificate.DER_SHA256) {
			return invalid("certificates."+id+".der_sha256", "must be a SHA-256 hex digest")
		}
		if certificate.SPKI_SHA256 != "" && !sha256Pattern.MatchString(certificate.SPKI_SHA256) {
			return invalid("certificates."+id+".spki_sha256", "must be a SHA-256 hex digest")
		}
	}
	ports := make(map[uint16]string)
	if protocol := state.Protocols.VLESSReality; protocol != nil {
		if err := ValidateVLESSReality(protocol); err != nil {
			return err
		}
		if err := reservePort(ports, protocol.Port, "protocols.vless_reality.port"); err != nil {
			return err
		}
	}
	if protocol := state.Protocols.Hysteria2; protocol != nil {
		if err := ValidateHysteria2(protocol, state.Certificates); err != nil {
			return err
		}
		if err := reservePort(ports, protocol.Port, "protocols.hysteria2.port"); err != nil {
			return err
		}
	}
	if protocol := state.Protocols.AnyTLS; protocol != nil {
		if err := ValidateAnyTLS(protocol, state.Certificates); err != nil {
			return err
		}
		if err := reservePort(ports, protocol.Port, "protocols.anytls.port"); err != nil {
			return err
		}
	}
	return nil
}

func ValidateVLESSReality(protocol *VLESSReality) error {
	if protocol == nil {
		return nil
	}
	return protocol.validate()
}

func ValidateHysteria2(protocol *Hysteria2, certificates map[string]Certificate) error {
	if protocol == nil {
		return nil
	}
	return protocol.validate(certificates)
}

func ValidateAnyTLS(protocol *AnyTLS, certificates map[string]Certificate) error {
	if protocol == nil {
		return nil
	}
	return protocol.validate(certificates)
}

func reservePort(ports map[uint16]string, port uint16, field string) error {
	if port == 0 {
		return invalid(field, "must be between 1 and 65535")
	}
	if previous, exists := ports[port]; exists {
		return invalid(field, "duplicates "+previous)
	}
	ports[port] = field
	return nil
}

func (protocol VLESSReality) validate() error {
	const prefix = "protocols.vless_reality."
	if protocol.Engine != RealityEngineSingBox && protocol.Engine != RealityEngineXray {
		return invalid(prefix+"engine", "must be sing-box or xray")
	}
	if !uuidPattern.MatchString(protocol.UUID) {
		return invalid(prefix+"uuid", "must be an RFC 4122 UUID")
	}
	if !validHostname(protocol.SNI) {
		return invalid(prefix+"sni", "must be a valid hostname")
	}
	if !realityKeyPattern.MatchString(protocol.PrivateKey) {
		return invalid(prefix+"private_key", "must be a Reality key")
	}
	if !realityKeyPattern.MatchString(protocol.PublicKey) {
		return invalid(prefix+"public_key", "must be a Reality key")
	}
	if !shortIDPattern.MatchString(protocol.ShortID) {
		return invalid(prefix+"short_id", "must be 1 to 8 hexadecimal bytes")
	}
	if protocol.Xray.MaxTimeDiff < 0 {
		return invalid(prefix+"xray.max_time_diff", "must not be negative")
	}
	if profile := protocol.Xray.FallbackProfile; profile != "" && profile != "off" && profile != "balanced" && profile != "strict" {
		return invalid(prefix+"xray.fallback_profile", "must be off, balanced, or strict")
	}
	if protocol.Engine == RealityEngineXray {
		if protocol.Xray.Target == "" {
			return invalid(prefix+"xray.target", "is required for xray")
		}
		host, port, err := net.SplitHostPort(protocol.Xray.Target)
		if err != nil || !validHostname(host) || port != "443" {
			return invalid(prefix+"xray.target", "must be a hostname on port 443")
		}
		if len(protocol.Xray.ServerNames) == 0 {
			return invalid(prefix+"xray.server_names", "must contain at least one hostname")
		}
		for _, name := range protocol.Xray.ServerNames {
			if !validHostname(name) {
				return invalid(prefix+"xray.server_names", "contains an invalid hostname")
			}
		}
	}
	return nil
}

func (protocol Hysteria2) validate(certificates map[string]Certificate) error {
	const prefix = "protocols.hysteria2."
	if protocol.Password == "" {
		return invalid(prefix+"password", "must not be empty")
	}
	if !validHostname(protocol.Domain) {
		return invalid(prefix+"domain", "must be a valid hostname")
	}
	if err := validateCertificateRef(protocol.CertificateID, certificates, prefix); err != nil {
		return err
	}
	if protocol.UpMbps < 1 || protocol.DownMbps < 1 {
		return invalid(prefix+"bandwidth", "must be positive")
	}
	return validateUDPHop(protocol.UDPHop, prefix+"udp_hop")
}

func (protocol AnyTLS) validate(certificates map[string]Certificate) error {
	const prefix = "protocols.anytls."
	if protocol.Password == "" {
		return invalid(prefix+"password", "must not be empty")
	}
	if !validHostname(protocol.Domain) {
		return invalid(prefix+"domain", "must be a valid hostname")
	}
	if err := validateCertificateRef(protocol.CertificateID, certificates, prefix); err != nil {
		return err
	}
	if protocol.Padding.Mode != PaddingDefault && protocol.Padding.Mode != PaddingCustom {
		return invalid(prefix+"padding.mode", "must be default or custom")
	}
	if protocol.Padding.Mode == PaddingCustom && len(protocol.Padding.Lines) == 0 {
		return invalid(prefix+"padding.lines", "must not be empty for custom padding")
	}
	return nil
}

func validateCertificateRef(id string, certificates map[string]Certificate, prefix string) error {
	if id == "" {
		return invalid(prefix+"certificate_id", "must not be empty")
	}
	if _, found := certificates[id]; !found {
		return invalid(prefix+"certificate_id", "does not exist")
	}
	return nil
}

func validateUDPHop(value, field string) error {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return invalid(field, "must be start:end")
	}
	first, firstErr := strconv.ParseUint(parts[0], 10, 16)
	last, lastErr := strconv.ParseUint(parts[1], 10, 16)
	if firstErr != nil || lastErr != nil || first == 0 || last == 0 || first > last {
		return invalid(field, "must be an ordered port range")
	}
	return nil
}

func validHost(value string) bool { return net.ParseIP(value) != nil || validHostname(value) }

func validHostname(host string) bool {
	if len(host) == 0 || len(host) > 253 || strings.ContainsAny(host, ":/ \\t") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, char := range label {
			if !(char == '-' || char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9') {
				return false
			}
		}
	}
	return true
}
