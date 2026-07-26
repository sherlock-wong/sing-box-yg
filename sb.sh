#!/usr/bin/env bash
# VPS-only sing-box-yg manager.  Serv00 files intentionally live separately.
set -Eeuo pipefail
export LANG=C.UTF-8

SCRIPT_VERSION='v26.7.25-fork.12'
FORK_OWNER='sherlock-wong'
FORK_REPO='sing-box-yg'
STATE_DIR="${SBYG_STATE_DIR:-/etc/s-box}"
STATE_FILE="$STATE_DIR/protocols.json"
CONFIG_FILE="$STATE_DIR/sb.json"
XRAY_CONFIG_FILE="$STATE_DIR/xray.json"
SERVICE_NAME=sing-box
XRAY_SERVICE_NAME=sing-box-xray
FORK_BRANCH=
FORK_RAW=

# Keep this block and DEPENDENCY_LOCKS.md in sync when refreshing a dependency.
SB_DEFAULT=1.13.14
SB_110=1.10.7
XRAY_DEFAULT=26.3.27
XRAY_AMD64_SHA256=23cd9af937744d97776ee35ecad4972cf4b2109d1e0fe6be9930467608f7c8ae
XRAY_ARM64_SHA256=4d30283ae614e3057f730f67cd088a42be6fdf91f8639d82cb69e48cde80413c
XRAY_AMD64_DGST_SHA256=052fc1c5c4bd5b44d799f785792a9631bce8da4aa0d385a783e9a711ad352a58
XRAY_ARM64_DGST_SHA256=1cafbf4fa746688990a12a6d344b638f706e531f1b81b8b583f9b2164561ad2f
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
REALITY_TARGETS_SHA256=eb83de80c1aaee01b11cceed5610ac3936ef7fbbcbfce49738a4a6503a010bda

red(){ printf '\033[31;1m%s\033[0m\n' "$*"; }
green(){ printf '\033[32;1m%s\033[0m\n' "$*"; }
yellow(){ printf '\033[33;1m%s\033[0m\n' "$*"; }
blue(){ printf '\033[36;1m%s\033[0m\n' "$*"; }
dim(){ printf '\033[2m%s\033[0m\n' "$*"; }
die(){ red "$*"; exit 1; }
ask(){ read -r -p "$1" "$2"; }
need_root(){ [[ $EUID -eq 0 ]] || die '请使用 root 运行。'; }
need(){ command -v "$1" >/dev/null 2>&1 || die "缺少依赖：$1"; }
sha256(){ sha256sum "$1" 2>/dev/null || shasum -a 256 "$1"; }
verify(){ [[ "$(sha256 "$1" | awk '{print $1}')" == "$2" ]] || die "完整性校验失败：$3（已保留原配置，未执行文件）"; }
valid_channel(){
  [[ "$1" =~ ^[A-Za-z0-9][A-Za-z0-9._/-]*$ && "$1" != *..* && "$1" != *//* && "$1" != */ ]]
}
set_channel(){
  valid_channel "$1" || { red '渠道格式无效，只能使用安全的 Git 分支或标签名称。'; return 1; }
  FORK_BRANCH=$1
  FORK_RAW="https://raw.githubusercontent.com/${FORK_OWNER}/${FORK_REPO}/${FORK_BRANCH}"
}
load_channel(){
  local selected=${SBYG_CHANNEL:-}
  if [[ -z "$selected" && -r "$STATE_DIR/channel" ]]; then
    IFS= read -r selected < "$STATE_DIR/channel" || true
  fi
  [[ -n "$selected" ]] || selected=main
  set_channel "$selected" || set_channel main
}
persist_channel(){
  local channel_tmp
  install -d -m 755 "$STATE_DIR"
  channel_tmp="$STATE_DIR/.channel.$$"
  printf '%s\n' "$FORK_BRANCH" > "$channel_tmp"
  chmod 600 "$channel_tmp"
  mv "$channel_tmp" "$STATE_DIR/channel"
}
menu_rule(){ printf '%s\n' '────────────────────────────────────────'; }
menu_header(){
  echo
  blue "$1"
  menu_rule
  [[ -n "${2:-}" ]] && dim "当前位置：$2"
  return 0
}
menu_item(){ printf '  %2s. %s\n' "$1" "$2"; }
menu_back(){ printf '\n'; menu_item 0 "$1"; }
confirm_change(){
  local x title=${1:-确认执行这项修改？} detail=${2:-}
  echo
  yellow "$title"
  [[ -n "$detail" ]] && printf '  %s\n' "$detail"
  menu_item 1 '确认'
  menu_item 0 '取消并返回'
  ask '请选择 [0-1]：' x
  [[ "$x" == 1 ]]
}
load_channel
normalize_state(){ # schema 1/2 -> schema 3/per-protocol engine
  jq --arg xray "$XRAY_DEFAULT" '
    if .schema == 1 then
      .certificates = {
        default: ((.certificate // {}) + {
          name:"迁移证书",
          mode:(.certificate.mode // (if .certificate.insecure then "pinned" else "trusted" end)),
          insecure:(.certificate.insecure // true)
        })
      } |
      .protocols.vmess.certificate_id="default" |
      .protocols.hy2.certificate_id="default" |
      .protocols.anytls.certificate_id="default" |
      del(.certificate)
    else . end |
    del(.protocols.tuic) |
    .protocols.vless.engine = (.protocols.vless.engine // "sing-box") |
    .protocols.vmess.engine = "sing-box" |
    .protocols.hy2.engine = "sing-box" |
    .protocols.anytls.engine = "sing-box" |
    .protocols.vless.xray = ({
      target:((.protocols.vless.sni // "www.apple.com")+":443"),
      server_names:[(.protocols.vless.sni // "www.apple.com")],
      fingerprint:"chrome", spider_x:"/", max_time_diff:0,
      min_client_ver:"", max_client_ver:"",
      mldsa65_seed:"", mldsa65_verify:"",
      fallback_profile:"off"
    } * (.protocols.vless.xray // {})) |
    .xray_core = (.xray_core // $xray) |
    .schema=3
  '
}
certificate_id_for_tag(){ jq -r --arg t "$1" '.protocols[$t].certificate_id' "$STATE_FILE"; }
certificate_mode(){
  local source=$1 tag=${2:-anytls}
  jq -r --arg t "$tag" '.certificates[.protocols[$t].certificate_id] |
    .mode // (if .insecure then "pinned" else "trusted" end)' "$source"
}
certificate_der_sha256(){
  openssl x509 -in "$1" -outform DER 2>/dev/null | sha256sum | awk '{print $1}'
}
certificate_spki_sha256(){
  openssl x509 -in "$1" -pubkey -noout 2>/dev/null |
    openssl pkey -pubin -outform DER 2>/dev/null |
    openssl dgst -sha256 -binary |
    openssl base64 -A
}
client_certificate_pin(){
  local tag=$1 mode cert pin core major minor
  core=$(jq -r '.core' "$STATE_FILE")
  IFS=. read -r major minor _ <<<"$core"
  [[ "$major" =~ ^[0-9]+$ && "$minor" =~ ^[0-9]+$ ]] || return 1
  (( major > 1 || (major == 1 && minor >= 13) )) || return 0
  mode=$(certificate_mode "$STATE_FILE" "$tag")
  [[ "$mode" == pinned ]] || return 0
  cert=$(jq -r --arg t "$tag" '.certificates[.protocols[$t].certificate_id].cert' "$STATE_FILE")
  [[ -r "$cert" ]] || { red '固定模式的证书不可读，拒绝生成不安全的客户端配置。' >&2; return 1; }
  pin=$(certificate_spki_sha256 "$cert") || true
  [[ "$pin" =~ ^[A-Za-z0-9+/]{43}=$ ]] || { red '证书公钥 SHA256 计算失败，拒绝生成不安全的客户端配置。' >&2; return 1; }
  printf '%s\n' "$pin"
}
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
protocol_label(){ case "$1" in vless) echo Vless-Reality;; vmess) echo Vmess-WS;; hy2) echo Hysteria2;; anytls) echo AnyTLS;; esac; }
protocol_transport(){ case "$1" in vless|vmess|anytls) echo TCP;; hy2) echo UDP;; esac; }
protocol_detail(){
  local tag=$1 detail mode tls cert_id cert_name engine
  case "$tag" in
    vless)
      engine=$(jq -r '.protocols.vless.engine' "$STATE_FILE")
      [[ "$engine" == xray ]] && engine='Xray' || engine='Sing-box'
      detail="${engine} · SNI $(jq -r '.protocols.vless.sni' "$STATE_FILE")"
      ;;
    vmess)
      tls=$(jq -r '.protocols.vmess.tls' "$STATE_FILE")
      if [[ "$tls" == true ]]; then
        cert_id=$(certificate_id_for_tag vmess)
        cert_name=$(jq -r --arg id "$cert_id" '.certificates[$id].name' "$STATE_FILE")
        detail="TLS · ${cert_name}"
      else
        detail='WS（无 TLS）'
      fi
      ;;
    hy2|anytls)
      mode=$(certificate_mode "$STATE_FILE" "$tag")
      cert_id=$(certificate_id_for_tag "$tag")
      cert_name=$(jq -r --arg id "$cert_id" '.certificates[$id].name' "$STATE_FILE")
      [[ "$mode" == trusted ]] && mode='受信证书' || mode='固定证书'
      detail="${mode} · ${cert_name}"
      ;;
  esac
  printf '%s\n' "$detail"
}
protocol_status_line(){
  local index=$1 tag=$2 line port
  if tag_enabled "$tag"; then
    port=$(jq -r --arg t "$tag" '.protocols[$t].port' "$STATE_FILE")
    printf -v line '  %s. %-16s [已启用] %-3s %-5s  %s' \
      "$index" "$(protocol_label "$tag")" "$(protocol_transport "$tag")" "$port" "$(protocol_detail "$tag")"
    green "$line"
  else
    printf -v line '  %s. %-16s [未启用]' "$index" "$(protocol_label "$tag")"
    dim "$line"
  fi
}
service_runtime_status(){
  local state sing=未用 xray=未用
  if [[ ! -f "$STATE_FILE" ]]; then
    printf '未安装'
    return
  fi
  state=$(cat "$STATE_FILE")
  if state_uses_engine "$state" sing-box; then
    if command -v systemctl >/dev/null 2>&1 && systemctl is-active --quiet sing-box 2>/dev/null; then sing=运行
    elif command -v rc-service >/dev/null 2>&1 && rc-service sing-box status >/dev/null 2>&1; then sing=运行
    else sing=停止
    fi
  fi
  if state_uses_engine "$state" xray; then
    if command -v systemctl >/dev/null 2>&1 && systemctl is-active --quiet sing-box-xray 2>/dev/null; then xray=运行
    elif command -v rc-service >/dev/null 2>&1 && rc-service sing-box-xray status >/dev/null 2>&1; then xray=运行
    else xray=停止
    fi
  fi
  printf 'SB:%s XR:%s' "$sing" "$xray"
}
main_dashboard(){
  local core='--' xray= protocols=0
  if [[ -f "$STATE_FILE" ]]; then
    core=$(jq -r '.core // "--"' "$STATE_FILE" 2>/dev/null || printf '%s' '--')
    jq -e '.protocols.vless.enabled and .protocols.vless.engine=="xray"' "$STATE_FILE" >/dev/null 2>&1 &&
      xray=" / Xray $(jq -r '.xray_core' "$STATE_FILE")"
    protocols=$(enabled_count 2>/dev/null || printf '0')
  fi
  menu_header "sing-box-yg ${SCRIPT_VERSION}" '主菜单'
  printf '  渠道：%-18s 服务：%s\n' "$FORK_BRANCH" "$(service_runtime_status)"
  printf '  内核：Sing-box %s%s\n' "$core" "$xray"
  printf '  协议：%s 个已启用\n' "$protocols"
  menu_rule
}
cancel_input(){ [[ -z "$1" || "$1" == 0 ]]; }
apply_state(){ printf '%s\n' "$1" | commit_state || true; }

install_packages(){
  if command -v apt-get >/dev/null; then apt-get update -y && apt-get install -y curl jq openssl qrencode tar unzip iproute2 iptables coreutils ca-certificates;
  elif command -v dnf >/dev/null; then dnf install -y curl jq openssl qrencode tar unzip iproute iptables coreutils ca-certificates;
  elif command -v yum >/dev/null; then yum install -y curl jq openssl qrencode tar unzip iproute iptables coreutils ca-certificates;
  elif command -v apk >/dev/null; then apk add --no-cache curl jq openssl qrencode tar unzip iproute2 iptables coreutils ca-certificates;
  else die '仅支持 Debian/Ubuntu、RHEL 系和 Alpine。'; fi
}

download_verified(){ # URL SHA OUT DESCRIPTION
  local url=$1 expected=$2 out=$3 label=$4
  curl --fail --location --retry 2 --proto '=https' --tlsv1.2 -o "$out" "$url" || die "下载失败：$label"
  verify "$out" "$expected" "$label"
}

reality_targets_file(){ printf '%s/reality-targets.txt\n' "$STATE_DIR"; }
sync_reality_targets(){ # [force: 0|1]
  local force=${1:-0} target_file temp
  target_file=$(reality_targets_file)
  [[ "$force" != 1 && -s "$target_file" ]] && return 0
  temp=$(tmpdir)/reality-targets.txt
  if ! curl --fail --location --retry 2 --proto '=https' --tlsv1.2 -o "$temp" "$FORK_RAW/assets/reality-targets.txt"; then
    red '无法下载 Reality 候选域名清单。' >&2
    return 1
  fi
  if [[ "$(sha256 "$temp" | awk '{print $1}')" != "$REALITY_TARGETS_SHA256" ]]; then
    red 'Reality 候选域名清单完整性校验失败。' >&2
    return 1
  fi
  install -d -m 755 "$STATE_DIR"
  install -m 644 "$temp" "$target_file"
}
reality_target_candidates(){ # prints validated local file entries, one per line
  local target_file raw target count=0 seen=$'\n'
  target_file=$(reality_targets_file)
  sync_reality_targets || return 1
  while IFS= read -r raw || [[ -n "$raw" ]]; do
    target=${raw%%#*}; target=${target//[[:space:]]/}
    [[ -n "$target" ]] || continue
    valid_host "$target" && [[ "$target" != *:* && ! "$target" =~ ^[0-9.]+$ ]] || {
      red "Reality 候选域名清单包含无效域名：${target}" >&2
      return 1
    }
    [[ "$seen" == *$'\n'"$target"$'\n'* ]] && continue
    seen+="$target"$'\n'
    printf '%s\n' "$target"
    count=$((count + 1))
  done < "$target_file"
  ((count > 0)) || { red 'Reality 候选域名清单为空。' >&2; return 1; }
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

install_xray(){
  local arch asset archive_hash digest_hash base archive digest expected
  arch=$(cpu)
  case "$arch" in
    amd64)
      asset='Xray-linux-64.zip'
      archive_hash=$XRAY_AMD64_SHA256
      digest_hash=$XRAY_AMD64_DGST_SHA256
      ;;
    arm64)
      asset='Xray-linux-arm64-v8a.zip'
      archive_hash=$XRAY_ARM64_SHA256
      digest_hash=$XRAY_ARM64_DGST_SHA256
      ;;
  esac
  base="https://github.com/XTLS/Xray-core/releases/download/v${XRAY_DEFAULT}"
  WORKDIR=$(tmpdir); archive="$WORKDIR/$asset"; digest="$WORKDIR/$asset.dgst"
  download_verified "$base/$asset.dgst" "$digest_hash" "$digest" "Xray ${XRAY_DEFAULT} 官方摘要"
  expected=$(awk -F'= *' '$1=="SHA2-256"{print tolower($2)}' "$digest")
  [[ "$expected" =~ ^[0-9a-f]{64}$ && "$expected" == "$archive_hash" ]] ||
    die 'Xray 官方摘要与预置锁定值不一致，拒绝安装。'
  download_verified "$base/$asset" "$expected" "$archive" "Xray ${XRAY_DEFAULT}/${arch}"
  unzip -q "$archive" xray -d "$WORKDIR" || die 'Xray 发布包内容无效。'
  [[ -x "$WORKDIR/xray" ]] || die 'Xray 发布包缺少可执行文件。'
  install -d -m 755 "$STATE_DIR"
  install -m 755 "$WORKDIR/xray" "$STATE_DIR/xray"
  printf '%s\n' "$XRAY_DEFAULT" > "$STATE_DIR/xray-version"
  printf '%s\n' "$archive_hash" > "$STATE_DIR/xray-archive.sha256"
  chmod 600 "$STATE_DIR/xray-version" "$STATE_DIR/xray-archive.sha256"
  green "已下载并双重校验 Xray-core ${XRAY_DEFAULT}。"
}
xray_install_valid(){
  local expected reported
  [[ -x "$STATE_DIR/xray" &&
     -r "$STATE_DIR/xray-version" &&
     -r "$STATE_DIR/xray-archive.sha256" ]] || return 1
  case "$(cpu)" in
    amd64) expected=$XRAY_AMD64_SHA256;;
    arm64) expected=$XRAY_ARM64_SHA256;;
  esac
  [[ $(cat "$STATE_DIR/xray-version") == "$XRAY_DEFAULT" &&
     $(cat "$STATE_DIR/xray-archive.sha256") == "$expected" ]] || return 1
  reported=$("$STATE_DIR/xray" version 2>/dev/null) || return 1
  reported=${reported%%$'\n'*}
  [[ "$reported" == "Xray ${XRAY_DEFAULT} "* ]]
}

write_services(){
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
    cat > /etc/systemd/system/sing-box-xray.service <<EOF
[Unit]
Description=sing-box-yg Xray
After=network-online.target
[Service]
Type=simple
ExecStart=$STATE_DIR/xray run -c $XRAY_CONFIG_FILE
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
    cat > /etc/init.d/sing-box-xray <<EOF
#!/sbin/openrc-run
description="sing-box-yg Xray"
command="$STATE_DIR/xray"
command_args="run -c $XRAY_CONFIG_FILE"
command_background=true
pidfile="/run/xray.pid"
depend() { need net; }
EOF
    chmod 755 /etc/init.d/sing-box-xray
  else
    return 1
  fi
}
systemd_enable_restart(){
  systemctl enable "$1" >/dev/null 2>&1 || return 1
  systemctl restart "$1"
}
state_uses_engine(){ # state-json engine
  jq -e --arg engine "$2" '[.protocols[] | select(.enabled and .engine==$engine)] | length > 0' <<<"$1" >/dev/null
}
stop_disable_service(){
  local service=$1
  if command -v systemctl >/dev/null; then
    systemctl disable --now "$service" >/dev/null 2>&1 || true
  else
    rc-service "$service" stop >/dev/null 2>&1 || true
    rc-update del "$service" default >/dev/null 2>&1 || true
  fi
}
reconcile_core_services(){ # state-json
  local state=$1
  # Stop both managed cores first: changing the owner of a Reality port cannot
  # succeed if the old core is still listening on that same port.
  stop_disable_service "$SERVICE_NAME"
  stop_disable_service "$XRAY_SERVICE_NAME"
  if command -v systemctl >/dev/null; then
    if state_uses_engine "$state" sing-box; then systemd_enable_restart "$SERVICE_NAME"; fi
    if state_uses_engine "$state" xray; then systemd_enable_restart "$XRAY_SERVICE_NAME"; fi
  else
    if state_uses_engine "$state" sing-box; then
      rc-update add sing-box default >/dev/null 2>&1 || true
      rc-service sing-box restart
    fi
    if state_uses_engine "$state" xray; then
      rc-update add sing-box-xray default >/dev/null 2>&1 || true
      rc-service sing-box-xray restart
    fi
  fi
}
restart_service(){
  [[ -f "$STATE_FILE" ]] || return 1
  reconcile_core_services "$(cat "$STATE_FILE")"
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
  openssl req -x509 -newkey rsa:2048 -nodes -days 3650 -subj '/CN=www.bing.com' \
    -addext 'subjectAltName=DNS:www.bing.com' \
    -addext 'basicConstraints=critical,CA:FALSE' \
    -addext 'keyUsage=critical,digitalSignature,keyEncipherment' \
    -addext 'extendedKeyUsage=serverAuth' \
    -keyout "$cert_key" -out "$cert_crt" >/dev/null 2>&1
  jq -n --arg core "$core" --arg xray "$XRAY_DEFAULT" --arg id "$id" --arg priv "$reality_private" --arg pub "$reality_public" --arg sid "$sid" --arg cert "$cert_crt" --arg key "$cert_key" \
    '{schema:3,core:$core,xray_core:$xray,public_address:"",protocols:{vless:{enabled:false,engine:"sing-box",name:"vless-reality",port:0,uuid:$id,sni:"www.apple.com",private_key:$priv,public_key:$pub,short_id:$sid,xray:{target:"www.apple.com:443",server_names:["www.apple.com"],fingerprint:"chrome",spider_x:"/",max_time_diff:0,min_client_ver:"",max_client_ver:"",mldsa65_seed:"",mldsa65_verify:"",fallback_profile:"off"}},vmess:{enabled:false,engine:"sing-box",name:"vmess-ws",port:0,uuid:$id,path:("/"+$id+"-vm"),tls:true,domain:"www.bing.com",certificate_id:"default",cdn:"",argo_domain:"",argo_token:""},hy2:{enabled:false,engine:"sing-box",name:"hysteria2",port:0,password:$id,domain:"www.bing.com",certificate_id:"default",up_mbps:100,down_mbps:100,udp_hop:""},anytls:{enabled:false,engine:"sing-box",name:"anytls",port:0,password:$id,domain:"www.bing.com",certificate_id:"default"}},certificates:{default:{name:"默认自签证书",cert:$cert,key:$key,mode:"pinned",insecure:true}}}'
}

choose_protocol_tags(){ # core -> space-separated tags
  local core=$1 choice p tag valid
  while :; do
    menu_header '安装向导：选择协议' '主菜单 / 安装 / 协议多选' >&2
    menu_item 1 'Vless-Reality' >&2
    menu_item 2 'Vmess-WS' >&2
    menu_item 3 'Hysteria2' >&2
    [[ "$core" != 1.10* ]] && menu_item 4 'AnyTLS' >&2
    menu_back '取消安装并返回主菜单' >&2
    ask '请输入编号，多个用逗号分隔（例如 1,3,4）：' choice; choice=${choice// /}
    cancel_input "$choice" && return 2
    IFS=, read -ra picks <<< "$choice"; valid=1; local chosen=()
    for p in "${picks[@]}"; do
      tag=
      case "$p" in 1) tag=vless;;2) tag=vmess;;3) tag=hy2;;4) [[ "$core" == 1.10* ]] && valid=0 || tag=anytls;;*) valid=0;;esac
      [[ -n "$tag" && " ${chosen[*]-} " != *" $tag "* ]] && chosen+=("$tag")
    done
    ((valid)) && ((${#chosen[@]})) || { red '输入无效；1.10 内核不支持 AnyTLS。' >&2; continue; }
    printf '%s\n' "${chosen[*]}"
    return 0
  done
}
choose_initial_reality_engine(){
  local choice
  menu_header '安装向导：Reality 服务端内核' '主菜单 / 安装 / Vless-Reality' >&2
  menu_item 1 'Sing-box（推荐入门，与其他协议共用进程）' >&2
  menu_item 2 "Xray-core ${XRAY_DEFAULT}（完整 Reality 服务端能力）" >&2
  menu_back '取消安装并返回主菜单' >&2
  ask '请选择 [0-2]：' choice
  case "$choice" in 1)printf 'sing-box\n';;2)printf 'xray\n';;0|'')return 2;;*)red '无效输入，安装已取消。' >&2; return 2;;esac
}
state_for_protocol_tags(){ # core "tag tag ..." -> JSON state
  local core=$1 tags=$2 p tag state
  state=$(new_state "$core")
  for tag in $tags; do
    while :; do
      p=$(random_port)
      [[ " $(jq -r '[.protocols[].port]|join(" ")' <<<"$state") " != *" $p "* ]] &&
        port_available "$p" && break
    done
    state=$(jq --arg t "$tag" --argjson p "$p" \
      '.protocols[$t].enabled=true | .protocols[$t].port=$p' <<<"$state")
  done
  printf '%s\n' "$state"
}
select_protocols(){ # compatibility wrapper used by tests and library callers
  local core=$1 tags
  tags=$(choose_protocol_tags "$core") || return $?
  state_for_protocol_tags "$core" "$tags"
}

