package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/sherlock-wong/vps-net-manager/internal/app"
	"github.com/sherlock-wong/vps-net-manager/internal/bbr"
	"github.com/sherlock-wong/vps-net-manager/internal/model"
	"github.com/sherlock-wong/vps-net-manager/internal/platform"
	"github.com/sherlock-wong/vps-net-manager/internal/reality"
	"github.com/sherlock-wong/vps-net-manager/internal/subscription"
	"github.com/sherlock-wong/vps-net-manager/internal/ui"
	"github.com/sherlock-wong/vps-net-manager/internal/web"
	"golang.org/x/term"
)

var (
	sourceCommit = "development"
	builtAt      = "unknown"
)

func main() {
	if len(os.Args) < 2 {
		menu()
		return
	}
	switch os.Args[1] {
	case "install":
		install(os.Args[2:])
	case "apply":
		apply(os.Args[2:])
	case "update":
		update(os.Args[2:])
	case "uninstall":
		uninstall(os.Args[2:])
	case "bbr":
		bbrCommand(os.Args[2:])
	case "realm":
		realmCommand(os.Args[2:])
	case "cert":
		certCommand(os.Args[2:])
	case "reality":
		if len(os.Args) >= 3 && os.Args[2] == "scan" {
			realityScan(os.Args[3:])
			return
		}
		usage()
		os.Exit(2)
	case "state":
		if len(os.Args) >= 3 && os.Args[2] == "validate" {
			stateValidate(os.Args[3:])
			return
		}
		usage()
		os.Exit(2)
	case "reality-scan": // temporary compatibility with the initial Go scanner
		realityScan(os.Args[2:])
	case "targets-validate":
		targetsValidate(os.Args[2:])
	case "version":
		version(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
}

func menu() {
	stateDirectory := "/etc/vps-net-manager"
	scanner := bufio.NewScanner(os.Stdin)
	for {
		state, err := app.LoadState(stateDirectory + "/state.json")
		if err != nil {
			fmt.Fprintln(os.Stderr, "vpnm:", err)
			fmt.Fprintln(os.Stderr, "请先运行：vm install")
			return
		}
		fmt.Println("\nVPS Net Manager")
		fmt.Printf("  Vless-Reality：%s\n", protocolStatus(state.Protocols.VLESSReality != nil, state.Protocols.VLESSReality != nil && state.Protocols.VLESSReality.Enabled))
		fmt.Printf("  Hysteria2：%s\n", protocolStatus(state.Protocols.Hysteria2 != nil, state.Protocols.Hysteria2 != nil && state.Protocols.Hysteria2.Enabled))
		fmt.Printf("  AnyTLS：%s\n", protocolStatus(state.Protocols.AnyTLS != nil, state.Protocols.AnyTLS != nil && state.Protocols.AnyTLS.Enabled))
		fmt.Println("\n  1. 查看当前配置")
		fmt.Println("  2. 重新应用当前配置")
		fmt.Println("  3. 管理协议")
		fmt.Println("  4. Realm 端口转发")
		fmt.Println("  5. BBR 管理")
		fmt.Println("  6. 显示分享链接和二维码")
		fmt.Println("  7. Web 服务与反向代理")
		fmt.Println("  8. 证书管理")
		fmt.Println("  9. 查看原始 JSON 配置")
		fmt.Println("  0. 退出")
		fmt.Print("请选择：")
		if !scanner.Scan() {
			return
		}
		switch scanner.Text() {
		case "1":
			showCurrentConfiguration(state)
		case "2":
			if os.Geteuid() != 0 {
				fmt.Fprintln(os.Stderr, "vpnm: 重新应用必须以 root 运行")
				continue
			}
			if _, err := app.DefaultApplyOptions(stateDirectory, &state).Apply(context.Background(), state); err != nil {
				fmt.Fprintln(os.Stderr, "vpnm:", err)
			} else {
				fmt.Println("配置已应用。")
			}
		case "3":
			if os.Geteuid() != 0 {
				fmt.Fprintln(os.Stderr, "vpnm: 管理协议必须以 root 运行")
				continue
			}
			candidate, changed, err := ui.EditProtocols(context.Background(), scanner, os.Stdout, stateDirectory, state)
			if err != nil {
				fmt.Fprintln(os.Stderr, "vpnm:", err)
				continue
			}
			if !changed {
				continue
			}
			if _, err := app.DefaultApplyOptions(stateDirectory, &state).Apply(context.Background(), candidate); err != nil {
				fmt.Fprintln(os.Stderr, "vpnm:", err)
			} else {
				fmt.Println("协议配置已成功应用，以下为当前生效配置：")
				showCurrentConfiguration(candidate)
				showProtocolChangeGuide(candidate)
			}
		case "4":
			if os.Geteuid() != 0 {
				fmt.Fprintln(os.Stderr, "vpnm: Realm 管理必须以 root 运行")
				continue
			}
			realmState, err := app.LoadRealmState(stateDirectory + "/realm.json")
			if err != nil {
				fmt.Fprintln(os.Stderr, "vm: Realm 尚未安装；请先运行：vm realm install")
				continue
			}
			candidate, changed, err := ui.EditRealm(context.Background(), scanner, os.Stdout, realmState)
			if err != nil {
				fmt.Fprintln(os.Stderr, "vpnm:", err)
				continue
			}
			if !changed {
				continue
			}
			if err := app.DefaultRealmApplyOptions(stateDirectory).Apply(context.Background(), candidate); err != nil {
				fmt.Fprintln(os.Stderr, "vpnm:", err)
			} else {
				fmt.Println("Realm 规则已应用。")
			}
		case "5":
			bbrMenu(scanner)
		case "6":
			registry, err := app.DefaultRegistry()
			if err != nil {
				fmt.Fprintln(os.Stderr, "vpnm:", err)
				continue
			}
			links, err := subscription.Render(state, registry)
			if err != nil {
				fmt.Fprintln(os.Stderr, "vpnm:", err)
				continue
			}
			if len(links.Links) == 0 {
				if len(links.MissingAddresses) > 0 {
					fmt.Printf("以下已启用协议尚未设置分享链接对外地址：%s；请在“管理协议”中分别设置。\n", strings.Join(links.MissingAddresses, "、"))
					continue
				}
				fmt.Println("当前没有可导出的已启用协议。")
				continue
			}
			if err := ui.PrintShareLinks(os.Stdout, links.Links); err != nil {
				fmt.Fprintln(os.Stderr, "vpnm:", err)
			}
			if len(links.MissingAddresses) > 0 {
				fmt.Printf("未导出以下已启用协议（尚未设置分享链接对外地址）：%s。\n", strings.Join(links.MissingAddresses, "、"))
			}
		case "7":
			if os.Geteuid() != 0 {
				fmt.Fprintln(os.Stderr, "vpnm: Web 服务与反向代理管理必须以 root 运行")
				continue
			}
			webMenu(scanner, stateDirectory)
		case "8":
			if os.Geteuid() != 0 {
				fmt.Fprintln(os.Stderr, "vpnm: 证书管理必须以 root 运行")
				continue
			}
			certificateMenu(scanner, stateDirectory)
		case "9":
			showRawConfiguration(state)
		case "0":
			return
		default:
			fmt.Println("无效选择。")
		}
	}
}

func webMenu(scanner *bufio.Scanner, stateDirectory string) {
	for {
		state, err := app.LoadState(filepath.Join(stateDirectory, "state.json"))
		if err != nil {
			fmt.Fprintln(os.Stderr, "vpnm:", err)
			return
		}
		fmt.Println("\nWeb 服务与反向代理")
		fmt.Println("  1. 安装 Nginx")
		fmt.Println("  2. 管理 HTTPS 反向代理")
		fmt.Println("  3. Komari 监控")
		fmt.Println("  4. 移除全部 VPNM 反向代理规则")
		fmt.Println("  5. 卸载 Nginx")
		fmt.Println("  0. 返回主菜单")
		fmt.Print("请选择：")
		if !scanner.Scan() {
			return
		}
		switch strings.TrimSpace(scanner.Text()) {
		case "1":
			fmt.Println("将通过系统 APT 安装 Nginx；不会修改已有站点配置。")
			if err := (app.NginxInstaller{}).Install(context.Background()); err != nil {
				fmt.Fprintln(os.Stderr, "vpnm:", err)
				continue
			}
			fmt.Println("Nginx 已安装。添加反向代理时将只写入 /etc/nginx/conf.d/vps-net-manager.conf。")
		case "2":
			if !(app.NginxInstaller{}).Installed(context.Background()) {
				fmt.Fprintln(os.Stderr, "vpnm: 未安装 Nginx；请先在本菜单选择“1. 安装 Nginx”，再管理 HTTPS 反向代理。")
				continue
			}
			webState, err := app.LoadWebStateOrEmpty(filepath.Join(stateDirectory, app.DefaultWebStateFile), state.Certificates)
			if err != nil {
				fmt.Fprintln(os.Stderr, "vpnm:", err)
				continue
			}
			candidateState, candidateWeb, changed, err := ui.EditWeb(context.Background(), scanner, os.Stdout, stateDirectory, state, webState)
			if err != nil {
				fmt.Fprintln(os.Stderr, "vpnm:", err)
				continue
			}
			if !changed {
				continue
			}
			if !reflect.DeepEqual(state, candidateState) {
				if _, err := app.DefaultApplyOptions(stateDirectory, &state).Apply(context.Background(), candidateState); err != nil {
					fmt.Fprintln(os.Stderr, "vpnm: 保存证书库变更失败：", err)
					continue
				}
			}
			if err := app.DefaultWebApplyOptions(stateDirectory).Apply(context.Background(), webState, candidateWeb, candidateState.Certificates); err != nil {
				fmt.Fprintln(os.Stderr, "vpnm:", err)
				continue
			}
			fmt.Println("HTTPS 反向代理已成功应用。")
		case "3":
			webState, err := app.LoadWebStateOrEmpty(filepath.Join(stateDirectory, app.DefaultWebStateFile), state.Certificates)
			if err != nil {
				fmt.Fprintln(os.Stderr, "vpnm:", err)
				continue
			}
			komariState, err := app.LoadKomariStateOrEmpty(filepath.Join(stateDirectory, app.DefaultKomariStateFile))
			if err != nil {
				fmt.Fprintln(os.Stderr, "vpnm:", err)
				continue
			}
			nginxInstalled := (app.NginxInstaller{}).Installed(context.Background())
			candidateState, candidateWeb, candidateKomari, changed, updateRequested, uninstallRequested, deleteKomariData, err := ui.EditKomari(context.Background(), scanner, os.Stdout, stateDirectory, state, webState, komariState, nginxInstalled)
			if err != nil {
				fmt.Fprintln(os.Stderr, "vpnm:", err)
				continue
			}
			if changed && candidateKomari.Enabled && candidateKomari.Mode == "domain" && !nginxInstalled {
				fmt.Fprintln(os.Stderr, "vpnm: 未安装 Nginx；请先在本菜单选择“1. 安装 Nginx”，再配置域名 HTTPS 方式。")
				continue
			}
			if !changed && !updateRequested && !uninstallRequested {
				continue
			}
			if uninstallRequested {
				if !reflect.DeepEqual(webState, candidateWeb) {
					if err := app.DefaultWebApplyOptions(stateDirectory).Apply(context.Background(), webState, candidateWeb, state.Certificates); err != nil {
						fmt.Fprintln(os.Stderr, "vpnm: 移除 Komari 反向代理失败，已取消卸载：", err)
						continue
					}
				}
				if err := app.UninstallKomari(context.Background(), stateDirectory, "/etc/systemd/system", komariState, deleteKomariData, nil, app.UFWController{}); err != nil {
					fmt.Fprintln(os.Stderr, "vpnm:", err)
					continue
				}
				if deleteKomariData {
					fmt.Println("Komari 已彻底卸载，数据已删除。")
				} else {
					fmt.Println("Komari 已彻底卸载，数据保留在 /etc/vps-net-manager/komari。")
				}
				continue
			}
			host, err := platform.InspectSupportedHost()
			if err != nil {
				fmt.Fprintln(os.Stderr, "vpnm:", err)
				continue
			}
			if _, err := (app.KomariInstaller{StateDirectory: stateDirectory, UnitDirectory: "/etc/systemd/system"}).Install(context.Background(), host.Architecture); err != nil {
				fmt.Fprintln(os.Stderr, "vpnm: 安装 Komari 失败：", err)
				continue
			}
			if updateRequested {
				if err := app.RestartKomari(context.Background(), komariState, nil); err != nil {
					fmt.Fprintln(os.Stderr, "vpnm: Komari 程序已替换，但重启失败：", err)
					continue
				}
				fmt.Println("Komari 已更新到当前管理器锁定版本。")
				continue
			}
			if !reflect.DeepEqual(state, candidateState) {
				if _, err := app.DefaultApplyOptions(stateDirectory, &state).Apply(context.Background(), candidateState); err != nil {
					fmt.Fprintln(os.Stderr, "vpnm: 保存证书库变更失败：", err)
					continue
				}
			}
			if err := app.DefaultKomariApplyOptions(stateDirectory).Apply(context.Background(), komariState, candidateKomari); err != nil {
				fmt.Fprintln(os.Stderr, "vpnm:", err)
				continue
			}
			if !reflect.DeepEqual(webState, candidateWeb) {
				if err := app.DefaultWebApplyOptions(stateDirectory).Apply(context.Background(), webState, candidateWeb, candidateState.Certificates); err != nil {
					fmt.Fprintln(os.Stderr, "vpnm: Komari 已更新，但 Nginx 反向代理未成功应用：", err)
					continue
				}
			}
			fmt.Println("Komari 已成功应用。首次管理员账号信息请执行：journalctl -u vps-net-manager-komari --no-pager -n 80")
		case "4":
			removeAllWebProxies(scanner, stateDirectory, state)
		case "5":
			uninstallNginxMenu(scanner, stateDirectory, state)
		case "0":
			return
		default:
			fmt.Println("无效选择。")
		}
	}
}

func removeAllWebProxies(scanner *bufio.Scanner, stateDirectory string, state model.State) {
	webState, err := app.LoadWebStateOrEmpty(filepath.Join(stateDirectory, app.DefaultWebStateFile), state.Certificates)
	if err != nil {
		fmt.Fprintln(os.Stderr, "vpnm:", err)
		return
	}
	if len(webState.Proxies) == 0 {
		fmt.Println("当前没有 VPNM 管理的反向代理规则。")
		return
	}
	if komariState, err := app.LoadKomariStateOrEmpty(filepath.Join(stateDirectory, app.DefaultKomariStateFile)); err != nil {
		fmt.Fprintln(os.Stderr, "vpnm:", err)
		return
	} else if komariState.Enabled && komariState.Mode == "domain" {
		fmt.Println("Komari 当前使用域名 HTTPS 反向代理；请先在 Komari 菜单停止它或改为 IP:端口访问，再移除全部反代规则。")
		return
	}
	value, ok := promptMenuValue(scanner, fmt.Sprintf("将移除 %d 条 VPNM 反向代理规则并关闭对应 UFW 端口。输入 DELETE 确认：", len(webState.Proxies)))
	if !ok || value != "DELETE" {
		fmt.Println("已取消。")
		return
	}
	if err := app.DefaultWebApplyOptions(stateDirectory).Apply(context.Background(), webState, web.NewState(), state.Certificates); err != nil {
		fmt.Fprintln(os.Stderr, "vpnm:", err)
		return
	}
	fmt.Println("已移除全部 VPNM 反向代理规则；Nginx 软件包和其他站点配置保持不变。")
}

func uninstallNginxMenu(scanner *bufio.Scanner, stateDirectory string, state model.State) {
	installer := app.NginxInstaller{}
	if !installer.Installed(context.Background()) {
		fmt.Println("Nginx 尚未安装。")
		return
	}
	komariState, err := app.LoadKomariStateOrEmpty(filepath.Join(stateDirectory, app.DefaultKomariStateFile))
	if err != nil {
		fmt.Fprintln(os.Stderr, "vpnm:", err)
		return
	}
	if komariState.Enabled && komariState.Mode == "domain" {
		fmt.Println("Komari 当前使用域名 HTTPS 反向代理；请先在 Komari 菜单停止它或改为 IP:端口访问，再卸载 Nginx。")
		return
	}
	webState, err := app.LoadWebStateOrEmpty(filepath.Join(stateDirectory, app.DefaultWebStateFile), state.Certificates)
	if err != nil {
		fmt.Fprintln(os.Stderr, "vpnm:", err)
		return
	}
	configs, err := installer.UnmanagedConfigFiles()
	if err != nil {
		fmt.Fprintln(os.Stderr, "vpnm:", err)
		return
	}
	fmt.Println("\n即将停止并通过 APT purge 卸载 Nginx 与 nginx-common。VPNM 的反向代理规则也会被移除。")
	if len(configs) > 0 {
		fmt.Println("检测到以下非 VPNM Nginx 配置；它们不会由 VPNM 删除，但相关站点会因 Nginx 被卸载而停止：")
		for _, path := range configs {
			fmt.Println("  -", path)
		}
	}
	value, ok := promptMenuValue(scanner, "输入 UNINSTALL NGINX 确认：")
	if !ok || value != "UNINSTALL NGINX" {
		fmt.Println("已取消。")
		return
	}
	if len(webState.Proxies) > 0 {
		if err := app.DefaultWebApplyOptions(stateDirectory).Apply(context.Background(), webState, web.NewState(), state.Certificates); err != nil {
			fmt.Fprintln(os.Stderr, "vpnm: 移除 VPNM 反向代理失败，已取消卸载 Nginx：", err)
			return
		}
	}
	if err := installer.Uninstall(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "vpnm:", err)
		return
	}
	fmt.Println("Nginx 已卸载；VPNM 反向代理规则也已移除。")
}

func showCurrentConfiguration(state model.State) {
	if configuration := state.Protocols.VLESSReality; configuration != nil {
		fmt.Printf("\nVless-Reality（%s）\n  分享地址：%s\n  端口：%d/TCP\n  内核：%s\n  UUID：%s\n  Reality SNI：%s\n  Public Key：%s\n  Short ID：%s\n", protocolStatus(true, configuration.Enabled), valueOrUnset(configuration.PublicAddress), configuration.Port, configuration.Engine, configuration.UUID, configuration.SNI, configuration.PublicKey, configuration.ShortID)
	}
	if configuration := state.Protocols.Hysteria2; configuration != nil {
		certificate := state.Certificates[configuration.CertificateID]
		fmt.Printf("\nHysteria2（%s）\n  分享地址：%s\n  端口：%d/UDP\n  TLS 域名：%s\n  证书：%s（%s）\n  密码：%s\n", protocolStatus(true, configuration.Enabled), valueOrUnset(configuration.PublicAddress), configuration.Port, configuration.Domain, certificate.Name, configuration.CertificateID, configuration.Password)
	}
	if configuration := state.Protocols.AnyTLS; configuration != nil {
		certificate := state.Certificates[configuration.CertificateID]
		fmt.Printf("\nAnyTLS（%s）\n  分享地址：%s\n  端口：%d/TCP\n  TLS 域名：%s\n  证书：%s（%s）\n  密码：%s\n", protocolStatus(true, configuration.Enabled), valueOrUnset(configuration.PublicAddress), configuration.Port, configuration.Domain, certificate.Name, configuration.CertificateID, configuration.Password)
	}
	if state.Protocols.VLESSReality == nil && state.Protocols.Hysteria2 == nil && state.Protocols.AnyTLS == nil {
		fmt.Println("\n当前没有已添加的协议配置。")
	}
}

func showProtocolChangeGuide(state model.State) {
	fmt.Println("\n后续可在“管理协议”中调整：")
	if state.Protocols.VLESSReality != nil {
		fmt.Println("  Vless-Reality：启停、端口、UUID、Reality SNI、Reality 密钥与 Short ID、服务端内核、分享地址。")
	}
	if state.Protocols.Hysteria2 != nil {
		fmt.Println("  Hysteria2：启停、端口、密码、TLS 域名、证书绑定和分享地址。")
	}
	if state.Protocols.AnyTLS != nil {
		fmt.Println("  AnyTLS：启停、端口、密码、TLS 域名、证书绑定和分享地址。")
	}
}

func showRawConfiguration(state model.State) {
	encoded, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "vpnm:", err)
		return
	}
	fmt.Printf("\n%s\n", encoded)
}

