#!/usr/bin/env bash
# VPS-only sing-box-yg manager.  Serv00 files intentionally live separately.
set -Eeuo pipefail
export LANG=C.UTF-8

SCRIPT_VERSION='v26.7.25-fork.1'
FORK_OWNER='sherlock-wong'
FORK_REPO='sing-box-yg'
FORK_BRANCH="${SBYG_CHANNEL:-main}"
FORK_RAW="https://raw.githubusercontent.com/${FORK_OWNER}/${FORK_REPO}/${FORK_BRANCH}"
STATE_DIR="${SBYG_STATE_DIR:-/etc/s-box}"
STATE_FILE="$STATE_DIR/protocols.json"
CONFIG_FILE="$STATE_DIR/sb.json"
SERVICE_NAME=sing-box

# Keep this block and DEPENDENCY_LOCKS.md in sync when refreshing a dependency.
SB_DEFAULT=1.13.14
SB_110=1.10.7
ACME_COMMIT=8ffa90d950ec9562248b8712634b335e8684e01b
ACME_SHA256=5e43b5eea48987574730cecf77b5de483d4d7ec6e1e5f242b80f1321863f0521
WARP_COMMIT=f2f634ba79452a0ffadcd93a6e6524cf4b7b84df
WARP_SHA256=7ebb2eba5c230d22643cdc77fdea0163877abcb0b5dde22b6b227f47523926d9
BBR_COMMIT=fdb40962837b2e24bc94b87c2b1786ad2308489a
BBR_SHA256=17f447d78ba82468727e97cfdaa2a18150840a4c00c207592e5329df36544e85
SBWPPH_AMD64_SHA256=93c7c5d7cb2c82cef44de782ae030b5f8fdb15038e3e95662e451bce7d3ee531
SBWPPH_ARM64_SHA256=4a8f0419e4b848b99017128d532bd760f6daa4a7b0bc9f59ff166105db5c6e33
CLOUDFLARED_VERSION=2026.7.3
CLOUDFLARED_AMD64_SHA256=9d71c677db00134c1bd4144b7783486b654ad281b1ea62b4972098d19f770f17
CLOUDFLARED_ARM64_SHA256=65259e652a7bea08bf5df603233ab22b8bf3116af8df9f9206209af6a1b955c0
METACUBEX_RELEASE=2026-07-24T23:32:15Z
METACUBEX_GEOIP_ASSET=488939089
METACUBEX_GEOIP_SHA256=fad057ea2b145d243383db031b5804836e92de30203f31691e974cb14820bf36
METACUBEX_GEOSITE_ASSET=488939110
METACUBEX_GEOSITE_SHA256=2c17d05a29c30797f57101c2268eb1b8b640004f380c8c963773a3587cb320aa
REALITY_SCAN_SAMPLES=3
REALITY_CANDIDATES=(
  www.cloudflare.com
  www.microsoft.com
  www.amazon.com
  aws.amazon.com
  www.samsung.com
  www.nvidia.com
  www.amd.com
  www.intel.com
  www.sony.com
  dl.google.com
)

red(){ printf '\033[31;1m%s\033[0m\n' "$*"; }
green(){ printf '\033[32;1m%s\033[0m\n' "$*"; }
yellow(){ printf '\033[33;1m%s\033[0m\n' "$*"; }
blue(){ printf '\033[36;1m%s\033[0m\n' "$*"; }
die(){ red "$*"; exit 1; }
ask(){ read -r -p "$1" "$2"; }
need_root(){ [[ $EUID -eq 0 ]] || die '请使用 root 运行。'; }
need(){ command -v "$1" >/dev/null 2>&1 || die "缺少依赖：$1"; }
sha256(){ sha256sum "$1" 2>/dev/null || shasum -a 256 "$1"; }
verify(){ [[ "$(sha256 "$1" | awk '{print $1}')" == "$2" ]] || die "完整性校验失败：$3（已保留原配置，未执行文件）"; }
TMP_REGISTRY=$(mktemp "${TMPDIR:-/tmp}/sing-box-yg-registry.XXXXXX")
tmpdir(){ local d; d=$(mktemp -d "${TMPDIR:-/tmp}/sing-box-yg.XXXXXX"); printf '%s\n' "$d" >> "$TMP_REGISTRY"; printf '%s\n' "$d"; }
cleanup_tmp(){
  local d
  while IFS= read -r d; do [[ "$d" == "${TMPDIR:-/tmp}"/sing-box-yg.* && -d "$d" ]] && rm -rf "$d"; done < "$TMP_REGISTRY"
  rm -f "$TMP_REGISTRY"
}
trap cleanup_tmp EXIT

cpu(){ case "$(uname -m)" in x86_64) echo amd64;; aarch64) echo arm64;; *) die "不支持的 CPU：$(uname -m)";; esac; }
json(){ jq -e . "$1" >/dev/null; }
tag_enabled(){ jq -e --arg t "$1" '.protocols[$t].enabled == true' "$STATE_FILE" >/dev/null; }
enabled_count(){ jq '[.protocols[] | select(.enabled)] | length' "$STATE_FILE"; }
random_port(){ local n; n=$(od -An -N4 -tu4 /dev/urandom | tr -d ' '); printf '%s\n' "$((10000 + n % 55536))"; }
valid_port(){ [[ "$1" =~ ^[1-9][0-9]{0,4}$ ]] && (( $1 <= 65535 )); }
valid_host(){
  local value=$1 part
  if [[ "$value" == *:* ]]; then
    [[ "$value" =~ ^[0-9A-Fa-f:]+$ && "$value" != *:::* ]]
  elif [[ "$value" =~ ^[0-9.]+$ ]]; then
    IFS=. read -r -a parts <<<"$value"; [[ ${#parts[@]} -eq 4 ]] || return 1
    for part in "${parts[@]}"; do [[ "$part" =~ ^[0-9]{1,3}$ ]] && ((10#$part <= 255)) || return 1; done
  else
    [[ ${#value} -le 253 && "$value" =~ ^([A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?\.)*[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$ ]]
  fi
}
valid_uuid(){ [[ "$1" =~ ^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89aAbB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$ ]]; }
port_available(){ ! ss -H -lntup 2>/dev/null | awk '{print $5}' | grep -Eq "[:.]$1$"; }
protocol_label(){ case "$1" in vless) echo Vless-Reality;; vmess) echo Vmess-WS;; hy2) echo Hysteria2;; tuic) echo 'TUIC v5';; anytls) echo AnyTLS;; esac; }
cancel_input(){ [[ -z "$1" || "$1" == 0 ]]; }
apply_state(){ printf '%s\n' "$1" | commit_state || true; }
confirm_change(){
  local x
  echo '1 确认  0 返回上一级'
  ask '选择：' x
  [[ "$x" == 1 ]]
}

install_packages(){
  if command -v apt-get >/dev/null; then apt-get update -y && apt-get install -y curl jq openssl qrencode tar iproute2 iptables coreutils ca-certificates;
  elif command -v dnf >/dev/null; then dnf install -y curl jq openssl qrencode tar iproute iptables coreutils ca-certificates;
  elif command -v yum >/dev/null; then yum install -y curl jq openssl qrencode tar iproute iptables coreutils ca-certificates;
  elif command -v apk >/dev/null; then apk add --no-cache curl jq openssl qrencode tar iproute2 iptables coreutils ca-certificates;
  else die '仅支持 Debian/Ubuntu、RHEL 系和 Alpine。'; fi
}

download_verified(){ # URL SHA OUT DESCRIPTION
  local url=$1 expected=$2 out=$3 label=$4
  curl --fail --location --retry 2 --proto '=https' --tlsv1.2 -o "$out" "$url" || die "下载失败：$label"
  verify "$out" "$expected" "$label"
}

sb_hash(){ # version arch
  case "$1/$2" in
    1.13.14/amd64) echo f48703461a15476951ac4967cdad339d986f4b8096b4eb3ff0829a500502d697;;
    1.13.14/arm64) echo 4742df6a4314e8ecc41736849fca6d73b8f9e91b6e8b06ee794ff17ba180579e;;
    1.10.7/amd64) echo 1951a0785c8b4e1e21e0640227a49528ca772aec3d680061652e3d6b687e00fe;;
    1.10.7/arm64) echo 15b43a0a50b4e6962aca819d4f3055aaac75ca7481350d4aaebe93ed06b7af49;;
    *) return 1;; esac
}

