package vlessreality

import (
	"net"
	"net/url"
	"strconv"

	"github.com/sherlock-wong/vps-net-manager/internal/model"
	"github.com/sherlock-wong/vps-net-manager/internal/protocol"
)

type Module struct{}

func (Module) Key() string { return "vless-reality" }

func (Module) Validate(view model.StateView) error {
	return model.ValidateVLESSReality(view.Snapshot().Protocols.VLESSReality)
}

func (Module) SingBoxInbound(view model.StateView) (any, bool, error) {
	state := view.Snapshot()
	configuration := state.Protocols.VLESSReality
	if configuration == nil || !configuration.Enabled || configuration.Engine != model.RealityEngineSingBox {
		return nil, false, nil
	}
	return singBoxInbound{Type: "vless", Tag: "vless-reality", Listen: "::", ListenPort: configuration.Port, Users: []vlessUser{{UUID: configuration.UUID, Flow: "xtls-rprx-vision"}}, TLS: realityTLS{Enabled: true, ServerName: configuration.SNI, Reality: realitySettings{Enabled: true, Handshake: realityHandshake{Server: configuration.SNI, ServerPort: 443}, PrivateKey: configuration.PrivateKey, ShortID: []string{configuration.ShortID}}}}, true, nil
}

func (Module) XrayInbound(view model.StateView) (any, bool, error) {
	state := view.Snapshot()
	configuration := state.Protocols.VLESSReality
	if configuration == nil || !configuration.Enabled || configuration.Engine != model.RealityEngineXray {
		return nil, false, nil
	}
	reality := xrayRealitySettings{Show: false, Xver: 0, Target: configuration.Xray.Target, ServerNames: configuration.Xray.ServerNames, PrivateKey: configuration.PrivateKey, MinClientVer: configuration.Xray.MinClientVer, MaxClientVer: configuration.Xray.MaxClientVer, MaxTimeDiff: configuration.Xray.MaxTimeDiff, ShortIDs: []string{configuration.ShortID}, MLDSA65Seed: configuration.Xray.MLDSA65Seed}
	applyFallbackProfile(&reality, configuration.Xray.FallbackProfile)
	return xrayInbound{Listen: "::", Port: configuration.Port, Protocol: "vless", Settings: xraySettings{Clients: []xrayClient{{ID: configuration.UUID, Flow: "xtls-rprx-vision"}}, Decryption: "none"}, StreamSettings: xrayStreamSettings{Network: "tcp", Security: "reality", TCPSettings: xrayTCPSettings{Header: xrayHeader{Type: "none"}}, RealitySettings: reality}, Tag: "vless-reality"}, true, nil
}

func (Module) ClientOutbound(view model.StateView, server string) (any, error) {
	configuration := view.Snapshot().Protocols.VLESSReality
	if configuration == nil || !configuration.Enabled {
		return nil, nil
	}
	fingerprint := configuration.Xray.Fingerprint
	if fingerprint == "" {
		fingerprint = "chrome"
	}
	return clientOutbound{Type: "vless", Tag: displayName(configuration.Name, "vless-reality"), Server: server, ServerPort: configuration.Port, UUID: configuration.UUID, Flow: "xtls-rprx-vision", TLS: clientTLS{Enabled: true, ServerName: configuration.SNI, UTLS: utls{Enabled: true, Fingerprint: fingerprint}, Reality: clientReality{Enabled: true, PublicKey: configuration.PublicKey, ShortID: configuration.ShortID}}}, nil
}
func (Module) ShareLink(view model.StateView, server string) (string, error) {
	configuration := view.Snapshot().Protocols.VLESSReality
	if configuration == nil || !configuration.Enabled {
		return "", nil
	}
	fingerprint := configuration.Xray.Fingerprint
	if fingerprint == "" {
		fingerprint = "chrome"
	}
	spiderX := configuration.Xray.SpiderX
	if spiderX == "" {
		spiderX = "/"
	}
	query := url.Values{"encryption": {"none"}, "flow": {"xtls-rprx-vision"}, "security": {"reality"}, "sni": {configuration.SNI}, "fp": {fingerprint}, "pbk": {configuration.PublicKey}, "sid": {configuration.ShortID}, "spx": {spiderX}, "type": {"tcp"}}
	if configuration.Xray.MLDSA65Verify != "" {
		query.Set("pqv", configuration.Xray.MLDSA65Verify)
	}
	return (&url.URL{Scheme: "vless", User: url.User(configuration.UUID), Host: net.JoinHostPort(server, strconv.Itoa(int(configuration.Port))), RawQuery: query.Encode(), Fragment: displayName(configuration.Name, "vless-reality")}).String(), nil
}
func (Module) MihomoProxy(view model.StateView, server string) (protocol.MihomoProxy, bool, error) {
	configuration := view.Snapshot().Protocols.VLESSReality
	if configuration == nil || !configuration.Enabled {
		return protocol.MihomoProxy{}, false, nil
	}
	fingerprint := configuration.Xray.Fingerprint
	if fingerprint == "" {
		fingerprint = "chrome"
	}
	name := displayName(configuration.Name, "vless-reality")
	return protocol.MihomoProxy{Name: name, Config: mihomoProxy{Name: name, Type: "vless", Server: server, Port: configuration.Port, UUID: configuration.UUID, Network: "tcp", TLS: true, UDP: true, Flow: "xtls-rprx-vision", ClientFingerprint: fingerprint, RealityOpts: realityOptions{PublicKey: configuration.PublicKey, ShortID: configuration.ShortID}, ServerName: configuration.SNI}}, true, nil
}
func (Module) FirewallRules(view model.StateView) []protocol.FirewallRule {
	configuration := view.Snapshot().Protocols.VLESSReality
	if configuration == nil || !configuration.Enabled {
		return nil
	}
	return []protocol.FirewallRule{{Network: "tcp", Port: configuration.Port}}
}