func certificateMenu(scanner *bufio.Scanner, stateDirectory string) {
	for {
		state, err := app.LoadState(stateDirectory + "/state.json")
		if err != nil {
			fmt.Fprintln(os.Stderr, "vpnm:", err)
			return
		}
		fmt.Println("\n证书管理")
		fmt.Println("  1. 查看证书库和协议绑定")
		fmt.Println("  2. 导入已有证书和私钥")
		fmt.Println("  3. 创建固定自签证书")
		fmt.Println("  4. 立即同步受管证书来源")
		fmt.Println("  5. 管理证书来源路径")
		fmt.Println("  6. 申请域名证书（ACME）")
		fmt.Println("  7. 更换 Cloudflare DNS API Token")
		fmt.Println("  0. 返回主菜单")
		fmt.Print("请选择：")
		if !scanner.Scan() {
			return
		}
		switch strings.TrimSpace(scanner.Text()) {
		case "1":
			showCertificateLibrary(state)
		case "2":
			importCertificateMenu(scanner, stateDirectory, state)
		case "3":
			createPinnedCertificateMenu(scanner, stateDirectory, state)
		case "4":
			changed, err := app.SyncCertificates(context.Background(), state, time.Now(), app.DefaultApplyOptions(stateDirectory, &state))
			if err != nil {
				fmt.Fprintln(os.Stderr, "vpnm:", err)
			} else if changed {
				fmt.Println("证书已同步并重新应用配置。")
			} else {
				fmt.Println("没有配置可同步的证书。")
			}
		case "5":
			manageCertificateSourceMenu(scanner, stateDirectory, state)
		case "6":
			acmeCertificateMenu(scanner, stateDirectory, state)
		case "7":
			configureCloudflareTokenMenu(scanner, stateDirectory)
		case "0":
			return
		default:
			fmt.Println("无效选择。")
		}
	}
}

