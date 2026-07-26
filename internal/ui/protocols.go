package ui

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/sherlock-wong/vps-net-manager/internal/app"
	"github.com/sherlock-wong/vps-net-manager/internal/model"
)

// EditProtocols mutates only an in-memory candidate. The caller decides when
// to run app.Apply, keeping all service and firewall changes transactional.
func EditProtocols(ctx context.Context, scanner *bufio.Scanner, output io.Writer, stateDirectory string, state model.State) (model.State, bool, error) {
	prompter := &prompt{scanner: scanner, output: output}
	candidate := model.NewSnapshot(state).Snapshot()
	changed := false
	for {
		fmt.Fprintln(output, "\n管理协议")
		fmt.Fprintf(output, "  1. Vless-Reality（%s）\n", status(candidate.Protocols.VLESSReality != nil, vlessEnabled(candidate.Protocols.VLESSReality)))
		fmt.Fprintf(output, "  2. Hysteria2（%s）\n", status(candidate.Protocols.Hysteria2 != nil, hy2Enabled(candidate.Protocols.Hysteria2)))
		fmt.Fprintf(output, "  3. AnyTLS（%s）\n", status(candidate.Protocols.AnyTLS != nil, anyTLSEnabled(candidate.Protocols.AnyTLS)))
		fmt.Fprintln(output, "  0. 返回主菜单")
		choice, err := prompter.ask("请选择：")
		if err != nil {
			return model.State{}, false, err
		}
		if choice == "0" {
			return candidate, changed, nil
		}
		var didChange bool
		switch choice {
		case "1":
			candidate, didChange, err = editVLESS(ctx, prompter, stateDirectory, candidate)
		case "2":
			candidate, didChange, err = editHysteria2(ctx, prompter, stateDirectory, candidate)
		case "3":
			candidate, didChange, err = editAnyTLS(ctx, prompter, stateDirectory, candidate)
		default:
			fmt.Fprintln(output, "无效选择。")
			continue
		}
		if err != nil {
			fmt.Fprintln(output, "操作未保存：", err)
			continue
		}
		if didChange {
			if err := candidate.Validate(); err != nil {
				return model.State{}, false, err
			}
			changed = true
		}
	}
}

func editVLESS(ctx context.Context, prompt *prompt, stateDirectory string, state model.State) (model.State, bool, error) {
	_ = ctx
	_ = stateDirectory
	if state.Protocols.VLESSReality == nil {
		sni, err := prompt.askDefault("Reality SNI", "www.apple.com")
		if err != nil {
			return state, false, err
		}
		port, err := availablePort("tcp", usedPorts(state))
		if err != nil {
			return state, false, err
		}
		configuration, err := newVLESS(port, sni)
		if err != nil {
			return state, false, err
		}
		state.Protocols.VLESSReality = configuration
		return state, true, nil
	}
	choice, err := prompt.ask("1 启停  2 修改端口  3 轮换 UUID  4 删除配置  0 返回：")
	if err != nil {
		return state, false, err
	}
	configuration := state.Protocols.VLESSReality
	switch choice {
	case "1":
		configuration.Enabled = !configuration.Enabled
	case "2":
		port, err := promptPort(prompt, configuration.Port, state, "tcp")
		if err != nil {
			return state, false, err
		}
		configuration.Port = port
	case "3":
		uuid, err := randomUUID()
		if err != nil {
			return state, false, err
		}
		configuration.UUID = uuid
	case "4":
		state.Protocols.VLESSReality = nil
	case "0":
		return state, false, nil
	default:
		return state, false, fmt.Errorf("无效选择")
	}
	return state, true, nil
}

func editHysteria2(ctx context.Context, prompt *prompt, stateDirectory string, state model.State) (model.State, bool, error) {
	if state.Protocols.Hysteria2 == nil {
		domain, err := prompt.askDefault("普通 TLS 域名", "www.bing.com")
		if err != nil {
			return state, false, err
		}
		var certificateID string
		state, certificateID, err = ensurePinnedCertificate(ctx, prompt, stateDirectory, state, domain)
		if err != nil {
			return state, false, err
		}
		port, err := availablePort("udp", usedPorts(state))
		if err != nil {
			return state, false, err
		}
		password, err := randomToken(24)
		if err != nil {
			return state, false, err
		}
		state.Protocols.Hysteria2 = &model.Hysteria2{Enabled: true, Name: "hysteria2", Port: port, Password: password, Domain: domain, CertificateID: certificateID, UpMbps: 100, DownMbps: 100}
		return state, true, nil
	}
	choice, err := prompt.ask("1 启停  2 修改端口  3 轮换密码  4 删除配置  0 返回：")
	if err != nil {
		return state, false, err
	}
	configuration := state.Protocols.Hysteria2
	switch choice {
	case "1":
		configuration.Enabled = !configuration.Enabled
	case "2":
		port, err := promptPort(prompt, configuration.Port, state, "udp")
		if err != nil {
			return state, false, err
		}
		configuration.Port = port
	case "3":
		password, err := randomToken(24)
		if err != nil {
			return state, false, err
		}
		configuration.Password = password
	case "4":
		state.Protocols.Hysteria2 = nil
	case "0":
		return state, false, nil
	default:
		return state, false, fmt.Errorf("无效选择")
	}
	return state, true, nil
}

