package ui

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sherlock-wong/vps-net-manager/internal/app"
	"github.com/sherlock-wong/vps-net-manager/internal/certificate"
	"github.com/sherlock-wong/vps-net-manager/internal/model"
	"github.com/sherlock-wong/vps-net-manager/internal/reality"
)

const vlessManagementPrompt = `
Vless-Reality 管理
  1. 查看当前配置
  2. 启停
  3. 修改端口
  4. 轮换 UUID
  5. 删除配置
  6. 筛选/修改 Reality SNI
  7. 轮换 Reality 密钥和 Short ID
  8. 切换服务端内核
  0. 返回
请选择：`

const hysteria2ManagementPrompt = `
Hysteria2 管理
  1. 查看当前配置
  2. 启停
  3. 修改端口
  4. 轮换密码
  5. 删除配置
  6. 修改 TLS 域名
  7. 选择证书
  0. 返回
请选择：`

const anyTLSManagementPrompt = `
AnyTLS 管理
  1. 查看当前配置
  2. 启停
  3. 修改端口
  4. 轮换密码
  5. 删除配置
  6. 修改 TLS 域名
  7. 选择证书
  0. 返回
请选择：`

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
	if state.Protocols.VLESSReality == nil {
		sni, err := selectRealitySNI(ctx, prompt, stateDirectory)
		if err != nil {
			return state, false, err
		}
		port, err := selectNewPort(prompt, "tcp", state)
		if err != nil {
			return state, false, err
		}
		engine, err := selectRealityEngine(prompt)
		if err != nil {
			return state, false, err
		}
		configuration, err := newVLESS(port, sni, engine)
		if err != nil {
			return state, false, err
		}
		state.Protocols.VLESSReality = configuration
		return state, true, nil
	}
	configuration := state.Protocols.VLESSReality
	for {
		choice, err := prompt.ask(vlessManagementPrompt)
		if err != nil {
			return state, false, err
		}
		switch choice {
		case "1":
			showVLESSConfiguration(prompt.output, configuration)
			continue
		case "2":
			configuration.Enabled = !configuration.Enabled
		case "3":
			port, err := promptPort(prompt, configuration.Port, state, "tcp")
			if err != nil {
				return state, false, err
			}
			configuration.Port = port
		case "4":
			uuid, err := randomUUID()
			if err != nil {
				return state, false, err
			}
			configuration.UUID = uuid
		case "5":
			state.Protocols.VLESSReality = nil
		case "6":
			sni, err := selectRealitySNI(ctx, prompt, stateDirectory)
			if err != nil {
				return state, false, err
			}
			setVLESSSNI(configuration, sni)
		case "7":
			privateKey, publicKey, shortID, err := newRealityKeyMaterial()
			if err != nil {
				return state, false, err
			}
			configuration.PrivateKey = privateKey
			configuration.PublicKey = publicKey
			configuration.ShortID = shortID
			fmt.Fprintln(prompt.output, "Reality 密钥和 Short ID 已轮换；旧 Vless 节点会立即失效。")
		case "8":
			engine, err := selectRealityEngine(prompt)
			if err != nil {
				return state, false, err
			}
			if engine == configuration.Engine {
				fmt.Fprintln(prompt.output, "已是该服务端内核，配置未改变。")
				continue
			}
			configuration.Engine = engine
		case "0":
			return state, false, nil
		default:
			fmt.Fprintln(prompt.output, "无效选择。")
			continue
		}
		return state, true, nil
	}
}

func selectRealityEngine(prompt *prompt) (model.RealityEngine, error) {
	choice, err := prompt.ask("Reality 服务端内核：1 Sing-box（默认）  2 Xray-core：")
	if err != nil {
		return "", err
	}
	switch choice {
	case "", "1":
		return model.RealityEngineSingBox, nil
	case "2":
		return model.RealityEngineXray, nil
	default:
		return "", fmt.Errorf("无效选择")
	}
}

// selectRealitySNI keeps the quick manual path while exposing the embedded or
// user-maintained candidate list directly in the Vless configuration flow.
func selectRealitySNI(ctx context.Context, prompt *prompt, stateDirectory string) (string, error) {
	choice, err := prompt.ask("Reality SNI：1 手动输入（默认 www.apple.com）  2 筛选候选域名：")
	if err != nil {
		return "", err
	}
	switch choice {
	case "", "1":
		return prompt.askDefault("Reality SNI", "www.apple.com")
	case "2":
		return scanRealitySNI(ctx, prompt, stateDirectory)
	default:
		return "", fmt.Errorf("无效选择")
	}
}