func configureCloudflareTokenMenu(scanner *bufio.Scanner, stateDirectory string) {
	fmt.Println("\nCloudflare DNS API Token")
	fmt.Println("此 Token 供当前 VPNM 的 Cloudflare ACME 证书自动续期使用；更换后再撤销旧 Token。")
	accountID, ok := promptMenuValue(scanner, "Cloudflare Account ID（输入 0 取消）：")
	if !ok || accountID == "" || accountID == "0" {
		return
	}
	fmt.Print("Cloudflare DNS API Token（输入不回显）：")
	var token string
	if term.IsTerminal(int(os.Stdin.Fd())) {
		value, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			fmt.Fprintln(os.Stderr, "vm:", err)
			return
		}
		token = strings.TrimSpace(string(value))
	} else {
		if !scanner.Scan() {
			return
		}
		token = strings.TrimSpace(scanner.Text())
	}
	if err := app.UpdateCloudflareCredentials(stateDirectory, accountID, token); err != nil {
		fmt.Fprintln(os.Stderr, "vm:", err)
		return
	}
	fmt.Printf("Cloudflare DNS API 凭据已更新。Token 摘要：%s\n", maskedSecret(token))
	fmt.Println("可运行 vm cert renew 检查续期流程；确认无误后再撤销旧 Token。")
}

