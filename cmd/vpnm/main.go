package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/sherlock-wong/vps-net-manager/internal/app"
	"github.com/sherlock-wong/vps-net-manager/internal/bbr"
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
			fmt.Fprintln(os.Stderr, "请先运行：vpnm install")
			return
		}
		fmt.Println("\nVPS Net Manager")
		fmt.Printf("  Vless-Reality：%s\n", protocolStatus(state.Protocols.VLESSReality != nil, state.Protocols.VLESSReality != nil && state.Protocols.VLESSReality.Enabled))
		fmt.Printf("  Hysteria2：%s\n", protocolStatus(state.Protocols.Hysteria2 != nil, state.Protocols.Hysteria2 != nil && state.Protocols.Hysteria2.Enabled))
		fmt.Printf("  AnyTLS：%s\n", protocolStatus(state.Protocols.AnyTLS != nil, state.Protocols.AnyTLS != nil && state.Protocols.AnyTLS.Enabled))
		fmt.Println("\n  1. 查看当前状态")
		fmt.Println("  2. 重新应用当前配置")
		fmt.Println("  3. 管理协议")
		fmt.Println("  4. Realm 端口转发")
		fmt.Println("  5. BBR 管理")
		fmt.Println("  6. 显示分享链接和二维码")
		fmt.Println("  0. 退出")
		fmt.Print("请选择：")
		if !scanner.Scan() {
			return
		}
		switch scanner.Text() {
		case "1":
			fmt.Printf("\n公网地址：%s\n", valueOrUnset(state.PublicAddress))
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
				fmt.Fprintln(os.Stderr, "vpnm: Realm 尚未安装；请先运行：vpnm realm install")
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
				fmt.Println("没有已启用协议，因此没有分享链接。")
				continue
			}
			if err := ui.PrintShareLinks(os.Stdout, links.Links); err != nil {
				fmt.Fprintln(os.Stderr, "vpnm:", err)
			}
		case "0":
			return
		default:
			fmt.Println("无效选择。")
		}
	}
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
	fmt.Println("安装完成。之后运行 vpnm 管理节点。")
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
	binaryPath := flags.String("binary", "/usr/local/bin/vpnm", "VPNM executable path")
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
	fmt.Printf("vpnm main-%s\nsource commit: %s\nbuilt at: %s\n", shortCommit(sourceCommit), sourceCommit, builtAt)
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
		fatalIf(fmt.Errorf("卸载会删除 VPNM 状态、项目 unit 和 /usr/local/bin/vpnm；请使用 vpnm uninstall --yes 确认"))
	}
	if os.Geteuid() != 0 {
		fatalIf(fmt.Errorf("uninstall 必须以 root 运行"))
	}
	fatalIf((app.Uninstaller{StateDirectory: "/etc/vps-net-manager", UnitDirectory: "/etc/systemd/system", BinaryPath: "/usr/local/bin/vpnm"}).Uninstall(context.Background()))
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
	fmt.Fprintln(os.Stderr, "usage: vpnm install | vpnm apply | vpnm update | vpnm uninstall --yes | vpnm cert <sync|acme> | vpnm bbr <status|enable|restore> | vpnm realm <install|validate|apply> | vpnm reality scan | vpnm state validate | vpnm version")
}
