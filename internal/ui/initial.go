// Package ui contains the interactive Chinese setup flows. It only builds a
// typed candidate state; app.Apply remains responsible for host changes.
package ui

import (
	"bufio"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"math/big"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/sherlock-wong/vps-net-manager/internal/app"
	"github.com/sherlock-wong/vps-net-manager/internal/model"
)

// InitialSetup asks for an initial protocol selection. An empty or 0
// selection intentionally creates a valid zero-protocol state.
func InitialSetup(ctx context.Context, input io.Reader, output io.Writer, stateDirectory string, state model.State) (model.State, error) {
	if state.Protocols.VLESSReality != nil || state.Protocols.Hysteria2 != nil || state.Protocols.AnyTLS != nil {
		return model.State{}, fmt.Errorf("初始向导只能用于未配置协议的状态")
	}
	prompter := &prompt{scanner: bufio.NewScanner(input), output: output}
	fmt.Fprintln(output, "\n首次节点配置")
	fmt.Fprintln(output, "  1. Vless-Reality（Sing-box）")
	fmt.Fprintln(output, "  2. Hysteria2")
	fmt.Fprintln(output, "  3. AnyTLS")
	fmt.Fprintln(output, "  0. 暂不添加协议（保留零协议状态）")
	selection, err := prompter.ask("选择协议（如 1,2,3；回车或 0 跳过）：")
	if err != nil {
		return model.State{}, err
	}
	selected, err := parseSelection(selection)
	if err != nil {
		return model.State{}, err
	}
	address, err := prompter.ask("对外地址（可稍后设置；支持域名、IPv4 或 IPv6）：")
	if err != nil {
		return model.State{}, err
	}
	candidate := model.NewSnapshot(state).Snapshot()
	candidate.PublicAddress = strings.TrimSpace(address)
	if selected[1] {
		sni, err := selectRealitySNI(ctx, prompter, stateDirectory)
		if err != nil {
			return model.State{}, err
		}
		engine, err := selectRealityEngine(prompter)
		if err != nil {
			return model.State{}, err
		}
		port, err := selectNewPort(prompter, "tcp", candidate)
		if err != nil {
			return model.State{}, err
		}
		vless, err := newVLESS(port, sni, engine)
		if err != nil {
			return model.State{}, err
		}
		candidate.Protocols.VLESSReality = vless
	}
	if selected[2] || selected[3] {
		domain, err := prompter.askDefault("普通 TLS 域名（固定自签证书）", "www.bing.com")
		if err != nil {
			return model.State{}, err
		}
		candidate, err = app.AddPinnedCertificate(ctx, stateDirectory, candidate, "default", "初始固定证书（"+domain+"）", domain, time.Now())
		if err != nil {
			return model.State{}, err
		}
		if selected[2] {
			port, err := selectNewPort(prompter, "udp", candidate)
			if err != nil {
				return model.State{}, err
			}
			password, err := randomToken(24)
			if err != nil {
				return model.State{}, err
			}
			candidate.Protocols.Hysteria2 = &model.Hysteria2{Enabled: true, Name: "hysteria2", Port: port, Password: password, Domain: domain, CertificateID: "default", UpMbps: 100, DownMbps: 100}
		}
		if selected[3] {
			port, err := selectNewPort(prompter, "tcp", candidate)
			if err != nil {
				return model.State{}, err
			}
			password, err := randomToken(24)
			if err != nil {
				return model.State{}, err
			}
			candidate.Protocols.AnyTLS = &model.AnyTLS{Enabled: true, Name: "anytls", Port: port, Password: password, Domain: domain, CertificateID: "default", Padding: model.Padding{Mode: model.PaddingDefault}}
		}
	}
	if err := candidate.Validate(); err != nil {
		return model.State{}, err
	}
	return candidate, nil
}

type prompt struct {
	scanner *bufio.Scanner
	output  io.Writer
}

func (prompt *prompt) ask(label string) (string, error) {
	fmt.Fprint(prompt.output, label+" ")
	if !prompt.scanner.Scan() {
		if err := prompt.scanner.Err(); err != nil {
			return "", err
		}
		return "", io.EOF
	}
	return strings.TrimSpace(prompt.scanner.Text()), nil
}
func (prompt *prompt) askDefault(label, fallback string) (string, error) {
	value, err := prompt.ask(label + "（回车使用 " + fallback + "）：")
	if err != nil {
		return "", err
	}
	if value == "" {
		return fallback, nil
	}
	return value, nil
}