func maskedSecret(value string) string {
	const prefixLength = 6
	const suffixLength = 4
	if len(value) <= suffixLength {
		return "****"
	}
	if len(value) <= prefixLength+suffixLength {
		return value[:2] + "****" + value[len(value)-suffixLength:]
	}
	return value[:prefixLength] + "****" + value[len(value)-suffixLength:]
}

func showCertificateLibrary(state model.State) {
	fmt.Println("\n普通 TLS 协议的证书绑定（Vless-Reality 使用 Reality 密钥，不使用此证书库）：")
	if len(state.Certificates) == 0 {
		fmt.Println("  证书库为空。")
		return
	}
	ids := make([]string, 0, len(state.Certificates))
	for id := range state.Certificates {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		item := state.Certificates[id]
		mode := item.Mode
		if mode == "" {
			mode = model.CertificateModeTrusted
		}
		fmt.Printf("\n[%s] %s（%s）\n  证书：%s\n  私钥：%s\n  DER SHA-256：%s\n  SPKI SHA-256：%s\n", id, item.Name, mode, item.Cert, item.Key, valueOrUnset(item.DER_SHA256), valueOrUnset(item.SPKI_SHA256))
		if item.SourceCert != "" {
			fmt.Printf("  受管同步来源：%s\n", item.SourceCert)
		}
	}
	if configuration := state.Protocols.Hysteria2; configuration != nil {
		fmt.Printf("\nHysteria2 绑定：%s（TLS 域名 %s）\n", configuration.CertificateID, configuration.Domain)
	}
	if configuration := state.Protocols.AnyTLS; configuration != nil {
		fmt.Printf("AnyTLS 绑定：%s（TLS 域名 %s）\n", configuration.CertificateID, configuration.Domain)
	}
}

