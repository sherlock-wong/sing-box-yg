# VPS 安装与使用

本说明只适用于 VPS。Serv00 的安装、更新和保活脚本保持原项目地址与原有行为，不受本分支影响。

## 准备与安装

使用受支持的 Debian、Ubuntu、RHEL 系或 Alpine 系统，并以 `root` 运行。VPS 控制台防火墙/安全组应放行实际启用协议的 TCP/UDP 端口；Reality、Vmess 一般为 TCP，Hysteria2 与 TUIC 为 UDP。服务一直监听 `::`；分享链接的对外地址是独立设置。

正式 fork 安装命令：

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/sherlock-wong/sing-box-yg/main/sb.sh)
```

测试本功能分支：

```bash
SBYG_CHANNEL=feature/selectable-protocols bash <(curl -fsSL https://raw.githubusercontent.com/sherlock-wong/sing-box-yg/feature/selectable-protocols/sb.sh)
```

安装时输入协议编号的逗号分隔列表。例如 `1,3,5` 为 Vless-Reality、Hysteria2、AnyTLS；至少要一个协议。内核和协议选择均可输入 `0` 返回；协议选择取消时不会写入协议状态或启动服务。选择 `1.10.7` 时不显示 AnyTLS，输入 `5` 会被拒绝。

## 菜单、节点与订阅

运行 `sb` 进入主菜单，`2` 是配置菜单：查看启用协议和完整链接、显示二维码、重新生成订阅，或进入协议管理。生成文件在 `/etc/s-box/`：`subscription.txt`（聚合节点）、`subscription.base64`（聚合订阅）、`mihomo-subscription.txt` 和 `sing-box-client.json`。它们都只包含已启用协议，并在每次修改后同步更新。主菜单 `3` 和配置菜单 `7` 都会从最新 `protocols.json` 重新渲染配置，通过 JSON 与 `sing-box check` 后原子替换并强制重启，避免进程继续使用旧端口或旧凭据。

## 修改配置

“管理协议”可启用/停用协议、改节点名、端口和 UUID/密码。不能停用最后一个协议；新增协议自动分配端口，端口修改检查格式、重复及系统监听冲突。

所有配置子菜单都显示 `0 返回上一级`，并且只返回紧邻的上一层（例如 SNI 候选返回 Vless 专项参数，再返回 Vless 操作菜单）。进入某项后如果不想修改，输入 `0` 或直接回车即可取消；取消不会写状态文件、生成配置或重启服务。启停协议、轮换 Reality 密钥和手动重新生成订阅还会再次要求确认。

常见操作示例：

1. `sb → 2 → 2`，选择协议后用“启用/停用”在安装后增删协议。
2. 在同一协议菜单用“名称”“端口”“凭据”修改节点名、监听端口和 UUID/密码。
3. Vless 专项参数可更换 Reality SNI，或一次性轮换匹配的私钥、公钥和 Short ID。“扫描目标”会从当前 VPS 并发扫描 3x-ui v3.4.2 的 10 个默认候选，每个目标执行三次 TLS 1.3、HTTP/2、X25519 和证书检测。脚本淘汰成功不足两次的目标，按成功次数、平均握手耗时及抖动排序，只显示前五项供选择；也可以单独扫描自定义域名。
4. Vmess 专项参数可改 WS Path、TLS、CDN 地址、证书域名；Argo 固定隧道需填域名和 Cloudflare Tunnel token，启用时会自动关闭服务端 Vmess TLS，并使用经过校验的固定 Cloudflared 版本。
5. Hy2 专项参数可改上/下行 Mbps 和 UDP 跳跃范围，例如 `20000:30000`；关闭跳跃使用单独的“关闭”选项，回车仅取消修改。脚本使用独立 `SBYG_HY2` 防火墙链转发到主端口。
6. `sb → 2 → 6` 是统一的证书管理菜单，可查看当前证书/SAN/有效期，导入全局证书和私钥，为 Vmess、Hy2、TUIC、AnyTLS 分别或统一设置证书域名，也可运行锁定版本的 ACME 签发脚本。ACME 完成后需从证书管理菜单导入生成的证书和私钥路径；脚本会验证证书格式、公私钥匹配、受信证书是否覆盖所填域名以及最终 Sing-box 配置。

配置菜单“对外地址”可填 IPv4（`203.0.113.10`）、IPv6（`2001:db8::10`）或解析到本 VPS 的域名（`node.example.com`）；不要带协议头、方括号或端口。“自动探测”是单独选项，回车仅取消修改。它仅影响分享链接，服务仍监听 `::`。

AnyTLS、TUIC 和 Hysteria2 使用普通 TLS：SNI 应与服务端实际证书覆盖的域名相符（自签证书则由客户端跳过验证），没有 Reality 那种借用第三方站点作为握手目标的“大厂优选域名”概念。因此脚本只给 Reality 提供上述候选，不会把这些域名套到 AnyTLS。

## 运维与故障处理

主菜单 `3` 应用最新状态并重启，`4` 更新/切换内核，`5` 更新脚本，`6` 查看日志，`11` 卸载。确认卸载后会停止并禁用服务，删除 `/etc/s-box`、systemd/OpenRC 服务文件、Hy2 防火墙跳跃链和 `/usr/local/bin/sb`，随后立即返回终端；取消卸载则留在主菜单。自选其他官方内核必须有对应官方 `sha256sum.txt`，没有就拒绝安装。下载、校验或 `sing-box check` 失败时，临时文件会清理，旧配置不会替换。

遇到连接问题先检查安全组、防火墙、协议端口和 `sb → 6` 日志，再执行主菜单 `3` 重启。切换内核时，如果现有配置不能通过新内核检查，旧内核会保留。卸载需要输入大写 `YES`。

旧版安装若没有 `/etc/s-box/protocols.json`，脚本会禁止直接修改并提示先卸载、后重装。依赖校验失败时不要跳过校验；检查 VPS 到 GitHub/API 发布源的网络和系统时间后重试，或等待维护者同时更新脚本常量、`DEPENDENCY_LOCKS.md` 与版本说明。