install_singbox(){ # version [destination]; official checksum required for non-default
  local ver=$1 destination=${2:-"$STATE_DIR/sing-box"} arch hash url archive extracted official
  arch=$(cpu); url="https://github.com/SagerNet/sing-box/releases/download/v${ver}/sing-box-${ver}-linux-${arch}.tar.gz"
  WORKDIR=$(tmpdir); archive="$WORKDIR/sing-box.tar.gz"
  if hash=$(sb_hash "$ver" "$arch"); then
    download_verified "$url" "$hash" "$archive" "Sing-box ${ver}/${arch}"
  else
    # User selected a different official release: checksum is mandatory, never best-effort.
    official="$WORKDIR/checksums.txt"
    curl --fail --location --proto '=https' --tlsv1.2 -o "$official" "https://github.com/SagerNet/sing-box/releases/download/v${ver}/sha256sum.txt" \
      || die '该内核没有可获取的官方 sha256sum.txt，拒绝安装。'
    hash=$(awk -v f="sing-box-${ver}-linux-${arch}.tar.gz" '$2==f {print $1}' "$official")
    [[ "$hash" =~ ^[0-9a-f]{64}$ ]] || die '官方校验文件未包含所需内核，拒绝安装。'
    download_verified "$url" "$hash" "$archive" "Sing-box ${ver}/${arch}"
  fi
  tar -xzf "$archive" -C "$WORKDIR" || die 'Sing-box 压缩包无效。'
  extracted="$WORKDIR/sing-box-${ver}-linux-${arch}/sing-box"
  [[ -x "$extracted" ]] || die 'Sing-box 发布包内容不完整。'
  install -d -m 755 "$STATE_DIR"
  install -m 755 "$extracted" "$destination"
  green "已下载并校验 Sing-box ${ver}。"
}

write_service(){
  if command -v systemctl >/dev/null; then
    cat > /etc/systemd/system/sing-box.service <<EOF
[Unit]
Description=sing-box-yg
After=network-online.target
[Service]
Type=simple
ExecStart=$STATE_DIR/sing-box run -c $CONFIG_FILE
Restart=on-failure
[Install]
WantedBy=multi-user.target
EOF
    systemctl daemon-reload
  elif command -v rc-service >/dev/null; then
    cat > /etc/init.d/sing-box <<EOF
#!/sbin/openrc-run
description="sing-box-yg"
command="$STATE_DIR/sing-box"
command_args="run -c $CONFIG_FILE"
command_background=true
pidfile="/run/sing-box.pid"
depend() { need net; }
EOF
    chmod 755 /etc/init.d/sing-box
    rc-update add sing-box default >/dev/null 2>&1 || true
  else
    return 1
  fi
}
systemd_enable_restart(){
  systemctl enable "$1" >/dev/null 2>&1 || return 1
  systemctl restart "$1"
}
restart_service(){
  if command -v systemctl >/dev/null; then
    systemd_enable_restart "$SERVICE_NAME"
  else
    rc-service sing-box restart
  fi
}

new_state(){
  local core=$1 id reality_private reality_public sid cert_key cert_crt
  local pair
  id=$($STATE_DIR/sing-box generate uuid)
  pair=$($STATE_DIR/sing-box generate reality-keypair)
  reality_private=$(printf '%s\n' "$pair" | awk -F': ' '/PrivateKey/{print $2}')
  reality_public=$(printf '%s\n' "$pair" | awk -F': ' '/PublicKey/{print $2}')
  [[ -n "$reality_private" && -n "$reality_public" ]] || die 'Reality 密钥生成失败。'
  sid=$(openssl rand -hex 4); cert_key="$STATE_DIR/private.key"; cert_crt="$STATE_DIR/cert.pem"
  openssl req -x509 -newkey rsa:2048 -nodes -days 3650 -subj '/CN=www.bing.com' -keyout "$cert_key" -out "$cert_crt" >/dev/null 2>&1
  jq -n --arg core "$core" --arg id "$id" --arg priv "$reality_private" --arg pub "$reality_public" --arg sid "$sid" --arg cert "$cert_crt" --arg key "$cert_key" \
    '{schema:1,core:$core,public_address:"",protocols:{vless:{enabled:false,name:"vless-reality",port:0,uuid:$id,sni:"www.apple.com",private_key:$priv,public_key:$pub,short_id:$sid},vmess:{enabled:false,name:"vmess-ws",port:0,uuid:$id,path:("/"+$id+"-vm"),tls:true,domain:"www.bing.com",cdn:"",argo_domain:"",argo_token:""},hy2:{enabled:false,name:"hysteria2",port:0,password:$id,domain:"www.bing.com",up_mbps:100,down_mbps:100,udp_hop:""},tuic:{enabled:false,name:"tuic-v5",port:0,uuid:$id,password:$id,domain:"www.bing.com"},anytls:{enabled:false,name:"anytls",port:0,password:$id,domain:"www.bing.com"}},certificate:{cert:$cert,key:$key,insecure:true}}'
}

select_protocols(){ # outputs a JSON state with enabled protocols and unique random ports
  local core=$1 choice p tag state valid
  while :; do
    echo '选择协议（可多选，逗号分隔）：1 Vless-Reality  2 Vmess-WS  3 Hysteria2  4 TUIC v5' >&2
    [[ "$core" != 1.10* ]] && echo '5 AnyTLS' >&2
    echo '0 取消安装并返回上一级' >&2
    ask '例如 1,3,5：' choice; choice=${choice// /}
    cancel_input "$choice" && return 2
    IFS=, read -ra picks <<< "$choice"; valid=1; local chosen=()
    for p in "${picks[@]}"; do
      tag=
      case "$p" in 1) tag=vless;;2) tag=vmess;;3) tag=hy2;;4) tag=tuic;;5) [[ "$core" == 1.10* ]] && valid=0 || tag=anytls;;*) valid=0;;esac
      [[ -n "$tag" && " ${chosen[*]-} " != *" $tag "* ]] && chosen+=("$tag")
    done
    ((valid)) && ((${#chosen[@]})) || { red '输入无效；1.10 内核不支持 AnyTLS。' >&2; continue; }
    state=$(new_state "$core")
    for tag in "${chosen[@]}"; do
      while :; do p=$(random_port); [[ " $(jq -r '[.protocols[].port]|join(" ")' <<<"$state") " != *" $p "* ]] && port_available "$p" && break; done
      state=$(jq --arg t "$tag" --argjson p "$p" '.protocols[$t].enabled=true | .protocols[$t].port=$p' <<<"$state")
    done
    printf '%s\n' "$state"; return
  done
}

inbound_for(){ # state tag -> JSON object; all listeners remain ::
  local s=$1 t=$2
  case "$t" in
  vless) jq -c '.protocols.vless as $p | {type:"vless",tag:"vless-sb",listen:"::",listen_port:$p.port,users:[{uuid:$p.uuid,flow:"xtls-rprx-vision"}],tls:{enabled:true,server_name:$p.sni,reality:{enabled:true,handshake:{server:$p.sni,server_port:443},private_key:$p.private_key,short_id:[$p.short_id]}}}' <<<"$s";;
  vmess) jq -c '.protocols.vmess as $p | .certificate as $c | {type:"vmess",tag:"vmess-sb",listen:"::",listen_port:$p.port,users:[{uuid:$p.uuid,alterId:0}],transport:{type:"ws",path:$p.path},tls:{enabled:$p.tls,server_name:$p.domain,certificate_path:$c.cert,key_path:$c.key}}' <<<"$s";;
  hy2) jq -c '.protocols.hy2 as $p | .certificate as $c | {type:"hysteria2",tag:"hy2-sb",listen:"::",listen_port:$p.port,users:[{password:$p.password}],ignore_client_bandwidth:false,up_mbps:$p.up_mbps,down_mbps:$p.down_mbps,tls:{enabled:true,alpn:["h3"],server_name:$p.domain,certificate_path:$c.cert,key_path:$c.key}}' <<<"$s";;
  tuic) jq -c '.protocols.tuic as $p | .certificate as $c | {type:"tuic",tag:"tuic5-sb",listen:"::",listen_port:$p.port,users:[{uuid:$p.uuid,password:$p.password}],congestion_control:"bbr",tls:{enabled:true,alpn:["h3"],server_name:$p.domain,certificate_path:$c.cert,key_path:$c.key}}' <<<"$s";;
  anytls) jq -c '.protocols.anytls as $p | .certificate as $c | {type:"anytls",tag:"anytls-sb",listen:"::",listen_port:$p.port,users:[{password:$p.password}],tls:{enabled:true,server_name:$p.domain,certificate_path:$c.cert,key_path:$c.key}}' <<<"$s";; esac
}