func scanRealitySNI(ctx context.Context, prompt *prompt, stateDirectory string) (string, error) {
	targets, err := reality.LoadTargetsOrDefault(stateDirectory + "/reality-targets.txt")
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 55*time.Second)
	defer cancel()
	fmt.Fprintln(prompt.output, "正在筛选 Reality 候选域名，请稍候……")
	results, err := reality.Scan(ctx, targets, 2, 10)
	if err != nil {
		return "", err
	}
	usable := make([]reality.Result, 0, len(results))
	for _, result := range results {
		if result.Successes > 0 {
			usable = append(usable, result)
		}
	}
	if len(usable) == 0 {
		return "", fmt.Errorf("没有可用候选，请稍后重试或手动输入")
	}
	for index, result := range usable {
		fmt.Fprintf(prompt.output, "  %d. %s（成功 %d/%d，%dms，%s）\n", index+1, result.Host, result.Successes, result.Samples, result.AverageMS, result.Reason)
	}
	choice, err := prompt.ask("选择候选编号（0 改为手动输入）：")
	if err != nil {
		return "", err
	}
	if choice == "0" {
		return prompt.askDefault("Reality SNI", "www.apple.com")
	}
	index, err := strconv.Atoi(choice)
	if err != nil || index < 1 || index > len(usable) {
		return "", fmt.Errorf("候选编号无效")
	}
	return usable[index-1].Host, nil
}

func setVLESSSNI(configuration *model.VLESSReality, sni string) {
	configuration.SNI = sni
	configuration.Xray.Target = sni + ":443"
	configuration.Xray.ServerNames = []string{sni}
}