inbound_for(){ # state tag -> JSON object; all listeners remain ::
  local s=$1 t=$2
  case "$t" in
  vless) jq -c '.protocols.vless as $p | {type:"vless",tag:"vless-sb",listen:"::",listen_port:$p.port,users:[{uuid:$p.uuid,flow:"xtls-rprx-vision"}],tls:{enabled:true,server_name:$p.sni,reality:{enabled:true,handshake:{server:$p.sni,server_port:443},private_key:$p.private_key,short_id:[$p.short_id]}}}' <<<"$s";;
  vmess) jq -c '.protocols.vmess as $p | .certificates[$p.certificate_id] as $c | {type:"vmess",tag:"vmess-sb",listen:"::",listen_port:$p.port,users:[{uuid:$p.uuid,alterId:0}],transport:{type:"ws",path:$p.path},tls:{enabled:$p.tls,server_name:$p.domain,certificate_path:$c.cert,key_path:$c.key}}' <<<"$s";;
  hy2) jq -c '.protocols.hy2 as $p | .certificates[$p.certificate_id] as $c | {type:"hysteria2",tag:"hy2-sb",listen:"::",listen_port:$p.port,users:[{password:$p.password}],ignore_client_bandwidth:false,up_mbps:$p.up_mbps,down_mbps:$p.down_mbps,tls:{enabled:true,alpn:["h3"],server_name:$p.domain,certificate_path:$c.cert,key_path:$c.key}}' <<<"$s";;
  anytls) jq -c '.protocols.anytls as $p | .certificates[$p.certificate_id] as $c | {type:"anytls",tag:"anytls-sb",listen:"::",listen_port:$p.port,users:[{password:$p.password}],tls:{enabled:true,server_name:$p.domain,certificate_path:$c.cert,key_path:$c.key}}' <<<"$s";; esac
}

render_config(){ # state output
  local s=$1 out=$2 tag ib; jq -n '{log:{level:"info",timestamp:true},inbounds:[],outbounds:[{type:"direct",tag:"direct"},{type:"block",tag:"block"}]}' > "$out"
  for tag in vless vmess hy2 anytls; do
    jq -e --arg t "$tag" '.protocols[$t].enabled' <<<"$s" >/dev/null || continue
    [[ $(jq -r --arg t "$tag" '.protocols[$t].engine' <<<"$s") == sing-box ]] || continue
    ib=$(inbound_for "$s" "$tag") || return 1
    jq --argjson i "$ib" '.inbounds += [$i]' "$out" > "$out.next" && mv "$out.next" "$out"
  done
  json "$out"
}

