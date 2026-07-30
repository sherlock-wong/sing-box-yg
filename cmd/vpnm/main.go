package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
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
		fmt.Println("  7. 修改分享链接对外地址")
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
				fmt.Println("协议配置已应用。")
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
			if !links.AddressAvailable {
				fmt.Println("尚未设置分享链接对外地址；请先选择 7 填写域名、IPv4 或 IPv6。")
				continue
			}
			if len(links.Links) == 0 {
				fmt.Println("当前没有可导出的已启用协议。")
				continue
			}
			if err := ui.PrintShareLinks(os.Stdout, links.Links); err != nil {
				fmt.Fprintln(os.Stderr, "vpnm:", err)
			}
		case "7":
			if os.Geteuid() != 0 {
				fmt.Fprintln(os.Stderr, "vpnm: 修改对外地址必须以 root 运行")
				continue
			}
			fmt.Print("对外地址（域名、IPv4 或 IPv6；输入 0 取消）：")
			if !scanner.Scan() {
				return
			}
			address := strings.TrimSpace(scanner.Text())
			if address == "0" {
				continue
			}
			candidate := state
			candidate.PublicAddress = address
			if _, err := app.DefaultApplyOptions(stateDirectory, &state).Apply(context.Background(), candidate); err != nil {
				fmt.Fprintln(os.Stderr, "vpnm:", err)
			} else {
				fmt.Println("分享链接对外地址已更新。")
			}
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

func showCurrentConfiguration(state model.State) {
	fmt.Printf("\n公网地址：%s\n", valueOrUnset(state.PublicAddress))
	if configuration := state.Protocols.VLESSReality; configuration != nil {
		fmt.Printf("\nVless-Reality（%s）\n  端口：%d/TCP\n  内核：%s\n  UUID：%s\n  Reality SNI：%s\n  Public Key：%s\n  Short ID：%s\n", protocolStatus(true, configuration.Enabled), configuration.Port, configuration.Engine, configuration.UUID, configuration.SNI, configuration.PublicKey, configuration.ShortID)
	}
	if configuration := state.Protocols.Hysteria2; configuration != nil {
		certificate := state.Certificates[configuration.CertificateID]
		fmt.Printf("\nHysteria2（%s）\n  端口：%d/UDP\n  TLS 域名：%s\n  证书：%s（%s）\n  密码：%s\n", protocolStatus(true, configuration.Enabled), configuration.Port, configuration.Domain, certificate.Name, configuration.CertificateID, configuration.Password)
	}
	if configuration := state.Protocols.AnyTLS; configuration != nil {
		certificate := state.Certificates[configuration.CertificateID]
		fmt.Printf("\nAnyTLS（%s）\n  端口：%d/TCP\n  TLS 域名：%s\n  证书：%s（%s）\n  密码：%s\n", protocolStatus(true, configuration.Enabled), configuration.Port, configuration.Domain, certificate.Name, configuration.CertificateID, configuration.Password)
	}
	if state.Protocols.VLESSReality == nil && state.Protocols.Hysteria2 == nil && state.Protocols.AnyTLS == nil {
		fmt.Println("\n当前没有已添加的协议配置。")
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
		fmt.Println("  5. 申请域名证书（ACME）")
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
			acmeCertificateMenu(scanner, stateDirectory, state)
		case "0":
			return
		default:
			fmt.Println("无效选择。")
		}
	}
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
	id, ok := promptMenuValue(scanner, "证书 ID（回车使用域名转换值）：")
	if !ok {
		return
	}
	if id == "" {
		id = strings.ReplaceAll(domain, ".", "-")
	}
	_, registered := state.Certificates[id]
	targetDirectory := filepath.Join(stateDirectory, "certs", id)
	_, certificateExists := os.Stat(filepath.Join(targetDirectory, "fullchain.pem"))
	_, keyExists := os.Stat(filepath.Join(targetDirectory, "privkey.pem"))
	overwrite := registered || certificateExists == nil || keyExists == nil
	if overwrite {
		fmt.Printf("检测到证书 ID 或保存路径已存在：%s\n", targetDirectory)
		choice, ok := promptMenuValue(scanner, "1 覆盖现有证书  0 取消：")
		if !ok || choice != "1" {
			fmt.Println("已取消，不会覆盖现有证书。")
			return
		}
	}
	certificatePath, ok := promptMenuValue(scanner, "ACME 脚本临时证书路径（回车使用 /root/ygkkkca/cert.crt）：")
	if !ok {
		return
	}
	if certificatePath == "" {
		certificatePath = "/root/ygkkkca/cert.crt"
	}
	keyPath, ok := promptMenuValue(scanner, "ACME 脚本临时私钥路径（回车使用 /root/ygkkkca/private.key）：")
	if !ok {
		return
	}
	if keyPath == "" {
		keyPath = "/root/ygkkkca/private.key"
	}
	fmt.Println("即将启动 ACME 交互流程；完成后 VPNM 会验证并导入到 /etc/vps-net-manager/certs/<证书ID>/fullchain.pem 和 privkey.pem。")
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
	if _, err := app.Bootstrap(context.Background(), *stateDirectory, *unitDirectory, nil); err != nil {
		fatalIf(err)
	}
	previous, err := app.LoadState(*stateDirectory + "/state.json")
	fatalIf(err)
	candidate, err := ui.InitialSetup(context.Background(), os.Stdin, os.Stdout, *stateDirectory, previous)
	fatalIf(err)
	if _, err := app.DefaultApplyOptions(*stateDirectory, &previous).Apply(context.Background(), candidate); err != nil {
		fatalIf(err)
	}
	fmt.Println("安装完成。之后运行 vm 管理节点。")
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
	fmt.Fprintln(os.Stderr, "vpnm:", err)
	os.Exit(1)
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: vm install | vm apply | vm update | vm uninstall --yes | vm cert <sync|acme> | vm bbr <status|enable|restore> | vm realm <install|validate|apply> | vm reality scan | vm state validate | vm version")
}