func importCertificateMenu(scanner *bufio.Scanner, stateDirectory string, state model.State) {
	id, ok := promptMenuValue(scanner, "证书 ID（字母、数字、_ 或 -；输入 0 取消）：")
	if !ok || id == "0" {
		return
	}
	name, ok := promptMenuValue(scanner, "证书名称：")
	if !ok || name == "" {
		fmt.Println("证书名称不能为空。")
		return
	}
	certificatePath, ok := promptMenuValue(scanner, "证书 PEM 文件路径：")
	if !ok || certificatePath == "" {
		return
	}
	keyPath, ok := promptMenuValue(scanner, "私钥 PEM 文件路径：")
	if !ok || keyPath == "" {
		return
	}
	modeChoice, ok := promptMenuValue(scanner, "客户端校验：1 受信证书（默认）  2 固定指纹：")
	if !ok {
		return
	}
	mode := model.CertificateModeTrusted
	if modeChoice == "2" {
		mode = model.CertificateModePinned
	} else if modeChoice != "" && modeChoice != "1" {
		fmt.Println("无效选择。")
		return
	}
	followChoice, ok := promptMenuValue(scanner, "跟踪该文件并每 6 小时同步更新？1 是（默认）  0 否：")
	if !ok {
		return
	}
	if followChoice != "" && followChoice != "0" && followChoice != "1" {
		fmt.Println("无效选择。")
		return
	}
	candidate, artifacts, err := app.StageImportedCertificate(context.Background(), stateDirectory, state, id, name, certificatePath, keyPath, mode, followChoice != "0", time.Now())
	if err != nil {
		fmt.Fprintln(os.Stderr, "vpnm:", err)
		return
	}
	options := app.DefaultApplyOptions(stateDirectory, &state)
	options.ExtraArtifacts = artifacts
	if _, err := options.Apply(context.Background(), candidate); err != nil {
		fmt.Fprintln(os.Stderr, "vpnm:", err)
		return
	}
	fmt.Println("证书已导入证书库。可在协议管理中选择并绑定它。")
}