render_xray_config(){ # state output
  local s=$1 out=$2 inbound fallback
  jq -n '{log:{loglevel:"warning"},inbounds:[],outbounds:[
    {protocol:"freedom",tag:"direct",settings:{}},
    {protocol:"blackhole",tag:"blocked",settings:{}}
  ]}' > "$out"
  jq -e '.protocols.vless.enabled and .protocols.vless.engine=="xray"' <<<"$s" >/dev/null || return 0
  fallback=$(jq -c '
    .protocols.vless.xray.fallback_profile |
    if .=="balanced" then
      {limitFallbackUpload:{afterBytes:65536,bytesPerSec:1048576,burstBytesPerSec:2097152},
       limitFallbackDownload:{afterBytes:65536,bytesPerSec:5242880,burstBytesPerSec:10485760}}
    elif .=="strict" then
      {limitFallbackUpload:{afterBytes:32768,bytesPerSec:262144,burstBytesPerSec:524288},
       limitFallbackDownload:{afterBytes:32768,bytesPerSec:1048576,burstBytesPerSec:2097152}}
    else {} end
  ' <<<"$s")
  inbound=$(jq -c --argjson fallback "$fallback" '
    .protocols.vless as $p | $p.xray as $x |
    {tag:"vless-xray",listen:"::",port:$p.port,protocol:"vless",
     settings:{clients:[{id:$p.uuid,flow:"xtls-rprx-vision"}],decryption:"none"},
     streamSettings:{network:"tcp",security:"reality",tcpSettings:{header:{type:"none"}},
       realitySettings:({
         show:false,xver:0,target:$x.target,serverNames:$x.server_names,
         privateKey:$p.private_key,minClientVer:$x.min_client_ver,
         maxClientVer:$x.max_client_ver,maxTimeDiff:$x.max_time_diff,
         shortIds:[$p.short_id],mldsa65Seed:$x.mldsa65_seed
       } + $fallback)}}' <<<"$s") || return 1
  jq --argjson inbound "$inbound" '.inbounds += [$inbound]' "$out" > "$out.next" &&
    mv "$out.next" "$out"
  json "$out"
}

validate_state(){
  local s=$1 tag p
  jq -e '
    .schema==3 and
    (.core|type=="string") and
    (.xray_core|type=="string") and
    (.public_address|type=="string") and
    (.certificates|type=="object") and (.certificates|length>0) and
    ([.certificates[] |
      (.name|type=="string") and (.cert|type=="string") and (.key|type=="string") and
      (.insecure|type=="boolean") and
      ((.mode // (if .insecure then "pinned" else "trusted" end)) as $m |
        ($m=="pinned" or $m=="trusted") and (.insecure == ($m=="pinned")))
    ] | all) and
    ([.protocols.vmess,.protocols.hy2,.protocols.anytls] |
      all(.certificate_id|type=="string") and all(.certificate_id as $id | $certs[$id] != null)) and
    ([.protocols[]|select(.enabled)]|length)>0 and
    (.protocols.vless.engine=="sing-box" or .protocols.vless.engine=="xray") and
    .protocols.vmess.engine=="sing-box" and
    .protocols.hy2.engine=="sing-box" and
    .protocols.anytls.engine=="sing-box" and
    (.protocols.vless.xray |
      (.target|type=="string") and (.server_names|type=="array") and
      (.server_names|length>0) and all(.server_names[]; type=="string" and length>0) and
      (.fingerprint|IN("chrome","firefox","safari","edge","random")) and
      (.spider_x|type=="string") and (.spider_x|startswith("/")) and
      (.max_time_diff|type=="number") and (.max_time_diff>=0) and
      (.mldsa65_seed|type=="string") and (.mldsa65_verify|type=="string") and
      ((.mldsa65_seed=="" and .mldsa65_verify=="") or
       (.mldsa65_seed!="" and .mldsa65_verify!="")) and
      (.fallback_profile|IN("off","balanced","strict"))) and
    (.protocols.vless.xray.target == (.protocols.vless.sni+":443")) and
    (.protocols.vless as $v | $v.xray.server_names | index($v.sni) != null) and
    (.protocols.vmess.tls|type=="boolean") and
    (.protocols.hy2.up_mbps|type=="number") and (.protocols.hy2.up_mbps>0) and
    (.protocols.hy2.down_mbps|type=="number") and (.protocols.hy2.down_mbps>0)
  ' --argjson certs "$(jq '.certificates' <<<"$s")" <<<"$s" >/dev/null || return 1
  [[ $(jq -r '.xray_core' <<<"$s") == "$XRAY_DEFAULT" ]] || return 1
  [[ $(jq -r '.core' <<<"$s") != 1.10* ]] || ! jq -e '.protocols.anytls.enabled' <<<"$s" >/dev/null || return 1
  for tag in vless vmess hy2 anytls; do
    jq -e --arg t "$tag" '.protocols[$t].enabled' <<<"$s" >/dev/null || continue
    p=$(jq -r --arg t "$tag" '.protocols[$t].port' <<<"$s"); valid_port "$p" || return 1
  done
  valid_uuid "$(jq -r '.protocols.vless.uuid' <<<"$s")" || return 1
  valid_uuid "$(jq -r '.protocols.vmess.uuid' <<<"$s")" || return 1
  [[ $(jq -r '.protocols.vless.private_key' <<<"$s") =~ ^[A-Za-z0-9_-]{43}$ ]] || return 1
  [[ $(jq -r '.protocols.vless.public_key' <<<"$s") =~ ^[A-Za-z0-9_-]{43}$ ]] || return 1
  [[ $(jq -r '.protocols.vless.short_id' <<<"$s") =~ ^([0-9a-fA-F]{2}){1,8}$ ]] || return 1
  [[ $(jq -r '.protocols.vmess.path' <<<"$s") == /* ]] || return 1
  [[ $(jq -r '.protocols.hy2.udp_hop' <<<"$s") =~ ^$|^[1-9][0-9]{0,4}:[1-9][0-9]{0,4}$ ]] || return 1
  [[ $(jq '[.protocols[] | select(.enabled) | .port] | unique | length' <<<"$s") == $(jq '[.protocols[] | select(.enabled)] | length' <<<"$s") ]]
}

ufw_active(){
  command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -q '^Status: active'
}
ufw_desired_rules(){ # state -> tag<TAB>port/protocol
  local s=$1 tag port hop
  for tag in vless vmess hy2 anytls; do
    jq -e --arg t "$tag" '.protocols[$t].enabled' <<<"$s" >/dev/null || continue
    port=$(jq -r --arg t "$tag" '.protocols[$t].port' <<<"$s")
    case "$tag" in vless|vmess|anytls) printf '%s\t%s/tcp\n' "$tag" "$port";;hy2) printf '%s\t%s/udp\n' "$tag" "$port";;esac
  done
  if jq -e '.protocols.hy2.enabled and ((.protocols.hy2.udp_hop|length)>0)' <<<"$s" >/dev/null; then
    hop=$(jq -r '.protocols.hy2.udp_hop' <<<"$s")
    printf 'hy2-hop\t%s/udp\n' "$hop"
  fi
}
ufw_rule_desired(){ # state tag spec
  local s=$1 wanted_tag=$2 wanted_spec=$3 tag spec
  while IFS=$'\t' read -r tag spec; do
    [[ "$tag" == "$wanted_tag" && "$spec" == "$wanted_spec" ]] && return 0
  done < <(ufw_desired_rules "$s")
  return 1
}
ufw_allow_exists(){ # spec [managed-tag]
  local spec=$1 tag=${2:-}
  ufw status 2>/dev/null | awk -v spec="$spec" -v marker="${tag:+# sing-box-yg:$tag}" '
    $1==spec && $2=="ALLOW" && (marker=="" || index($0,marker)>0) { found=1 }
    END { exit !found }
  '
}
ufw_add_rule(){ # tag spec
  local tag=$1 spec=$2
  ufw_allow_exists "$spec" "$tag" && return 0
  # Respect an existing administrator-owned allow rule and never claim or later delete it.
  ufw_allow_exists "$spec" && return 0
  ufw allow "$spec" comment "sing-box-yg:$tag" >/dev/null
}
ufw_delete_rule(){ # tag spec; only rules carrying our marker are removable
  local tag=$1 spec=$2
  ufw_allow_exists "$spec" "$tag" || return 0
  ufw --force delete allow "$spec" comment "sing-box-yg:$tag" >/dev/null
}
ufw_prepare_candidate(){ # candidate transaction-file
  local candidate=$1 transaction=$2 tag spec
  ufw_active || return 0
  : > "$transaction"
  while IFS=$'\t' read -r tag spec; do
    if ufw_allow_exists "$spec" "$tag" || ufw_allow_exists "$spec"; then
      continue
    fi
    if ! ufw_add_rule "$tag" "$spec"; then
      red "UFW 无法放行新端口：$spec；原配置未改变。"
      ufw_rollback_candidate "$transaction"
      return 1
    fi
    printf '%s\t%s\n' "$tag" "$spec" >> "$transaction"
  done < <(ufw_desired_rules "$candidate")
}
ufw_rollback_candidate(){ # transaction-file
  local transaction=$1 tag spec
  ufw_active || return 0
  [[ -f "$transaction" ]] || return 0
  while IFS=$'\t' read -r tag spec; do
    ufw_delete_rule "$tag" "$spec" || true
  done < "$transaction"
}
ufw_finalize_candidate(){ # old-state-file candidate
  local old_file=$1 candidate=$2 old tag spec
  ufw_active || return 0
  [[ -s "$old_file" ]] || return 0
  old=$(cat "$old_file")
  while IFS=$'\t' read -r tag spec; do
    ufw_rule_desired "$candidate" "$tag" "$spec" && continue
    ufw_delete_rule "$tag" "$spec" || {
      yellow "UFW 旧规则未能删除：$spec（服务已使用新配置，请手动检查 ufw status）。"
    }
  done < <(ufw_desired_rules "$old")
}
ufw_remove_all_managed(){
  local number
  ufw_active || return 0
  while :; do
    number=$(ufw status numbered 2>/dev/null | awk '/# sing-box-yg:/{
      line=$0
      sub(/^[^[]*\[[[:space:]]*/, "", line)
      sub(/\].*$/, "", line)
      gsub(/[[:space:]]/, "", line)
      n=line
    } END{print n}')
    [[ "$number" =~ ^[0-9]+$ ]] || break
    ufw --force delete "$number" >/dev/null || break
  done
}
ufw_remove_managed_tag(){
  local tag=$1 number
  ufw_active || return 0
  while :; do
    number=$(ufw status numbered 2>/dev/null | awk -v marker="# sing-box-yg:${tag}" '
      index($0,marker)>0 {
        line=$0
        sub(/^[^[]*\[[[:space:]]*/, "", line)
        sub(/\].*$/, "", line)
        gsub(/[[:space:]]/, "", line)
        n=line
      } END{print n}')
    [[ "$number" =~ ^[0-9]+$ ]] || break
    ufw --force delete "$number" >/dev/null || break
  done
}

commit_state(){ # candidate JSON stdin
  local candidate tmp state_tmp config_tmp xray_tmp old_state old_config old_xray ufw_transaction
  candidate=$(normalize_state <<<"$(cat)") ||
    { red '状态 JSON 无效，原配置未改变。'; return 1; }
  validate_state "$candidate" || { red '状态无效：检查协议数量、输入类型、UUID、Path、证书模式与端口。'; return 1; }
  tmp=$(tmpdir)
  state_tmp="$tmp/protocols.json"; config_tmp="$tmp/sb.json"; xray_tmp="$tmp/xray.json"
  old_state="$tmp/old-state.json"; old_config="$tmp/old-config.json"; old_xray="$tmp/old-xray.json"
  ufw_transaction="$tmp/ufw-added.tsv"
  printf '%s\n' "$candidate" > "$state_tmp"
  render_config "$candidate" "$config_tmp" || { red 'JSON 渲染失败，原配置未改变。'; return 1; }
  render_xray_config "$candidate" "$xray_tmp" || { red 'Xray JSON 渲染失败，原配置未改变。'; return 1; }
  "$STATE_DIR/sing-box" check -c "$config_tmp" || { red 'sing-box check 失败，原配置未改变。'; return 1; }
  if state_uses_engine "$candidate" xray; then
    xray_install_valid || { red 'Xray 内核版本或安装校验标记无效，请重新选择 Xray 并完成下载。'; return 1; }
    "$STATE_DIR/xray" run -test -c "$xray_tmp" ||
      { red 'Xray 配置检查失败，原配置未改变。'; return 1; }
  fi
  install -d -m 755 "$STATE_DIR"
  [[ -f "$STATE_FILE" ]] && cp "$STATE_FILE" "$old_state"
  [[ -f "$CONFIG_FILE" ]] && cp "$CONFIG_FILE" "$old_config"
  [[ -f "$XRAY_CONFIG_FILE" ]] && cp "$XRAY_CONFIG_FILE" "$old_xray"
  ufw_prepare_candidate "$candidate" "$ufw_transaction" || return 1
  chmod 600 "$state_tmp" "$config_tmp" "$xray_tmp"
  mv "$state_tmp" "$STATE_FILE"; mv "$config_tmp" "$CONFIG_FILE"; mv "$xray_tmp" "$XRAY_CONFIG_FILE"
  if ! write_services || ! reconcile_core_services "$candidate"; then
    red '重启失败，正在回滚原配置。'
    if [[ -f "$old_state" ]]; then cp "$old_state" "$STATE_FILE"; else rm -f "$STATE_FILE"; fi
    if [[ -f "$old_config" ]]; then cp "$old_config" "$CONFIG_FILE"; else rm -f "$CONFIG_FILE"; fi
    if [[ -f "$old_xray" ]]; then cp "$old_xray" "$XRAY_CONFIG_FILE"; else rm -f "$XRAY_CONFIG_FILE"; fi
    ufw_rollback_candidate "$ufw_transaction"
    if [[ -f "$old_state" ]]; then reconcile_core_services "$(cat "$old_state")" || true
    else stop_disable_service sing-box; stop_disable_service sing-box-xray
    fi
    return 1
  fi
  if ! generate_subscriptions; then
    red '订阅安全参数生成失败，正在回滚原配置。'
    if [[ -f "$old_state" ]]; then cp "$old_state" "$STATE_FILE"; else rm -f "$STATE_FILE"; fi
    if [[ -f "$old_config" ]]; then cp "$old_config" "$CONFIG_FILE"; else rm -f "$CONFIG_FILE"; fi
    if [[ -f "$old_xray" ]]; then cp "$old_xray" "$XRAY_CONFIG_FILE"; else rm -f "$XRAY_CONFIG_FILE"; fi
    ufw_rollback_candidate "$ufw_transaction"
    if [[ -f "$old_state" ]]; then reconcile_core_services "$(cat "$old_state")" || true
    else stop_disable_service sing-box; stop_disable_service sing-box-xray
    fi
    return 1
  fi
  ufw_finalize_candidate "$old_state" "$candidate"
  reconcile_hy2_hop || yellow 'Hy2 UDP 跳跃规则未能应用，请检查 iptables。'
  reconcile_argo || yellow 'Argo 服务未能启动；Sing-box 配置不受影响。'
  green '配置已通过 JSON、Sing-box 与 Xray（如启用）检查；双内核配置已原子替换并应用。'
}
apply_current_state(){
  [[ -f "$STATE_FILE" ]] || { red '缺少协议状态文件，无法应用配置。'; return 1; }
  commit_state < "$STATE_FILE"
}
ensure_current_state_schema(){
  local normalized supported
  [[ -f "$STATE_FILE" ]] || return 0
  jq -e '.schema==3 and (.protocols|has("tuic")|not)' "$STATE_FILE" >/dev/null && return 0
  normalized=$(normalize_state < "$STATE_FILE") || return 1
  supported=$(jq '[.protocols[]|select(.enabled)]|length' <<<"$normalized")
  if ((supported == 0)); then
    red '旧状态仅启用了已移除的 TUIC v5，无法自动迁移。请卸载后重新安装其他协议。'
    return 2
  fi
  yellow '正在迁移旧状态：建立多证书库并移除 TUIC v5。'
  printf '%s\n' "$normalized" | commit_state || return 1
  ufw_remove_managed_tag tuic
}

address(){ local a; a=$(jq -r '.public_address' "$STATE_FILE"); [[ -n "$a" ]] && printf '%s' "$a" || { curl -4fsS --max-time 5 https://icanhazip.com 2>/dev/null | tr -d '\n' || true; }; }
uri_host(){ [[ "$1" == *:* && "$1" != \[*\] ]] && printf '[%s]' "$1" || printf '%s' "$1"; }
urlencode(){ jq -rn --arg x "$1" '$x|@uri'; }
client_outbound_for(){
  local tag=$1 host=$2 server port argo pin
  port=$(jq -r ".protocols.$tag.port" "$STATE_FILE"); server=$host
  case "$tag" in
    vless) jq -c --arg s "$server" --argjson p "$port" '.protocols.vless as $x | {type:"vless",tag:$x.name,server:$s,server_port:$p,uuid:$x.uuid,flow:"xtls-rprx-vision",tls:{enabled:true,server_name:$x.sni,utls:{enabled:true,fingerprint:$x.xray.fingerprint},reality:{enabled:true,public_key:$x.public_key,short_id:$x.short_id}}}' "$STATE_FILE";;
    vmess)
      argo=$(jq -r '.protocols.vmess.argo_domain' "$STATE_FILE")
      [[ -n "$argo" ]] && { server=$argo; port=443; } || { server=$(jq -r --arg d "$host" '.protocols.vmess.cdn // "" | if length>0 then . else $d end' "$STATE_FILE"); }
      [[ -n "$argo" ]] && pin= || pin=$(client_certificate_pin vmess)
      jq -c --arg s "$server" --argjson p "$port" --arg argo "$argo" --arg pin "$pin" '
        .protocols.vmess as $x | .certificates[$x.certificate_id] as $c |
        {type:"vmess",tag:$x.name,server:$s,server_port:$p,uuid:$x.uuid,security:"auto",
         transport:{type:"ws",path:$x.path,headers:{Host:(if ($argo|length)>0 then $argo else $x.domain end)}},
         tls:{enabled:($x.tls or (($argo|length)>0)),server_name:(if ($argo|length)>0 then $argo else $x.domain end),
              insecure:(if ($argo|length)>0 then false else $c.insecure end)}} |
        if ($pin|length)>0 and .tls.enabled then .tls.certificate_public_key_sha256=[$pin] else . end' "$STATE_FILE";;
    hy2)
      pin=$(client_certificate_pin hy2)
      jq -c --arg s "$server" --argjson p "$port" --arg pin "$pin" '
        .protocols.hy2 as $x | .certificates[$x.certificate_id] as $c |
        {type:"hysteria2",tag:$x.name,server:$s,server_port:$p,password:$x.password,up_mbps:$x.up_mbps,down_mbps:$x.down_mbps,
         tls:{enabled:true,server_name:$x.domain,insecure:$c.insecure}} |
        if ($pin|length)>0 then .tls.certificate_public_key_sha256=[$pin] else . end' "$STATE_FILE";;
    anytls)
      pin=$(client_certificate_pin anytls)
      jq -c --arg s "$server" --argjson p "$port" --arg pin "$pin" '
        .protocols.anytls as $x | .certificates[$x.certificate_id] as $c |
        {type:"anytls",tag:$x.name,server:$s,server_port:$p,password:$x.password,
         tls:{enabled:true,server_name:$x.domain,insecure:$c.insecure}} |
        if ($pin|length)>0 then .tls.certificate_public_key_sha256=[$pin] else . end' "$STATE_FILE";;
  esac
}
mihomo_proxy_for(){
  local tag=$1 host=$2 server port argo
  port=$(jq -r ".protocols.$tag.port" "$STATE_FILE"); server=$host
  case "$tag" in
    vless) jq -c --arg s "$server" --argjson p "$port" '.protocols.vless as $x | {name:$x.name,type:"vless",server:$s,port:$p,uuid:$x.uuid,network:"tcp",tls:true,udp:true,flow:"xtls-rprx-vision","client-fingerprint":$x.xray.fingerprint,"reality-opts":{"public-key":$x.public_key,"short-id":$x.short_id},servername:$x.sni}' "$STATE_FILE";;
    vmess)
      argo=$(jq -r '.protocols.vmess.argo_domain' "$STATE_FILE")
      [[ -n "$argo" ]] && { server=$argo; port=443; } || { server=$(jq -r --arg d "$host" '.protocols.vmess.cdn // "" | if length>0 then . else $d end' "$STATE_FILE"); }
      jq -c --arg s "$server" --argjson p "$port" --arg argo "$argo" '.protocols.vmess as $x | .certificates[$x.certificate_id] as $c | {name:$x.name,type:"vmess",server:$s,port:$p,uuid:$x.uuid,alterId:0,cipher:"auto",udp:true,network:"ws",tls:($x.tls or (($argo|length)>0)),servername:(if ($argo|length)>0 then $argo else $x.domain end),"skip-cert-verify":(if ($argo|length)>0 then false else $c.insecure end),"ws-opts":{path:$x.path,headers:{Host:(if ($argo|length)>0 then $argo else $x.domain end)}}}' "$STATE_FILE";;
    hy2) jq -c --arg s "$server" --argjson p "$port" '.protocols.hy2 as $x | .certificates[$x.certificate_id] as $c | {name:$x.name,type:"hysteria2",server:$s,port:$p,password:$x.password,sni:$x.domain,"skip-cert-verify":$c.insecure,up:(($x.up_mbps|tostring)+" Mbps"),down:(($x.down_mbps|tostring)+" Mbps")}' "$STATE_FILE";;
    anytls) jq -c --arg s "$server" --argjson p "$port" '.protocols.anytls as $x | .certificates[$x.certificate_id] as $c | {name:$x.name,type:"anytls",server:$s,port:$p,password:$x.password,sni:$x.domain,"skip-cert-verify":$c.insecure,udp:true}' "$STATE_FILE";;
  esac
}
generate_subscriptions(){
  local out="$STATE_DIR/subscription.txt" host endpoint tag name port uuid path sni pk sid password line tls argo hop ob proxy insecure cert_mode xray_pin cert fp spx pqv
  local client="$STATE_DIR/sing-box-client.json" mihomo="$STATE_DIR/mihomo.yaml"
  host=$(address); endpoint=$(uri_host "$host")
  : > "$out"
  jq -n '{log:{level:"warn"},outbounds:[]}' > "$client"; jq -n '{proxies:[],"proxy-groups":[{name:"PROXY",type:"select",proxies:[]}]}' > "$mihomo"
  [[ -n "$host" ]] || { : > "$STATE_DIR/subscription.base64"; : > "$STATE_DIR/mihomo-subscription.txt"; yellow '未填写公网地址，暂不生成分享链接。'; return; }
  for tag in vless vmess hy2 anytls; do
    endpoint=$(uri_host "$host")
    tag_enabled "$tag" || continue; name=$(jq -r ".protocols.$tag.name" "$STATE_FILE"); port=$(jq -r ".protocols.$tag.port" "$STATE_FILE")
    insecure=0; cert_mode=trusted; xray_pin=
    if [[ "$tag" != vless ]]; then
      insecure=$(jq -r --arg t "$tag" '.certificates[.protocols[$t].certificate_id].insecure | if . then 1 else 0 end' "$STATE_FILE")
      cert_mode=$(certificate_mode "$STATE_FILE" "$tag")
      if [[ "$cert_mode" == pinned &&
            ! ( "$tag" == vmess &&
                ( $(jq -r '.protocols.vmess.tls' "$STATE_FILE") != true ||
                  $(jq -r '.protocols.vmess.argo_domain|length>0' "$STATE_FILE") == true ) ) ]]; then
        cert=$(jq -r --arg t "$tag" '.certificates[.protocols[$t].certificate_id].cert' "$STATE_FILE")
        xray_pin=$(certificate_der_sha256 "$cert") || true
        [[ "$xray_pin" =~ ^[0-9a-f]{64}$ ]] || {
          red "$(protocol_label "$tag") 固定证书 SHA256 计算失败，拒绝生成不安全的客户端配置。"
          return 1
        }
      fi
    fi
    case "$tag" in
      vless)
        uuid=$(jq -r '.protocols.vless.uuid' "$STATE_FILE")
        sni=$(jq -r '.protocols.vless.sni' "$STATE_FILE")
        pk=$(jq -r '.protocols.vless.public_key' "$STATE_FILE")
        sid=$(jq -r '.protocols.vless.short_id' "$STATE_FILE")
        fp=$(jq -r '.protocols.vless.xray.fingerprint' "$STATE_FILE")
        spx=$(jq -r '.protocols.vless.xray.spider_x' "$STATE_FILE")
        pqv=$(jq -r '.protocols.vless.xray.mldsa65_verify' "$STATE_FILE")
        line="vless://${uuid}@${endpoint}:${port}?encryption=none&flow=xtls-rprx-vision&security=reality&sni=${sni}&fp=${fp}&pbk=${pk}&sid=${sid}&spx=$(urlencode "$spx")$([[ -n "$pqv" ]] && printf '&pqv=%s' "$(urlencode "$pqv")")&type=tcp#$(urlencode "$name")"
        ;;
      vmess) uuid=$(jq -r '.protocols.vmess.uuid' "$STATE_FILE"); path=$(jq -r '.protocols.vmess.path' "$STATE_FILE"); tls=$(jq -r '.protocols.vmess.tls' "$STATE_FILE"); argo=$(jq -r '.protocols.vmess.argo_domain' "$STATE_FILE"); [[ -n "$argo" ]] && { endpoint=$argo; port=443; tls=true; } || endpoint=$(jq -r --arg d "$host" '.protocols.vmess.cdn // "" | if length>0 then . else $d end' "$STATE_FILE"); line="vmess://$(jq -nc --arg add "$endpoint" --argjson p "$port" --arg id "$uuid" --arg path "$path" --arg ps "$name" --arg tls "$tls" --arg host "${argo:-$(jq -r '.protocols.vmess.domain' "$STATE_FILE")}" '{v:"2",ps:$ps,add:$add,port:($p|tostring),id:$id,aid:"0",net:"ws",type:"none",host:$host,path:$path,tls:(if $tls=="true" then "tls" else "" end),sni:$host}' | base64 | tr -d '\n')";;
      hy2) password=$(jq -r '.protocols.hy2.password' "$STATE_FILE"); sni=$(jq -r '.protocols.hy2.domain' "$STATE_FILE"); hop=$(jq -r '.protocols.hy2.udp_hop' "$STATE_FILE"); line="hysteria2://$(urlencode "$password")@${endpoint}:${port}?security=tls&sni=${sni}&insecure=${insecure}$([[ -n "$hop" ]] && printf '&mport=%s' "$hop")#$(urlencode "$name")";;
      anytls)
        password=$(jq -r '.protocols.anytls.password' "$STATE_FILE"); sni=$(jq -r '.protocols.anytls.domain' "$STATE_FILE")
        line="anytls://$(urlencode "$password")@${endpoint}:${port}?sni=${sni}$([[ "$cert_mode" == pinned ]] && printf '&pcs=%s' "$xray_pin")#$(urlencode "$name")"
        ;;
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
  menu_header '节点与完整配置' '主菜单 / 配置 / 查看节点'
  dim '仅显示已启用协议；服务监听地址始终为 ::。'
  jq '{public_address,certificates,protocols:(.protocols|with_entries(select(.value.enabled)))}' "$STATE_FILE"
  [[ -s "$STATE_DIR/subscription.txt" ]] && { echo; cat "$STATE_DIR/subscription.txt"; }
}
show_qr(){ need qrencode; [[ -s "$STATE_DIR/subscription.txt" ]] || generate_subscriptions; while IFS= read -r link; do qrencode -t ANSIUTF8 "$link"; done < "$STATE_DIR/subscription.txt"; }

