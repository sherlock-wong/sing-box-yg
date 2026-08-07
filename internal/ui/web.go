package ui

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/sherlock-wong/vps-net-manager/internal/model"
	"github.com/sherlock-wong/vps-net-manager/internal/web"
)

// EditWeb changes only candidates in memory. A certificate selected or
// requested here is returned as part of deployment state so the caller can
// persist it through the normal manager transaction before applying Nginx.
func EditWeb(ctx context.Context, input *bufio.Scanner, output io.Writer, stateDirectory string, deployment model.State, state web.State) (model.State, web.State, bool, error) {
	prompter := &prompt{scanner: input, output: output}
	candidateDeployment, candidate := deployment, state
	changed := false
	for {
		fmt.Fprintln(output, "\nWeb 服务与 HTTPS 反向代理")
		if changed {
			fmt.Fprintln(output, "  ⚠ 当前显示的是未保存草稿，Nginx 服务和防火墙规则尚未变更。")
		}
		fmt.Fprintln(output, "  1. 查看当前反向代理")
		fmt.Fprintln(output, "  2. 添加 HTTPS 反向代理")
		fmt.Fprintln(output, "  3. 修改 HTTPS 反向代理")
		fmt.Fprintln(output, "  4. 删除 HTTPS 反向代理")
		if changed {
			fmt.Fprintln(output, "  5. 保存草稿、应用 Nginx 并返回主菜单")
			fmt.Fprintln(output, "  6. 删除草稿并返回主菜单")
		}
		fmt.Fprintln(output, "  0. 返回主菜单")
		fmt.Fprint(output, "请选择：")
		if !input.Scan() {
			return deployment, state, false, input.Err()
		}

		switch strings.TrimSpace(input.Text()) {
		case "1":
			showWebProxies(output, candidate.Proxies, candidateDeployment.Certificates, changed)
		case "2":
			nextDeployment, proxy, accepted, err := promptWebProxy(ctx, prompter, stateDirectory, candidateDeployment, nil)
			if err != nil {
				fmt.Fprintln(output, "无法添加反向代理：", err)
				continue
			}
			if !accepted {
				continue
			}
			if err := checkNewWebPort(proxy.ListenPort, candidate); err != nil {
				fmt.Fprintln(output, "无法添加反向代理：", err)
				continue
			}
			candidateDeployment = nextDeployment
			candidate.Proxies = append(candidate.Proxies, proxy)
			if err := candidate.Validate(candidateDeployment.Certificates); err != nil {
				candidate.Proxies = candidate.Proxies[:len(candidate.Proxies)-1]
				fmt.Fprintln(output, "无法添加反向代理：", err)
				continue
			}
			changed = true
			fmt.Fprintln(output, "⚠ 反向代理已暂存，尚未写入 Nginx 或 UFW；可选择 5 保存应用、6 删除草稿，或 0 返回时再决定。")
		case "3":
			if len(candidate.Proxies) == 0 {
				fmt.Fprintln(output, "当前没有可修改的反向代理。")
				continue
			}
			showWebProxies(output, candidate.Proxies, candidateDeployment.Certificates, changed)
			index, ok := promptWebSelection(prompter, len(candidate.Proxies), "选择要修改的编号（输入 0 取消）：")
			if !ok {
				continue
			}
			nextDeployment, proxy, accepted, err := promptWebProxy(ctx, prompter, stateDirectory, candidateDeployment, &candidate.Proxies[index])
			if err != nil {
				fmt.Fprintln(output, "无法修改反向代理：", err)
				continue
			}
			if !accepted {
				continue
			}
			previous := candidate.Proxies[index]
			candidate.Proxies[index] = proxy
			if err := candidate.Validate(nextDeployment.Certificates); err != nil {
				candidate.Proxies[index] = previous
				fmt.Fprintln(output, "无法修改反向代理：", err)
				continue
			}
			candidateDeployment = nextDeployment
			changed = true
			fmt.Fprintln(output, "⚠ 修改已暂存，尚未写入 Nginx 或 UFW；可选择 5 保存应用、6 删除草稿，或 0 返回时再决定。")
		case "4":
			if len(candidate.Proxies) == 0 {
				fmt.Fprintln(output, "当前没有可删除的反向代理。")
				continue
			}
			showWebProxies(output, candidate.Proxies, candidateDeployment.Certificates, changed)
			index, ok := promptWebSelection(prompter, len(candidate.Proxies), "选择要删除的编号（输入 0 取消）：")
			if !ok {
				continue
			}
			candidate.Proxies = append(candidate.Proxies[:index], candidate.Proxies[index+1:]...)
			changed = true
			fmt.Fprintln(output, "⚠ 删除已暂存，尚未写入 Nginx 或 UFW；可选择 5 保存应用、6 删除草稿，或 0 返回时再决定。")
		case "5":
			if changed {
				return candidateDeployment, candidate, true, nil
			}
			fmt.Fprintln(output, "当前没有草稿需要保存。")
		case "6":
			if changed {
				fmt.Fprintln(output, "草稿已删除，已生效的 Nginx 配置和防火墙保持不变。")
				return deployment, state, false, nil
			}
			fmt.Fprintln(output, "当前没有草稿需要删除。")
		case "0":
			if !changed {
				return deployment, state, false, nil
			}
			choice, err := prompter.ask("当前已经设置了草稿：1 保存并应用 Nginx  2 删除草稿  0 继续编辑：")
			if err != nil {
				return deployment, state, false, err
			}
			switch choice {
			case "1":
				return candidateDeployment, candidate, true, nil
			case "2":
				fmt.Fprintln(output, "草稿已删除，已生效的 Nginx 配置和防火墙保持不变。")
				return deployment, state, false, nil
			case "", "0":
				continue
			default:
				fmt.Fprintln(output, "无效选择，请继续编辑或再次选择 0 返回。")
			}
		default:
			fmt.Fprintln(output, "无效选择。")
		}
	}
}