func manageCertificateSourceMenu(scanner *bufio.Scanner, stateDirectory string, state model.State) {
	if len(state.Certificates) == 0 {
		fmt.Println("证书库为空，暂无可管理的来源路径。")
		return
	}
	ids := make([]string, 0, len(state.Certificates))
	for id := range state.Certificates {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	fmt.Println("\n证书来源路径管理")
	for _, id := range ids {
		item := state.Certificates[id]
		if item.SourceCert == "" {
			fmt.Printf("  %s：未跟踪外部来源\n", id)
			continue
		}
		fmt.Printf("  %s：%s\n", id, item.SourceCert)
	}
	id, ok := promptMenuValue(scanner, "证书 ID（输入 0 返回）：")
	if !ok || id == "0" || id == "" {
		return
	}
	item, exists := state.Certificates[id]
	if !exists {
		fmt.Println("未找到该证书 ID。")
		return
	}
	fmt.Printf("\n[%s] %s\n  服务证书路径：%s\n  服务私钥路径：%s\n  当前来源证书：%s\n  当前来源私钥：%s\n", id, item.Name, item.Cert, item.Key, valueOrUnset(item.SourceCert), valueOrUnset(item.SourceKey))
	choice, ok := promptMenuValue(scanner, "1 设置/更换来源路径并立即同步  2 停止追踪来源  0 返回：")
	if !ok || choice == "0" || choice == "" {
		return
	}
	options := app.DefaultApplyOptions(stateDirectory, &state)
	switch choice {
	case "1":
		certificatePath, ok := promptMenuValue(scanner, "来源证书 PEM 路径（输入 0 取消）：")
		if !ok || certificatePath == "0" || certificatePath == "" {
			return
		}
		keyPath, ok := promptMenuValue(scanner, "来源私钥 PEM 路径（输入 0 取消）：")
		if !ok || keyPath == "0" || keyPath == "" {
			return
		}
		candidate, artifacts, err := app.StageCertificateSourceUpdate(context.Background(), stateDirectory, state, id, certificatePath, keyPath, time.Now())
		if err != nil {
			fmt.Fprintln(os.Stderr, "vpnm:", err)
			return
		}
		options.ExtraArtifacts = artifacts
		if _, err := options.Apply(context.Background(), candidate); err != nil {
			fmt.Fprintln(os.Stderr, "vpnm:", err)
			return
		}
		fmt.Println("证书来源路径已更新，并已同步到服务证书目录。")
	case "2":
		candidate, err := app.StopCertificateSourceTracking(state, id)
		if err != nil {
			fmt.Fprintln(os.Stderr, "vpnm:", err)
			return
		}
		if _, err := options.Apply(context.Background(), candidate); err != nil {
			fmt.Fprintln(os.Stderr, "vpnm:", err)
			return
		}
		fmt.Println("已停止追踪外部来源；当前服务证书会保留。")
	default:
		fmt.Println("无效选择。")
	}
}

func createPinnedCertificateMenu(scanner *bufio.Scanner, stateDirectory string, state model.State) {
	id, ok := promptMenuValue(scanner, "证书 ID（字母、数字、_ 或 -；输入 0 取消）：")
	if !ok || id == "0" {
		return
	}
	name, ok := promptMenuValue(scanner, "证书名称：")
	if !ok || name == "" {
		fmt.Println("证书名称不能为空。")
		return
	}
	domain, ok := promptMenuValue(scanner, "证书域名：")
	if !ok || domain == "" {
		fmt.Println("证书域名不能为空。")
		return
	}
	candidate, artifacts, err := app.StagePinnedCertificate(context.Background(), stateDirectory, state, id, name, domain, time.Now())
	if err != nil {
		fmt.Fprintln(os.Stderr, "vpnm:", err)
		return
	}
	options := app.DefaultApplyOptions(stateDirectory, &state)
	options.ExtraArtifacts = artifacts
	if _, err := options.Apply(context.Background(), candidate); err != nil {
		fmt.Fprintln(os.Stderr, "vpnm:", err)
		return
	}
	fmt.Println("固定自签证书已创建。绑定协议后，请通过重新生成的分享链接导入客户端指纹。")
}

func acmeCertificateMenu(scanner *bufio.Scanner, stateDirectory string, state model.State) {
	fmt.Println("\n申请域名证书（ACME）")
	fmt.Println("Cloudflare DNS 请保持灰云；接下来锁定版本的 ACME 脚本会直接在终端中询问验证方式。")
	domain, ok := promptMenuValue(scanner, "证书域名（输入 0 取消）：")
	if !ok || domain == "" || domain == "0" {
		return
	}
	id, ok := promptMenuValue(scanner, "证书 ID（回车使用域名转换值；输入 0 取消）：")
	if !ok || id == "0" {
		return
	}
	if id == "" {
		id = strings.ReplaceAll(domain, ".", "-")
	}
	_, registered := state.Certificates[id]
	targetDirectory := filepath.Join(stateDirectory, "certs", id)
	acmeDirectory := filepath.Join(stateDirectory, "acme", id)
	_, certificateExists := os.Stat(filepath.Join(targetDirectory, "fullchain.pem"))
	_, keyExists := os.Stat(filepath.Join(targetDirectory, "privkey.pem"))
	_, acmeCertificateExists := os.Stat(filepath.Join(acmeDirectory, "fullchain.pem"))
	_, acmeKeyExists := os.Stat(filepath.Join(acmeDirectory, "privkey.pem"))
	overwrite := registered || certificateExists == nil || keyExists == nil || acmeCertificateExists == nil || acmeKeyExists == nil
	if overwrite {
		fmt.Printf("检测到证书 ID 或保存路径已存在：%s\n", targetDirectory)
		choice, ok := promptMenuValue(scanner, "1 覆盖现有证书  0 取消：")
		if !ok || choice != "1" {
			fmt.Println("已取消，不会覆盖现有证书。")
			return
		}
	}
	certificatePath := filepath.Join(acmeDirectory, "fullchain.pem")
	keyPath := filepath.Join(acmeDirectory, "privkey.pem")
	fmt.Printf("即将启动官方 acme.sh 域名证书流程；证书临时输出到 %s，完成后会验证并归档到 %s。下一层可选 Cloudflare DNS API（推荐）或独立 80 端口验证；不会停止其他服务。\n", acmeDirectory, targetDirectory)
	if _, err := (app.ACMEAdapter{}).RunInteractive(context.Background(), certificatePath, keyPath, domain, time.Now()); err != nil {
		fmt.Fprintln(os.Stderr, "vpnm:", err)
		return
	}
	var candidate model.State
	var artifacts []app.Artifact
	var err error
	if registered {
		candidate, artifacts, err = app.StageReplacementCertificate(context.Background(), stateDirectory, state, id, certificatePath, keyPath, time.Now())
	} else {
		candidate, artifacts, err = app.StageImportedCertificate(context.Background(), stateDirectory, state, id, domain, certificatePath, keyPath, model.CertificateModeTrusted, true, time.Now())
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "vpnm:", err)
		return
	}
	options := app.DefaultApplyOptions(stateDirectory, &state)
	options.ExtraArtifacts = artifacts
	if _, err := options.Apply(context.Background(), candidate); err != nil {
		fmt.Fprintln(os.Stderr, "vpnm:", err)
		return
	}
	fmt.Println("域名证书已验证并导入证书库；请在 Hysteria2 或 AnyTLS 的协议菜单中选择并绑定它。")
}

func promptMenuValue(scanner *bufio.Scanner, label string) (string, bool) {
	fmt.Print(label)
	if !scanner.Scan() {
		return "", false
	}
	return strings.TrimSpace(scanner.Text()), true
}

func bbrMenu(scanner *bufio.Scanner) {
	manager := bbr.Manager{}
	status, err := manager.Status(context.Background())
	if err == nil {
		fmt.Printf("\nBBR：%s（拥塞控制 %s，队列 %s）\n", enabledText(status.Enabled), status.CongestionControl, status.Qdisc)
	} else {
		fmt.Fprintln(os.Stderr, "vpnm:", err)
	}
	fmt.Println("  1. 启用 BBR + fq")
	fmt.Println("  2. 恢复 VPNM 的 BBR 设置")
	fmt.Println("  0. 返回")
	fmt.Print("请选择：")
	if !scanner.Scan() || scanner.Text() == "0" {
		return
	}
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "vpnm: BBR 管理必须以 root 运行")
		return
	}
	switch scanner.Text() {
	case "1":
		if err := manager.Enable(context.Background()); err != nil {
			fmt.Fprintln(os.Stderr, "vpnm:", err)
		} else {
			fmt.Println("BBR 已启用。")
		}
	case "2":
		if err := manager.Restore(context.Background()); err != nil {
			fmt.Fprintln(os.Stderr, "vpnm:", err)
		} else {
			fmt.Println("已恢复 VPNM 的 BBR 设置。")
		}
	default:
		fmt.Println("无效选择。")
	}
}

func protocolStatus(configured, enabled bool) string {
	if !configured {
		return "未添加"
	}
	if enabled {
		return "已启用"
	}
	return "已停用"
}
func valueOrUnset(value string) string {
	if value == "" {
		return "未设置"
	}
	return value
}