type singBoxInbound struct {
	Type       string      `json:"type"`
	Tag        string      `json:"tag"`
	Listen     string      `json:"listen"`
	ListenPort uint16      `json:"listen_port"`
	Users      []vlessUser `json:"users"`
	TLS        realityTLS  `json:"tls"`
}
type vlessUser struct {
	UUID string `json:"uuid"`
	Flow string `json:"flow"`
}
type realityTLS struct {
	Enabled    bool            `json:"enabled"`
	ServerName string          `json:"server_name"`
	Reality    realitySettings `json:"reality"`
}
type realitySettings struct {
	Enabled    bool             `json:"enabled"`
	Handshake  realityHandshake `json:"handshake"`
	PrivateKey string           `json:"private_key"`
	ShortID    []string         `json:"short_id"`
}
type realityHandshake struct {
	Server     string `json:"server"`
	ServerPort int    `json:"server_port"`
}
type xrayInbound struct {
	Listen         string             `json:"listen"`
	Port           uint16             `json:"port"`
	Protocol       string             `json:"protocol"`
	Settings       xraySettings       `json:"settings"`
	StreamSettings xrayStreamSettings `json:"streamSettings"`
	Tag            string             `json:"tag"`
}
type xraySettings struct {
	Clients    []xrayClient `json:"clients"`
	Decryption string       `json:"decryption"`
}
type xrayClient struct {
	ID   string `json:"id"`
	Flow string `json:"flow"`
}
type xrayStreamSettings struct {
	Network         string              `json:"network"`
	Security        string              `json:"security"`
	TCPSettings     xrayTCPSettings     `json:"tcpSettings"`
	RealitySettings xrayRealitySettings `json:"realitySettings"`
}
type xrayTCPSettings struct {
	Header xrayHeader `json:"header"`
}
type xrayHeader struct {
	Type string `json:"type"`
}
type xrayRealitySettings struct {
	Show                  bool               `json:"show"`
	Xver                  int                `json:"xver"`
	Target                string             `json:"target"`
	ServerNames           []string           `json:"serverNames"`
	PrivateKey            string             `json:"privateKey"`
	MinClientVer          string             `json:"minClientVer,omitempty"`
	MaxClientVer          string             `json:"maxClientVer,omitempty"`
	MaxTimeDiff           int                `json:"maxTimeDiff"`
	ShortIDs              []string           `json:"shortIds"`
	MLDSA65Seed           string             `json:"mldsa65Seed,omitempty"`
	LimitFallbackUpload   *xrayFallbackLimit `json:"limitFallbackUpload,omitempty"`
	LimitFallbackDownload *xrayFallbackLimit `json:"limitFallbackDownload,omitempty"`
}
type xrayFallbackLimit struct {
	AfterBytes       int `json:"afterBytes"`
	BytesPerSec      int `json:"bytesPerSec"`
	BurstBytesPerSec int `json:"burstBytesPerSec"`
}

func applyFallbackProfile(settings *xrayRealitySettings, profile string) {
	switch profile {
	case "balanced":
		settings.LimitFallbackUpload = &xrayFallbackLimit{AfterBytes: 65536, BytesPerSec: 1048576, BurstBytesPerSec: 2097152}
		settings.LimitFallbackDownload = &xrayFallbackLimit{AfterBytes: 65536, BytesPerSec: 5242880, BurstBytesPerSec: 10485760}
	case "strict":
		settings.LimitFallbackUpload = &xrayFallbackLimit{AfterBytes: 32768, BytesPerSec: 262144, BurstBytesPerSec: 524288}
		settings.LimitFallbackDownload = &xrayFallbackLimit{AfterBytes: 32768, BytesPerSec: 1048576, BurstBytesPerSec: 2097152}
	}
}

type clientOutbound struct {
	Type       string    `json:"type"`
	Tag        string    `json:"tag"`
	Server     string    `json:"server"`
	ServerPort uint16    `json:"server_port"`
	UUID       string    `json:"uuid"`
	Flow       string    `json:"flow"`
	TLS        clientTLS `json:"tls"`
}
type clientTLS struct {
	Enabled    bool          `json:"enabled"`
	ServerName string        `json:"server_name"`
	UTLS       utls          `json:"utls"`
	Reality    clientReality `json:"reality"`
}
type utls struct {
	Enabled     bool   `json:"enabled"`
	Fingerprint string `json:"fingerprint"`
}
type clientReality struct {
	Enabled   bool   `json:"enabled"`
	PublicKey string `json:"public_key"`
	ShortID   string `json:"short_id"`
}
type mihomoProxy struct {
	Name              string         `yaml:"name"`
	Type              string         `yaml:"type"`
	Server            string         `yaml:"server"`
	Port              uint16         `yaml:"port"`
	UUID              string         `yaml:"uuid"`
	Network           string         `yaml:"network"`
	TLS               bool           `yaml:"tls"`
	UDP               bool           `yaml:"udp"`
	Flow              string         `yaml:"flow"`
	ClientFingerprint string         `yaml:"client-fingerprint"`
	RealityOpts       realityOptions `yaml:"reality-opts"`
	ServerName        string         `yaml:"servername"`
}
type realityOptions struct {
	PublicKey string `yaml:"public-key"`
	ShortID   string `yaml:"short-id"`
}

func displayName(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
