# VPS Net Manager 使用说明

## 支持范围

仅支持 Debian/Ubuntu、systemd、amd64/arm64。管理器不迁移旧 Bash 状态；若安装器发现旧 `protocols.json` 或 Shell 版 `vpnm`，会拒绝覆盖。

## 安装与菜单

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/sherlock-wong/vps-net-manager/main/install.sh)
vm
```

首次安装只会建立零协议状态，不会强制进入配置向导。之后运行 `vm`，再从“管理协议”按需添加协议。协议配置不存在表示未添加；存在但关闭表示已停用；存在且开启表示已启用。

## 协议与客户端输出

- Vless-Reality 在创建时可选择 Sing-box 或 Xray；已添加的协议可在协议菜单中随时切换服务端内核。Xray 配置保留指纹、SpiderX、时间差、ML-DSA-65 与回落限制。
- 新建 Hysteria2 或 AnyTLS 必须填写自己控制的 TLS 域名；不再提供第三方域名作为默认值。
- Hysteria2 支持 UDP 跳跃。跳跃范围会被项目管理的 IPv4/IPv6 NAT 规则转发到主端口。
- AnyTLS 支持默认或自定义 Padding。
- 菜单的“显示分享链接和二维码”只展示已启用协议；同时生成 Sing-box 客户端 JSON 与 Mihomo YAML 到 `generated/`。

## 证书

在 `vm` 主菜单选择“证书管理”可查看证书及 Hysteria2/AnyTLS 的绑定、导入已有 PEM 证书/私钥、创建固定自签证书、运行锁定版 ACME 域名证书申请，或立即同步受管来源。“管理证书来源路径”可为已入库证书更换外部 PEM 证书/私钥路径，或停止追踪来源；服务实际使用的归档路径固定在 `/etc/vps-net-manager/certs/<证书ID>/`。导入时可选择持续跟踪原始证书文件；启用后，systemd 每 6 小时执行一次同步。申请 ACME 时，脚本会在当前终端交互；VPNM 将临时输出固定在 `/etc/vps-net-manager/acme/<证书ID>/`，验证 SAN、公私钥与有效期后归档为 `/etc/vps-net-manager/certs/<证书ID>/fullchain.pem` 和 `privkey.pem`。

新增 Hysteria2 或 AnyTLS 后，VPNM 会先查找覆盖所填域名的有效证书；找不到时可选择申请 ACME 受信任证书，或仅为测试创建固定自签证书。ACME 前请确认域名可由该 VPS 完成验证；自签证书仅建议用于测试，客户端应校验证书指纹。

VPNM 不兼容或迁移旧证书目录；服务使用的证书路径必须严格为上述 `certs/<证书ID>/` 布局。

固定证书使用 Go 标准库生成并记录 DER/SPKI SHA-256。受管证书可以设置 `source_cert` 和 `source_key`：`vm cert sync --quiet` 会先读取、验证公私钥、SAN 与有效期，再将证书、状态和服务配置作为同一事务替换。导入或创建后，在协议管理的 Hysteria2/AnyTLS 菜单选择“选择证书”完成绑定；TLS 域名必须由所选证书的 SAN 覆盖。

`vm cert acme` 只下载锁定 commit 和 SHA-256 的 ACME 脚本；脚本返回成功并不代表成功，管理器会再次验证指定证书和私钥。

## Realm

先执行 `vm realm install`，再从菜单添加规则。Realm 状态位于 `/etc/vps-net-manager/realm.json`，每条规则同时转发 TCP 与 UDP。规则替换会预开放 UFW、原子替换状态/TOML、重启服务，失败则回滚。

## BBR

```bash
vm bbr status
vm bbr enable
vm bbr restore
```

只创建或删除 VPNM 自己的 `/etc/sysctl.d/99-vps-net-manager-bbr.conf` 与 `/etc/modules-load.d/vps-net-manager-bbr.conf`；不会安装内核或更改引导项。

## 更新与卸载

`vm update` 仅在用户主动执行时联网，解析 `main-build` 的不可变 commit 并校验 manifest、checksums、新二进制和核心文件。失败会恢复原二进制、核心与服务。

```bash
vm uninstall --yes
```

卸载仅删除 VPNM 创建的 systemd unit、状态目录、标记 UFW 规则和 `/usr/local/bin/vm`，不会删除不属于 VPNM 的服务或防火墙规则。
