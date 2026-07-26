package anytls

import (
	"net"
	"net/url"
	"strconv"

	"github.com/sherlock-wong/vps-net-manager/internal/model"
	"github.com/sherlock-wong/vps-net-manager/internal/protocol"
)

type Module struct{}

var defaultPadding = []string{"stop=8", "0=30-30", "1=100-400", "2=400-500,c,500-1000,c,500-1000,c,500-1000,c,500-1000", "3=9-9,500-1000", "4=500-1000", "5=500-1000", "6=500-1000", "7=500-1000"}

func (Module) Key() string { return "anytls" }
func (Module) Validate(view model.StateView) error {
	state := view.Snapshot()
	return model.ValidateAnyTLS(state.Protocols.AnyTLS, state.Certificates)
}
func (Module) SingBoxInbound(view model.StateView) (any, bool, error) {
	state := view.Snapshot()
	configuration := state.Protocols.AnyTLS
	if configuration == nil || !configuration.Enabled {
		return nil, false, nil
	}
	certificate := state.Certificates[configuration.CertificateID]
	padding := configuration.Padding.Lines
	if configuration.Padding.Mode == model.PaddingDefault {
		padding = append([]string(nil), defaultPadding...)
	}
	return inbound{Type: "anytls", Tag: "anytls", Listen: "::", ListenPort: configuration.Port, Users: []user{{Password: configuration.Password}}, PaddingScheme: padding, TLS: tls{Enabled: true, ServerName: configuration.Domain, CertificatePath: certificate.Cert, KeyPath: certificate.Key}}, true, nil
}
func (Module) XrayInbound(model.StateView) (any, bool, error) { return nil, false, nil }
func (Module) ClientOutbound(view model.StateView, server string) (any, error) {
	state := view.Snapshot()
	configuration := state.Protocols.AnyTLS
	if configuration == nil || !configuration.Enabled {
		return nil, nil
	}
	certificate := state.Certificates[configuration.CertificateID]
	return clientOutbound{Type: "anytls", Tag: displayName(configuration.Name, "anytls"), Server: server, ServerPort: configuration.Port, Password: configuration.Password, TLS: clientTLS{Enabled: true, ServerName: configuration.Domain, Insecure: certificate.Insecure}}, nil
}
func (Module) ShareLink(view model.StateView, server string) (string, error) {
	state := view.Snapshot()
	configuration := state.Protocols.AnyTLS
	if configuration == nil || !configuration.Enabled {
		return "", nil
	}
	query := url.Values{"sni": {configuration.Domain}}
	certificate := state.Certificates[configuration.CertificateID]
	if certificate.Mode == model.CertificateModePinned && certificate.DER_SHA256 != "" {
		query.Set("pcs", certificate.DER_SHA256)
	}
	return (&url.URL{Scheme: "anytls", User: url.User(configuration.Password), Host: net.JoinHostPort(server, strconv.Itoa(int(configuration.Port))), RawQuery: query.Encode(), Fragment: displayName(configuration.Name, "anytls")}).String(), nil
}
func (Module) MihomoProxy(view model.StateView, server string) (protocol.MihomoProxy, bool, error) {
	state := view.Snapshot()
	configuration := state.Protocols.AnyTLS
	if configuration == nil || !configuration.Enabled {
		return protocol.MihomoProxy{}, false, nil
	}
	certificate := state.Certificates[configuration.CertificateID]
	name := displayName(configuration.Name, "anytls")
	return protocol.MihomoProxy{Name: name, Config: mihomoProxy{Name: name, Type: "anytls", Server: server, Port: configuration.Port, Password: configuration.Password, SNI: configuration.Domain, SkipCertVerify: certificate.Insecure, UDP: true}}, true, nil
}
func (Module) FirewallRules(view model.StateView) []protocol.FirewallRule {
	configuration := view.Snapshot().Protocols.AnyTLS
	if configuration == nil || !configuration.Enabled {
		return nil
	}
	return []protocol.FirewallRule{{Network: "tcp", Port: configuration.Port}}
}

type inbound struct {
	Type          string   `json:"type"`
	Tag           string   `json:"tag"`
	Listen        string   `json:"listen"`
	ListenPort    uint16   `json:"listen_port"`
	Users         []user   `json:"users"`
	PaddingScheme []string `json:"padding_scheme,omitempty"`
	TLS           tls      `json:"tls"`
}
type user struct {
	Password string `json:"password"`
}
type tls struct {
	Enabled         bool   `json:"enabled"`
	ServerName      string `json:"server_name"`
	CertificatePath string `json:"certificate_path"`
	KeyPath         string `json:"key_path"`
}
type clientOutbound struct {
	Type       string    `json:"type"`
	Tag        string    `json:"tag"`
	Server     string    `json:"server"`
	ServerPort uint16    `json:"server_port"`
	Password   string    `json:"password"`
	TLS        clientTLS `json:"tls"`
}
type clientTLS struct {
	Enabled    bool   `json:"enabled"`
	ServerName string `json:"server_name"`
	Insecure   bool   `json:"insecure"`
}
type mihomoProxy struct {
	Name           string `yaml:"name"`
	Type           string `yaml:"type"`
	Server         string `yaml:"server"`
	Port           uint16 `yaml:"port"`
	Password       string `yaml:"password"`
	SNI            string `yaml:"sni"`
	SkipCertVerify bool   `yaml:"skip-cert-verify"`
	UDP            bool   `yaml:"udp"`
}

func displayName(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
