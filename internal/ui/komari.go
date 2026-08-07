package ui

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"github.com/sherlock-wong/vps-net-manager/internal/komari"
	"github.com/sherlock-wong/vps-net-manager/internal/model"
	"github.com/sherlock-wong/vps-net-manager/internal/web"
)

// EditKomari prepares Komari and, for domain mode, its linked generic Nginx
// proxy. Nothing starts or writes until the caller applies the returned draft.
func EditKomari(ctx context.Context, input *bufio.Scanner, output io.Writer, stateDirectory string, deployment model.State, webState web.State, state komari.State, nginxInstalled bool) (model.State, web.State, komari.State, bool, bool, bool, bool, error) {
	prompter := &prompt{scanner: input, output: output}
	candidateDeployment := deployment
	candidateWeb := web.State{Schema: webState.Schema, Proxies: append([]web.Proxy(nil), webState.Proxies...)}
	candidate := state
	changed := false
	for {
		fmt.Fprintln(output, "\nKomari 监控")
		if changed {
			fmt.Fprintln(output, "  ⚠ 当前显示的是未保存草稿，Komari、Nginx 和防火墙尚未变更。")
		}
		fmt.Fprintln(output, "  1. 查看当前配置")
		fmt.Fprintln(output, "  2. 配置并启动 Komari")
		fmt.Fprintln(output, "  3. 停止 Komari 并移除其反向代理")
		fmt.Fprintln(output, "  4. 更新 Komari 程序（使用当前管理器锁定版本）")
		fmt.Fprintln(output, "  5. 彻底卸载 Komari")
		if changed {
			fmt.Fprintln(output, "  6. 保存草稿并应用")
			fmt.Fprintln(output, "  7. 删除草稿并返回")
		}
		fmt.Fprintln(output, "  0. 返回 Web 服务菜单")
		fmt.Fprint(output, "请选择：")
		if !input.Scan() {
			return deployment, webState, state, false, false, false, false, input.Err()
		}
		switch strings.TrimSpace(input.Text()) {
		case "1":
			showKomari(output, candidate, candidateWeb, changed)
		case "2":
			nextDeployment, nextWeb, next, accepted, err := configureKomari(ctx, prompter, stateDirectory, candidateDeployment, candidateWeb, candidate, nginxInstalled)
			if err != nil {
				fmt.Fprintln(output, "无法配置 Komari：", err)
				continue
			}
			if !accepted {
				continue
			}
			candidateDeployment, candidateWeb, candidate, changed = nextDeployment, nextWeb, next, true
			fmt.Fprintln(output, "⚠ Komari 配置已暂存，尚未写入服务、Nginx 或 UFW；可选择 6 保存应用、7 删除草稿，或 0 返回时再决定。")
		case "3":
			candidateWeb = removeKomariProxy(candidateWeb, candidate.ProxyID)
			candidate = komari.NewState()
			changed = true
			fmt.Fprintln(output, "⚠ 停止操作已暂存，尚未停止服务或修改 Nginx/UFW；可选择 6 保存应用、7 删除草稿，或 0 返回时再决定。")
		case "4":
			if changed {
				fmt.Fprintln(output, "请先保存或删除当前草稿，再更新 Komari 程序。")
				continue
			}
			return deployment, webState, state, false, true, false, false, nil
		case "5":
			if changed {
				fmt.Fprintln(output, "请先保存或删除当前草稿，再彻底卸载 Komari。")
				continue
			}
			choice, err := prompter.ask("1 保留 Komari 数据  2 删除 Komari 数据  0 取消：")
			if err != nil {
				return deployment, webState, state, false, false, false, false, err
			}
			switch choice {
			case "1":
				return deployment, removeKomariProxy(webState, state.ProxyID), state, false, false, true, false, nil
			case "2":
				return deployment, removeKomariProxy(webState, state.ProxyID), state, false, false, true, true, nil
			case "", "0":
				continue
			default:
				fmt.Fprintln(output, "无效选择。")
			}
		case "6":
			if changed {
				return candidateDeployment, candidateWeb, candidate, true, false, false, false, nil
			}
			fmt.Fprintln(output, "当前没有草稿需要保存。")
		case "7":
			if changed {
				fmt.Fprintln(output, "草稿已删除，已生效的 Komari、Nginx 和防火墙保持不变。")
				return deployment, webState, state, false, false, false, false, nil
			}
			fmt.Fprintln(output, "当前没有草稿需要删除。")
		case "0":
			if !changed {
				return deployment, webState, state, false, false, false, false, nil
			}
			choice, err := prompter.ask("当前已经设置了草稿：1 保存并应用  2 删除草稿  0 继续编辑：")
			if err != nil {
				return deployment, webState, state, false, false, false, false, err
			}
			switch choice {
			case "1":
				return candidateDeployment, candidateWeb, candidate, true, false, false, false, nil
			case "2":
				fmt.Fprintln(output, "草稿已删除，已生效的 Komari、Nginx 和防火墙保持不变。")
				return deployment, webState, state, false, false, false, false, nil
			case "", "0":
				continue
			default:
				fmt.Fprintln(output, "无效选择。")
			}
		default:
			fmt.Fprintln(output, "无效选择。")
		}
	}
}

