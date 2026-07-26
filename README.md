# sing-box-yg（VPS）

这是一个仅面向 VPS 的 Sing-box 管理器，支持 Vless-Reality、Vmess-WS、Hysteria2 与 AnyTLS；Vless-Reality 可选择 Sing-box 或 Xray-core 服务端。

本仓库不再包含 Serv00、Hostuno、保活网页或相关 GitHub Actions。

## 稳定版安装

当前稳定版为 `v0.0.4`：

```bash
SBYG_CHANNEL=v0.0.4 bash <(curl -fsSL https://raw.githubusercontent.com/sherlock-wong/sing-box-yg/v0.0.4/sb.sh)
```

开发版 `main`：

```bash
SBYG_CHANNEL=main bash <(curl -fsSL https://raw.githubusercontent.com/sherlock-wong/sing-box-yg/main/sb.sh)
```

安装完成后运行 `sb` 进入管理菜单。

## 包含功能

- 安装时多选协议，运行中可启用、停用和修改协议。
- 生成分享链接、二维码、Mihomo、Sing-box 和聚合订阅。
- Reality 候选扫描与推荐/可用/不可三级校验。
- 多证书管理、证书来源同步与原子回滚。
- UFW 端口放行、Argo、WARP、BBR 与内核管理。
- Debian/Ubuntu 上的 Realm TCP+UDP 端口转发规则管理。

完整的系统准备、证书、Reality、订阅、运维和故障处理说明见 [VPS_USAGE.md](VPS_USAGE.md)。

## 依赖安全

VPS 使用的外部脚本和二进制均锁定版本或 commit，并在下载后进行 SHA-256 校验；详情见 [DEPENDENCY_LOCKS.md](DEPENDENCY_LOCKS.md)。