func editHysteria2(ctx context.Context, prompt *prompt, stateDirectory string, state model.State) (model.State, bool, error) {
	if state.Protocols.Hysteria2 == nil {
		domain, confirmed, err := promptTLSDomain(prompt, "")
		if err != nil {
			return state, false, err
		}
		if !confirmed {
			return state, false, nil
		}
		var certificateID string
		var certificateConfirmed bool
		state, certificateID, certificateConfirmed, err = ensureTLSCertificate(ctx, prompt, stateDirectory, state, domain)
		if err != nil {
			return state, false, err
		}
		if !certificateConfirmed {
			return state, false, nil
		}
		port, err := selectNewPort(prompt, "udp", state)
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
	choice, err := prompt.ask(hysteria2ManagementPrompt)
	if err != nil {
		return state, false, err
	}
	configuration := state.Protocols.Hysteria2
	switch choice {
	case "1":
		showHysteria2Configuration(prompt.output, configuration, state.Certificates[configuration.CertificateID])
		return editHysteria2(ctx, prompt, stateDirectory, state)
	case "2":
		configuration.Enabled = !configuration.Enabled
	case "3":
		port, err := promptPort(prompt, configuration.Port, state, "udp")
		if err != nil {
			return state, false, err
		}
		configuration.Port = port
	case "4":
		password, err := randomToken(24)
		if err != nil {
			return state, false, err
		}
		configuration.Password = password
	case "5":
		state.Protocols.Hysteria2 = nil
	case "6":
		domain, confirmed, err := promptTLSDomain(prompt, configuration.Domain)
		if err != nil {
			return state, false, err
		}
		if !confirmed {
			return state, false, nil
		}
		configuration.Domain = domain
	case "7":
		certificateID, err := selectCertificate(prompt, state, configuration.Domain)
		if err != nil {
			return state, false, err
		}
		configuration.CertificateID = certificateID
	case "0":
		return state, false, nil
	default:
		return state, false, fmt.Errorf("无效选择")
	}
	return state, true, nil
}

func editAnyTLS(ctx context.Context, prompt *prompt, stateDirectory string, state model.State) (model.State, bool, error) {
	if state.Protocols.AnyTLS == nil {
		domain, confirmed, err := promptTLSDomain(prompt, "")
		if err != nil {
			return state, false, err
		}
		if !confirmed {
			return state, false, nil
		}
		var certificateID string
		var certificateConfirmed bool
		state, certificateID, certificateConfirmed, err = ensureTLSCertificate(ctx, prompt, stateDirectory, state, domain)
		if err != nil {
			return state, false, err
		}
		if !certificateConfirmed {
			return state, false, nil
		}
		port, err := selectNewPort(prompt, "tcp", state)
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
	choice, err := prompt.ask(anyTLSManagementPrompt)
	if err != nil {
		return state, false, err
	}
	configuration := state.Protocols.AnyTLS
	switch choice {
	case "1":
		showAnyTLSConfiguration(prompt.output, configuration, state.Certificates[configuration.CertificateID])
		return editAnyTLS(ctx, prompt, stateDirectory, state)
	case "2":
		configuration.Enabled = !configuration.Enabled
	case "3":
		port, err := promptPort(prompt, configuration.Port, state, "tcp")
		if err != nil {
			return state, false, err
		}
		configuration.Port = port
	case "4":
		password, err := randomToken(24)
		if err != nil {
			return state, false, err
		}
		configuration.Password = password
	case "5":
		state.Protocols.AnyTLS = nil
	case "6":
		domain, confirmed, err := promptTLSDomain(prompt, configuration.Domain)
		if err != nil {
			return state, false, err
		}
		if !confirmed {
			return state, false, nil
		}
		configuration.Domain = domain
	case "7":
		certificateID, err := selectCertificate(prompt, state, configuration.Domain)
		if err != nil {
			return state, false, err
		}
		configuration.CertificateID = certificateID
	case "0":
		return state, false, nil
	default:
		return state, false, fmt.Errorf("无效选择")
	}
	return state, true, nil
}

func promptTLSDomain(prompt *prompt, fallback string) (string, bool, error) {
	label := "普通 TLS 域名（填写自己控制的域名；输入 0 取消）："
	if fallback != "" {
		label = "普通 TLS 域名（回车保留 " + fallback + "；输入 0 取消）："
	}
	value, err := prompt.ask(label)
	if err != nil {
		return "", false, err
	}
	if value == "0" || value == "" && fallback == "" {
		return "", false, nil
	}
	if value == "" {
		return fallback, true, nil
	}
	return value, true, nil
}

func ensureTLSCertificate(ctx context.Context, prompt *prompt, stateDirectory string, state model.State, domain string) (model.State, string, bool, error) {
	matches := matchingCertificateIDs(state, domain)
	if len(matches) == 1 {
		item := state.Certificates[matches[0]]
		fmt.Fprintf(prompt.output, "证书库中已找到覆盖 %s 的证书：%s（%s）。\n", domain, item.Name, matches[0])
		return state, matches[0], true, nil
	}
	if len(matches) > 1 {
		fmt.Fprintf(prompt.output, "证书库中找到 %d 张覆盖 %s 的证书：\n", len(matches), domain)
		for index, id := range matches {
			item := state.Certificates[id]
			fmt.Fprintf(prompt.output, "  %d. %s（%s，%s）\n", index+1, item.Name, id, item.Mode)
		}
		choice, err := prompt.ask("选择证书编号（0 取消）：")
		if err != nil {
			return state, "", false, err
		}
		if choice == "0" {
			return state, "", false, nil
		}
		index, err := strconv.Atoi(choice)
		if err != nil || index < 1 || index > len(matches) {
			return state, "", false, fmt.Errorf("证书编号无效")
		}
		return state, matches[index-1], true, nil
	}

	fmt.Fprintf(prompt.output, "证书库中没有覆盖 %s 的有效证书。\n", domain)
	choice, err := prompt.ask("1 申请受信任域名证书（ACME）  2 使用固定自签证书（仅测试）  0 取消：")
	if err != nil {
		return state, "", false, err
	}
	switch choice {
	case "0", "":
		return state, "", false, nil
	case "1":
		return issueACMECertificate(ctx, prompt, stateDirectory, state, domain)
	case "2":
		id := "default"
		if _, exists := state.Certificates[id]; exists {
			id = "self-" + certificateIDForDomain(domain)
		}
		candidate, err := app.AddPinnedCertificate(ctx, stateDirectory, state, id, "固定自签证书（"+domain+"）", domain, time.Now())
		if err != nil {
			return state, "", false, err
		}
		fmt.Fprintln(prompt.output, "已创建固定自签证书，仅建议用于测试；客户端应校验证书指纹。")
		return candidate, id, true, nil
	default:
		return state, "", false, fmt.Errorf("无效选择")
	}
}

func issueACMECertificate(ctx context.Context, prompt *prompt, stateDirectory string, state model.State, domain string) (model.State, string, bool, error) {
	fmt.Fprintln(prompt.output, "申请前请确认该域名由你控制，且 DNS/CDN 设置满足 ACME 验证要求（Cloudflare 请保持灰云）。")
	id, err := prompt.askDefault("证书 ID", "acme-"+certificateIDForDomain(domain))
	if err != nil {
		return state, "", false, err
	}
	if _, exists := state.Certificates[id]; exists {
		return state, "", false, fmt.Errorf("证书 ID 已存在，请使用其他 ID 或在证书管理中更新")
	}
	certificatePath, err := prompt.askDefault("ACME 脚本临时证书路径", "/root/ygkkkca/cert.crt")
	if err != nil {
		return state, "", false, err
	}
	keyPath, err := prompt.askDefault("ACME 脚本临时私钥路径", "/root/ygkkkca/private.key")
	if err != nil {
		return state, "", false, err
	}
	fmt.Fprintln(prompt.output, "即将启动 ACME 交互流程；成功后会验证并写入证书管理。")
	candidate, err := app.AddInteractiveACMECertificate(ctx, stateDirectory, state, id, domain, certificatePath, keyPath, domain, time.Now())
	if err != nil {
		return state, "", false, err
	}
	return candidate, id, true, nil
}

func matchingCertificateIDs(state model.State, hostname string) []string {
	ids := make([]string, 0, len(state.Certificates))
	for id, item := range state.Certificates {
		certificatePEM, certErr := os.ReadFile(item.Cert)
		keyPEM, keyErr := os.ReadFile(item.Key)
		if certErr != nil || keyErr != nil {
			continue
		}
		if _, err := certificate.Inspect(certificatePEM, keyPEM, hostname, time.Now()); err == nil {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func certificateIDForDomain(domain string) string {
	return strings.NewReplacer(".", "-", "*", "wildcard-").Replace(domain)
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
		if !portFree(network, port) {
			return 0, fmt.Errorf("%d 端口当前不可用", port)
		}
	}
	return port, nil
}

func showVLESSConfiguration(output io.Writer, configuration *model.VLESSReality) {
	fmt.Fprintln(output, "\nVless-Reality 当前配置")
	fmt.Fprintf(output, "  状态：%s\n  端口：%d/TCP\n  节点名称：%s\n  内核：%s\n  UUID：%s\n  Reality SNI：%s\n  Public Key：%s\n  Short ID：%s\n", status(true, configuration.Enabled), configuration.Port, configuration.Name, configuration.Engine, configuration.UUID, configuration.SNI, configuration.PublicKey, configuration.ShortID)
}

func showHysteria2Configuration(output io.Writer, configuration *model.Hysteria2, certificate model.Certificate) {
	fmt.Fprintln(output, "\nHysteria2 当前配置")
	fmt.Fprintf(output, "  状态：%s\n  端口：%d/UDP\n  节点名称：%s\n  TLS 域名：%s\n  证书：%s（%s）\n  密码：%s\n  带宽：上行 %d Mbps / 下行 %d Mbps\n", status(true, configuration.Enabled), configuration.Port, configuration.Name, configuration.Domain, certificate.Name, configuration.CertificateID, configuration.Password, configuration.UpMbps, configuration.DownMbps)
}

func showAnyTLSConfiguration(output io.Writer, configuration *model.AnyTLS, certificate model.Certificate) {
	fmt.Fprintln(output, "\nAnyTLS 当前配置")
	fmt.Fprintf(output, "  状态：%s\n  端口：%d/TCP\n  节点名称：%s\n  TLS 域名：%s\n  证书：%s（%s）\n  密码：%s\n  Padding：%s\n", status(true, configuration.Enabled), configuration.Port, configuration.Name, configuration.Domain, certificate.Name, configuration.CertificateID, configuration.Password, configuration.Padding.Mode)
}

func selectCertificate(prompt *prompt, state model.State, hostname string) (string, error) {
	if len(state.Certificates) == 0 {
		return "", fmt.Errorf("证书库为空；请先在主菜单的证书管理中导入或创建证书")
	}
	ids := make([]string, 0, len(state.Certificates))
	for id := range state.Certificates {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	fmt.Fprintln(prompt.output, "可用证书：")
	for index, id := range ids {
		item := state.Certificates[id]
		fmt.Fprintf(prompt.output, "  %d. %s（%s，%s）\n", index+1, item.Name, id, item.Mode)
	}
	choice, err := prompt.ask("选择证书编号：")
	if err != nil {
		return "", err
	}
	index, err := strconv.Atoi(choice)
	if err != nil || index < 1 || index > len(ids) {
		return "", fmt.Errorf("证书编号无效")
	}
	item := state.Certificates[ids[index-1]]
	certificatePEM, err := os.ReadFile(item.Cert)
	if err != nil {
		return "", fmt.Errorf("读取证书失败：%w", err)
	}
	keyPEM, err := os.ReadFile(item.Key)
	if err != nil {
		return "", fmt.Errorf("读取私钥失败：%w", err)
	}
	if _, err := certificate.Inspect(certificatePEM, keyPEM, hostname, time.Now()); err != nil {
		return "", fmt.Errorf("所选证书不覆盖当前 TLS 域名 %s：%w", hostname, err)
	}
	return ids[index-1], nil
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