func parseSelection(value string) (map[int]bool, error) {
	selected := make(map[int]bool)
	value = strings.TrimSpace(value)
	if value == "" || value == "0" {
		return selected, nil
	}
	for _, item := range strings.Split(value, ",") {
		choice, err := strconv.Atoi(strings.TrimSpace(item))
		if err != nil || choice < 1 || choice > 3 || selected[choice] {
			return nil, fmt.Errorf("协议选择无效")
		}
		selected[choice] = true
	}
	return selected, nil
}

func availablePort(network string, used map[uint16]struct{}) (uint16, error) {
	for attempt := 0; attempt < 100; attempt++ {
		value, err := rand.Int(rand.Reader, bigPortRange)
		if err != nil {
			return 0, err
		}
		port := uint16(20000 + value.Int64())
		if _, exists := used[port]; exists {
			continue
		}
		if portFree(network, port) {
			used[port] = struct{}{}
			return port, nil
		}
	}
	return 0, fmt.Errorf("无法分配可用 %s 端口", network)
}

// selectNewPort makes the port choice explicit whenever a protocol is added.
// An automatic choice is still the default, but custom values are checked
// against both configured protocols and the currently occupied local ports.
func selectNewPort(prompt *prompt, network string, state model.State) (uint16, error) {
	choice, err := prompt.ask("端口：1 自动分配随机空闲端口（默认）  2 自定义端口：")
	if err != nil {
		return 0, err
	}
	used := usedPorts(state)
	switch choice {
	case "", "1":
		return availablePort(network, used)
	case "2":
		value, err := prompt.ask("请输入端口（1-65535）：")
		if err != nil {
			return 0, err
		}
		parsed, err := strconv.ParseUint(value, 10, 16)
		if err != nil || parsed == 0 {
			return 0, fmt.Errorf("端口无效")
		}
		port := uint16(parsed)
		if _, exists := used[port]; exists {
			return 0, fmt.Errorf("端口与现有协议重复")
		}
		if !portFree(network, port) {
			return 0, fmt.Errorf("%d 端口当前不可用", port)
		}
		return port, nil
	default:
		return 0, fmt.Errorf("无效选择")
	}
}

var bigPortRange = big.NewInt(40000)

func portFree(network string, port uint16) bool {
	address := net.JoinHostPort("", strconv.Itoa(int(port)))
	if network == "udp" {
		listener, err := net.ListenPacket("udp", address)
		if err != nil {
			return false
		}
		listener.Close()
		return true
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return false
	}
	listener.Close()
	return true
}

func newVLESS(port uint16, sni string, engine model.RealityEngine) (*model.VLESSReality, error) {
	uuid, err := randomUUID()
	if err != nil {
		return nil, err
	}
	privateKey, publicKey, shortID, err := newRealityKeyMaterial()
	if err != nil {
		return nil, err
	}
	return &model.VLESSReality{Enabled: true, Name: "vless-reality", Port: port, Engine: engine, UUID: uuid, SNI: sni, PrivateKey: privateKey, PublicKey: publicKey, ShortID: shortID, Xray: model.XrayReality{Target: sni + ":443", ServerNames: []string{sni}, Fingerprint: "chrome", SpiderX: "/", FallbackProfile: "off"}}, nil
}

func newRealityKeyMaterial() (string, string, string, error) {
	curve := ecdh.X25519()
	key, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", "", err
	}
	shortBytes := make([]byte, 8)
	if _, err := io.ReadFull(rand.Reader, shortBytes); err != nil {
		return "", "", "", err
	}
	return base64.RawURLEncoding.EncodeToString(key.Bytes()), base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), hex.EncodeToString(shortBytes), nil
}

func randomUUID() (string, error) {
	value := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, value); err != nil {
		return "", err
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	hexValue := hex.EncodeToString(value)
	return hexValue[:8] + "-" + hexValue[8:12] + "-" + hexValue[12:16] + "-" + hexValue[16:20] + "-" + hexValue[20:], nil
}
func randomToken(bytes int) (string, error) {
	value := make([]byte, bytes)
	if _, err := io.ReadFull(rand.Reader, value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