install_locked_script(){ # name url sha
  WORKDIR=$(tmpdir); local f="$WORKDIR/$1.sh"; download_verified "$2" "$3" "$f" "$1"; bash "$f"
}
acme(){ install_locked_script acme-yg "https://raw.githubusercontent.com/yonggekkk/acme-yg/${ACME_COMMIT}/acme.sh" "$ACME_SHA256"; }
warp(){ install_locked_script warp-yg "https://raw.githubusercontent.com/yonggekkk/warp-yg/${WARP_COMMIT}/CFwarp.sh" "$WARP_SHA256"; }
bbr_sysctl_file(){ printf '%s\n' "${SBYG_BBR_SYSCTL_FILE:-/etc/sysctl.d/99-sing-box-yg-bbr.conf}"; }
bbr_module_file(){ printf '%s\n' "${SBYG_BBR_MODULE_FILE:-/etc/modules-load.d/sing-box-yg-bbr.conf}"; }
bbr_state_file(){ printf '%s/bbr-state\n' "$STATE_DIR"; }
bbr_sysctl_value(){ sysctl -n "$1" 2>/dev/null || true; }
bbr_available_algorithms(){ bbr_sysctl_value net.ipv4.tcp_available_congestion_control; }
bbr_is_available(){ [[ " $(bbr_available_algorithms) " == *' bbr '* ]]; }
bbr_try_load(){
  bbr_is_available && return 0
  command -v modprobe >/dev/null 2>&1 || return 1
  modprobe tcp_bbr >/dev/null 2>&1 || return 1
  bbr_is_available
}
bbr_show_status(){
  local kernel available current qdisc owned
  kernel=$(uname -r)
  available=$(bbr_available_algorithms); current=$(bbr_sysctl_value net.ipv4.tcp_congestion_control)
  qdisc=$(bbr_sysctl_value net.core.default_qdisc)
  [[ -f "$(bbr_sysctl_file)" ]] && owned='本脚本管理' || owned='未由本脚本管理'
  printf '  内核：%s\n' "${kernel:-未知}"
  printf '  可用拥塞控制：%s\n' "${available:-无法读取}"
  printf '  当前 TCP 拥塞控制：%s\n' "${current:-无法读取}"
  printf '  默认队列规则：%s\n' "${qdisc:-无法读取}"
  printf '  持久化状态：%s\n' "$owned"
}
bbr_enable_native(){
  local sysctl_file module_file state_file old_cc old_qdisc temp
  sysctl_file=$(bbr_sysctl_file); module_file=$(bbr_module_file); state_file=$(bbr_state_file)
  if ! bbr_try_load; then
    red '当前内核未提供 BBR；不会自动更换内核。可升级 VPS 官方内核后重试。'
    return 0
  fi
  old_cc=$(bbr_sysctl_value net.ipv4.tcp_congestion_control)
  old_qdisc=$(bbr_sysctl_value net.core.default_qdisc)
  if [[ "$old_cc" == bbr && "$old_qdisc" == fq ]]; then
    green '原生 BBR + fq 已生效，无需修改。'
    return 0
  fi
  confirm_change '启用原生 BBR + fq？' \
    "将只写入 $(bbr_sysctl_file)，保存当前值以便回退；不安装或替换内核。" || return 0
  install -d -m 755 "$(dirname "$sysctl_file")" "$(dirname "$module_file")" "$STATE_DIR"
  temp=$(tmpdir)
  printf 'net.core.default_qdisc = fq\nnet.ipv4.tcp_congestion_control = bbr\n' > "$temp/bbr.conf"
  printf 'tcp_bbr\n' > "$temp/tcp_bbr"
  printf 'tcp_congestion_control=%s\ndefault_qdisc=%s\n' "$old_cc" "$old_qdisc" > "$temp/bbr-state"
  install -m 644 "$temp/bbr.conf" "$sysctl_file"
  install -m 644 "$temp/tcp_bbr" "$module_file"
  install -m 600 "$temp/bbr-state" "$state_file"
  if ! sysctl -q -p "$sysctl_file" || [[ "$(bbr_sysctl_value net.ipv4.tcp_congestion_control)" != bbr ]] || [[ "$(bbr_sysctl_value net.core.default_qdisc)" != fq ]]; then
    rm -f "$sysctl_file" "$module_file" "$state_file"
    red 'BBR 参数未能验证，已撤销本次写入。'
    return 0
  fi
  green '已启用原生 BBR + fq；重启后由本脚本的配置文件继续生效。'
}
bbr_revert_native(){
  local sysctl_file module_file state_file key value old_cc= old_qdisc= current_cc current_qdisc
  sysctl_file=$(bbr_sysctl_file); module_file=$(bbr_module_file); state_file=$(bbr_state_file)
  [[ -f "$sysctl_file" || -f "$module_file" ]] || { yellow '没有发现由本脚本创建的 BBR 配置。'; return 0; }
  confirm_change '撤销本脚本的 BBR 持久化配置？' \
    '不会修改其他工具或系统已有的 BBR 设置；若当前值仍是本脚本写入的 bbr + fq，将恢复启用前的值。' || return 0
  if [[ -r "$state_file" ]]; then
    while IFS='=' read -r key value; do
      case "$key" in tcp_congestion_control) old_cc=$value;; default_qdisc) old_qdisc=$value;; esac
    done < "$state_file"
  fi
  current_cc=$(bbr_sysctl_value net.ipv4.tcp_congestion_control)
  current_qdisc=$(bbr_sysctl_value net.core.default_qdisc)
  rm -f "$sysctl_file" "$module_file"
  if [[ "$current_cc" == bbr && -n "$old_cc" ]]; then sysctl -qw "net.ipv4.tcp_congestion_control=$old_cc" || true; fi
  if [[ "$current_qdisc" == fq && -n "$old_qdisc" ]]; then sysctl -qw "net.core.default_qdisc=$old_qdisc" || true; fi
  rm -f "$state_file"
  green '已撤销本脚本的 BBR 持久化配置。'
}
bbr_legacy_kernel_installer(){
  confirm_change '运行旧版内核安装器？' \
    '该兼容入口可能下载并修改系统内核、引导项及 sysctl，完成后可能需要重启；仅在内核不支持 BBR 且你明确接受风险时使用。' || return 0
  install_locked_script bbr "https://raw.githubusercontent.com/teddysun/across/${BBR_COMMIT}/bbr.sh" "$BBR_SHA256"
}
bbr(){
  local choice
  while :; do
    menu_header 'TCP / BBR 管理' '主菜单 / BBR'
    bbr_show_status
    echo
    menu_item 1 '启用或修复原生 BBR + fq（推荐，不更换内核）'
    menu_item 2 '撤销本脚本的 BBR 持久化配置'
    menu_item 3 '运行旧版内核安装器（高风险，可能需重启）'
    menu_back '返回主菜单'
    ask '请选择 [0-3]：' choice
    case "$choice" in
      1) bbr_enable_native;;
      2) bbr_revert_native;;
      3) bbr_legacy_kernel_installer;;
      0|'') return 0;;
      *) red '无效输入。';;
    esac
  done
}
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
  local choice x candidate current
  current=$(jq -r '.public_address | if length>0 then . else "自动探测" end' "$STATE_FILE")
  menu_header '对外地址' '主菜单 / 配置 / 对外地址'
  printf '  当前值：%s\n\n' "$current"
  menu_item 1 '设置 IPv4、IPv6 或域名'
  menu_item 2 '恢复自动探测'
  menu_back '返回配置菜单'
  ask '请选择 [0-2]：' choice
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
  menu_header '开关设置' "主菜单 / 配置 / 协议 / $(protocol_label "$tag")"
  menu_item 1 '开启'
  menu_item 2 '关闭'
  menu_back '返回专项参数'
  ask '请选择 [0-2]：' x
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
  current=$(jq -r --arg t "$tag" '.protocols[$t].port' "$STATE_FILE")
  printf '当前端口：%s/%s\n' "$current" "$(protocol_transport "$tag")"
  ask '新端口（1-65535，回车/0 返回上一级）：' x
  cancel_input "$x" && return 0
  valid_port "$x" || { red '端口无效。'; return 0; }
  [[ "$x" == "$current" ]] || port_available "$x" || { red '端口被其他进程占用。'; return 0; }
  [[ "$x" == "$current" ]] && { yellow '新端口与当前端口相同，未做修改。'; return 0; }
  confirm_change "确认修改 $(protocol_label "$tag") 端口？" \
    "${current}/$(protocol_transport "$tag") → ${x}/$(protocol_transport "$tag")；服务、订阅和 UFW 规则将事务同步。" || return 0
  candidate=$(jq --arg t "$tag" --argjson p "$x" '.protocols[$t].port=$p' "$STATE_FILE")
  apply_state "$candidate"
}
rotate_reality_keys(){
  local pair priv pub sid
  confirm_change '确认轮换 Reality 密钥和 Short ID？' \
    '现有 Vless-Reality 节点链接会立即失效，修改成功后必须重新导入订阅。' || return 0
  pair=$("$STATE_DIR/sing-box" generate reality-keypair); priv=$(awk -F': ' '/PrivateKey/{print $2}' <<<"$pair"); pub=$(awk -F': ' '/PublicKey/{print $2}' <<<"$pair"); sid=$(openssl rand -hex 4)
  [[ -n "$priv" && -n "$pub" ]] || { red '密钥生成失败。'; return 0; }
  apply_state "$(jq --arg priv "$priv" --arg pub "$pub" --arg sid "$sid" '.protocols.vless.private_key=$priv | .protocols.vless.public_key=$pub | .protocols.vless.short_id=$sid' "$STATE_FILE")"
}
set_reality_engine(){
  local choice current selected candidate detail
  current=$(jq -r '.protocols.vless.engine' "$STATE_FILE")
  menu_header 'Reality 服务端内核' '主菜单 / 配置 / 协议 / Vless-Reality / 内核'
  printf '  当前：%s\n\n' "$([[ "$current" == xray ]] && printf 'Xray-core %s' "$(jq -r '.xray_core' "$STATE_FILE")" || printf 'Sing-box %s' "$(jq -r '.core' "$STATE_FILE")")"
  menu_item 1 'Sing-box（简洁、与其他协议共用进程）'
  menu_item 2 "Xray-core ${XRAY_DEFAULT}（完整 Reality 服务端能力）"
  menu_back '返回专项参数'
  ask '请选择 [0-2]：' choice
  cancel_input "$choice" && return 0
  case "$choice" in 1) selected=sing-box;;2) selected=xray;;*) red '无效输入。'; return 0;;esac
  [[ "$selected" != "$current" ]] || { yellow '已是当前内核，未做修改。'; return 0; }
  detail='端口和节点凭据保持不变；新旧服务会在配置校验通过后事务切换。'
  if [[ "$selected" == xray ]] && ! xray_install_valid; then
    detail+=" 需要下载 Xray-core ${XRAY_DEFAULT}，发布包与官方摘要均会进行 SHA256 校验。"
  fi
  confirm_change '确认切换 Vless-Reality 服务端内核？' "$detail" || return 0
  if [[ "$selected" == xray ]] && ! xray_install_valid; then
    command -v unzip >/dev/null 2>&1 || install_packages
    install_xray
  fi
  candidate=$(jq --arg engine "$selected" '.protocols.vless.engine=$engine' "$STATE_FILE")
  apply_state "$candidate"
}
set_xray_fingerprint(){
  local choice value candidate
  menu_header 'Reality 客户端指纹' '主菜单 / 配置 / Vless-Reality / Xray 参数'
  menu_item 1 'Chrome（推荐）'
  menu_item 2 'Firefox'
  menu_item 3 'Safari'
  menu_item 4 'Edge'
  menu_item 5 'Random'
  menu_back '返回 Xray 参数'
  ask '请选择 [0-5]：' choice
  cancel_input "$choice" && return 0
  case "$choice" in 1)value=chrome;;2)value=firefox;;3)value=safari;;4)value=edge;;5)value=random;;*)red '无效输入。'; return 0;;esac
  candidate=$(jq --arg value "$value" '.protocols.vless.xray.fingerprint=$value' "$STATE_FILE")
  apply_state "$candidate"
}
set_xray_spider_x(){
  local choice value candidate
  menu_header 'Reality SpiderX' '主菜单 / 配置 / Vless-Reality / Xray 参数'
  menu_item 1 '使用 /（兼容默认）'
  menu_item 2 '自动生成随机路径'
  menu_back '返回 Xray 参数'
  ask '请选择 [0-2]：' choice
  cancel_input "$choice" && return 0
  case "$choice" in 1)value=/;;2)value="/$(openssl rand -hex 8)";;*)red '无效输入。'; return 0;;esac
  candidate=$(jq --arg value "$value" '.protocols.vless.xray.spider_x=$value' "$STATE_FILE")
  apply_state "$candidate"
}
set_xray_time_diff(){
  local choice value candidate
  menu_header 'Reality 最大时间差' '主菜单 / 配置 / Vless-Reality / Xray 参数'
  menu_item 1 '不限制（0，兼容性最好）'
  menu_item 2 '30 秒'
  menu_item 3 '60 秒'
  menu_item 4 '5 分钟'
  menu_back '返回 Xray 参数'
  ask '请选择 [0-4]：' choice
  cancel_input "$choice" && return 0
  case "$choice" in 1)value=0;;2)value=30000;;3)value=60000;;4)value=300000;;*)red '无效输入。'; return 0;;esac
  candidate=$(jq --argjson value "$value" '.protocols.vless.xray.max_time_diff=$value' "$STATE_FILE")
  apply_state "$candidate"
}
set_xray_fallback_profile(){
  local choice value candidate
  menu_header 'Reality 非认证流量限制' '主菜单 / 配置 / Vless-Reality / Xray 参数'
  dim '该设置限制未通过 Reality 认证、被转发到目标站点的流量。'
  menu_item 1 '关闭限制（官方默认，同 ASN 目标推荐）'
  menu_item 2 '均衡限制（CDN 目标防偷跑）'
  menu_item 3 '严格限制（更易形成流量特征）'
  menu_back '返回 Xray 参数'
  ask '请选择 [0-3]：' choice
  cancel_input "$choice" && return 0
  case "$choice" in 1)value=off;;2)value=balanced;;3)value=strict;;*)red '无效输入。'; return 0;;esac
  candidate=$(jq --arg value "$value" '.protocols.vless.xray.fallback_profile=$value' "$STATE_FILE")
  apply_state "$candidate"
}
xray_mldsa_target_compatible(){
  local target=$1 output cert_length pq
  output=$("$STATE_DIR/xray" tls ping "$target" 2>&1) || return 1
  cert_length=$(awk -F': *' '/Certificate chain.s total length:/{if ($2+0>max) max=$2+0} END{print max+0}' <<<"$output")
  pq=$(awk -F': *' '/TLS Post-Quantum key exchange:/{if ($2 ~ /^true/) found=1} END{print found+0}' <<<"$output")
  printf '  目标检测：证书链 %s bytes，X25519MLKEM768 %s\n' \
    "$cert_length" "$([[ "$pq" == 1 ]] && printf '支持' || printf '不支持')"
  ((cert_length > 3500 && pq == 1))
}
set_xray_mldsa65(){
  local choice pair seed verify candidate target
  menu_header 'Reality ML-DSA-65' '主菜单 / 配置 / Vless-Reality / Xray 参数'
  menu_item 1 '自动生成并启用'
  menu_item 2 '关闭'
  menu_back '返回 Xray 参数'
  ask '请选择 [0-2]：' choice
  cancel_input "$choice" && return 0
  case "$choice" in
    1)
      [[ -x "$STATE_DIR/xray" ]] || { red '请先把 Reality 服务端内核切换为 Xray。'; return 0; }
      target=$(jq -r '.protocols.vless.xray.target' "$STATE_FILE")
      blue "正在用 Xray 检查 ${target} 的证书长度和后量子密钥交换..."
      xray_mldsa_target_compatible "$target" || {
        red '目标不满足安全预设：证书链需大于 3500 bytes 且支持 X25519MLKEM768。'
        yellow '请先在 Reality SNI 扫描中更换目标；原配置未改变。'
        return 0
      }
      pair=$("$STATE_DIR/xray" mldsa65)
      seed=$(awk -F': ' '/^Seed:/{print $2}' <<<"$pair")
      verify=$(awk -F': ' '/^Verify:/{print $2}' <<<"$pair")
      [[ "$seed" =~ ^[A-Za-z0-9_-]+$ && "$verify" =~ ^[A-Za-z0-9_-]+$ ]] ||
        { red 'ML-DSA-65 密钥生成失败，原配置未改变。'; return 0; }
      candidate=$(jq --arg seed "$seed" --arg verify "$verify" '
        .protocols.vless.xray.mldsa65_seed=$seed |
        .protocols.vless.xray.mldsa65_verify=$verify
      ' "$STATE_FILE")
      ;;
    2)
      candidate=$(jq '.protocols.vless.xray.mldsa65_seed="" | .protocols.vless.xray.mldsa65_verify=""' "$STATE_FILE")
      ;;
    *) red '无效输入。'; return 0;;
  esac
  apply_state "$candidate"
}
xray_reality_menu(){
  local choice
  [[ $(jq -r '.protocols.vless.engine' "$STATE_FILE") == xray ]] ||
    { yellow '这些参数仅在 Xray 服务端内核下生效，请先切换内核。'; return 0; }
  while :; do
    menu_header 'Xray Reality 参数' '主菜单 / 配置 / 协议 / Vless-Reality / Xray 参数'
    printf '  指纹：%-8s SpiderX：%s\n' \
      "$(jq -r '.protocols.vless.xray.fingerprint' "$STATE_FILE")" \
      "$(jq -r '.protocols.vless.xray.spider_x' "$STATE_FILE")"
    printf '  时间差：%-8s ML-DSA-65：%-6s 非认证限制：%s\n\n' \
      "$(jq -r '.protocols.vless.xray.max_time_diff' "$STATE_FILE") ms" \
      "$(jq -r 'if .protocols.vless.xray.mldsa65_seed=="" then "关闭" else "启用" end' "$STATE_FILE")" \
      "$(jq -r '.protocols.vless.xray.fallback_profile' "$STATE_FILE")"
    menu_item 1 '选择客户端指纹'
    menu_item 2 '设置 SpiderX'
    menu_item 3 '设置最大时间差'
    menu_item 4 '生成或关闭 ML-DSA-65'
    menu_item 5 '选择非认证流量限制'
    menu_back '返回专项参数'
    ask '请选择 [0-5]：' choice
    case "$choice" in
      1)set_xray_fingerprint;;2)set_xray_spider_x;;3)set_xray_time_diff;;
      4)set_xray_mldsa65;;5)set_xray_fallback_profile;;0|'')return 0;;*)red '无效输入。';;
    esac
  done
}
probe_reality_once(){ # host [sample-number] -> DNS + TCP + TLS handshake milliseconds
  local host=$1 timing
  timing=$(curl --noproxy '*' -sS -I -o /dev/null --connect-timeout 8 --max-time 8 \
    --tlsv1.3 --tls-max 1.3 --http2 -w '%{time_appconnect}' "https://${host}/" 2>/dev/null || true)
  if [[ ! "$timing" =~ ^0*[1-9][0-9]*\.[0-9]+$|^0+\.[0-9]*[1-9][0-9]*$ ]]; then
    timing=$(curl --noproxy '*' -sS -I -o /dev/null --connect-timeout 8 --max-time 8 \
      --tlsv1.3 --tls-max 1.3 -w '%{time_appconnect}' "https://${host}/" 2>/dev/null || true)
  fi
  if [[ ! "$timing" =~ ^0*[1-9][0-9]*\.[0-9]+$|^0+\.[0-9]*[1-9][0-9]*$ ]]; then
    timing=$(curl --noproxy '*' -sS -I -o /dev/null --connect-timeout 8 --max-time 8 \
      -w '%{time_appconnect}' "https://${host}/" 2>/dev/null || true)
  fi
  [[ "$timing" =~ ^[0-9]+\.[0-9]+$ ]] || return 1
  awk -v seconds="$timing" 'BEGIN {
    milliseconds=int(seconds*1000+0.5)
    if (milliseconds < 1) exit 1
    print milliseconds
  }'
}
probe_reality_metadata(){ # host -> TLS-version, ALPN, key-exchange, certificate-subject
  local host=$1 output tls alpn curve cert
  output=$(mktemp "${TMPDIR:-/tmp}/sing-box-yg-reality.XXXXXX")
  if ! timeout 8 openssl s_client -connect "${host}:443" -servername "$host" -tls1_3 \
    -alpn 'h2,http/1.1' -verify_hostname "$host" -verify_return_error </dev/null >"$output" 2>&1; then
    rm -f "$output"
    return 1
  fi
  tr -d '\000' < "$output" > "$output.text" && mv "$output.text" "$output"
  grep -aq 'Verify return code: 0 (ok)' "$output" || { rm -f "$output"; return 1; }
  tls=$(sed -n 's/^New, TLSv\([^,]*\),.*/\1/p' "$output" | head -n1)
  alpn=$(sed -n 's/^ALPN protocol: //p' "$output" | head -n1)
  curve=$(sed -n 's/^Server Temp Key: \([^,]*\).*/\1/p' "$output" | head -n1)
  cert=$(sed -n 's/^subject=.*CN *= *\([^,]*\).*/\1/p' "$output" | head -n1)
  [[ "$tls" == 1.3 && "$alpn" == h2 && "$curve" == X25519* ]] || {
    rm -f "$output"
    return 1
  }
  [[ -n "$cert" ]] || cert=$host
  rm -f "$output"
  printf '%s\t%s\t%s\t%s\n' "$tls" "$alpn" "$curve" "$cert"
}
scan_reality_candidates(){ # outputs host, successes, average-ms, jitter-ms, TLS, ALPN, curve, certificate
  local work host file sample latency metadata tls alpn curve cert successes total min max avg jitter configured
  local -a candidates=("$@") pids=()
  if ((${#candidates[@]} == 0)); then
    configured=$(reality_target_candidates) || return 1
    while IFS= read -r host; do [[ -n "$host" ]] && candidates+=("$host"); done <<<"$configured"
  fi
  ((${#candidates[@]})) || { red '未能读取 Reality 候选域名清单。' >&2; return 1; }
  work=$(tmpdir)
  blue "正在从当前 VPS 并发扫描 ${#candidates[@]} 个 Reality 目标，每个采样 ${REALITY_SCAN_SAMPLES} 次..." >&2
  for host in "${candidates[@]}"; do
    file="$work/$(printf '%s' "$host" | tr -c 'A-Za-z0-9._-' '_').result"
    (
      metadata=$(probe_reality_metadata "$host") || exit 0
      IFS=$'\t' read -r tls alpn curve cert <<<"$metadata"
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
      printf '%s\t%d\t%d\t%d\t%s\t%s\t%s\t%s\n' \
        "$host" "$successes" "$avg" "$jitter" "$tls" "$alpn" "$curve" "$cert" > "$file"
    ) &
    pids+=("$!")
  done
  for sample in "${pids[@]}"; do wait "$sample" || true; done
  # Prefer fully successful and low-latency targets; require at least 2/3 successful samples.
  awk -F '\t' -v minimum="$((REALITY_SCAN_SAMPLES - 1))" '$2 >= minimum' "$work"/*.result |
    sort -t $'\t' -k2,2nr -k3,3n -k4,4n |
    head -n 10
}
choose_scanned_reality_sni(){
  local results choice i host successes avg jitter tls alpn curve cert candidate
  local -a rows=()
  results=$(scan_reality_candidates) || true
  [[ -n "$results" ]] || { red '候选清单中的目标均未达到稳定性要求，原配置未改变。'; return 0; }
  while IFS= read -r candidate; do rows+=("$candidate"); done <<<"$results"
  menu_header 'Reality 目标扫描结果' '主菜单 / 配置 / 协议 / Vless-Reality / Reality SNI'
  dim '延迟为 DNS + TCP + TLS 握手；可用性优先，其次平均延迟和抖动。'
  # printf counts UTF-8 bytes, while CJK characters take two terminal cells.
  # The compensated widths below keep the Chinese header over its ASCII data.
  printf '%-4s %-25s %-7s %-4s %-5s %-22s %-26s%10s%10s%7s\n' \
    '序' '目标' '状态' 'TLS' 'ALPN' '密钥交换' '证书' '平均' '抖动' '成功'
  for ((i=0; i<${#rows[@]}; i++)); do
    IFS=$'\t' read -r host successes avg jitter tls alpn curve cert <<<"${rows[$i]}"
    printf '%-3d %-23s %-5s %-4s %-5s %-18s %-24s %4d ms %4d ms %d/%d\n' \
      "$((i + 1))" "$host" '可用' "$tls" "$alpn" "$curve" "$cert" "$avg" "$jitter" "$successes" "$REALITY_SCAN_SAMPLES"
  done
  menu_back '返回 Reality 设置'
  ask '请选择扫描目标：' choice
  cancel_input "$choice" && return 0
  [[ "$choice" =~ ^([1-9]|10)$ && "$choice" -le "${#rows[@]}" ]] || { red '无效输入。'; return 0; }
  IFS=$'\t' read -r host successes avg jitter tls alpn curve cert <<<"${rows[$((choice - 1))]}"
  jq -e '.protocols.vless.xray.mldsa65_seed!=""' "$STATE_FILE" >/dev/null &&
    yellow 'Reality 目标已改变，ML-DSA-65 将先关闭；请在 Xray 参数中重新检测并启用。'
  candidate=$(jq --arg x "$host" '
    .protocols.vless.sni=$x |
    .protocols.vless.xray.target=($x+":443") |
    .protocols.vless.xray.server_names=[$x] |
    .protocols.vless.xray.mldsa65_seed="" |
    .protocols.vless.xray.mldsa65_verify=""
  ' "$STATE_FILE")
  apply_state "$candidate"
}
set_reality_sni(){
  local choice host results candidate
  menu_header 'Reality SNI' '主菜单 / 配置 / 协议 / Vless-Reality / Reality SNI'
  printf '  当前 SNI：%s\n\n' "$(jq -r '.protocols.vless.sni' "$STATE_FILE")"
  menu_item 1 '扫描候选清单并选择（显示前 10 个）'
  menu_item 2 '扫描自定义目标'
  menu_item 3 "查看候选清单位置：$(reality_targets_file)"
  menu_item 4 '从当前渠道重新下载候选清单（覆盖本机清单）'
  menu_back '返回专项参数'
  ask '请选择 [0-4]：' choice
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
      jq -e '.protocols.vless.xray.mldsa65_seed!=""' "$STATE_FILE" >/dev/null &&
        yellow 'Reality 目标已改变，ML-DSA-65 将先关闭；请在 Xray 参数中重新检测并启用。'
      candidate=$(jq --arg x "$host" '
        .protocols.vless.sni=$x |
        .protocols.vless.xray.target=($x+":443") |
        .protocols.vless.xray.server_names=[$x] |
        .protocols.vless.xray.mldsa65_seed="" |
        .protocols.vless.xray.mldsa65_verify=""
      ' "$STATE_FILE")
      apply_state "$candidate"
      ;;
    3)
      if sync_reality_targets; then
        printf '\n'; green '候选清单已就绪；可按“一行一个域名”的格式编辑该文件。'
        sed -n '1,120p' "$(reality_targets_file)"
      fi
      ;;
    4)
      confirm_change '重新下载并覆盖本机 Reality 候选清单？' \
        '会从当前更新渠道下载并校验清单；你在本机文件中手工加入的域名将被替换。' || return 0
      sync_reality_targets 1 && green '候选清单已更新；下次扫描将使用新清单。'
      ;;
    *) red '无效输入。';;
  esac
}
certificate_matches_domain(){ openssl x509 -in "$1" -noout -checkhost "$2" >/dev/null 2>&1; }
certificate_pair_valid(){
  local cert=$1 key=$2 cert_pub key_pub
  [[ -r "$cert" && -r "$key" ]] || return 1
  openssl x509 -in "$cert" -noout >/dev/null 2>&1 || return 1
  openssl x509 -in "$cert" -noout -checkend 0 >/dev/null 2>&1 || return 1
  cert_pub=$(openssl x509 -in "$cert" -pubkey -noout | openssl pkey -pubin -outform pem 2>/dev/null | openssl dgst -sha256 | awk '{print $NF}') || true
  key_pub=$(openssl pkey -in "$key" -pubout -outform pem 2>/dev/null | openssl dgst -sha256 | awk '{print $NF}') || true
  [[ -n "$cert_pub" && "$cert_pub" == "$key_pub" ]]
}
certificate_entry_id(){
  printf 'cert_%s\n' "$(printf '%s' "$1" | sha256sum | awk '{print substr($1,1,12)}')"
}
set_certificate(){
  local name cert key choice mode insecure candidate id
  ask '证书名称（例如 example.com；回车/0 返回上一级）：' name
  cancel_input "$name" && return 0
  ask '证书完整路径（回车/0 返回上一级）：' cert
  cancel_input "$cert" && return 0
  ask '私钥完整路径（回车/0 返回上一级）：' key
  cancel_input "$key" && return 0
  certificate_pair_valid "$cert" "$key" || { red '证书/私钥不可读、格式无效或公私钥不匹配。'; return 0; }
  menu_header '客户端证书校验' '主菜单 / 配置 / 证书管理 / 导入证书'
  menu_item 1 '自签证书固定 SHA256'
  menu_item 2 '系统 CA 验证（ACME / 受信证书，推荐）'
  menu_back '取消导入'
  ask '请选择 [0-2]：' choice
  cancel_input "$choice" && return 0
  case "$choice" in
    1) mode=pinned; insecure=true;;
    2) mode=trusted; insecure=false;;
    *) red '无效输入。'; return 0;;
  esac
  if [[ "$mode" == pinned ]]; then
    [[ "$(certificate_der_sha256 "$cert")" =~ ^[0-9a-f]{64}$ &&
       "$(certificate_spki_sha256 "$cert")" =~ ^[A-Za-z0-9+/]{43}=$ ]] ||
      { red '证书固定值计算失败，原配置未改变。'; return 0; }
  fi
  id=$(certificate_entry_id "${name}:${cert}")
  candidate=$(jq --arg id "$id" --arg name "$name" --arg cert "$cert" --arg key "$key" --arg mode "$mode" --argjson insecure "$insecure" \
    '.certificates[$id]={name:$name,cert:$cert,key:$key,mode:$mode,insecure:$insecure}' "$STATE_FILE")
  if printf '%s\n' "$candidate" | commit_state; then
    green "证书已加入证书库：${name}（${id}）。请使用“为协议选择证书”完成绑定。"
  fi
}
find_domain_certificate(){ # domain [acme-only]; sets FOUND_CERT, FOUND_KEY and FOUND_CERT_ID
  local domain=$1 acme_only=${2:-false} id cert key mode
  FOUND_CERT=; FOUND_KEY=; FOUND_CERT_ID=
  cert=/root/ygkkkca/cert.crt; key=/root/ygkkkca/private.key
  if certificate_pair_valid "$cert" "$key" && certificate_matches_domain "$cert" "$domain"; then
    FOUND_CERT=$cert; FOUND_KEY=$key; return 0
  fi
  if [[ "$acme_only" != true ]]; then
    while IFS=$'\t' read -r id mode cert key; do
      [[ "$mode" == trusted ]] || continue
      if certificate_pair_valid "$cert" "$key" && certificate_matches_domain "$cert" "$domain"; then
        FOUND_CERT_ID=$id; FOUND_CERT=$cert; FOUND_KEY=$key; return 0
      fi
    done < <(jq -r '.certificates | to_entries[] | [.key,(.value.mode // (if .value.insecure then "pinned" else "trusted" end)),.value.cert,.value.key] | @tsv' "$STATE_FILE")
  fi
  return 1
}
prompt_domain_certificate(){ # domain; sets FOUND_CERT and FOUND_KEY
  local domain=$1 cert key
  ask '已有证书完整路径（回车/0 返回上一级）：' cert
  cancel_input "$cert" && return 1
  ask '已有私钥完整路径（回车/0 返回上一级）：' key
  cancel_input "$key" && return 1
  certificate_pair_valid "$cert" "$key" || { red '证书/私钥不可读、格式无效或公私钥不匹配。'; return 1; }
  certificate_matches_domain "$cert" "$domain" || { red "证书 SAN 不覆盖域名：$domain"; return 1; }
  FOUND_CERT=$cert; FOUND_KEY=$key
}
persist_domain_certificate(){ # domain source-cert source-key; sets FOUND_CERT, FOUND_KEY and FOUND_CERT_ID
  local domain=$1 source_cert=$2 source_key=$3 dir
  FOUND_CERT_ID=$(certificate_entry_id "$domain")
  dir="$STATE_DIR/certificates/$FOUND_CERT_ID"
  install -d -m 700 "$dir"
  if [[ "$source_cert" != "$dir/cert.pem" ]]; then install -m 644 "$source_cert" "$dir/cert.pem"; fi
  if [[ "$source_key" != "$dir/private.key" ]]; then install -m 600 "$source_key" "$dir/private.key"; fi
  FOUND_CERT="$dir/cert.pem"; FOUND_KEY="$dir/private.key"
}
bind_trusted_domain_certificate(){ # tag domain cert key
  local tag=$1 domain=$2 cert=$3 key=$4 candidate id
  persist_domain_certificate "$domain" "$cert" "$key"
  cert=$FOUND_CERT; key=$FOUND_KEY; id=$FOUND_CERT_ID
  if [[ "$tag" == all ]]; then
    candidate=$(jq --arg d "$domain" '
      .protocols.vmess.domain=$d | .protocols.hy2.domain=$d |
      .protocols.anytls.domain=$d
    ' "$STATE_FILE")
  else
    candidate=$(jq --arg t "$tag" --arg d "$domain" '.protocols[$t].domain=$d' "$STATE_FILE")
  fi
  candidate=$(jq --arg id "$id" --arg domain "$domain" --arg cert "$cert" --arg key "$key" \
    '.certificates[$id]={name:$domain,cert:$cert,key:$key,mode:"trusted",insecure:false}' <<<"$candidate")
  if [[ "$tag" == all ]]; then
    candidate=$(jq --arg id "$id" '
      .protocols.vmess.certificate_id=$id |
      .protocols.hy2.certificate_id=$id |
      .protocols.anytls.certificate_id=$id
    ' <<<"$candidate")
  else
    candidate=$(jq --arg t "$tag" --arg id "$id" '.protocols[$t].certificate_id=$id' <<<"$candidate")
  fi
  if printf '%s\n' "$candidate" | commit_state; then
    if [[ "$tag" == all ]]; then
      green "域名证书已保存到证书库并绑定到全部 TLS 协议：${domain}"
    else
      green "域名证书已保存到证书库并绑定到 $(protocol_label "$tag")：${domain}"
    fi
    green '请在客户端重新导入最新订阅/分享链接。'
    return 0
  fi
  red '证书绑定失败，已保留原自签证书和协议配置。'
  return 1
}
domain_certificate_wizard(){ # target tag
  local tag=$1 domain choice
  menu_header '域名证书向导' "主菜单 / 域名证书 / $( [[ "$tag" == all ]] && printf '全部协议' || protocol_label "$tag" )"
  dim 'Cloudflare DNS 记录应保持灰云；脚本会先查找可复用证书，再提供 ACME 或手动导入。'
  ask '准备使用的证书域名（Cloudflare 请保持灰云；回车/0 返回上一级）：' domain
  cancel_input "$domain" && return 0
  valid_host "$domain" && [[ "$domain" != *:* && ! "$domain" =~ ^[0-9.]+$ ]] ||
    { red '必须填写有效域名。'; return 0; }
  if find_domain_certificate "$domain"; then
    green "检测到覆盖 ${domain} 且公私钥匹配的现有证书：$FOUND_CERT"
  else
    yellow "没有检测到覆盖 ${domain} 的可用域名证书。"
    menu_item 1 '运行锁定版 ACME 申请'
    menu_item 2 '手动指定已有证书'
    menu_back '取消并返回'
    ask '请选择 [0-2]：' choice
    case "$choice" in
      1)
        if ! acme; then yellow 'ACME 脚本未正常完成，正在检查是否生成了有效证书。'; fi
        find_domain_certificate "$domain" true || {
          red "ACME 后仍未发现覆盖 ${domain} 的证书。"
          yellow '预期路径：/root/ygkkkca/cert.crt 和 /root/ygkkkca/private.key；原配置未改变。'
          return 0
        }
        green "ACME 证书验证成功：$FOUND_CERT"
        ;;
      2) prompt_domain_certificate "$domain" || return 0;;
      0|'') return 0;;
      *) red '无效输入。'; return 0;;
    esac
  fi
  bind_trusted_domain_certificate "$tag" "$domain" "$FOUND_CERT" "$FOUND_KEY" || true
}
domain_certificate_target_menu(){
  local choice tag
  menu_header '域名证书向导' '主菜单 / 域名证书'
  menu_item 1 '应用到 Vmess-WS'
  menu_item 2 '应用到 Hysteria2'
  menu_item 3 '应用到 AnyTLS'
  menu_item 4 '应用到全部普通 TLS 协议'
  menu_back '返回上一级'
  ask '请选择 [0-4]：' choice
  case "$choice" in
    1)tag=vmess;;2)tag=hy2;;3)tag=anytls;;4)tag=all;;0|'')return 0;;
    *)red '无效输入。'; return 0;;
  esac
  domain_certificate_wizard "$tag"
}
show_certificate(){
  local id name cert mode der_pin spki_pin
  echo '证书库与协议绑定：'
  jq '{bindings:{vmess:{domain:.protocols.vmess.domain,certificate_id:.protocols.vmess.certificate_id},hy2:{domain:.protocols.hy2.domain,certificate_id:.protocols.hy2.certificate_id},anytls:{domain:.protocols.anytls.domain,certificate_id:.protocols.anytls.certificate_id}},certificates}' "$STATE_FILE"
  while IFS=$'\t' read -r id name cert mode; do
    echo
    printf '[%s] %s（%s）\n' "$id" "$name" "$mode"
    if [[ -r "$cert" ]]; then
    openssl x509 -in "$cert" -noout -subject -issuer -dates
    openssl x509 -in "$cert" -noout -ext subjectAltName 2>/dev/null || true
    if [[ "$mode" == pinned ]]; then
      der_pin=$(certificate_der_sha256 "$cert"); spki_pin=$(certificate_spki_sha256 "$cert")
      printf 'Xray/v2rayN pcs（证书 DER SHA256）：%s\n' "$der_pin"
      printf 'Sing-box certificate_public_key_sha256：%s\n' "$spki_pin"
      yellow '固定证书一旦轮换，客户端必须重新导入订阅。'
    else
      green '当前使用系统 CA 验证；续期后客户端不需要更新指纹。托管副本请重跑向导同步新证书。'
    fi
    else yellow '当前证书文件不可读。'; fi
  done < <(jq -r '.certificates | to_entries[] | [.key,.value.name,.value.cert,(.value.mode // (if .value.insecure then "pinned" else "trusted" end))] | @tsv' "$STATE_FILE")
}
set_tls_domain(){
  local tag=$1 domain cert insecure candidate cert_id
  ask '证书域名（回车/0 返回上一级）：' domain
  cancel_input "$domain" && return 0
  valid_host "$domain" && [[ "$domain" != *:* && ! "$domain" =~ ^[0-9.]+$ ]] || { red '必须填写有效域名。'; return 0; }
  cert_id=$(certificate_id_for_tag "$tag")
  cert=$(jq -r --arg id "$cert_id" '.certificates[$id].cert' "$STATE_FILE")
  insecure=$(jq -r --arg id "$cert_id" '.certificates[$id].insecure' "$STATE_FILE")
  if [[ -r "$cert" ]] && ! certificate_matches_domain "$cert" "$domain"; then
    if [[ "$insecure" == false ]]; then
      red '当前受信证书不覆盖该域名，拒绝修改。请先导入匹配证书。'
      return 0
    fi
    yellow '当前为自签证书固定模式；域名不在 SAN 中，但客户端将校验证书固定值。'
  fi
  candidate=$(jq --arg t "$tag" --arg d "$domain" '.protocols[$t].domain=$d' "$STATE_FILE")
  apply_state "$candidate"
}
tls_domain_menu(){
  local choice tag
  while :; do
    menu_header '修改协议证书域名' '主菜单 / 配置 / 证书管理 / 证书域名'
    menu_item 1 'Vmess-WS'
    menu_item 2 'Hysteria2'
    menu_item 3 'AnyTLS'
    menu_back '返回证书管理'
    ask '请选择 [0-3]：' choice
    case "$choice" in
      1)tag=vmess;;2)tag=hy2;;3)tag=anytls;;0|'')return 0;;*)red '无效输入。'; continue;;
    esac
    set_tls_domain "$tag"
  done
}
select_certificate_for_protocol(){
  local target_choice tag index choice id name mode cert domain candidate new_domain key certificate_row
  local -a certificate_rows=()
  menu_header '选择协议证书' '主菜单 / 配置 / 证书管理 / 选择证书'
  menu_item 1 'Vmess-WS'
  menu_item 2 'Hysteria2'
  menu_item 3 'AnyTLS'
  menu_back '返回证书管理'
  ask '请选择协议 [0-3]：' target_choice
  case "$target_choice" in 1)tag=vmess;;2)tag=hy2;;3)tag=anytls;;0|'')return 0;;*)red '无效输入。'; return 0;;esac
  domain=$(jq -r --arg t "$tag" '.protocols[$t].domain' "$STATE_FILE")
  while IFS= read -r certificate_row; do certificate_rows+=("$certificate_row"); done < <(
    jq -r '.certificates | to_entries[] | [.key,.value.name,(.value.mode // (if .value.insecure then "pinned" else "trusted" end)),.value.cert] | @tsv' "$STATE_FILE"
  )
  ((${#certificate_rows[@]})) || { red '证书库为空。'; return 0; }
  echo "$(protocol_label "$tag") 当前域名：${domain}；请选择证书："
  for ((index=0; index<${#certificate_rows[@]}; index++)); do
    IFS=$'\t' read -r id name mode cert <<<"${certificate_rows[$index]}"
    printf '%d %s [%s] %s\n' "$((index + 1))" "$name" "$mode" "$cert"
  done
  menu_back '返回证书管理'
  ask '请选择证书：' choice
  cancel_input "$choice" && return 0
  [[ "$choice" =~ ^[1-9][0-9]*$ && "$choice" -le "${#certificate_rows[@]}" ]] || { red '无效输入。'; return 0; }
  IFS=$'\t' read -r id name mode cert <<<"${certificate_rows[$((choice - 1))]}"
  key=$(jq -r --arg id "$id" '.certificates[$id].key' "$STATE_FILE")
  certificate_pair_valid "$cert" "$key" ||
    { red '所选证书已失效、不可读或公私钥不匹配。'; return 0; }
  if [[ "$mode" == trusted ]] && ! certificate_matches_domain "$cert" "$domain"; then
    yellow "所选受信证书不覆盖当前域名 ${domain}。"
    ask '请输入该证书覆盖的新协议域名（回车/0 返回上一级）：' new_domain
    cancel_input "$new_domain" && return 0
    valid_host "$new_domain" && [[ "$new_domain" != *:* && ! "$new_domain" =~ ^[0-9.]+$ ]] ||
      { red '必须填写有效域名。'; return 0; }
    certificate_matches_domain "$cert" "$new_domain" ||
      { red "所选证书不覆盖 ${new_domain}，原配置未改变。"; return 0; }
    candidate=$(jq --arg t "$tag" --arg id "$id" --arg d "$new_domain" \
      '.protocols[$t].certificate_id=$id | .protocols[$t].domain=$d' "$STATE_FILE")
  else
    candidate=$(jq --arg t "$tag" --arg id "$id" '.protocols[$t].certificate_id=$id' "$STATE_FILE")
  fi
  apply_state "$candidate"
}
certificate_menu(){
  local choice
  while :; do
    menu_header '证书管理' '主菜单 / 配置 / 证书管理'
    menu_item 1 '查看证书库与协议绑定'
    menu_item 2 '导入证书到证书库'
    menu_item 3 '修改协议证书域名'
    menu_item 4 '域名证书向导（检测 / ACME / 自动绑定）'
    menu_item 5 '为协议选择证书'
    menu_back '返回配置菜单'
    ask '请选择 [0-5]：' choice
    case "$choice" in
      1)show_certificate;;2)set_certificate;;3)tls_domain_menu;;
      4)domain_certificate_target_menu;;5)select_certificate_for_protocol;;
      0|'')return 0;;*)red '无效输入。';;
    esac
  done
}
set_hy2_hop(){
  local choice x start end candidate
  menu_header 'UDP 端口跳跃' '主菜单 / 配置 / 协议 / Hysteria2 / UDP 端口跳跃'
  printf '  当前范围：%s\n\n' "$(jq -r '.protocols.hy2.udp_hop | if length>0 then . else "未启用" end' "$STATE_FILE")"
  menu_item 1 '设置端口范围'
  menu_item 2 '关闭端口跳跃'
  menu_back '返回专项参数'
  ask '请选择 [0-2]：' choice
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
  menu_header 'Argo 固定隧道' '主菜单 / 配置 / 协议 / Vmess-WS / Argo'
  printf '  当前域名：%s\n\n' "$(jq -r '.protocols.vmess.argo_domain | if length>0 then . else "未启用" end' "$STATE_FILE")"
  menu_item 1 '设置固定隧道'
  menu_item 2 '关闭固定隧道'
  menu_back '返回专项参数'
  ask '请选择 [0-2]：' choice
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
  local tag=$1 state p label
  label=$(protocol_label "$tag")
  if tag_enabled "$tag"; then
    (( $(enabled_count) > 1 )) || { red '至少保留一个协议。'; return 0; }
    confirm_change "确认停用 ${label}？" \
      '服务配置、节点链接、二维码、订阅及脚本管理的 UFW 规则将同步移除；协议参数会保留。' || return 0
    state=$(jq --arg t "$tag" '.protocols[$t].enabled=false' "$STATE_FILE")
  else
    [[ "$tag" != anytls || $(jq -r '.core' "$STATE_FILE") != 1.10* ]] || { red '1.10 内核不支持 AnyTLS。'; return 0; }
    p=$(jq -r --arg t "$tag" '.protocols[$t].port' "$STATE_FILE")
    if ! valid_port "$p" || ! port_available "$p"; then while :; do p=$(random_port); port_available "$p" && ! jq -e --argjson p "$p" '[.protocols[]|select(.enabled)|.port] | index($p)' "$STATE_FILE" >/dev/null && break; done; fi
    confirm_change "确认启用 ${label}？" \
      "将使用 $(protocol_transport "$tag") 端口 ${p}，并同步配置、节点、订阅和 UFW（如已启用）。" || return 0
    state=$(jq --arg t "$tag" --argjson p "$p" '.protocols[$t].port=$p | .protocols[$t].enabled=true' "$STATE_FILE")
  fi
  apply_state "$state"
}

credential_menu(){
  local tag=$1 choice
  while :; do
    menu_header '凭据管理' "主菜单 / 配置 / 协议 / $(protocol_label "$tag") / 凭据"
    case "$tag" in
      vless|vmess)
        menu_item 1 '修改 UUID'
        menu_back '返回协议设置'
        ask '请选择 [0-1]：' choice
        case "$choice" in 1)set_uuid "$tag";;0|'')return 0;;*)red '无效输入。';;esac
        ;;
      hy2|anytls)
        menu_item 1 '修改密码'
        menu_back '返回协议设置'
        ask '请选择 [0-1]：' choice
        case "$choice" in 1)set_protocol "$tag" password '密码：';;0|'')return 0;;*)red '无效输入。';;esac
        ;;
    esac
  done
}

specialty_menu(){
  local tag=$1 choice
  while :; do
    menu_header '专项参数' "主菜单 / 配置 / 协议 / $(protocol_label "$tag") / 专项参数"
    case "$tag" in
      vless)
        menu_item 1 'Reality SNI 与目标扫描'
        menu_item 2 '轮换 Reality 密钥和 Short ID'
        menu_item 3 '切换 Sing-box / Xray 服务端内核'
        menu_item 4 'Xray Reality 参数向导'
        menu_back '返回协议设置'
        ask '请选择 [0-4]：' choice
        case "$choice" in
          1)set_reality_sni;;2)rotate_reality_keys;;3)set_reality_engine;;
          4)xray_reality_menu;;0|'')return 0;;*)red '无效输入。';;
        esac
        ;;
      vmess)
        menu_item 1 '修改 WS Path'
        menu_item 2 '开启或关闭 TLS'
        menu_item 3 '修改 CDN 地址'
        menu_item 4 '配置 Argo 固定隧道'
        menu_item 5 '修改证书域名'
        menu_back '返回协议设置'
        ask '请选择 [0-5]：' choice
        case "$choice" in 1)set_protocol vmess path 'WS Path（以 / 开头）：';;2)set_bool vmess tls;;3)set_protocol vmess cdn 'CDN 地址：';;4)set_argo;;5)set_tls_domain vmess;;0|'')return 0;;*)red '无效输入。';;esac
        ;;
      hy2)
        menu_item 1 '修改证书域名'
        menu_item 2 '修改上行带宽'
        menu_item 3 '修改下行带宽'
        menu_item 4 '配置 UDP 端口跳跃'
        menu_back '返回协议设置'
        ask '请选择 [0-4]：' choice
        case "$choice" in 1)set_tls_domain hy2;;2)set_number hy2 up_mbps '上行 Mbps：';;3)set_number hy2 down_mbps '下行 Mbps：';;4)set_hy2_hop;;0|'')return 0;;*)red '无效输入。';;esac
        ;;
      anytls)
        dim '普通 TLS 的 SNI 必须匹配实际证书域名，不使用 Reality 的第三方目标。'
        menu_item 1 '仅修改证书域名'
        menu_item 2 '域名证书向导（检测 / ACME / 自动绑定）'
        menu_back '返回协议设置'
        ask '请选择 [0-2]：' choice
        case "$choice" in 1)set_tls_domain "$tag";;2)domain_certificate_wizard "$tag";;0|'')return 0;;*)red '无效输入。';;esac
        ;;
    esac
  done
}

protocol_menu(){
  local tag choice action port
  while :; do
    menu_header '协议管理' '主菜单 / 配置 / 协议管理'
    protocol_status_line 1 vless
    protocol_status_line 2 vmess
    protocol_status_line 3 hy2
    protocol_status_line 4 anytls
    menu_back '返回配置菜单'
    ask '请选择协议 [0-4]：' choice
    case "$choice" in 1) tag=vless;;2) tag=vmess;;3) tag=hy2;;4) tag=anytls;;0|'') return 0;;*) red '无效输入。'; continue;; esac
    while :; do
      tag_enabled "$tag" && action='停用协议' || action='启用协议'
      port=$(jq -r --arg t "$tag" '.protocols[$t].port' "$STATE_FILE")
      valid_port "$port" || port='未分配'
      menu_header "$(protocol_label "$tag")" "主菜单 / 配置 / 协议管理 / $(protocol_label "$tag")"
      printf '  状态：%s   端口：%s/%s\n\n' \
        "$([[ "$action" == 停用协议 ]] && printf '已启用' || printf '未启用')" \
        "${port:-未分配}" "$(protocol_transport "$tag")"
      menu_item 1 "$action"
      menu_item 2 '修改节点名称'
      menu_item 3 '修改监听端口'
      menu_item 4 '修改凭据'
      menu_item 5 '专项参数'
      menu_back '返回协议列表'
      ask '请选择 [0-5]：' choice
      case "$choice" in
        1) toggle_protocol "$tag";;2) set_protocol "$tag" name '节点名称：';;3) set_port "$tag";;
        4) credential_menu "$tag";;5) specialty_menu "$tag";;0|'') break;;*) red '无效输入。';;
      esac
    done
  done
}

configure_menu(){
  while :; do
    menu_header '配置、节点与订阅' '主菜单 / 配置'
    menu_item 1 '查看已启用节点和完整链接'
    menu_item 2 '管理协议'
    menu_item 3 '修改分享链接的对外地址'
    menu_item 4 '显示节点二维码'
    menu_item 5 '重新生成订阅'
    menu_item 6 '证书管理'
    menu_item 7 '应用当前配置并重启'
    menu_back '返回主菜单'
    ask '请选择 [0-7]：' m
    case "$m" in
      1) show_nodes;;2) protocol_menu;;3) set_address;;4) show_qr;;
      5) if confirm_change '确认重新生成全部订阅？' '只读取当前已启用协议，不修改服务端配置。'; then generate_subscriptions; green '订阅已更新。'; fi;;
      6)certificate_menu;;7)apply_current_state || true;;0|'') return 0;;*) red '无效输入。';;
    esac
  done
}

offer_tls_certificate_setup(){
  local choice
  jq -e '
    .protocols.anytls.enabled or .protocols.hy2.enabled or
    (.protocols.vmess.enabled and .protocols.vmess.tls)
  ' "$STATE_FILE" >/dev/null || return 0
  menu_header '安装完成：证书建议' '主菜单 / 安装 / 证书'
  yellow '普通 TLS 协议当前使用安全的自签证书固定模式。'
  dim '有自有域名时，推荐使用灰云 DNS + ACME 受信证书。'
  menu_item 1 '现在进入证书管理'
  menu_item 2 '暂时保留固定模式'
  menu_back '返回主菜单'
  ask '请选择 [0-2]：' choice
  case "$choice" in
    1) certificate_menu;;
    2|0|'') return 0;;
    *) red '无效输入；暂时保留固定模式，可稍后使用 sb → 2 → 6 配置。';;
  esac
}

install_flow(){
  local core choice state tags tag labels= vless_engine=sing-box
  menu_header '安装向导：选择内核' '主菜单 / 安装'
  menu_item 1 "${SB_DEFAULT}（推荐，支持 AnyTLS）"
  menu_item 2 "${SB_110}（兼容版，不支持 AnyTLS）"
  menu_back '取消安装并返回主菜单'
  ask '请选择 [0-2]：' choice
  case "$choice" in 1)core=$SB_DEFAULT;;2)core=$SB_110;;0|'')return 0;;*)red '无效输入。'; return 0;;esac
  tags=$(choose_protocol_tags "$core") || { yellow '安装已取消，系统未做修改。'; return 0; }
  if [[ " $tags " == *' vless '* ]]; then
    vless_engine=$(choose_initial_reality_engine) ||
      { yellow '安装已取消，系统未做修改。'; return 0; }
  fi
  for tag in $tags; do labels+="${labels:+、}$(protocol_label "$tag")"; done
  menu_header '安装摘要' '主菜单 / 安装 / 确认'
  printf '  Sing-box 内核：%s\n' "$core"
  printf '  启用协议：%s\n' "$labels"
  [[ " $tags " == *' vless '* ]] &&
    printf '  Reality 内核：%s\n' "$([[ "$vless_engine" == xray ]] && printf 'Xray-core %s' "$XRAY_DEFAULT" || printf 'Sing-box %s' "$core")"
  printf '  更新渠道：%s\n' "$FORK_BRANCH"
  dim '确认后才会安装依赖、下载并校验文件、生成配置和启动服务。'
  confirm_change '确认开始安装？' '协议端口将自动选择未占用端口，安装后可随时修改。' ||
    { yellow '安装已取消，系统未做修改。'; return 0; }
  install_packages
  sync_reality_targets || { red '无法准备经过校验的 Reality 候选域名清单，安装未继续。'; return 0; }
  install_singbox "$core"; install_rule_databases
  state=$(state_for_protocol_tags "$core" "$tags")
  state=$(jq --arg engine "$vless_engine" '.protocols.vless.engine=$engine' <<<"$state")
  if [[ "$vless_engine" == xray ]]; then install_xray; fi
  if ! printf '%s\n' "$state" | commit_state; then
    red '安装配置未能应用，请根据上方错误处理后重试。'
    return 0
  fi
  printf '%s\n' "$core" > "$STATE_DIR/core-version"
  update_script no-reload
  green '安装完成。可通过 sb → 配置菜单随时增删协议。'
  offer_tls_certificate_setup
}

update_script(){
  local reload=${1:-reload} expected
  if [[ "$reload" == reload ]]; then
    menu_header '更新脚本' '主菜单 / 更新管理'
    printf '  更新渠道：%s\n' "$FORK_BRANCH"
    printf '  下载地址：%s/sb.sh\n' "$FORK_RAW"
    confirm_change '确认下载、校验并切换脚本？' \
      "更新成功后将保存渠道 ${FORK_BRANCH}，并立即进入新脚本。" || return 2
  fi
  WORKDIR=$(tmpdir)
  expected=$(curl -fsSL --proto '=https' --tlsv1.2 "$FORK_RAW/sb.sh.sha256" | awk '{print $1}')
  [[ "$expected" =~ ^[0-9a-f]{64}$ ]] || die 'fork 快捷脚本校验文件无效，拒绝更新。'
  download_verified "$FORK_RAW/sb.sh" "$expected" "$WORKDIR/sb.sh" 'fork 快捷脚本'
  install -m 755 "$WORKDIR/sb.sh" /usr/local/bin/sb
  persist_channel
  green "脚本更新完成，渠道已保存为 ${FORK_BRANCH}。"
  if [[ "$reload" == reload ]]; then
    green '正在切换到新脚本进程...'
    trap - EXIT
    cleanup_tmp
    exec /usr/local/bin/sb
  fi
}
update_menu(){
  local choice custom previous
  while :; do
    menu_header '更新管理' '主菜单 / 更新管理'
    printf '  当前渠道：%s\n\n' "$FORK_BRANCH"
    menu_item 1 "从当前渠道更新（${FORK_BRANCH}）"
    menu_item 2 '切换到稳定版 v0.0.2 并更新'
    menu_item 3 '切换到开发版 main 并更新'
    menu_item 4 '输入其他分支或标签'
    menu_back '返回主菜单'
    ask '请选择 [0-4]：' choice
    previous=$FORK_BRANCH
    case "$choice" in
      1) update_script && return 0 || true;;
      2)
        set_channel v0.0.2
        update_script && return 0 || set_channel "$previous"
        ;;
      3)
        set_channel main
        update_script && return 0 || set_channel "$previous"
        ;;
      4)
        ask '分支或标签（回车/0 返回上一级）：' custom
        cancel_input "$custom" && continue
        if set_channel "$custom"; then
          update_script && return 0 || set_channel "$previous"
        else
          set_channel "$previous"
        fi
        ;;
      0|'') return 0;;
      *) red '无效输入。';;
    esac
  done
}
check_version(){
  local latest
  latest=$(curl -fsSL --max-time 3 "$FORK_RAW/version" 2>/dev/null | head -n1 || true)
  [[ -n "$latest" && "$latest" != "$SCRIPT_VERSION"* ]] &&
    yellow "渠道 ${FORK_BRANCH} 可用版本：$latest（当前 $SCRIPT_VERSION）"
  return 0
}
update_core(){
  local v candidate tmp cfg
  menu_header '更新或切换 Sing-box 内核' '主菜单 / 内核管理'
  printf '  当前版本：%s\n' "$(jq -r '.core' "$STATE_FILE")"
  printf '  推荐版本：%s\n\n' "$SB_DEFAULT"
  dim '也可以输入其他官方版本；必须获取并验证对应官方校验文件。'
  ask '版本号（回车/0 返回上一级）：' v
  cancel_input "$v" && return 0
  [[ "$v" != 1.10* || $(jq -r '.protocols.anytls.enabled' "$STATE_FILE") != true ]] || { red '请先停用 AnyTLS，再切换到 1.10。'; return 0; }
  install_singbox "$v" "$STATE_DIR/sing-box.new"
  candidate=$(jq --arg v "$v" '.core=$v' "$STATE_FILE"); tmp=$(tmpdir); cfg="$tmp/sb.json"; render_config "$candidate" "$cfg"
  "$STATE_DIR/sing-box.new" check -c "$cfg" || { rm -f "$STATE_DIR/sing-box.new"; red '新内核无法验证现有配置，拒绝切换。'; return 0; }
  cp "$STATE_DIR/sing-box" "$STATE_DIR/sing-box.old"; mv "$STATE_DIR/sing-box.new" "$STATE_DIR/sing-box"
  if printf '%s\n' "$candidate" | commit_state; then printf '%s\n' "$v" > "$STATE_DIR/core-version"; rm -f "$STATE_DIR/sing-box.old"; else mv "$STATE_DIR/sing-box.old" "$STATE_DIR/sing-box"; restart_service || true; fi
}
systemd_stop_disable(){
  local unit=$1 pid i
  pid=$(systemctl show "$unit" -p MainPID --value 2>/dev/null || true)
  systemctl stop "$unit" 2>/dev/null || true
  systemctl disable "$unit" 2>/dev/null || true
  [[ "$pid" =~ ^[1-9][0-9]*$ ]] || return 0
  kill -0 "$pid" 2>/dev/null || return 0
  kill -TERM "$pid" 2>/dev/null || true
  for ((i=0; i<20; i++)); do
    kill -0 "$pid" 2>/dev/null || return 0
    sleep 0.1
  done
  kill -KILL "$pid" 2>/dev/null || true
}
stop_systemd_services(){
  command -v systemctl >/dev/null 2>&1 || return 0
  systemd_stop_disable sing-box.service || true
  systemd_stop_disable sing-box-xray.service || true
  systemd_stop_disable sing-box-argo.service || true
}
uninstall(){
  local x fw
  read -r -p '确认卸载 VPS Sing-box（输入 YES；回车/0 返回上一级）：' x
  [[ $x == YES ]] || return 1
  [[ "$STATE_DIR" == /etc/s-box ]] || { red "拒绝删除非标准状态目录：$STATE_DIR"; return 1; }
  stop_systemd_services
  rc-service sing-box stop 2>/dev/null || true
  rc-service sing-box-xray stop 2>/dev/null || true
  rc-update del sing-box default 2>/dev/null || true
  rc-update del sing-box-xray default 2>/dev/null || true
  ufw_remove_all_managed
  for fw in iptables ip6tables; do
    command -v "$fw" >/dev/null 2>&1 || continue
    while "$fw" -t nat -C PREROUTING -j SBYG_HY2 2>/dev/null; do "$fw" -t nat -D PREROUTING -j SBYG_HY2 || break; done
    "$fw" -t nat -F SBYG_HY2 2>/dev/null || true
    "$fw" -t nat -X SBYG_HY2 2>/dev/null || true
  done
  rm -f "$(bbr_sysctl_file)" "$(bbr_module_file)"
  rm -rf "$STATE_DIR" /etc/systemd/system/sing-box.service /etc/systemd/system/sing-box-xray.service /etc/systemd/system/sing-box-argo.service /etc/init.d/sing-box /etc/init.d/sing-box-xray
  rm -f /usr/local/bin/sb
  systemctl daemon-reload 2>/dev/null || true
  green '卸载完成：服务、状态文件、防火墙跳跃链和 sb 快捷命令已删除。'
  return 0
}

main(){
  local migration_rc migration_choice
  need_root; if [[ -f "$CONFIG_FILE" && ! -f "$STATE_FILE" ]]; then die '检测到旧版安装，缺少新版状态文件。为避免错误修改，请先卸载后重装。'; fi
  if ensure_current_state_schema; then
    :
  else
    migration_rc=$?
    if [[ "$migration_rc" == 2 ]]; then
      menu_header '旧版 TUIC 状态处理' '启动检查'
      menu_item 1 '卸载旧 TUIC 安装'
      menu_back '返回终端'
      ask '请选择 [0-1]：' migration_choice
      [[ "$migration_choice" == 1 ]] && { uninstall && exit 0; }
      exit 0
    fi
    die '状态迁移失败，原运行配置已保留。'
  fi
  check_version
  while :; do
    main_dashboard
    menu_item 1 '安装'
    menu_item 2 '配置、节点与订阅'
    menu_item 3 '应用当前配置并重启'
    menu_item 4 '更新或切换 Sing-box 内核'
    menu_item 5 '更新管理'
    menu_item 6 '查看服务日志'
    menu_item 7 '域名证书 / ACME'
    menu_item 8 'WARP'
    menu_item 9 'TCP / BBR 管理'
    menu_item 10 'WARP-plus'
    menu_item 11 '卸载'
    menu_back '退出'
    ask '请选择 [0-11]：' m
    case "$m" in
      1) [[ -f "$STATE_FILE" ]] && red '当前已安装，请进入“配置、节点与订阅”管理。' || install_flow;;
      2) require_install && configure_menu;;
      3) require_install && { apply_current_state || true; };;
      4) require_install && update_core;;
      5) update_menu;;
      6) require_install && {
        command -v journalctl >/dev/null &&
          journalctl -u sing-box -u sing-box-xray -n 100 --no-pager ||
          tail -n 100 /var/log/messages
      };;
      7) if [[ -f "$STATE_FILE" ]]; then domain_certificate_target_menu; else acme; fi;;
      8) warp;;
      9) bbr;;
      10) require_install && install_sbwpph;;
      11) if uninstall; then exit 0; fi;;
      0) exit 0;;
      *) red '无效输入。';;
    esac
  done
}
[[ "${SBYG_LIB_ONLY:-0}" == 1 ]] || main "$@"