func install(arguments []string) {
	flags := flag.NewFlagSet("install", flag.ExitOnError)
	stateDirectory := flags.String("state-dir", "/etc/vps-net-manager", "VPNM state directory")
	unitDirectory := flags.String("unit-dir", "/etc/systemd/system", "systemd unit directory")
	flags.Parse(arguments)
	if os.Geteuid() != 0 {
		fatalIf(fmt.Errorf("install 必须以 root 运行"))
	}
	statePath := *stateDirectory + "/state.json"
	if _, err := os.Stat(statePath); err == nil {
		if _, err := app.LoadState(statePath); err != nil {
			fatalIf(err)
		}
		fatalIf(app.InstallCertificateTimer(context.Background(), *unitDirectory, nil))
		fmt.Println("检测到已有配置，已更新 vm 命令和证书定时服务；未修改节点配置。")
		return
	} else if !os.IsNotExist(err) {
		fatalIf(fmt.Errorf("inspect existing state: %w", err))
	}
	if _, err := app.Bootstrap(context.Background(), *stateDirectory, *unitDirectory, nil); err != nil {
		fatalIf(err)
	}
	fmt.Println("安装完成。当前未添加协议；请稍后运行 vm 管理节点。")
}

func apply(arguments []string) {
	flags := flag.NewFlagSet("apply", flag.ExitOnError)
	stateDirectory := flags.String("state-dir", "/etc/vps-net-manager", "VPNM state directory")
	flags.Parse(arguments)
	if os.Geteuid() != 0 {
		fatalIf(fmt.Errorf("apply 必须以 root 运行"))
	}
	statePath := *stateDirectory + "/state.json"
	state, err := app.LoadState(statePath)
	fatalIf(err)
	if _, err := app.DefaultApplyOptions(*stateDirectory, &state).Apply(context.Background(), state); err != nil {
		fatalIf(err)
	}
	fmt.Println("配置已应用。")
}

func update(arguments []string) {
	flags := flag.NewFlagSet("update", flag.ExitOnError)
	stateDirectory := flags.String("state-dir", "/etc/vps-net-manager", "VPNM state directory")
	binaryPath := flags.String("binary", "/usr/local/bin/vm", "VM executable path")
	flags.Parse(arguments)
	if os.Geteuid() != 0 {
		fatalIf(fmt.Errorf("update 必须以 root 运行"))
	}
	state, err := app.LoadState(*stateDirectory + "/state.json")
	fatalIf(err)
	result, err := (app.Updater{StateDirectory: *stateDirectory, BinaryPath: *binaryPath}).Update(context.Background(), sourceCommit, state)
	fatalIf(err)
	fatalIf(app.InstallCertificateTimer(context.Background(), "/etc/systemd/system", nil))
	if !result.Updated {
		fmt.Printf("已是最新成功构建（%s）。\n", shortCommit(result.SourceCommit))
		return
	}
	fmt.Printf("已更新到 main-%s（sing-box %s，xray %s）。\n", shortCommit(result.SourceCommit), result.Cores.SingBox, result.Cores.Xray)
}

func version(arguments []string) {
	flags := flag.NewFlagSet("version", flag.ExitOnError)
	stateDirectory := flags.String("state-dir", "/etc/vps-net-manager", "VPNM state directory")
	flags.Parse(arguments)
	fmt.Printf("vm main-%s\nsource commit: %s\nbuilt at: %s\n", shortCommit(sourceCommit), sourceCommit, builtAt)
	cores, err := app.LoadInstalledCores(*stateDirectory)
	if err != nil {
		fmt.Println("sing-box: 未安装")
		fmt.Println("xray: 未安装")
		return
	}
	fmt.Printf("sing-box: %s\nxray: %s\n", cores.SingBox, cores.Xray)
}

func uninstall(arguments []string) {
	flags := flag.NewFlagSet("uninstall", flag.ExitOnError)
	yes := flags.Bool("yes", false, "confirm complete VPNM removal")
	flags.Parse(arguments)
	if !*yes {
		fatalIf(fmt.Errorf("卸载会删除 VPNM 状态、项目 unit 和 /usr/local/bin/vm；请使用 vm uninstall --yes 确认"))
	}
	if os.Geteuid() != 0 {
		fatalIf(fmt.Errorf("uninstall 必须以 root 运行"))
	}
	fatalIf((app.Uninstaller{StateDirectory: "/etc/vps-net-manager", UnitDirectory: "/etc/systemd/system", BinaryPath: "/usr/local/bin/vm"}).Uninstall(context.Background()))
	fmt.Println("VPNM 已卸载。")
}

func bbrCommand(arguments []string) {
	if len(arguments) != 1 {
		usage()
		os.Exit(2)
		return
	}
	manager := bbr.Manager{}
	switch arguments[0] {
	case "status":
		status, err := manager.Status(context.Background())
		fatalIf(err)
		fmt.Printf("拥塞控制：%s\n默认队列：%s\nBBR：%s\n", status.CongestionControl, status.Qdisc, enabledText(status.Enabled))
	case "enable":
		if os.Geteuid() != 0 {
			fatalIf(fmt.Errorf("bbr enable 必须以 root 运行"))
		}
		fatalIf(manager.Enable(context.Background()))
		fmt.Println("BBR 已启用。")
	case "restore":
		if os.Geteuid() != 0 {
			fatalIf(fmt.Errorf("bbr restore 必须以 root 运行"))
		}
		fatalIf(manager.Restore(context.Background()))
		fmt.Println("已移除 VPNM 管理的 BBR 设置。")
	default:
		usage()
		os.Exit(2)
	}
}

func realmCommand(arguments []string) {
	if len(arguments) == 0 {
		usage()
		os.Exit(2)
	}
	if arguments[0] == "install" {
		realmInstall(arguments[1:])
		return
	}
	if arguments[0] != "validate" && arguments[0] != "apply" {
		usage()
		os.Exit(2)
	}
	flags := flag.NewFlagSet("realm "+arguments[0], flag.ExitOnError)
	stateDirectory := flags.String("state-dir", "/etc/vps-net-manager", "VPNM state directory")
	statePath := flags.String("state", "", "Realm state JSON path")
	unitDirectory := flags.String("unit-dir", "/etc/systemd/system", "systemd unit directory")
	flags.Parse(arguments[1:])
	if *statePath == "" {
		*statePath = *stateDirectory + "/realm.json"
	}
	state, err := app.LoadRealmState(*statePath)
	fatalIf(err)
	switch arguments[0] {
	case "validate":
		fmt.Printf("Realm 状态校验通过：%d 条规则。\n", len(state.Rules))
	case "apply":
		if os.Geteuid() != 0 {
			fatalIf(fmt.Errorf("realm apply 必须以 root 运行"))
		}
		options := app.DefaultRealmApplyOptions(*stateDirectory)
		options.UnitDirectory = *unitDirectory
		fatalIf(options.Apply(context.Background(), state))
		fmt.Println("Realm 规则已应用。")
	}
}