func editAnyTLS(ctx context.Context, prompt *prompt, stateDirectory string, state model.State) (model.State, bool, error) {
	if state.Protocols.AnyTLS == nil {
		domain, err := prompt.askDefault("普通 TLS 域名", "www.bing.com")
		if err != nil {
			return state, false, err
		}
		var certificateID string
		state, certificateID, err = ensurePinnedCertificate(ctx, prompt, stateDirectory, state, domain)
		if err != nil {
			return state, false, err
		}
		port, err := availablePort("tcp", usedPorts(state))
		if err != nil {
			return state, false, err
		}
		password, err := randomToken(24)
		if err != nil {
			return state, false, err
		}
		state.Protocols.AnyTLS = &model.AnyTLS{Enabled: true, Name: "anytls", Port: port, Password: password, Domain: domain, CertificateID: certificateID, Padding: model.Padding{Mode: model.PaddingDefault}}
		return state, true, nil
	}
	choice, err := prompt.ask("1 启停  2 修改端口  3 轮换密码  4 删除配置  0 返回：")
	if err != nil {
		return state, false, err
	}
	configuration := state.Protocols.AnyTLS
	switch choice {
	case "1":
		configuration.Enabled = !configuration.Enabled
	case "2":
		port, err := promptPort(prompt, configuration.Port, state, "tcp")
		if err != nil {
			return state, false, err
		}
		configuration.Port = port
	case "3":
		password, err := randomToken(24)
		if err != nil {
			return state, false, err
		}
		configuration.Password = password
	case "4":
		state.Protocols.AnyTLS = nil
	case "0":
		return state, false, nil
	default:
		return state, false, fmt.Errorf("无效选择")
	}
	return state, true, nil
}

func ensurePinnedCertificate(ctx context.Context, prompt *prompt, stateDirectory string, state model.State, domain string) (model.State, string, error) {
	if _, exists := state.Certificates["default"]; exists {
		return state, "default", nil
	}
	choice, err := prompt.ask("证书库为空，创建固定自签证书？1 确认 / 0 取消：")
	if err != nil {
		return state, "", err
	}
	if choice != "1" {
		return state, "", fmt.Errorf("需要普通 TLS 证书")
	}
	candidate, err := app.AddPinnedCertificate(ctx, stateDirectory, state, "default", "初始固定证书（"+domain+"）", domain, time.Now())
	return candidate, "default", err
}

func promptPort(prompt *prompt, current uint16, state model.State, network string) (uint16, error) {
	value, err := prompt.ask(fmt.Sprintf("端口（回车保留 %d）：", current))
	if err != nil {
		return 0, err
	}
	if value == "" {
		return current, nil
	}
	parsed, err := strconv.ParseUint(value, 10, 16)
	if err != nil || parsed == 0 {
		return 0, fmt.Errorf("端口无效")
	}
	port := uint16(parsed)
	if port != current {
		for used := range usedPorts(state) {
			if used == port {
				return 0, fmt.Errorf("端口与现有协议重复")
			}
		}
	}
	_ = network
	return port, nil
}

func usedPorts(state model.State) map[uint16]struct{} {
	ports := make(map[uint16]struct{})
	if item := state.Protocols.VLESSReality; item != nil {
		ports[item.Port] = struct{}{}
	}
	if item := state.Protocols.Hysteria2; item != nil {
		ports[item.Port] = struct{}{}
	}
	if item := state.Protocols.AnyTLS; item != nil {
		ports[item.Port] = struct{}{}
	}
	return ports
}
func status(configured, enabled bool) string {
	if !configured {
		return "未添加"
	}
	if enabled {
		return "已启用"
	}
	return "已停用"
}
func vlessEnabled(value *model.VLESSReality) bool { return value != nil && value.Enabled }
func hy2Enabled(value *model.Hysteria2) bool      { return value != nil && value.Enabled }
func anyTLSEnabled(value *model.AnyTLS) bool      { return value != nil && value.Enabled }
