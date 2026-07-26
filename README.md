# VPS Net Manager

VPS Net Manager 是一个面向个人 VPS 的网络服务管理器。它集中管理 Sing-box、Xray-core Reality、Realm 端口转发、证书、UFW、防火墙跳跃、Argo、WARP 与 BBR。

项目不包含面板、多用户计费、流量配额、Serv00/Hostuno、保活网页或第三方订阅转换服务。

## 支持范围

| 模块 | 功能 |
| --- | --- |
| Sing-box | Vless-Reality、Vmess-WS、Hysteria2、AnyTLS |
| Xray-core | 可选的 Vless-Reality 服务端，支持 SpiderX、指纹、ML-DSA-65 与回落限制 |
| Realm | Debian/Ubuntu 上的 TCP+UDP 端口转发规则 |
| 证书 | 多证书库、协议独立绑定、受管源文件同步与回滚 |
| 客户端配置 | 分享链接、二维码、Mihomo、Sing-box 与聚合订阅 |

## 安装

当前版本：

```bash
VPNM_CHANNEL=main bash <(curl -fsSL https://raw.githubusercontent.com/sherlock-wong/vps-net-manager/main/sb.sh)
```

安装完成后运行 `vpnm`。如需使用某个已发布版本，将命令中的 `main` 替换为对应标签，并同时设置 `VPNM_CHANNEL=<标签>`。

## 安全原则

- 外部脚本和二进制锁定版本或 commit，下载后必须通过 SHA-256 校验。
- 协议和证书修改会先生成临时配置，通过 JSON、Sing-box 与 Xray（如启用）检查后再替换。
- UFW 启用时，先放行新端口；服务重启成功后再删除项目创建的旧规则。
- 所有订阅只生成已启用协议；自签证书使用固定值，受信证书使用正常 CA 校验。

完整的安装、协议、证书、Realm、更新、卸载与故障排查见 [VPS_USAGE.md](VPS_USAGE.md)。下载锁定清单见 [DEPENDENCY_LOCKS.md](DEPENDENCY_LOCKS.md)。