func configureKomari(ctx context.Context, prompter *prompt, stateDirectory string, deployment model.State, webState web.State, current komari.State, nginxInstalled bool) (model.State, web.State, komari.State, bool, error) {
	choice, err := prompter.ask("访问方式：1 直接用 IP:端口访问  2 域名 HTTPS 反向代理  0 取消：")
	if err != nil || choice == "0" || choice == "" {
		return deployment, webState, current, false, err
	}
	baseWeb := removeKomariProxy(webState, current.ProxyID)
	switch choice {
	case "1":
		port, accepted, err := promptWebPort(prompter, "Komari 对外监听端口", current.ListenPort, 25774)
		if err != nil || !accepted {
			return deployment, webState, current, false, err
		}
		if !(current.Enabled && current.Mode == komari.ModeDirect && current.ListenPort == port) && !portFree("tcp", port) {
			return deployment, webState, current, false, fmt.Errorf("端口 %d 已被其他服务占用", port)
		}
		return deployment, baseWeb, komari.State{Schema: komari.Schema, Enabled: true, Mode: komari.ModeDirect, ListenHost: "0.0.0.0", ListenPort: port}, true, nil
	case "2":
		if !nginxInstalled {
			fmt.Fprintln(prompter.output, "未安装 Nginx；请先返回“Web 服务与反向代理”菜单，选择“1. 安装 Nginx”，再配置域名 HTTPS 方式。")
			return deployment, webState, current, false, nil
		}
		domain, err := prompter.ask("访问域名（填写自己控制的域名；输入 0 取消）：")
		if err != nil || domain == "0" || domain == "" {
			return deployment, webState, current, false, err
		}
		for _, proxy := range baseWeb.Proxies {
			if strings.EqualFold(proxy.Domain, domain) {
				return deployment, webState, current, false, fmt.Errorf("域名 %s 已被受管反向代理使用", domain)
			}
		}
		nextDeployment, certificateID, accepted, err := ensureTLSCertificate(ctx, prompter, stateDirectory, deployment, domain)
		if err != nil || !accepted {
			return deployment, webState, current, false, err
		}
		externalPort, accepted, err := promptWebPort(prompter, "Nginx HTTPS 监听端口", 0, 443)
		if err != nil || !accepted {
			return deployment, webState, current, false, err
		}
		if err := checkNewWebPort(externalPort, baseWeb); err != nil {
			return deployment, webState, current, false, err
		}
		internalPort, accepted, err := promptWebPort(prompter, "Komari 内部监听端口", current.ListenPort, 25774)
		if err != nil || !accepted {
			return deployment, webState, current, false, err
		}
		if !(current.Enabled && current.ListenPort == internalPort) && !portFree("tcp", internalPort) {
			return deployment, webState, current, false, fmt.Errorf("内部端口 %d 已被其他服务占用", internalPort)
		}
		identifier := make([]byte, 4)
		if _, err := rand.Read(identifier); err != nil {
			return deployment, webState, current, false, err
		}
		proxyID := "web_" + hex.EncodeToString(identifier)
		proxy := web.Proxy{ID: proxyID, Domain: strings.ToLower(domain), ListenPort: externalPort, TargetHost: "127.0.0.1", TargetPort: internalPort, CertificateID: certificateID}
		baseWeb.Proxies = append(baseWeb.Proxies, proxy)
		if err := baseWeb.Validate(nextDeployment.Certificates); err != nil {
			return deployment, webState, current, false, err
		}
		return nextDeployment, baseWeb, komari.State{Schema: komari.Schema, Enabled: true, Mode: komari.ModeDomain, ListenHost: "127.0.0.1", ListenPort: internalPort, Domain: strings.ToLower(domain), ProxyID: proxyID}, true, nil
	default:
		return deployment, webState, current, false, fmt.Errorf("无效选择")
	}
}

func removeKomariProxy(state web.State, id string) web.State {
	if id == "" {
		return state
	}
	result := web.State{Schema: state.Schema, Proxies: make([]web.Proxy, 0, len(state.Proxies))}
	for _, proxy := range state.Proxies {
		if proxy.ID != id {
			result.Proxies = append(result.Proxies, proxy)
		}
	}
	return result
}

func showKomari(output io.Writer, state komari.State, webState web.State, draft bool) {
	if !state.Enabled {
		if draft {
			fmt.Fprintln(output, "\nKomari 草稿：停止（尚未生效）。")
		} else {
			fmt.Fprintln(output, "\nKomari：未启用。")
		}
		return
	}
	label := "Komari 当前配置"
	if draft {
		label = "Komari 配置草稿（尚未生效）"
	}
	fmt.Fprintf(output, "\n%s\n  监听：%s\n", label, endpoint(state.ListenHost, state.ListenPort))
	if state.Mode == komari.ModeDirect {
		fmt.Fprintf(output, "  访问方式：直接 IP:端口（http://服务器IP:%d）\n", state.ListenPort)
		return
	}
	for _, proxy := range webState.Proxies {
		if proxy.ID == state.ProxyID {
			fmt.Fprintf(output, "  访问方式：域名 HTTPS（%s）\n  证书：%s\n", httpsURL(state.Domain, proxy.ListenPort), proxy.CertificateID)
			return
		}
	}
	fmt.Fprintf(output, "  访问方式：域名 HTTPS（%s；关联反向代理缺失）\n", state.Domain)
}