render_config(){ # state output
  local s=$1 out=$2 tag ib; jq -n '{log:{level:"info",timestamp:true},inbounds:[],outbounds:[{type:"direct",tag:"direct"},{type:"block",tag:"block"}]}' > "$out"
  for tag in vless vmess hy2 tuic anytls; do
    jq -e --arg t "$tag" '.protocols[$t].enabled' <<<"$s" >/dev/null || continue
    ib=$(inbound_for "$s" "$tag") || return 1
    jq --argjson i "$ib" '.inbounds += [$i]' "$out" > "$out.next" && mv "$out.next" "$out"
  done
  json "$out" && [[ $(jq '.inbounds|length' "$out") -gt 0 ]]
}

validate_state(){
  local s=$1 tag p
  jq -e '
    .schema==1 and
    (.core|type=="string") and
    (.public_address|type=="string") and
    (.certificate.cert|type=="string") and (.certificate.key|type=="string") and (.certificate.insecure|type=="boolean") and
    ([.protocols[]|select(.enabled)]|length)>0 and
    (.protocols.vmess.tls|type=="boolean") and
    (.protocols.hy2.up_mbps|type=="number") and (.protocols.hy2.up_mbps>0) and
    (.protocols.hy2.down_mbps|type=="number") and (.protocols.hy2.down_mbps>0)
  ' <<<"$s" >/dev/null || return 1
  [[ $(jq -r '.core' <<<"$s") != 1.10* ]] || ! jq -e '.protocols.anytls.enabled' <<<"$s" >/dev/null || return 1
  for tag in vless vmess hy2 tuic anytls; do
    jq -e --arg t "$tag" '.protocols[$t].enabled' <<<"$s" >/dev/null || continue
    p=$(jq -r --arg t "$tag" '.protocols[$t].port' <<<"$s"); valid_port "$p" || return 1
  done
  valid_uuid "$(jq -r '.protocols.vless.uuid' <<<"$s")" || return 1
  valid_uuid "$(jq -r '.protocols.vmess.uuid' <<<"$s")" || return 1
  valid_uuid "$(jq -r '.protocols.tuic.uuid' <<<"$s")" || return 1
  [[ $(jq -r '.protocols.vmess.path' <<<"$s") == /* ]] || return 1
  [[ $(jq -r '.protocols.hy2.udp_hop' <<<"$s") =~ ^$|^[1-9][0-9]{0,4}:[1-9][0-9]{0,4}$ ]] || return 1
  [[ $(jq '[.protocols[] | select(.enabled) | .port] | unique | length' <<<"$s") == $(jq '[.protocols[] | select(.enabled)] | length' <<<"$s") ]]
}

commit_state(){ # candidate JSON stdin
  local candidate tmp state_tmp config_tmp old_state old_config
  candidate=$(cat); validate_state "$candidate" || { red '状态无效：检查协议数量、输入类型、UUID、Path 与端口。'; return 1; }
  tmp=$(tmpdir); state_tmp="$tmp/protocols.json"; config_tmp="$tmp/sb.json"; old_state="$tmp/old-state.json"; old_config="$tmp/old-config.json"
  printf '%s\n' "$candidate" > "$state_tmp"
  render_config "$candidate" "$config_tmp" || { red 'JSON 渲染失败，原配置未改变。'; return 1; }
  "$STATE_DIR/sing-box" check -c "$config_tmp" || { red 'sing-box check 失败，原配置未改变。'; return 1; }
  install -d -m 755 "$STATE_DIR"
  [[ -f "$STATE_FILE" ]] && cp "$STATE_FILE" "$old_state"
  [[ -f "$CONFIG_FILE" ]] && cp "$CONFIG_FILE" "$old_config"
  chmod 600 "$state_tmp" "$config_tmp"
  mv "$state_tmp" "$STATE_FILE"; mv "$config_tmp" "$CONFIG_FILE"
  if ! write_service || ! restart_service; then
    red '重启失败，正在回滚原配置。'
    [[ -f "$old_state" ]] && cp "$old_state" "$STATE_FILE"
    [[ -f "$old_config" ]] && cp "$old_config" "$CONFIG_FILE"
    restart_service || true
    return 1
  fi
  generate_subscriptions
  reconcile_hy2_hop || yellow 'Hy2 UDP 跳跃规则未能应用，请检查 iptables。'
  reconcile_argo || yellow 'Argo 服务未能启动；Sing-box 配置不受影响。'
  green '配置已通过 JSON 与 sing-box check；已原子替换并重启。'
}
apply_current_state(){
  [[ -f "$STATE_FILE" ]] || { red '缺少协议状态文件，无法应用配置。'; return 1; }
  commit_state < "$STATE_FILE"
}

address(){ local a; a=$(jq -r '.public_address' "$STATE_FILE"); [[ -n "$a" ]] && printf '%s' "$a" || { curl -4fsS --max-time 5 https://icanhazip.com 2>/dev/null | tr -d '\n' || true; }; }
uri_host(){ [[ "$1" == *:* && "$1" != \[*\] ]] && printf '[%s]' "$1" || printf '%s' "$1"; }
urlencode(){ jq -rn --arg x "$1" '$x|@uri'; }
client_outbound_for(){
  local tag=$1 host=$2 server port argo
  port=$(jq -r ".protocols.$tag.port" "$STATE_FILE"); server=$host
  case "$tag" in
    vless) jq -c --arg s "$server" --argjson p "$port" '.protocols.vless as $x | {type:"vless",tag:$x.name,server:$s,server_port:$p,uuid:$x.uuid,flow:"xtls-rprx-vision",tls:{enabled:true,server_name:$x.sni,utls:{enabled:true,fingerprint:"chrome"},reality:{enabled:true,public_key:$x.public_key,short_id:$x.short_id}}}' "$STATE_FILE";;
    vmess)
      argo=$(jq -r '.protocols.vmess.argo_domain' "$STATE_FILE")
      [[ -n "$argo" ]] && { server=$argo; port=443; } || { server=$(jq -r --arg d "$host" '.protocols.vmess.cdn // "" | if length>0 then . else $d end' "$STATE_FILE"); }
      jq -c --arg s "$server" --argjson p "$port" --arg argo "$argo" '.protocols.vmess as $x | .certificate as $c | {type:"vmess",tag:$x.name,server:$s,server_port:$p,uuid:$x.uuid,security:"auto",transport:{type:"ws",path:$x.path,headers:{Host:(if ($argo|length)>0 then $argo else $x.domain end)}},tls:{enabled:($x.tls or (($argo|length)>0)),server_name:(if ($argo|length)>0 then $argo else $x.domain end),insecure:$c.insecure}}' "$STATE_FILE";;
    hy2) jq -c --arg s "$server" --argjson p "$port" '.protocols.hy2 as $x | .certificate as $c | {type:"hysteria2",tag:$x.name,server:$s,server_port:$p,password:$x.password,up_mbps:$x.up_mbps,down_mbps:$x.down_mbps,tls:{enabled:true,server_name:$x.domain,insecure:$c.insecure}}' "$STATE_FILE";;
    tuic) jq -c --arg s "$server" --argjson p "$port" '.protocols.tuic as $x | .certificate as $c | {type:"tuic",tag:$x.name,server:$s,server_port:$p,uuid:$x.uuid,password:$x.password,congestion_control:"bbr",tls:{enabled:true,server_name:$x.domain,insecure:$c.insecure,alpn:["h3"]}}' "$STATE_FILE";;
    anytls) jq -c --arg s "$server" --argjson p "$port" '.protocols.anytls as $x | .certificate as $c | {type:"anytls",tag:$x.name,server:$s,server_port:$p,password:$x.password,tls:{enabled:true,server_name:$x.domain,insecure:$c.insecure}}' "$STATE_FILE";;
  esac
}
mihomo_proxy_for(){
  local tag=$1 host=$2 server port argo
  port=$(jq -r ".protocols.$tag.port" "$STATE_FILE"); server=$host
  case "$tag" in
    vless) jq -c --arg s "$server" --argjson p "$port" '.protocols.vless as $x | {name:$x.name,type:"vless",server:$s,port:$p,uuid:$x.uuid,network:"tcp",tls:true,udp:true,flow:"xtls-rprx-vision","client-fingerprint":"chrome","reality-opts":{"public-key":$x.public_key,"short-id":$x.short_id},servername:$x.sni}' "$STATE_FILE";;
    vmess)
      argo=$(jq -r '.protocols.vmess.argo_domain' "$STATE_FILE")
      [[ -n "$argo" ]] && { server=$argo; port=443; } || { server=$(jq -r --arg d "$host" '.protocols.vmess.cdn // "" | if length>0 then . else $d end' "$STATE_FILE"); }
      jq -c --arg s "$server" --argjson p "$port" --arg argo "$argo" '.protocols.vmess as $x | .certificate as $c | {name:$x.name,type:"vmess",server:$s,port:$p,uuid:$x.uuid,alterId:0,cipher:"auto",udp:true,network:"ws",tls:($x.tls or (($argo|length)>0)),servername:(if ($argo|length)>0 then $argo else $x.domain end),"skip-cert-verify":$c.insecure,"ws-opts":{path:$x.path,headers:{Host:(if ($argo|length)>0 then $argo else $x.domain end)}}}' "$STATE_FILE";;
    hy2) jq -c --arg s "$server" --argjson p "$port" '.protocols.hy2 as $x | .certificate as $c | {name:$x.name,type:"hysteria2",server:$s,port:$p,password:$x.password,sni:$x.domain,"skip-cert-verify":$c.insecure,up:(($x.up_mbps|tostring)+" Mbps"),down:(($x.down_mbps|tostring)+" Mbps")}' "$STATE_FILE";;
    tuic) jq -c --arg s "$server" --argjson p "$port" '.protocols.tuic as $x | .certificate as $c | {name:$x.name,type:"tuic",server:$s,port:$p,uuid:$x.uuid,password:$x.password,sni:$x.domain,"skip-cert-verify":$c.insecure,udp:true,"congestion-controller":"bbr"}' "$STATE_FILE";;
    anytls) jq -c --arg s "$server" --argjson p "$port" '.protocols.anytls as $x | .certificate as $c | {name:$x.name,type:"anytls",server:$s,port:$p,password:$x.password,sni:$x.domain,"skip-cert-verify":$c.insecure,udp:true}' "$STATE_FILE";;
  esac
}
generate_subscriptions(){
  local out="$STATE_DIR/subscription.txt" host endpoint tag name port uuid path sni pk sid password line tls argo hop ob proxy insecure
  local client="$STATE_DIR/sing-box-client.json" mihomo="$STATE_DIR/mihomo.yaml"
  host=$(address); endpoint=$(uri_host "$host"); insecure=$(jq -r '.certificate.insecure | if . then 1 else 0 end' "$STATE_FILE"); : > "$out"
  jq -n '{log:{level:"warn"},outbounds:[]}' > "$client"; jq -n '{proxies:[],"proxy-groups":[{name:"PROXY",type:"select",proxies:[]}]}' > "$mihomo"
  [[ -n "$host" ]] || { : > "$STATE_DIR/subscription.base64"; : > "$STATE_DIR/mihomo-subscription.txt"; yellow '未填写公网地址，暂不生成分享链接。'; return; }
  for tag in vless vmess hy2 tuic anytls; do
    endpoint=$(uri_host "$host")
    tag_enabled "$tag" || continue; name=$(jq -r ".protocols.$tag.name" "$STATE_FILE"); port=$(jq -r ".protocols.$tag.port" "$STATE_FILE")
    case "$tag" in
      vless) uuid=$(jq -r '.protocols.vless.uuid' "$STATE_FILE"); sni=$(jq -r '.protocols.vless.sni' "$STATE_FILE"); pk=$(jq -r '.protocols.vless.public_key' "$STATE_FILE"); sid=$(jq -r '.protocols.vless.short_id' "$STATE_FILE"); line="vless://${uuid}@${endpoint}:${port}?encryption=none&flow=xtls-rprx-vision&security=reality&sni=${sni}&fp=chrome&pbk=${pk}&sid=${sid}&type=tcp#$(urlencode "$name")";;
      vmess) uuid=$(jq -r '.protocols.vmess.uuid' "$STATE_FILE"); path=$(jq -r '.protocols.vmess.path' "$STATE_FILE"); tls=$(jq -r '.protocols.vmess.tls' "$STATE_FILE"); argo=$(jq -r '.protocols.vmess.argo_domain' "$STATE_FILE"); [[ -n "$argo" ]] && { endpoint=$argo; port=443; tls=true; } || endpoint=$(jq -r --arg d "$host" '.protocols.vmess.cdn // "" | if length>0 then . else $d end' "$STATE_FILE"); line="vmess://$(jq -nc --arg add "$endpoint" --argjson p "$port" --arg id "$uuid" --arg path "$path" --arg ps "$name" --arg tls "$tls" --arg host "${argo:-$(jq -r '.protocols.vmess.domain' "$STATE_FILE")}" '{v:"2",ps:$ps,add:$add,port:($p|tostring),id:$id,aid:"0",net:"ws",type:"none",host:$host,path:$path,tls:(if $tls=="true" then "tls" else "" end),sni:$host}' | base64 | tr -d '\n')";;
      hy2) password=$(jq -r '.protocols.hy2.password' "$STATE_FILE"); sni=$(jq -r '.protocols.hy2.domain' "$STATE_FILE"); hop=$(jq -r '.protocols.hy2.udp_hop' "$STATE_FILE"); line="hysteria2://$(urlencode "$password")@${endpoint}:${port}?security=tls&sni=${sni}&insecure=${insecure}$([[ -n "$hop" ]] && printf '&mport=%s' "$hop")#$(urlencode "$name")";;
      tuic) uuid=$(jq -r '.protocols.tuic.uuid' "$STATE_FILE"); password=$(jq -r '.protocols.tuic.password' "$STATE_FILE"); sni=$(jq -r '.protocols.tuic.domain' "$STATE_FILE"); line="tuic://${uuid}:$(urlencode "$password")@${endpoint}:${port}?congestion_control=bbr&sni=${sni}&insecure=${insecure}#$(urlencode "$name")";;
      anytls) password=$(jq -r '.protocols.anytls.password' "$STATE_FILE"); sni=$(jq -r '.protocols.anytls.domain' "$STATE_FILE"); line="anytls://$(urlencode "$password")@${endpoint}:${port}?sni=${sni}&insecure=${insecure}#$(urlencode "$name")";;
    esac
    printf '%s\n' "$line" >> "$out"
    ob=$(client_outbound_for "$tag" "$host"); jq --argjson x "$ob" '.outbounds += [$x]' "$client" > "$client.next" && mv "$client.next" "$client"
    proxy=$(mihomo_proxy_for "$tag" "$host"); jq --argjson x "$proxy" --arg n "$name" '.proxies += [$x] | ."proxy-groups"[0].proxies += [$n]' "$mihomo" > "$mihomo.next" && mv "$mihomo.next" "$mihomo"
  done
  base64 < "$out" | tr -d '\n' > "$STATE_DIR/subscription.base64"
  cp "$out" "$STATE_DIR/mihomo-subscription.txt"
}

show_nodes(){
  [[ -f "$STATE_FILE" ]] || return
  echo '已启用协议及完整状态（服务监听始终为 ::）：'
  jq '{public_address,certificate,protocols:(.protocols|with_entries(select(.value.enabled)))}' "$STATE_FILE"
  [[ -s "$STATE_DIR/subscription.txt" ]] && { echo; cat "$STATE_DIR/subscription.txt"; }
}
show_qr(){ need qrencode; [[ -s "$STATE_DIR/subscription.txt" ]] || generate_subscriptions; while IFS= read -r link; do qrencode -t ANSIUTF8 "$link"; done < "$STATE_DIR/subscription.txt"; }

install_locked_script(){ # name url sha
  WORKDIR=$(tmpdir); local f="$WORKDIR/$1.sh"; download_verified "$2" "$3" "$f" "$1"; bash "$f"
}
acme(){ install_locked_script acme-yg "https://raw.githubusercontent.com/yonggekkk/acme-yg/${ACME_COMMIT}/acme.sh" "$ACME_SHA256"; }
warp(){ install_locked_script warp-yg "https://raw.githubusercontent.com/yonggekkk/warp-yg/${WARP_COMMIT}/CFwarp.sh" "$WARP_SHA256"; }
bbr(){ install_locked_script bbr "https://raw.githubusercontent.com/teddysun/across/${BBR_COMMIT}/bbr.sh" "$BBR_SHA256"; }
install_sbwpph(){ local a=$(cpu) h; [[ $a == amd64 ]] && h=$SBWPPH_AMD64_SHA256 || h=$SBWPPH_ARM64_SHA256; WORKDIR=$(tmpdir); download_verified "$FORK_RAW/sbwpph_${a}" "$h" "$WORKDIR/sbwpph" sbwpph; install -m 755 "$WORKDIR/sbwpph" "$STATE_DIR/sbwpph"; }
require_install(){ [[ -f "$STATE_FILE" && -x "$STATE_DIR/sing-box" ]] || { red '请先安装。'; return 1; }; }

download_github_asset(){ # asset-id sha output label
  local id=$1 expected=$2 out=$3 label=$4
  curl --fail --location --retry 2 --proto '=https' --tlsv1.2 \
    -H 'Accept: application/octet-stream' \
    -o "$out" "https://api.github.com/repos/MetaCubeX/meta-rules-dat/releases/assets/$id" || die "下载失败：$label"
  verify "$out" "$expected" "$label"
}
install_rule_databases(){
  WORKDIR=$(tmpdir)
  download_github_asset "$METACUBEX_GEOIP_ASSET" "$METACUBEX_GEOIP_SHA256" "$WORKDIR/geoip.db" "MetaCubeX geoip.db ${METACUBEX_RELEASE}"
  download_github_asset "$METACUBEX_GEOSITE_ASSET" "$METACUBEX_GEOSITE_SHA256" "$WORKDIR/geosite.db" "MetaCubeX geosite.db ${METACUBEX_RELEASE}"
  install -m 644 "$WORKDIR/geoip.db" "$STATE_DIR/geoip.db"
  install -m 644 "$WORKDIR/geosite.db" "$STATE_DIR/geosite.db"
}
install_cloudflared(){
  local a h
  a=$(cpu); [[ $a == amd64 ]] && h=$CLOUDFLARED_AMD64_SHA256 || h=$CLOUDFLARED_ARM64_SHA256
  WORKDIR=$(tmpdir)
  download_verified "https://github.com/cloudflare/cloudflared/releases/download/${CLOUDFLARED_VERSION}/cloudflared-linux-${a}" "$h" "$WORKDIR/cloudflared" "Cloudflared ${CLOUDFLARED_VERSION}/${a}"
  install -m 755 "$WORKDIR/cloudflared" "$STATE_DIR/cloudflared"
}

reconcile_hy2_hop(){
  command -v iptables >/dev/null 2>&1 || return 0
  local enabled hop port fw
  enabled=$(jq -r '.protocols.hy2.enabled' "$STATE_FILE"); hop=$(jq -r '.protocols.hy2.udp_hop' "$STATE_FILE"); port=$(jq -r '.protocols.hy2.port' "$STATE_FILE")
  for fw in iptables ip6tables; do
    command -v "$fw" >/dev/null 2>&1 || continue
    "$fw" -t nat -N SBYG_HY2 2>/dev/null || true
    "$fw" -t nat -F SBYG_HY2
    "$fw" -t nat -C PREROUTING -j SBYG_HY2 2>/dev/null || "$fw" -t nat -A PREROUTING -j SBYG_HY2
    [[ "$enabled" == true && -n "$hop" ]] && "$fw" -t nat -A SBYG_HY2 -p udp --dport "$hop" -j REDIRECT --to-ports "$port"
  done
}

reconcile_argo(){
  command -v systemctl >/dev/null 2>&1 || return 0
  local enabled token port
  enabled=$(jq -r '.protocols.vmess.enabled and (.protocols.vmess.argo_token|length>0) and (.protocols.vmess.argo_domain|length>0) and (.protocols.vmess.tls==false)' "$STATE_FILE")
  if [[ "$enabled" != true ]]; then
    systemctl disable --now sing-box-argo.service >/dev/null 2>&1 || true
    return 0
  fi
  [[ -x "$STATE_DIR/cloudflared" ]] || install_cloudflared
  token=$(jq -r '.protocols.vmess.argo_token' "$STATE_FILE"); port=$(jq -r '.protocols.vmess.port' "$STATE_FILE")
  umask 077
  printf 'TUNNEL_TOKEN=%s\n' "$token" > "$STATE_DIR/argo.env"
  cat > /etc/systemd/system/sing-box-argo.service <<EOF
[Unit]
Description=sing-box-yg Cloudflare Tunnel
After=network-online.target sing-box.service
[Service]
Type=simple
EnvironmentFile=$STATE_DIR/argo.env
ExecStart=$STATE_DIR/cloudflared tunnel --no-autoupdate --url http://127.0.0.1:$port run --token \${TUNNEL_TOKEN}
Restart=on-failure
[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  systemd_enable_restart sing-box-argo.service
}

set_address(){
  local choice x candidate
  echo '对外地址：1 设置 IPv4/IPv6/域名  2 自动探测  0 返回上一级'
  ask '选择：' choice
  cancel_input "$choice" && return 0
  case "$choice" in
    1)
      ask 'IPv4、IPv6 或域名（回车/0 取消）：' x
      cancel_input "$x" && return 0
      valid_host "$x" || { red '地址格式无效。'; return 0; }
      candidate=$(jq --arg x "$x" '.public_address=$x' "$STATE_FILE")
      ;;
    2) candidate=$(jq '.public_address=""' "$STATE_FILE");;
    *) red '无效输入。'; return 0;;
  esac
  apply_state "$candidate"
}
set_protocol(){
  local tag=$1 field=$2 prompt=$3 x candidate
  ask "${prompt}（回车/0 返回上一级）" x
  cancel_input "$x" && return 0
  candidate=$(jq --arg t "$tag" --arg f "$field" --arg x "$x" '.protocols[$t][$f]=$x' "$STATE_FILE")
  apply_state "$candidate"
}
set_uuid(){
  local tag=$1 x candidate
  ask 'UUID（回车/0 返回上一级）：' x
  cancel_input "$x" && return 0
  valid_uuid "$x" || { red 'UUID 格式无效。'; return 0; }
  candidate=$(jq --arg t "$tag" --arg x "$x" '.protocols[$t].uuid=$x' "$STATE_FILE")
  apply_state "$candidate"
}
set_bool(){
  local tag=$1 field=$2 x value candidate
  echo '1 开启  2 关闭  0 返回上一级'
  ask '选择：' x
  cancel_input "$x" && return 0
  case "$x" in 1) value=true;;2) value=false;;*) red '无效输入。'; return 0;;esac
  candidate=$(jq --arg t "$tag" --arg f "$field" --argjson x "$value" '.protocols[$t][$f]=$x' "$STATE_FILE")
  apply_state "$candidate"
}
set_number(){
  local tag=$1 field=$2 prompt=$3 x candidate
  ask "${prompt}（回车/0 返回上一级）" x
  cancel_input "$x" && return 0
  [[ "$x" =~ ^[1-9][0-9]*$ ]] || { red '必须输入正整数。'; return 0; }
  candidate=$(jq --arg t "$tag" --arg f "$field" --argjson x "$x" '.protocols[$t][$f]=$x' "$STATE_FILE")
  apply_state "$candidate"
}
set_port(){
  local tag=$1 x candidate current
  ask '新端口（1-65535，回车/0 返回上一级）：' x
  cancel_input "$x" && return 0
  valid_port "$x" || { red '端口无效。'; return 0; }
  current=$(jq -r --arg t "$tag" '.protocols[$t].port' "$STATE_FILE")
  [[ "$x" == "$current" ]] || port_available "$x" || { red '端口被其他进程占用。'; return 0; }
  candidate=$(jq --arg t "$tag" --argjson p "$x" '.protocols[$t].port=$p' "$STATE_FILE")
  apply_state "$candidate"
}
rotate_reality_keys(){
  local pair priv pub sid
  confirm_change || return 0
  pair=$("$STATE_DIR/sing-box" generate reality-keypair); priv=$(awk -F': ' '/PrivateKey/{print $2}' <<<"$pair"); pub=$(awk -F': ' '/PublicKey/{print $2}' <<<"$pair"); sid=$(openssl rand -hex 4)
  [[ -n "$priv" && -n "$pub" ]] || { red '密钥生成失败。'; return 0; }
  apply_state "$(jq --arg priv "$priv" --arg pub "$pub" --arg sid "$sid" '.protocols.vless.private_key=$priv | .protocols.vless.public_key=$pub | .protocols.vless.short_id=$sid' "$STATE_FILE")"
}
now_millis(){
  local value
  value=$(date +%s%3N)
  [[ "$value" =~ ^[0-9]+$ ]] || value="$(date +%s)000"
  printf '%s\n' "$value"
}
probe_reality_once(){ # host [sample-number] -> handshake milliseconds
  local host=$1 start end output
  output=$(mktemp "${TMPDIR:-/tmp}/sing-box-yg-reality.XXXXXX")
  start=$(now_millis)
  if ! timeout 8 openssl s_client -connect "${host}:443" -servername "$host" -tls1_3 -alpn h2 -groups X25519 -verify_hostname "$host" -verify_return_error </dev/null >"$output" 2>&1; then
    rm -f "$output"
    return 1
  fi
  if ! grep -aq 'Verify return code: 0 (ok)' "$output" || ! grep -aq 'ALPN protocol: h2' "$output"; then
    rm -f "$output"
    return 1
  fi
  end=$(now_millis)
  rm -f "$output"
  printf '%s\n' "$((end - start))"
}
scan_reality_candidates(){ # optional candidate arguments; outputs host, successes, average-ms, jitter-ms
  local work host file sample latency successes total min max avg jitter
  local -a candidates=("$@") pids=()
  ((${#candidates[@]})) || candidates=("${REALITY_CANDIDATES[@]}")
  work=$(tmpdir)
  blue "正在从当前 VPS 并发扫描 ${#candidates[@]} 个 Reality 目标，每个采样 ${REALITY_SCAN_SAMPLES} 次..." >&2
  for host in "${candidates[@]}"; do
    file="$work/$(printf '%s' "$host" | tr -c 'A-Za-z0-9._-' '_').result"
    (
      successes=0; total=0; min=2147483647; max=0
      for ((sample=1; sample<=REALITY_SCAN_SAMPLES; sample++)); do
        if latency=$(probe_reality_once "$host" "$sample"); then
          successes=$((successes + 1)); total=$((total + latency))
          ((latency < min)) && min=$latency
          ((latency > max)) && max=$latency
        fi
      done
      if ((successes > 0)); then
        avg=$((total / successes)); jitter=$((max - min))
      else
        avg=999999; jitter=999999
      fi
      printf '%s\t%d\t%d\t%d\n' "$host" "$successes" "$avg" "$jitter" > "$file"
    ) &
    pids+=("$!")
  done
  for sample in "${pids[@]}"; do wait "$sample" || true; done
  # Prefer fully successful and low-latency targets; require at least 2/3 successful samples.
  awk -F '\t' -v minimum="$((REALITY_SCAN_SAMPLES - 1))" '$2 >= minimum' "$work"/*.result |
    sort -t $'\t' -k2,2nr -k3,3n -k4,4n |
    head -n 5
}
choose_scanned_reality_sni(){
  local results choice i host successes avg jitter candidate
  local -a rows=()
  results=$(scan_reality_candidates) || true
  [[ -n "$results" ]] || { red '10 个候选均未达到稳定性要求，原配置未改变。'; return 0; }
  while IFS= read -r candidate; do rows+=("$candidate"); done <<<"$results"
  echo '扫描结果（可用性优先，其次平均延迟和抖动）：'
  for ((i=0; i<${#rows[@]}; i++)); do
    IFS=$'\t' read -r host successes avg jitter <<<"${rows[$i]}"
    printf '%d %-24s 平均 %4d ms  抖动 %4d ms  成功 %d/%d\n' "$((i + 1))" "$host" "$avg" "$jitter" "$successes" "$REALITY_SCAN_SAMPLES"
  done
  echo '0 返回上一级'
  ask '选择扫描目标：' choice
  cancel_input "$choice" && return 0
  [[ "$choice" =~ ^[1-5]$ && "$choice" -le "${#rows[@]}" ]] || { red '无效输入。'; return 0; }
  IFS=$'\t' read -r host successes avg jitter <<<"${rows[$((choice - 1))]}"
  candidate=$(jq --arg x "$host" '.protocols.vless.sni=$x' "$STATE_FILE")
  apply_state "$candidate"
}
set_reality_sni(){
  local choice host results candidate
  echo 'Reality SNI：1 扫描 10 个默认目标并选择  2 扫描自定义目标  0 返回上一级'
  ask '选择：' choice
  cancel_input "$choice" && return 0
  case "$choice" in
    1) choose_scanned_reality_sni;;
    2)
      ask '自定义 Reality SNI（回车/0 返回上一级）：' host
      cancel_input "$host" && return 0
      valid_host "$host" || { red '域名格式无效。'; return 0; }
      [[ "$host" != *:* && ! "$host" =~ ^[0-9.]+$ ]] || { red 'Reality SNI 必须填写域名。'; return 0; }
      results=$(scan_reality_candidates "$host") || true
      [[ -n "$results" ]] || { red '该目标未通过稳定性和 Reality 兼容性检测，原配置未改变。'; return 0; }
      candidate=$(jq --arg x "$host" '.protocols.vless.sni=$x' "$STATE_FILE")
      apply_state "$candidate"
      ;;
    *) red '无效输入。';;
  esac
}
certificate_matches_domain(){ openssl x509 -in "$1" -noout -checkhost "$2" >/dev/null 2>&1; }
certificate_covers_enabled_domains(){
  local cert=$1 tag domain enabled
  for tag in vmess hy2 tuic anytls; do
    enabled=$(jq -r --arg t "$tag" '.protocols[$t].enabled' "$STATE_FILE")
    [[ "$tag" != vmess || $(jq -r '.protocols.vmess.tls' "$STATE_FILE") == true ]] || continue
    [[ "$enabled" == true ]] || continue
    domain=$(jq -r --arg t "$tag" '.protocols[$t].domain' "$STATE_FILE")
    certificate_matches_domain "$cert" "$domain" || {
      red "证书不覆盖已启用的 $(protocol_label "$tag") 域名：${domain}"
      return 1
    }
  done
}
set_certificate(){
  local cert key cert_pub key_pub insecure candidate
  ask '证书完整路径（回车/0 返回上一级）：' cert
  cancel_input "$cert" && return 0
  ask '私钥完整路径（回车/0 返回上一级）：' key
  cancel_input "$key" && return 0
  [[ -r "$cert" && -r "$key" ]] || { red '证书或私钥不可读。'; return 0; }
  openssl x509 -in "$cert" -noout >/dev/null 2>&1 || { red '证书格式无效。'; return 0; }
  cert_pub=$(openssl x509 -in "$cert" -pubkey -noout | openssl pkey -pubin -outform pem 2>/dev/null | openssl dgst -sha256 | awk '{print $NF}') || true
  key_pub=$(openssl pkey -in "$key" -pubout -outform pem 2>/dev/null | openssl dgst -sha256 | awk '{print $NF}') || true
  [[ -n "$cert_pub" && "$cert_pub" == "$key_pub" ]] || { red '证书和私钥不匹配。'; return 0; }
  echo '客户端证书校验：1 跳过（自签）  2 验证（受信证书）  0 返回上一级'
  ask '选择：' insecure
  cancel_input "$insecure" && return 0
  case "$insecure" in 1) insecure=true;;2) insecure=false;;*) red '无效输入。'; return 0;;esac
  [[ "$insecure" == true ]] || certificate_covers_enabled_domains "$cert" || {
    red '请先在证书管理中把协议域名改为证书覆盖的域名，再重新导入。'
    return 0
  }
  candidate=$(jq --arg cert "$cert" --arg key "$key" --argjson insecure "$insecure" '.certificate.cert=$cert | .certificate.key=$key | .certificate.insecure=$insecure' "$STATE_FILE")
  apply_state "$candidate"
}
show_certificate(){
  local cert
  cert=$(jq -r '.certificate.cert' "$STATE_FILE")
  echo '当前全局 TLS 证书：'
  jq '{certificate,domains:{vmess:.protocols.vmess.domain,hy2:.protocols.hy2.domain,tuic:.protocols.tuic.domain,anytls:.protocols.anytls.domain}}' "$STATE_FILE"
  if [[ -r "$cert" ]]; then
    openssl x509 -in "$cert" -noout -subject -issuer -dates
    openssl x509 -in "$cert" -noout -ext subjectAltName 2>/dev/null || true
  else
    yellow '当前证书文件不可读。'
  fi
}
set_tls_domain(){
  local tag=$1 domain cert insecure candidate
  ask '证书域名（回车/0 返回上一级）：' domain
  cancel_input "$domain" && return 0
  valid_host "$domain" && [[ "$domain" != *:* && ! "$domain" =~ ^[0-9.]+$ ]] || { red '必须填写有效域名。'; return 0; }
  cert=$(jq -r '.certificate.cert' "$STATE_FILE"); insecure=$(jq -r '.certificate.insecure' "$STATE_FILE")
  if [[ -r "$cert" ]] && ! certificate_matches_domain "$cert" "$domain"; then
    if [[ "$insecure" == false ]]; then
      red '当前受信证书不覆盖该域名，拒绝修改。请先导入匹配证书。'
      return 0
    fi
    yellow '当前为跳过证书校验模式，证书未覆盖该域名；客户端仍会跳过验证。'
  fi
  if [[ "$tag" == all ]]; then
    candidate=$(jq --arg d "$domain" '.protocols.vmess.domain=$d | .protocols.hy2.domain=$d | .protocols.tuic.domain=$d | .protocols.anytls.domain=$d' "$STATE_FILE")
  else
    candidate=$(jq --arg t "$tag" --arg d "$domain" '.protocols[$t].domain=$d' "$STATE_FILE")
  fi
  apply_state "$candidate"
}
tls_domain_menu(){
  local choice tag
  while :; do
    echo '证书域名：1 Vmess-WS  2 Hysteria2  3 TUIC v5  4 AnyTLS  5 全部统一  0 返回上一级'
    ask '选择：' choice
    case "$choice" in
      1)tag=vmess;;2)tag=hy2;;3)tag=tuic;;4)tag=anytls;;5)tag=all;;0|'')return 0;;*)red '无效输入。'; continue;;
    esac
    set_tls_domain "$tag"
  done
}
certificate_menu(){
  local choice
  while :; do
    echo '证书管理：1 查看证书和域名  2 导入证书/私钥  3 配置协议证书域名  4 运行 ACME 签发脚本  0 返回上一级'
    ask '选择：' choice
    case "$choice" in
      1)show_certificate;;2)set_certificate;;3)tls_domain_menu;;
      4)acme; yellow 'ACME 签发完成后，请回到“导入证书/私钥”绑定生成的证书路径。';;
      0|'')return 0;;*)red '无效输入。';;
    esac
  done
}
set_hy2_hop(){
  local choice x start end candidate
  echo 'UDP 端口跳跃：1 设置范围  2 关闭  0 返回上一级'
  ask '选择：' choice
  cancel_input "$choice" && return 0
  case "$choice" in
    1)
      ask '范围，例如 20000:30000（回车/0 返回上一级）：' x
      cancel_input "$x" && return 0
      [[ "$x" =~ ^([1-9][0-9]{0,4}):([1-9][0-9]{0,4})$ ]] || { red '范围格式无效。'; return 0; }
    start=${BASH_REMATCH[1]}; end=${BASH_REMATCH[2]}
      valid_port "$start" && valid_port "$end" && (( start <= end )) || { red '端口范围无效。'; return 0; }
      ;;
    2) x=;;
    *) red '无效输入。'; return 0;;
  esac
  candidate=$(jq --arg x "$x" '.protocols.hy2.udp_hop=$x' "$STATE_FILE")
  apply_state "$candidate"
}
set_argo(){
  local choice domain token candidate
  echo 'Argo 固定隧道：1 设置  2 关闭  0 返回上一级'
  ask '选择：' choice
  cancel_input "$choice" && return 0
  if [[ "$choice" == 2 ]]; then
    candidate=$(jq '.protocols.vmess.argo_domain="" | .protocols.vmess.argo_token=""' "$STATE_FILE")
  elif [[ "$choice" == 1 ]]; then
    ask 'Argo 固定域名（回车/0 返回上一级）：' domain
    cancel_input "$domain" && return 0
    valid_host "$domain" || { red '域名格式无效。'; return 0; }
    ask 'Cloudflare Tunnel token（回车/0 返回上一级）：' token
    cancel_input "$token" && return 0
    candidate=$(jq --arg d "$domain" --arg t "$token" '.protocols.vmess.argo_domain=$d | .protocols.vmess.argo_token=$t | .protocols.vmess.tls=false' "$STATE_FILE")
  else
    red '无效输入。'; return 0
  fi
  apply_state "$candidate"
}
toggle_protocol(){
  local tag=$1 state p
  confirm_change || return 0
  if tag_enabled "$tag"; then
    (( $(enabled_count) > 1 )) || { red '至少保留一个协议。'; return 0; }
    state=$(jq --arg t "$tag" '.protocols[$t].enabled=false' "$STATE_FILE")
  else
    [[ "$tag" != anytls || $(jq -r '.core' "$STATE_FILE") != 1.10* ]] || { red '1.10 内核不支持 AnyTLS。'; return 0; }
    p=$(jq -r --arg t "$tag" '.protocols[$t].port' "$STATE_FILE")
    if ! valid_port "$p" || ! port_available "$p"; then while :; do p=$(random_port); port_available "$p" && ! jq -e --argjson p "$p" '[.protocols[]|select(.enabled)|.port] | index($p)' "$STATE_FILE" >/dev/null && break; done; fi
    state=$(jq --arg t "$tag" --argjson p "$p" '.protocols[$t].port=$p | .protocols[$t].enabled=true' "$STATE_FILE")
  fi
  apply_state "$state"
}

credential_menu(){
  local tag=$1 choice
  while :; do
    case "$tag" in
      vless|vmess)
        echo '凭据：1 UUID  0 返回上一级'
        ask '选择：' choice
        case "$choice" in 1)set_uuid "$tag";;0|'')return 0;;*)red '无效输入。';;esac
        ;;
      tuic)
        echo '凭据：1 UUID  2 密码  0 返回上一级'
        ask '选择：' choice
        case "$choice" in 1)set_uuid tuic;;2)set_protocol tuic password '密码：';;0|'')return 0;;*)red '无效输入。';;esac
        ;;
      hy2|anytls)
        echo '凭据：1 密码  0 返回上一级'
        ask '选择：' choice
        case "$choice" in 1)set_protocol "$tag" password '密码：';;0|'')return 0;;*)red '无效输入。';;esac
        ;;
    esac
  done
}

specialty_menu(){
  local tag=$1 choice
  while :; do
    case "$tag" in
      vless)
        echo '专项参数：1 Reality SNI  2 轮换 Reality 密钥和 Short ID  0 返回上一级'
        ask '选择：' choice
        case "$choice" in 1)set_reality_sni;;2)rotate_reality_keys;;0|'')return 0;;*)red '无效输入。';;esac
        ;;
      vmess)
        echo '专项参数：1 WS Path  2 TLS  3 CDN 地址  4 Argo 固定隧道  5 证书域名  0 返回上一级'
        ask '选择：' choice
        case "$choice" in 1)set_protocol vmess path 'WS Path（以 / 开头）：';;2)set_bool vmess tls;;3)set_protocol vmess cdn 'CDN 地址：';;4)set_argo;;5)set_tls_domain vmess;;0|'')return 0;;*)red '无效输入。';;esac
        ;;
      hy2)
        echo '专项参数：1 证书域名  2 上行 Mbps  3 下行 Mbps  4 UDP 端口跳跃  0 返回上一级'
        ask '选择：' choice
        case "$choice" in 1)set_tls_domain hy2;;2)set_number hy2 up_mbps '上行 Mbps：';;3)set_number hy2 down_mbps '下行 Mbps：';;4)set_hy2_hop;;0|'')return 0;;*)red '无效输入。';;esac
        ;;
      tuic|anytls)
        echo '专项参数：1 证书域名  0 返回上一级'
        echo '普通 TLS 的 SNI 应匹配实际证书域名，不使用 Reality 的伪装候选。'
        ask '选择：' choice
        case "$choice" in 1)set_tls_domain "$tag";;0|'')return 0;;*)red '无效输入。';;esac
        ;;
    esac
  done
}

protocol_menu(){
  local tag choice
  while :; do
    echo '协议：1 Vless-Reality  2 Vmess-WS  3 Hysteria2  4 TUIC v5  5 AnyTLS  0 返回上一级'
    ask '选择协议：' choice
    case "$choice" in 1) tag=vless;;2) tag=vmess;;3) tag=hy2;;4) tag=tuic;;5) tag=anytls;;0|'') return 0;;*) red '无效输入。'; continue;; esac
    while :; do
      echo "$(protocol_label "$tag")：1 启用/停用  2 名称  3 端口  4 凭据  5 专项参数  0 返回上一级"
      ask '选择：' choice
      case "$choice" in
        1) toggle_protocol "$tag";;2) set_protocol "$tag" name '节点名称：';;3) set_port "$tag";;
        4) credential_menu "$tag";;5) specialty_menu "$tag";;0|'') break;;*) red '无效输入。';;
      esac
    done
  done
}

configure_menu(){
  while :; do echo; echo '配置菜单：1 查看节点 2 管理协议 3 对外地址 4 二维码 5 重新生成订阅 6 证书管理 7 应用当前配置并重启 0 返回'; ask '选择：' m
    case "$m" in
      1) show_nodes;;2) protocol_menu;;3) set_address;;4) show_qr;;
      5) if confirm_change; then generate_subscriptions; green '订阅已更新。'; fi;;
      6)certificate_menu;;7)apply_current_state || true;;0|'') return 0;;*) red '无效输入。';;
    esac
  done
}

install_flow(){
  local core choice state
  echo "选择内核：1 ${SB_DEFAULT}（默认，含 AnyTLS）  2 ${SB_110}（不含 AnyTLS）  0 返回上一级"
  ask '选择：' choice
  case "$choice" in 1)core=$SB_DEFAULT;;2)core=$SB_110;;0|'')return 0;;*)red '无效输入。'; return 0;;esac
  install_packages
  install_singbox "$core"; install_rule_databases
  if ! state=$(select_protocols "$core"); then yellow '安装已取消，尚未写入协议状态或启动服务。'; return 0; fi
  printf '%s\n' "$state" | commit_state
  printf '%s\n' "$core" > "$STATE_DIR/core-version"
  update_script
  green '安装完成。可通过 sb → 配置菜单随时增删协议。'
}

update_script(){
  local expected
  WORKDIR=$(tmpdir)
  expected=$(curl -fsSL --proto '=https' --tlsv1.2 "$FORK_RAW/sb.sh.sha256" | awk '{print $1}')
  [[ "$expected" =~ ^[0-9a-f]{64}$ ]] || die 'fork 快捷脚本校验文件无效，拒绝更新。'
  download_verified "$FORK_RAW/sb.sh" "$expected" "$WORKDIR/sb.sh" 'fork 快捷脚本'
  install -m 755 "$WORKDIR/sb.sh" /usr/local/bin/sb
}
check_version(){
  local latest
  latest=$(curl -fsSL --max-time 3 "$FORK_RAW/version" 2>/dev/null | head -n1 || true)
  [[ -n "$latest" && "$latest" != "$SCRIPT_VERSION"* ]] && yellow "fork main 可用版本：$latest（当前 $SCRIPT_VERSION）"
  return 0
}
update_core(){
  local v candidate tmp cfg
  echo "锁定版本：${SB_DEFAULT}；也可输入其他官方版本。"
  ask '版本号（回车/0 返回上一级）：' v
  cancel_input "$v" && return 0
  [[ "$v" != 1.10* || $(jq -r '.protocols.anytls.enabled' "$STATE_FILE") != true ]] || { red '请先停用 AnyTLS，再切换到 1.10。'; return 0; }
  install_singbox "$v" "$STATE_DIR/sing-box.new"
  candidate=$(jq --arg v "$v" '.core=$v' "$STATE_FILE"); tmp=$(tmpdir); cfg="$tmp/sb.json"; render_config "$candidate" "$cfg"
  "$STATE_DIR/sing-box.new" check -c "$cfg" || { rm -f "$STATE_DIR/sing-box.new"; red '新内核无法验证现有配置，拒绝切换。'; return 0; }
  cp "$STATE_DIR/sing-box" "$STATE_DIR/sing-box.old"; mv "$STATE_DIR/sing-box.new" "$STATE_DIR/sing-box"
  if printf '%s\n' "$candidate" | commit_state; then printf '%s\n' "$v" > "$STATE_DIR/core-version"; rm -f "$STATE_DIR/sing-box.old"; else mv "$STATE_DIR/sing-box.old" "$STATE_DIR/sing-box"; restart_service || true; fi
}
uninstall(){
  read -r -p '确认卸载 VPS Sing-box（输入 YES；回车/0 返回上一级）：' x; [[ $x == YES ]] || return 0
  systemctl disable --now sing-box sing-box-argo 2>/dev/null || true
  rc-service sing-box stop 2>/dev/null || true
  rc-update del sing-box default 2>/dev/null || true
  rm -rf "$STATE_DIR" /etc/systemd/system/sing-box.service /etc/systemd/system/sing-box-argo.service /etc/init.d/sing-box
  systemctl daemon-reload 2>/dev/null || true
}

main(){
  need_root; if [[ -f "$CONFIG_FILE" && ! -f "$STATE_FILE" ]]; then die '检测到旧版安装，缺少新版状态文件。为避免错误修改，请先卸载后重装。'; fi
  check_version
  while :; do echo; echo "sing-box-yg ${SCRIPT_VERSION}（VPS fork）"; echo '1 安装  2 配置/节点/订阅  3 应用配置/重启  4 更新内核  5 更新脚本  6 日志  7 Acme  8 WARP  9 BBR  10 WARP-plus  11 卸载  0 退出'; ask '选择：' m
    case "$m" in 1) [[ -f "$STATE_FILE" ]] && red '已安装；请使用配置菜单。' || install_flow;;2) require_install && configure_menu;;3) require_install && { apply_current_state || true; };;4) require_install && update_core;;5) update_script;;6) require_install && { command -v journalctl >/dev/null && journalctl -u sing-box -n 100 --no-pager || tail -n 100 /var/log/messages; };;7) acme;;8) warp;;9) bbr;;10) require_install && install_sbwpph;;11) uninstall;;0) exit 0;;*) red '无效输入。';;esac
  done
}
[[ "${SBYG_LIB_ONLY:-0}" == 1 ]] || main "$@"
