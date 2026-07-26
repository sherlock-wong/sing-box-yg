# VPS Net Manager

VPS Net Manager 是面向个人 VPS 的 Go 网络服务管理器。生产入口只有 `/usr/local/bin/vpnm`；仓库 `main` 只保存源码，CI 成功后把可安装的 Linux 二进制发布到滚动的 `main-build` 分支。

支持 Debian/Ubuntu、systemd、amd64/arm64，以及 Vless-Reality（Sing-box/Xray）、Hysteria2、AnyTLS、Realm、证书同步、Reality 扫描和原生 BBR。旧 Bash 版本不能迁移：请先用旧菜单卸载。

## 安装

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/sherlock-wong/vps-net-manager/main/install.sh)
```

安装器会解析 `main-build` 的不可变 commit，下载对应架构的 `vpnm` 并校验 SHA-256。VPS 无需 Go、GCC、jq、qrencode、tar 或 unzip。

安装完成后运行：

```bash
vpnm
```

## 非交互命令

```text
vpnm install
vpnm apply
vpnm update
vpnm uninstall --yes
vpnm cert sync --quiet
vpnm cert acme --domain <域名> --cert <证书路径> --key <私钥路径> -- <ACME 脚本参数>
vpnm reality scan
vpnm realm install
vpnm realm validate
vpnm realm apply
vpnm bbr status|enable|restore
vpnm state validate
vpnm version
```

`update` 会先验证新管理器、配置和已锁定核心；服务健康检查失败会恢复旧二进制与核心。`uninstall` 必须显式给出 `--yes`。

依赖锁定说明见 [DEPENDENCY_LOCKS.md](DEPENDENCY_LOCKS.md)。