func showWebProxies(output io.Writer, proxies []web.Proxy, certificates map[string]model.Certificate, draft bool) {
	if len(proxies) == 0 {
		if draft {
			fmt.Fprintln(output, "\n草稿中没有 HTTPS 反向代理（尚未生效）。")
		} else {
			fmt.Fprintln(output, "\n当前没有受管 HTTPS 反向代理。")
		}
		return
	}
	if draft {
		fmt.Fprintln(output, "\nHTTPS 反向代理草稿（尚未生效）：")
	} else {
		fmt.Fprintln(output, "\n当前受管 HTTPS 反向代理：")
	}
	for index, proxy := range proxies {
		certificate := certificates[proxy.CertificateID]
		fmt.Fprintf(output, "  %d. %s → http://%s（证书：%s / %s）\n", index+1, httpsURL(proxy.Domain, proxy.ListenPort), endpoint(proxy.TargetHost, proxy.TargetPort), certificate.Name, proxy.CertificateID)
	}
}

func httpsURL(domain string, port uint16) string {
	if port == 443 {
		return "https://" + domain
	}
	return fmt.Sprintf("https://%s:%d", domain, port)
}

func promptWebProxy(ctx context.Context, prompter *prompt, stateDirectory string, deployment model.State, current *web.Proxy) (model.State, web.Proxy, bool, error) {
	var proxy web.Proxy
	if current != nil {
		proxy = *current
	}
	domainLabel := "访问域名（填写自己控制的域名；输入 0 取消）："
	if current != nil {
		domainLabel = "访问域名（当前 " + current.Domain + "；回车保持；输入 0 取消）："
	}
	domain, err := prompter.ask(domainLabel)
	if err != nil {
		return deployment, web.Proxy{}, false, err
	}
	if domain == "0" {
		return deployment, web.Proxy{}, false, nil
	}
	if domain == "" {
		if current == nil {
			return deployment, web.Proxy{}, false, fmt.Errorf("域名不能为空")
		}
		domain = current.Domain
	}

	certificateID := proxy.CertificateID
	nextDeployment := deployment
	accepted := false
	if current == nil || !strings.EqualFold(domain, current.Domain) {
		nextDeployment, certificateID, accepted, err = ensureTLSCertificate(ctx, prompter, stateDirectory, deployment, domain)
		if err != nil || !accepted {
			return deployment, web.Proxy{}, false, err
		}
	}

	port, accepted, err := promptWebPort(prompter, "Nginx HTTPS 监听端口", proxy.ListenPort, 443)
	if err != nil || !accepted {
		return deployment, web.Proxy{}, false, err
	}
	targetHost, err := prompter.ask(defaultWebHostLabel(proxy.TargetHost))
	if err != nil {
		return deployment, web.Proxy{}, false, err
	}
	if targetHost == "0" {
		return deployment, web.Proxy{}, false, nil
	}
	if targetHost == "" {
		targetHost = "127.0.0.1"
		if current != nil && current.TargetHost != "" {
			targetHost = current.TargetHost
		}
	}
	targetPort, accepted, err := promptWebPort(prompter, "内部目标端口", proxy.TargetPort, 25774)
	if err != nil || !accepted {
		return deployment, web.Proxy{}, false, err
	}
	if proxy.ID == "" {
		identifier := make([]byte, 4)
		if _, err := rand.Read(identifier); err != nil {
			return deployment, web.Proxy{}, false, err
		}
		proxy.ID = "web_" + hex.EncodeToString(identifier)
	}
	proxy.Domain, proxy.ListenPort, proxy.TargetHost, proxy.TargetPort, proxy.CertificateID = strings.ToLower(domain), port, targetHost, targetPort, certificateID
	return nextDeployment, proxy, true, nil
}

func defaultWebHostLabel(current string) string {
	if current == "" {
		current = "127.0.0.1"
	}
	return "内部目标地址（回车使用 " + current + "；输入 0 取消）："
}

func promptWebPort(prompter *prompt, name string, current, fallback uint16) (uint16, bool, error) {
	if current == 0 {
		current = fallback
	}
	value, err := prompter.ask(fmt.Sprintf("%s（回车使用 %d；输入 0 取消）：", name, current))
	if err != nil {
		return 0, false, err
	}
	if value == "0" {
		return 0, false, nil
	}
	if value == "" {
		return current, true, nil
	}
	parsed, err := strconv.ParseUint(value, 10, 16)
	if err != nil || parsed == 0 {
		return 0, false, fmt.Errorf("端口必须是 1 到 65535")
	}
	return uint16(parsed), true, nil
}

func promptWebSelection(prompter *prompt, count int, label string) (int, bool) {
	value, err := prompter.ask(label)
	if err != nil || value == "0" {
		return 0, false
	}
	selected, err := strconv.Atoi(value)
	if err != nil || selected < 1 || selected > count {
		fmt.Fprintln(prompter.output, "编号无效。")
		return 0, false
	}
	return selected - 1, true
}

func checkNewWebPort(port uint16, state web.State) error {
	for _, proxy := range state.Proxies {
		if proxy.ListenPort == port {
			return nil // Nginx virtual hosts may deliberately share one HTTPS port.
		}
	}
	if !portFree("tcp", port) {
		return fmt.Errorf("监听端口 %d 已被其他服务占用", port)
	}
	return nil
}
