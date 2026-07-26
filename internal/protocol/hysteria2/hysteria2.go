package hysteria2

import (
	"net"
	"net/url"
	"strconv"

	"github.com/sherlock-wong/vps-net-manager/internal/model"
	"github.com/sherlock-wong/vps-net-manager/internal/protocol"
)

type Module struct{}

func (Module) Key() string { return "hysteria2" }
func (Module) Validate(view model.StateView) error {
	state := view.Snapshot()
	return model.ValidateHysteria2(state.Protocols.Hysteria2, state.Certificates)
}
func (Module) SingBoxInbound(view model.StateView) (any, bool, error) {
	state := view.Snapshot()
	configuration := state.Protocols.Hysteria2
	if configuration == nil || !configuration.Enabled {
		return nil, false, nil
	}
	certificate := state.Certificates[configuration.CertificateID]
	return inbound{Type: "hysteria2", Tag: "hysteria2", Listen: "::", ListenPort: configuration.Port, Users: []user{{Password: configuration.Password}}, IgnoreClientBandwidth: false, UpMbps: configuration.UpMbps, DownMbps: configuration.DownMbps, TLS: tls{Enabled: true, ALPN: []string{"h3"}, ServerName: configuration.Domain, CertificatePath: certificate.Cert, KeyPath: certificate.Key}}, true, nil
}
func (Module) XrayInbound(model.StateView) (any, bool, error) { return nil, false, nil }
func (Module) ClientOutbound(view model.StateView, server string) (any, error) {
	state := view.Snapshot()
	configuration := state.Protocols.Hysteria2
	if configuration == nil || !configuration.Enabled {
		return nil, nil
	}
	certificate := state.Certificates[configuration.CertificateID]
	return clientOutbound{Type: "hysteria2", Tag: displayName(configuration.Name, "hysteria2"), Server: server, ServerPort: configuration.Port, Password: configuration.Password, UpMbps: configuration.UpMbps, DownMbps: configuration.DownMbps, TLS: clientTLS{Enabled: true, ServerName: configuration.Domain, Insecure: certificate.Insecure}}, nil
}
func (Module) ShareLink(view model.StateView, server string) (string, error) {
	state := view.Snapshot()
	configuration := state.Protocols.Hysteria2
	if configuration == nil || !configuration.Enabled {
		return "", nil
	}
	certificate := state.Certificates[configuration.CertificateID]
	query := url.Values{"security": {"tls"}, "sni": {configuration.Domain}, "insecure": {strconv.Itoa(boolNumber(certificate.Insecure))}}
	if configuration.UDPHop != "" {
		query.Set("mport", configuration.UDPHop)
	}
	return (&url.URL{Scheme: "hysteria2", User: url.User(configuration.Password), Host: net.JoinHostPort(server, strconv.Itoa(int(configuration.Port))), RawQuery: query.Encode(), Fragment: displayName(configuration.Name, "hysteria2")}).String(), nil
}
func (Module) MihomoProxy(view model.StateView, server string) (protocol.MihomoProxy, bool, error) {
	state := view.Snapshot()
	configuration := state.Protocols.Hysteria2
	if configuration == nil || !configuration.Enabled {
		return protocol.MihomoProxy{}, false, nil
	}
	certificate := state.Certificates[configuration.CertificateID]
	name := displayName(configuration.Name, "hysteria2")
	return protocol.MihomoProxy{Name: name, Config: mihomoProxy{Name: name, Type: "hysteria2", Server: server, Port: configuration.Port, Password: configuration.Password, SNI: configuration.Domain, SkipCertVerify: certificate.Insecure, Up: strconv.Itoa(configuration.UpMbps) + " Mbps", Down: strconv.Itoa(configuration.DownMbps) + " Mbps"}}, true, nil
}
func (Module) FirewallRules(view model.StateView) []protocol.FirewallRule {
	configuration := view.Snapshot().Protocols.Hysteria2
	if configuration == nil || !configuration.Enabled {
		return nil
	}
	return []protocol.FirewallRule{{Network: "udp", Port: configuration.Port}}
}

type inbound struct {
	Type                  string `json:"type"`
	Tag                   string `json:"tag"`
	Listen                string `json:"listen"`
	ListenPort            uint16 `json:"listen_port"`
	Users                 []user `json:"users"`
	IgnoreClientBandwidth bool   `json:"ignore_client_bandwidth"`
	UpMbps                int    `json:"up_mbps"`
	DownMbps              int    `json:"down_mbps"`
	TLS                   tls    `json:"tls"`
}
type user struct {
	Password string `json:"password"`
}
type tls struct {
	Enabled         bool     `json:"enabled"`
	ALPN            []string `json:"alpn"`
	ServerName      string   `json:"server_name"`
	CertificatePath string   `json:"certificate_path"`
	KeyPath         string   `json:"key_path"`
}
type clientOutbound struct {
	Type       string    `json:"type"`
	Tag        string    `json:"tag"`
	Server     string    `json:"server"`
	ServerPort uint16    `json:"server_port"`
	Password   string    `json:"password"`
	UpMbps     int       `json:"up_mbps"`
	DownMbps   int       `json:"down_mbps"`
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
	Up             string `yaml:"up"`
	Down           string `yaml:"down"`
}

func displayName(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
func boolNumber(value bool) int {
	if value {
		return 1
	}
	return 0
}