func realmInstall(arguments []string) {
	flags := flag.NewFlagSet("realm install", flag.ExitOnError)
	stateDirectory := flags.String("state-dir", "/etc/vps-net-manager", "VPNM state directory")
	unitDirectory := flags.String("unit-dir", "/etc/systemd/system", "systemd unit directory")
	flags.Parse(arguments)
	if os.Geteuid() != 0 {
		fatalIf(fmt.Errorf("realm install 必须以 root 运行"))
	}
	host, err := platform.InspectSupportedHost()
	fatalIf(err)
	version, err := (app.RealmInstaller{StateDirectory: *stateDirectory, UnitDirectory: *unitDirectory}).Install(context.Background(), host.Architecture)
	fatalIf(err)
	fmt.Printf("Realm %s 已安装；请添加规则后应用。\n", version)
}

func certCommand(arguments []string) {
	if len(arguments) == 0 {
		usage()
		os.Exit(2)
	}
	if arguments[0] == "acme" {
		certACME(arguments[1:])
		return
	}
	if arguments[0] == "renew" {
		certRenew(arguments[1:])
		return
	}
	if arguments[0] != "sync" {
		usage()
		os.Exit(2)
	}
	flags := flag.NewFlagSet("cert sync", flag.ExitOnError)
	stateDirectory := flags.String("state-dir", "/etc/vps-net-manager", "VPNM state directory")
	quiet := flags.Bool("quiet", false, "suppress success output")
	flags.Parse(arguments[1:])
	if os.Geteuid() != 0 {
		fatalIf(fmt.Errorf("cert sync 必须以 root 运行"))
	}
	state, err := app.LoadState(*stateDirectory + "/state.json")
	fatalIf(err)
	changed, err := app.SyncCertificates(context.Background(), state, time.Now(), app.DefaultApplyOptions(*stateDirectory, &state))
	fatalIf(err)
	if !*quiet {
		if changed {
			fmt.Println("证书已同步并重新应用配置。")
		} else {
			fmt.Println("没有配置可同步的证书。")
		}
	}
}

func certRenew(arguments []string) {
	flags := flag.NewFlagSet("cert renew", flag.ExitOnError)
	stateDirectory := flags.String("state-dir", "/etc/vps-net-manager", "VPNM state directory")
	quiet := flags.Bool("quiet", false, "suppress success output")
	flags.Parse(arguments)
	if os.Geteuid() != 0 {
		fatalIf(fmt.Errorf("cert renew 必须以 root 运行"))
	}
	if err := (app.ACMEAdapter{}).Renew(context.Background(), *stateDirectory); err != nil {
		fatalIf(err)
	}
	state, err := app.LoadState(*stateDirectory + "/state.json")
	fatalIf(err)
	changed, err := app.SyncCertificates(context.Background(), state, time.Now(), app.DefaultApplyOptions(*stateDirectory, &state))
	fatalIf(err)
	if !*quiet {
		if changed {
			fmt.Println("ACME 已续期并同步证书。")
		} else {
			fmt.Println("ACME 已检查；当前无需同步证书。")
		}
	}
}

func certACME(arguments []string) {
	flags := flag.NewFlagSet("cert acme", flag.ExitOnError)
	domain := flags.String("domain", "", "certificate DNS name")
	certificatePath := flags.String("cert", "", "certificate PEM output path")
	keyPath := flags.String("key", "", "private key PEM output path")
	flags.Parse(arguments)
	if os.Geteuid() != 0 {
		fatalIf(fmt.Errorf("cert acme 必须以 root 运行"))
	}
	info, err := (app.ACMEAdapter{}).Run(context.Background(), flags.Args(), *certificatePath, *keyPath, *domain, time.Now())
	fatalIf(err)
	fmt.Printf("ACME 证书已校验：SAN %v，有效期至 %s。\n", info.DNSNames, info.NotAfter.Format(time.RFC3339))
}

func enabledText(enabled bool) string {
	if enabled {
		return "已启用"
	}
	return "未启用"
}

func realityScan(arguments []string) {
	flags := flag.NewFlagSet("reality-scan", flag.ExitOnError)
	targetsPath := flags.String("targets-file", "/etc/vps-net-manager/reality-targets.txt", "target hostname file")
	samples := flags.Int("samples", 3, "samples per target")
	top := flags.Int("top", 0, "number of ranked results (0 shows all)")
	timeout := flags.Duration("timeout", 35*time.Second, "whole scan timeout")
	flags.Parse(arguments)

	targets, err := reality.LoadTargetsOrDefault(*targetsPath)
	fatalIf(err)
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	results, err := reality.Scan(ctx, targets, *samples, *top)
	fatalIf(err)
	fatalIf(json.NewEncoder(os.Stdout).Encode(results))
}

func targetsValidate(arguments []string) {
	flags := flag.NewFlagSet("targets-validate", flag.ExitOnError)
	targetsPath := flags.String("targets-file", "/etc/vps-net-manager/reality-targets.txt", "target hostname file")
	flags.Parse(arguments)
	targets, err := reality.LoadTargetsOrDefault(*targetsPath)
	fatalIf(err)
	for _, target := range targets {
		fmt.Println(target)
	}
}

func stateValidate(arguments []string) {
	flags := flag.NewFlagSet("state validate", flag.ExitOnError)
	statePath := flags.String("state", app.DefaultStatePath, "state JSON path")
	flags.Parse(arguments)
	if _, err := app.LoadState(*statePath); err != nil {
		fatalIf(err)
	}
	fmt.Println("状态校验通过。")
}

func shortCommit(value string) string {
	if len(value) > 7 {
		return value[:7]
	}
	return value
}

func fatalIf(err error) {
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, "vm:", err)
	os.Exit(1)
}

func usage() {
	fmt.Fprintln(os.Stderr, `用法：
  vm install
  vm apply
  vm update
  vm uninstall --yes
  vm cert <sync|renew|acme>
  vm bbr <status|enable|restore>
  vm realm <install|validate|apply>
  vm reality scan
  vm state validate
  vm version`)
}
