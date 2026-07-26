#!/usr/bin/env bash
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
CORE_DEFAULT=${SB_TEST_CORE_DEFAULT:-}
CORE_110=${SB_TEST_CORE_110:-}
XRAY_CORE=${SB_TEST_XRAY_CORE:-}
[[ -x "$CORE_DEFAULT" && -x "$CORE_110" && -x "$XRAY_CORE" ]] || {
  echo "set SB_TEST_CORE_DEFAULT, SB_TEST_CORE_110 and SB_TEST_XRAY_CORE to executable test cores" >&2
  exit 77
}

TEST_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/vpnm-tests.XXXXXX")
export VPNM_LIB_ONLY=1
export VPNM_STATE_DIR="$TEST_ROOT/initial"
source "$ROOT/sb.sh"
trap 'cleanup_tmp; rm -rf "$TEST_ROOT"' EXIT

# The executable banner and update comparison must stay in lockstep with the
# published version file; otherwise a successful self-update looks stale.
[[ "$(head -n1 "$ROOT/version")" == "$SCRIPT_VERSION" ]]

# Existing systemd services must always restart after a validated config replacement.
systemctl_calls=
systemctl(){ systemctl_calls+="${*}"$'\n'; }
systemd_enable_restart vps-net-manager.service
[[ "$systemctl_calls" == $'enable vps-net-manager.service\nrestart vps-net-manager.service\n' ]]
unset -f systemctl

# Core reconciliation releases old listeners before starting the selected core.
systemctl_calls=
systemctl(){ systemctl_calls+="${*}"$'\n'; }
reconcile_core_services '{"protocols":{"vless":{"enabled":true,"engine":"xray"},"vmess":{"enabled":false,"engine":"sing-box"}}}'
[[ "$systemctl_calls" == $'disable --now vps-net-manager\ndisable --now vps-net-manager-xray\nenable vps-net-manager-xray\nrestart vps-net-manager-xray\n' ]]
unset -f systemctl

# Missing/failing optional Argo state must not prevent stopping the main service or vice versa.
saved_systemd_stop_disable=$(declare -f systemd_stop_disable)
stopped_units=
systemctl(){ :; }
systemd_stop_disable(){ stopped_units+="${1}"$'\n'; [[ "$1" != vps-net-manager.service ]]; }
stop_systemd_services
[[ "$stopped_units" == $'vps-net-manager.service\nvps-net-manager-xray.service\nvps-net-manager-argo.service\nvps-net-manager-cert-sync.timer\nvps-net-manager-realm.service\n' ]]
eval "$saved_systemd_stop_disable"
unset -f systemctl

set_case_dir(){
  STATE_DIR="$TEST_ROOT/$1"
  STATE_FILE="$STATE_DIR/protocols.json"
  CONFIG_FILE="$STATE_DIR/sb.json"
  XRAY_CONFIG_FILE="$STATE_DIR/xray.json"
  mkdir -p "$STATE_DIR"
}

# Native BBR management only owns its own sysctl/module files, verifies the
# resulting runtime values, and returns to the captured pre-change values.
set_case_dir bbr
export VPNM_BBR_SYSCTL_FILE="$TEST_ROOT/bbr/99-vps-net-manager-bbr.conf"
export VPNM_BBR_MODULE_FILE="$TEST_ROOT/bbr/vps-net-manager-bbr.conf"
bbr_cc=cubic
bbr_qdisc=fq_codel
sysctl(){
  case "$1" in
    -n)
      case "$2" in
        net.ipv4.tcp_available_congestion_control) printf 'reno cubic bbr\n';;
        net.ipv4.tcp_congestion_control) printf '%s\n' "$bbr_cc";;
        net.core.default_qdisc) printf '%s\n' "$bbr_qdisc";;
      esac
      ;;
    -q)
      [[ "$2" == -p ]] && { bbr_cc=bbr; bbr_qdisc=fq; }
      ;;
    -qw)
      case "$2" in
        net.ipv4.tcp_congestion_control=*) bbr_cc=${2#*=};;
        net.core.default_qdisc=*) bbr_qdisc=${2#*=};;
      esac
      ;;
  esac
}
saved_confirm_change=$(declare -f confirm_change)
confirm_change(){ return 0; }
bbr_enable_native
[[ -f "$VPNM_BBR_SYSCTL_FILE" && -f "$VPNM_BBR_MODULE_FILE" && -f "$(bbr_state_file)" ]]
[[ "$bbr_cc" == bbr && "$bbr_qdisc" == fq ]]
bbr_revert_native
[[ ! -e "$VPNM_BBR_SYSCTL_FILE" && ! -e "$VPNM_BBR_MODULE_FILE" && ! -e "$(bbr_state_file)" ]]
[[ "$bbr_cc" == cubic && "$bbr_qdisc" == fq_codel ]]
eval "$saved_confirm_change"
unset -f sysctl
unset VPNM_BBR_SYSCTL_FILE VPNM_BBR_MODULE_FILE

# Update channels persist across later `vpnm` runs, while an explicit environment
# selection wins for the current run and is validated before use.
set_case_dir channel
printf 'v0.0.1\n' > "$STATE_DIR/channel"
unset VPNM_CHANNEL
load_channel
[[ "$FORK_BRANCH" == v0.0.1 && "$FORK_RAW" == */v0.0.1 ]]
export VPNM_CHANNEL=feature/ux-test
load_channel
[[ "$FORK_BRANCH" == feature/ux-test && "$FORK_RAW" == */feature/ux-test ]]
persist_channel
[[ $(cat "$STATE_DIR/channel") == feature/ux-test ]]
! valid_channel 'bad..ref'
! valid_channel '/absolute'
! valid_channel 'double//slash'
unset VPNM_CHANNEL
set_channel main
saved_update_script=$(declare -f update_script)
selected_update_channel=
update_script(){ selected_update_channel=$FORK_BRANCH; }
update_menu <<<"2"
[[ "$selected_update_channel" == main ]]
set_channel main
update_menu <<< $'3\nfeature/next'
[[ "$selected_update_channel" == feature/next ]]
update_script(){ return 2; }
set_channel main
update_menu <<< $'2\n0'
[[ "$FORK_BRANCH" == main ]]
eval "$saved_update_script"
set_channel main
saved_install_packages=$(declare -f install_packages)
install_package_calls=0
install_packages(){ install_package_calls=$((install_package_calls + 1)); }
install_flow <<< $'1\n1,3\n0'
[[ $install_package_calls -eq 0 && ! -e "$STATE_FILE" ]]
eval "$saved_install_packages"

make_state(){
  local version=$1 core=$2
  cp "$core" "$STATE_DIR/sing-box"
  new_state "$version"
}

# Explicit apply/restart always consumes the latest state file.
set_case_dir apply-current
printf '{"marker":"latest"}\n' > "$STATE_FILE"
saved_commit_state=$(declare -f commit_state)
applied_state=
commit_state(){ applied_state=$(cat); }
apply_current_state
[[ $(jq -r '.marker' <<<"$applied_state") == latest ]]
eval "$saved_commit_state"

enable_tags(){
  local state=$1 tag port=25000
  shift
  for tag in "$@"; do
    port=$((port + 1))
    state=$(jq --arg tag "$tag" --argjson port "$port" '.protocols[$tag].configured=true | .protocols[$tag].enabled=true | .protocols[$tag].port=$port' <<<"$state")
  done
  printf '%s\n' "$state"
}

check_case(){
  local name=$1 version=$2 core=$3 expected=$4
  shift 4
  set_case_dir "$name"
  local state
  state=$(make_state "$version" "$core")
  state=$(enable_tags "$state" "$@")
  state=$(jq '.public_address="node.example.com"' <<<"$state")
  printf '%s\n' "$state" > "$STATE_FILE"
  validate_state "$state"
  render_config "$state" "$CONFIG_FILE"
  "$STATE_DIR/sing-box" check -c "$CONFIG_FILE"
  generate_subscriptions
  "$STATE_DIR/sing-box" check -c "$STATE_DIR/sing-box-client.json"
  [[ $(jq '.inbounds|length' "$CONFIG_FILE") -eq "$expected" ]]
  [[ $(jq '.outbounds|length' "$STATE_DIR/sing-box-client.json") -eq "$expected" ]]
  [[ $(jq '.proxies|length' "$STATE_DIR/mihomo.yaml") -eq "$expected" ]]
  [[ $(wc -l < "$STATE_DIR/subscription.txt" | tr -d ' ') -eq "$expected" ]]
  [[ $(jq '[.inbounds[].tag] | unique | length' "$CONFIG_FILE") -eq "$expected" ]]
}

set_case_dir selection
cp "$CORE_DEFAULT" "$STATE_DIR/sing-box"
saved_port_available=$(declare -f port_available)
port_available(){ return 0; }
selected=$(select_protocols 1.13.14 <<<"1,3,4")
eval "$saved_port_available"
jq -e '[.protocols|to_entries[]|select(.value.enabled)|.key] == ["vless","hy2","anytls"]' <<<"$selected" >/dev/null

# First-install custom mode keeps selected ports and initial address/domain
# settings in the state, while the default mode remains random and preset.
default_setup=$(initial_default_settings)
jq -e '.custom==false and .ports=={} and .reality_sni=="www.apple.com" and .tls_domain=="www.bing.com" and .tls_certificate.mode=="fixed"' <<<"$default_setup" >/dev/null
saved_port_available=$(declare -f port_available)
port_available(){ return 0; }
custom_setup=$(choose_initial_setup 'vless vmess anytls' <<< $'2\n21001\n21002\n21003\nreality.example.com\ntls.example.com\n1\nnode.example.com')
custom_state=$(state_for_protocol_tags 1.13.14 'vless vmess anytls' "$custom_setup")
eval "$saved_port_available"
jq -e '
  .public_address=="node.example.com" and
  .protocols.vless.port==21001 and .protocols.vmess.port==21002 and .protocols.anytls.port==21003 and
  .protocols.vless.sni=="reality.example.com" and
  .protocols.vless.xray.target=="reality.example.com:443" and
  .protocols.vless.xray.server_names==["reality.example.com"] and
  .protocols.vmess.domain=="tls.example.com" and .protocols.anytls.domain=="tls.example.com" and
  .protocols.hy2.domain=="www.bing.com" and .protocols.hy2.certificate_id=="default"
' <<<"$custom_state" >/dev/null
jq -e '.tls_certificate.mode=="acme"' <<<"$custom_setup" >/dev/null
openssl x509 -in "$STATE_DIR/cert.pem" -noout -text | grep -Fq 'DNS:www.bing.com'
if choose_initial_setup vless <<<"0" >/dev/null; then
  echo 'initial setup cancellation unexpectedly succeeded' >&2
  exit 1
fi

# A custom initial TLS domain uses a trusted library/ACME/import certificate
# when available, and binds it before the first service configuration commit.
set_case_dir initial-tls-certificate
cp "$CORE_DEFAULT" "$STATE_DIR/sing-box"
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj '/CN=initial-tls.example.com' -addext 'subjectAltName=DNS:initial-tls.example.com' \
  -keyout "$STATE_DIR/source.key" -out "$STATE_DIR/source.pem" >/dev/null 2>&1
saved_port_available=$(declare -f port_available)
port_available(){ return 0; }
initial_tls_settings=$(jq -cn --arg cert "$STATE_DIR/source.pem" --arg key "$STATE_DIR/source.key" \
  '{custom:true,ports:{anytls:21111},reality_sni:"www.apple.com",tls_domain:"initial-tls.example.com",tls_certificate:{mode:"import",cert:$cert,key:$key},public_address:""}')
initial_tls_state=$(state_for_protocol_tags 1.13.14 anytls "$initial_tls_settings")
eval "$saved_port_available"
saved_initial_certificate_matches_domain=$(declare -f certificate_matches_domain)
certificate_matches_domain(){ [[ "$1:$2" == "$STATE_DIR/source.pem:initial-tls.example.com" ]]; }
initial_tls_certificate_setup "$initial_tls_state" anytls "$initial_tls_settings"
eval "$saved_initial_certificate_matches_domain"
initial_tls_state=$INITIAL_TLS_STATE
initial_tls_cert_id=$(jq -r '.protocols.anytls.certificate_id' <<<"$initial_tls_state")
[[ "$initial_tls_cert_id" != default ]]
jq -e --arg id "$initial_tls_cert_id" '
  .protocols.anytls.domain=="initial-tls.example.com" and
  .protocols.vmess.domain=="www.bing.com" and .protocols.vmess.certificate_id=="default" and
  .protocols.hy2.domain=="www.bing.com" and .protocols.hy2.certificate_id=="default" and
  .certificates[$id].mode=="trusted" and .certificates[$id].insecure==false and
  .certificates[$id].source.auto_sync==true
' <<<"$initial_tls_state" >/dev/null
[[ -r "$(jq -r --arg id "$initial_tls_cert_id" '.certificates[$id].cert' <<<"$initial_tls_state")" ]]
printf '%s\n' "$initial_tls_state" > "$STATE_FILE"
[[ -z "$(offer_tls_certificate_setup <<< '')" ]]
[[ $(choose_initial_reality_engine <<<"1") == sing-box ]]
[[ $(choose_initial_reality_engine <<<"2") == xray ]]
if choose_initial_reality_engine <<<"0" >/dev/null; then
  echo 'Reality engine selection cancellation unexpectedly succeeded' >&2
  exit 1
fi
set_case_dir selection-cancel
cp "$CORE_DEFAULT" "$STATE_DIR/sing-box"
if select_protocols 1.13.14 <<<"0" >/dev/null; then
  echo 'protocol selection cancellation unexpectedly succeeded' >&2
  exit 1
fi
[[ ! -e "$STATE_DIR/cert.pem" && ! -e "$STATE_DIR/private.key" ]]

check_case single 1.13.14 "$CORE_DEFAULT" 1 vless
check_case multi 1.13.14 "$CORE_DEFAULT" 3 vless hy2 anytls
check_case four 1.13.14 "$CORE_DEFAULT" 4 vless vmess hy2 anytls
check_case legacy 1.10.7 "$CORE_110" 3 vless vmess hy2

# Enabling a previously disabled protocol must never silently consume its old
# port: every transport gets the same random/custom/preconfigured port wizard.
set_case_dir activation
activation_state=$(make_state 1.13.14 "$CORE_DEFAULT")
activation_state=$(jq '.protocols.vless.enabled=true | .protocols.vless.port=25001' <<<"$activation_state")
printf '%s\n' "$activation_state" > "$STATE_FILE"
saved_activation_apply=$(declare -f apply_state)
saved_activation_confirm=$(declare -f confirm_change)
saved_activation_port_available=$(declare -f port_available)
saved_activation_random_port=$(declare -f random_port)
saved_activation_certificate_matches_domain=$(declare -f certificate_matches_domain)
saved_activation_hop_preflight=$(declare -f hy2_hop_nat_preflight)
activation_candidate=
apply_state(){ activation_candidate=$1; }
confirm_change(){ return 0; }
port_available(){ return 0; }
random_port(){ printf '29001\n'; }
certificate_matches_domain(){ return 0; }
hy2_hop_nat_preflight(){ return 0; }
toggle_protocol vmess <<< $'2\n26001\n1'
[[ $(jq -r '.protocols.vmess.enabled' <<<"$activation_candidate") == true ]] || exit 1
[[ $(jq -r '.protocols.vmess.port' <<<"$activation_candidate") == 26001 ]] || exit 1
toggle_protocol hy2 <<< $'1\n1\n1'
[[ $(jq -r '.protocols.hy2.enabled' <<<"$activation_candidate") == true ]] || exit 1
[[ $(jq -r '.protocols.hy2.port' <<<"$activation_candidate") == 29001 ]] || exit 1
toggle_protocol hy2 <<< $'2\n32000:32010\n1\n1'
[[ $(jq -r '.protocols.hy2.udp_hop' <<<"$activation_candidate") == 32000:32010 ]] || exit 1
eval "$saved_activation_apply"
eval "$saved_activation_confirm"
eval "$saved_activation_port_available"
eval "$saved_activation_random_port"
eval "$saved_activation_certificate_matches_domain"
eval "$saved_activation_hop_preflight"

# Protocol menus expose live enabled state, transport, port and TLS details.
set_case_dir four
four_state=$(cat "$TEST_ROOT/four/protocols.json")
protocol_line=$(protocol_status_line 1 vless)
[[ "$protocol_line" == *'Vless-Reality'*'[已启用]'*'TCP'*'25001'*'Sing-box'*'SNI www.apple.com'* ]]
protocol_line=$(protocol_status_line 4 anytls)
[[ "$protocol_line" == *'AnyTLS'*'[已启用]'*'TCP'*'固定证书'* ]]
[[ "$protocol_line" == *$'\033[32;1m'* ]]
protocol_link=$(show_protocol_share_link vless)
[[ "$protocol_link" == *'当前分享链接（可直接复制）'*'vless://'* ]]

# Distinguish retained-but-disabled and removed protocol state at a glance.
disabled_state=$(jq '.protocols.vmess.enabled=false' "$STATE_FILE")
printf '%s\n' "$disabled_state" > "$STATE_FILE"
protocol_line=$(protocol_status_line 2 vmess)
[[ "$protocol_line" == *$'\033[33;1m'*'[未启用]'* ]]
removed_state=$(jq '.protocols.vmess.configured=false | .protocols.vmess.enabled=false' "$STATE_FILE")
printf '%s\n' "$removed_state" > "$STATE_FILE"
protocol_line=$(protocol_status_line 2 vmess)
[[ "$protocol_line" == *$'\033[31;1m'*'[未添加]'* ]]
printf '%s\n' "$four_state" > "$STATE_FILE"

# New self-signed certificates are leaf certificates with SAN and both client-specific pins.
four_state=$(cat "$TEST_ROOT/four/protocols.json")
[[ $(jq -r '.certificates.default.mode' <<<"$four_state") == pinned ]]
four_cert=$(jq -r '.certificates.default.cert' <<<"$four_state")
four_cert_text=$(openssl x509 -in "$four_cert" -noout -text)
grep -q 'DNS:www.bing.com' <<<"$four_cert_text"
grep -q 'CA:FALSE' <<<"$four_cert_text"
expected_der_pin=$(certificate_der_sha256 "$four_cert")
expected_spki_pin=$(certificate_spki_sha256 "$four_cert")

# A manager with no enabled protocol is valid: it keeps state/library files but
# produces an empty Sing-box config and stops both managed core services.
zero_state=$(jq '.protocols |= with_entries(.value.configured=false | .value.enabled=false | .value.port=0)' <<<"$four_state")
validate_state "$zero_state"
render_config "$zero_state" "$STATE_DIR/zero.json"
"$STATE_DIR/sing-box" check -c "$STATE_DIR/zero.json"
[[ $(jq '.inbounds|length' "$STATE_DIR/zero.json") -eq 0 ]]
systemctl_calls=
systemctl(){ systemctl_calls+="${*}"$'\n'; }
reconcile_core_services "$zero_state"
[[ "$systemctl_calls" == $'disable --now vps-net-manager\ndisable --now vps-net-manager-xray\n' ]] || exit 1
unset -f systemctl

# Viewing nodes with zero enabled protocols is a normal state and must return
# to the caller instead of letting `set -e` terminate the manager.
printf '%s\n' "$zero_state" > "$STATE_FILE"
zero_nodes=$(show_nodes)
[[ "$zero_nodes" == *'当前没有已启用协议'* ]]
printf '%s\n' "$four_state" > "$STATE_FILE"

# Credential rotation defaults to generated material, while deleting a protocol
# resets only that protocol and leaves the certificate library intact.
saved_rotation_apply=$(declare -f apply_state)
saved_rotation_confirm=$(declare -f confirm_change)
rotated_state=
apply_state(){ rotated_state=$1; }
confirm_change(){ return 0; }
old_vless_uuid=$(jq -r '.protocols.vless.uuid' "$STATE_FILE")
rotate_protocol_uuid vless
new_vless_uuid=$(jq -r '.protocols.vless.uuid' <<<"$rotated_state")
valid_uuid "$new_vless_uuid" && [[ "$new_vless_uuid" != "$old_vless_uuid" ]] || exit 1
delete_protocol vmess
jq -e '.protocols.vmess.configured==false and .protocols.vmess.enabled==false and .protocols.vmess.port==0 and (.protocols.vmess.path|startswith("/")) and .certificates.default != null' <<<"$rotated_state" >/dev/null || exit 1
eval "$saved_rotation_apply"
eval "$saved_rotation_confirm"

anytls_link=$(grep '^anytls://' "$TEST_ROOT/four/subscription.txt")
[[ "$anytls_link" == *"&pcs=${expected_der_pin}"* && "$anytls_link" != *'insecure='* ]]
jq -e --arg pin "$expected_spki_pin" '
  .outbounds[] | select(.type=="anytls") |
  .tls.insecure == true and .tls.certificate_public_key_sha256 == [$pin]
' "$TEST_ROOT/four/sing-box-client.json" >/dev/null

# 1.10 never accepts AnyTLS.
legacy_state=$(cat "$TEST_ROOT/legacy/protocols.json")
legacy_invalid=$(jq '.protocols.anytls.enabled=true | .protocols.anytls.port=26000' <<<"$legacy_state")
! validate_state "$legacy_invalid"

# Duplicate/invalid ports, certificate modes and a malformed WS path are rejected.
duplicate=$(jq '.protocols.vmess.port=.protocols.vless.port' <<<"$four_state")
! validate_state "$duplicate"
bad_path=$(jq '.protocols.vmess.path="missing-slash"' <<<"$four_state")
! validate_state "$bad_path"
bad_port=$(jq '.protocols.hy2.port=70000' <<<"$four_state")
! validate_state "$bad_port"
bad_cert_mode=$(jq '.certificates.default.mode="trusted" | .certificates.default.insecure=true' <<<"$four_state")
! validate_state "$bad_cert_mode"
bad_engine=$(jq '.protocols.anytls.engine="xray"' <<<"$four_state")
! validate_state "$bad_engine"
bad_xray_version=$(jq '.xray_core="latest"' <<<"$four_state")
! validate_state "$bad_xray_version"
bad_mldsa_pair=$(jq '.protocols.vless.xray.mldsa65_seed="seed" | .protocols.vless.xray.mldsa65_verify=""' <<<"$four_state")
! validate_state "$bad_mldsa_pair"
bad_cert_source=$(jq '.certificates.default.source={type:"files",auto_sync:true}' <<<"$four_state")
! validate_state "$bad_cert_source"
anytls_padding_valid default
anytls_padding_valid custom 'stop=3' '0=10-20' '1=20-40,c' '2=1-1'
! anytls_padding_valid custom 'stop=2' '2=1-1'
! anytls_padding_valid custom 'stop=2' '0=20-10'
custom_padding=$(jq '.protocols.anytls.padding={mode:"custom",lines:["stop=2","0=10-20","1=1-1,c"]}' <<<"$four_state")
validate_state "$custom_padding"
bad_padding=$(jq '.protocols.anytls.padding={mode:"custom",lines:["stop=2","0=10-20","0=1-1"]}' <<<"$four_state")
! validate_state "$bad_padding"

ss(){ printf 'udp UNCONN 0 0 [::]:28000 [::]:*\n'; }
! port_available 28000
port_available 28001
unset -f ss
valid_host 203.0.113.10
valid_host 2001:db8::10
valid_host node.example.com
! valid_host 999.1.1.1
! valid_host 'bad host!'

# Realm rules use a separate, constrained state file and render stable TOML
# without sharing Sing-box listener or certificate state.
set_case_dir realm
realm_state=$(realm_default_state)
realm_state=$(jq '.rules=[
  {id:"realm_a1b2c3d4",listen_host:"0.0.0.0",listen_port:40123,remote_host:"203.0.113.10",remote_port:443},
  {id:"realm_e5f60708",listen_host:"2001:db8::1",listen_port:40124,remote_host:"2001:db8::2",remote_port:8443}
]' <<<"$realm_state")
realm_validate_state "$realm_state"
realm_render_config "$realm_state" "$STATE_DIR/realm.toml"
grep -Fqx 'listen = "0.0.0.0:40123"' "$STATE_DIR/realm.toml"
grep -Fqx 'remote = "[2001:db8::2]:8443"' "$STATE_DIR/realm.toml"
grep -Fqx 'use_udp = true' "$STATE_DIR/realm.toml"
realm_duplicate=$(jq '.rules[1].listen_host="0.0.0.0" | .rules[1].listen_port=40123' <<<"$realm_state")
! realm_validate_state "$realm_duplicate"
realm_bad_id=$(jq '.rules[0].id="bad"' <<<"$realm_state")
! realm_validate_state "$realm_bad_id"

# Active UFW changes are transactional: open candidate ports first, then remove only owned old rules.
old_fw_state=$(jq '.protocols.vmess.enabled=false | .protocols.hy2.udp_hop=""' <<<"$four_state")
old_vless_port=$(jq -r '.protocols.vless.port' <<<"$old_fw_state")
old_anytls_port=$(jq -r '.protocols.anytls.port' <<<"$old_fw_state")
new_fw_state=$(jq '
  .protocols.vless.port=31000 |
  .protocols.anytls.enabled=false |
  .protocols.hy2.udp_hop="32000:32010"
' <<<"$old_fw_state")
mock_ufw_rules=$(ufw_desired_rules "$old_fw_state")
mock_ufw_added=
mock_ufw_deleted=
saved_ufw_active=$(declare -f ufw_active)
saved_ufw_allow_exists=$(declare -f ufw_allow_exists)
saved_ufw_add_rule=$(declare -f ufw_add_rule)
saved_ufw_delete_rule=$(declare -f ufw_delete_rule)
ufw_active(){ return 0; }
ufw_allow_exists(){
  local wanted_spec=$1 wanted_tag=${2:-} tag spec
  while IFS=$'\t' read -r tag spec; do
    [[ "$spec" == "$wanted_spec" && ( -z "$wanted_tag" || "$tag" == "$wanted_tag" ) ]] && return 0
  done <<<"$mock_ufw_rules"
  return 1
}
ufw_add_rule(){
  mock_ufw_added+="${1}"$'\t'"${2}"$'\n'
  mock_ufw_rules+=$'\n'"${1}"$'\t'"${2}"
}
ufw_delete_rule(){ mock_ufw_deleted+="${1}"$'\t'"${2}"$'\n'; }
ufw_transaction="$TEST_ROOT/ufw-transaction.tsv"
ufw_prepare_candidate "$new_fw_state" "$ufw_transaction"
[[ "$mock_ufw_added" == *$'vless\t31000/tcp\n'* ]]
[[ "$mock_ufw_added" == *$'hy2-hop\t32000:32010/udp\n'* ]]
[[ "$mock_ufw_added" != *$'hy2\t'* ]]
printf '%s\n' "$old_fw_state" > "$TEST_ROOT/old-ufw-state.json"
ufw_finalize_candidate "$TEST_ROOT/old-ufw-state.json" "$new_fw_state"
[[ "$mock_ufw_deleted" == *$'vless\t'"${old_vless_port}"$'/tcp\n'* ]]
[[ "$mock_ufw_deleted" == *$'anytls\t'"${old_anytls_port}"$'/tcp\n'* ]]
mock_ufw_deleted=
ufw_rollback_candidate "$ufw_transaction"
[[ "$mock_ufw_deleted" == *$'vless\t31000/tcp\n'* ]]
[[ "$mock_ufw_deleted" == *$'hy2-hop\t32000:32010/udp\n'* ]]
mock_ufw_numbered_count=2
mock_ufw_number_deletes=
ufw(){
  if [[ "$1" == status && "${2:-}" == numbered ]]; then
    if ((mock_ufw_numbered_count >= 1)); then printf '[ 1] 25001/tcp ALLOW IN Anywhere # vps-net-manager:vless\n'; fi
    if ((mock_ufw_numbered_count >= 2)); then printf '[12] 25005/tcp ALLOW IN Anywhere # vps-net-manager:anytls\n'; fi
  elif [[ "$1" == --force && "$2" == delete ]]; then
    mock_ufw_number_deletes+="${3}"$'\n'
    mock_ufw_numbered_count=$((mock_ufw_numbered_count - 1))
  fi
}
ufw_remove_all_managed
[[ "$mock_ufw_number_deletes" == $'12\n1\n' ]]
unset -f ufw
eval "$saved_ufw_active"
eval "$saved_ufw_allow_exists"
eval "$saved_ufw_add_rule"
eval "$saved_ufw_delete_rule"

# Every edit prompt can return without producing a candidate.
set_case_dir cancel
cp "$CORE_DEFAULT" "$STATE_DIR/sing-box"
cancel_state=$(make_state 1.13.14 "$CORE_DEFAULT")
cancel_state=$(enable_tags "$cancel_state" vless)
printf '%s\n' "$cancel_state" > "$STATE_FILE"
saved_apply_state=$(declare -f apply_state)
apply_calls=0
apply_state(){ apply_calls=$((apply_calls + 1)); }
set_protocol vless name '节点名称：' <<<""
set_uuid_manual vless <<<"0"
set_bool vmess tls <<<"0"
set_number hy2 up_mbps '上行 Mbps：' <<<"0"
set_port vless <<<"0"
set_address <<<"0"
set_certificate <<<"0"
set_tls_domain anytls <<<"0"
certificate_menu <<<"0"
domain_certificate_wizard <<<"0"
set_hy2_hop <<<"0"
set_argo <<<"0"
set_reality_sni <<<"0"
rotate_reality_keys <<<"0"
set_reality_engine <<<"0"
set_xray_fingerprint <<<"0"
set_xray_spider_x <<<"0"
set_xray_time_diff <<<"0"
set_xray_fallback_profile <<<"0"
set_xray_mldsa65 <<<"0"
toggle_protocol vless <<<"0"
update_core <<<"0"
protocol_menu <<< $'1\n6\n0\n0\n0'
if uninstall <<<"0"; then
  echo 'cancelled uninstall unexpectedly succeeded' >&2
  exit 1
fi
[[ $apply_calls -eq 0 ]]

# Reality reads and de-duplicates the independently maintained candidate file,
# ranks stability/latency, and exposes the top ten results.
cp "$ROOT/assets/reality-targets.txt" "$(reality_targets_file)"
[[ "$REALITY_TARGETS_SHA256" == "$(shasum -a 256 "$ROOT/assets/reality-targets.txt" | awk '{print $1}')" ]]
reality_targets=$(reality_target_candidates)
[[ $(wc -l <<<"$reality_targets" | tr -d ' ') -ge 10 ]]
[[ $(head -n1 <<<"$reality_targets") == www.cloudflare.com ]]
[[ $(sort <<<"$reality_targets" | uniq | wc -l | tr -d ' ') -eq $(wc -l <<<"$reality_targets" | tr -d ' ') ]]
reality_targets_are_current
[[ $(reality_targets_status) == '当前渠道默认清单（已校验）' ]]
printf '# local override marker\n' >> "$(reality_targets_file)"
if reality_targets_are_current; then
  echo 'edited Reality target file was incorrectly reported as current' >&2
  exit 1
fi
[[ $(reality_targets_status) == *'本机自定义或旧清单'* ]]
cp "$ROOT/assets/reality-targets.txt" "$(reality_targets_file)"
# Binary/NUL bytes in openssl output stay in a temporary file and never enter a Bash variable.
timeout(){
  printf 'New, TLSv1.3, Cipher is TLS_AES_128_GCM_SHA256\nServer Temp Key: X25519, 253 bits\n'
  printf 'subject=CN = binary.example\0binary\nALPN protocol: h2\nVerify return code: 0 (ok)\n'
}
probe_metadata=$(probe_reality_metadata binary.example)
[[ "$probe_metadata" == $'2\t满足严格推荐条件\t1.3\th2\tX25519\tbinary.example' ]]
unset -f timeout
curl(){ printf '0.013000'; }
[[ $(probe_reality_once fast.example 1) == 13 ]]
unset -f curl
saved_probe_reality_once=$(declare -f probe_reality_once)
saved_probe_reality_metadata=$(declare -f probe_reality_metadata)
probe_reality_once(){
  local host=$1 sample=$2
  case "$host" in
    fast.example) echo "$((20 + sample))";;
    slow.example) echo "$((80 + sample * 2))";;
    flaky.example) [[ $sample -lt 3 ]] && echo "$((30 + sample))" || return 1;;
    *) return 1;;
  esac
}
probe_reality_metadata(){
  case "$1" in
    fast.example|slow.example|flaky.example) printf '2\t满足严格推荐条件\t1.3\th2\tX25519\t%s\n' "$1";;
    *) printf '0\tTLS 1.3 握手失败或证书不受信\t-\t-\t-\t-\n';;
  esac
}
ranked=$(scan_reality_candidates slow.example flaky.example bad.example fast.example)
[[ $(awk -F '\t' 'NR==1{print $1}' <<<"$ranked") == fast.example ]]
[[ $(awk -F '\t' 'NR==2{print $1}' <<<"$ranked") == slow.example ]]
[[ $(awk -F '\t' 'NR==3{print $1}' <<<"$ranked") == flaky.example ]]
[[ $(awk -F '\t' 'NR==4{print $1,$2,$3}' <<<"$ranked") == 'bad.example 0 TLS 1.3 握手失败或证书不受信' ]]
[[ $(awk -F '\t' 'NR==1{print $7,$8,$9,$10}' <<<"$ranked") == '1.3 h2 X25519 fast.example' ]]
eval "$saved_probe_reality_once"
eval "$saved_probe_reality_metadata"

saved_scan_reality_candidates=$(declare -f scan_reality_candidates)
captured_state=
apply_state(){ captured_state=$1; }
scan_reality_candidates(){
  printf 'www.cloudflare.com\t2\t通过严格兼容性与稳定性检测\t3\t42\t6\t1.3\th2\tX25519MLKEM768\twww.cloudflare.com\n'
  printf 'www.microsoft.com\t2\t通过严格兼容性与稳定性检测\t3\t55\t9\t1.3\th2\tX25519\twww.microsoft.com\n'
}
set_reality_sni <<< $'1\n1'
[[ $(jq -r '.protocols.vless.sni' <<<"$captured_state") == www.cloudflare.com ]]
[[ $(jq -r '.protocols.vless.xray.target' <<<"$captured_state") == www.cloudflare.com:443 ]]
[[ $(jq -r '.protocols.vless.xray.server_names[0]' <<<"$captured_state") == www.cloudflare.com ]]
validate_state "$captured_state"
render_config "$captured_state" "$STATE_DIR/reality-candidate.json"
"$STATE_DIR/sing-box" check -c "$STATE_DIR/reality-candidate.json"
eval "$saved_apply_state"
eval "$saved_scan_reality_candidates"

# A populated scan exposes ten selectable ranked rows, including row 10.
saved_scan_reality_candidates=$(declare -f scan_reality_candidates)
saved_apply_state=$(declare -f apply_state)
captured_state=
apply_state(){ captured_state=$1; }
scan_reality_candidates(){
  local n
  for n in {1..10}; do
    printf 'target%s.example\t2\t通过严格兼容性与稳定性检测\t3\t%s\t1\t1.3\th2\tX25519\ttarget%s.example\n' "$n" "$((20 + n))" "$n"
  done
}
set_reality_sni <<< $'1\n10'
[[ $(jq -r '.protocols.vless.sni' <<<"$captured_state") == target10.example ]]
eval "$saved_apply_state"
eval "$saved_scan_reality_candidates"

# Incompatible targets remain visible but cannot be applied by their displayed number.
saved_scan_reality_candidates=$(declare -f scan_reality_candidates)
saved_apply_state=$(declare -f apply_state)
captured_state=
apply_state(){ captured_state=$1; }
scan_reality_candidates(){
  printf 'legacy.example\t0\t未协商 HTTP/2（h2）\t0\t999999\t999999\t1.3\thttp/1.1\tX25519\tlegacy.example\n'
}
set_reality_sni <<< $'1\n1'
[[ -z "$captured_state" ]]
eval "$saved_apply_state"
eval "$saved_scan_reality_candidates"

# The domain-certificate wizard reuses a matching certificate or validates ACME output before adding it to the library.
saved_find_domain_certificate=$(declare -f find_domain_certificate)
saved_store_trusted_domain_certificate=$(declare -f store_trusted_domain_certificate)
saved_acme=$(declare -f acme)
wizard_store=
wizard_acme_calls=0
find_domain_certificate(){
  FOUND_CERT=/tmp/existing-cert.crt
  FOUND_KEY=/tmp/existing-private.key
  return 0
}
store_trusted_domain_certificate(){ wizard_store="${1}"$'\t'"${2}"$'\t'"${3}"; }
domain_certificate_wizard <<<"tls.example.com"
[[ "$wizard_store" == $'tls.example.com\t/tmp/existing-cert.crt\t/tmp/existing-private.key' ]]
wizard_store=
find_domain_certificate(){
  if [[ "${2:-false}" == true && $wizard_acme_calls -eq 1 ]]; then
    FOUND_CERT=/root/ygkkkca/cert.crt
    FOUND_KEY=/root/ygkkkca/private.key
    return 0
  fi
  return 1
}
acme(){ wizard_acme_calls=$((wizard_acme_calls + 1)); }
domain_certificate_wizard <<< $'tls.example.com\n1'
[[ $wizard_acme_calls -eq 1 ]]
[[ "$wizard_store" == $'tls.example.com\t/root/ygkkkca/cert.crt\t/root/ygkkkca/private.key' ]]
eval "$saved_find_domain_certificate"
eval "$saved_store_trusted_domain_certificate"
eval "$saved_acme"

# Different TLS protocols can bind different trusted certificates.
set_case_dir certificate
cp "$CORE_DEFAULT" "$STATE_DIR/sing-box"
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj '/CN=tls.example.com' -addext 'subjectAltName=DNS:tls.example.com' \
  -keyout "$STATE_DIR/trusted.key" -out "$STATE_DIR/trusted.pem" >/dev/null 2>&1
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj '/CN=hy2.example.com' -addext 'subjectAltName=DNS:hy2.example.com' \
  -keyout "$STATE_DIR/hy2.key" -out "$STATE_DIR/hy2.pem" >/dev/null 2>&1
cert_state=$(jq --arg cert "$STATE_DIR/trusted.pem" --arg key "$STATE_DIR/trusted.key" \
  --arg hy2cert "$STATE_DIR/hy2.pem" --arg hy2key "$STATE_DIR/hy2.key" '
   .public_address="node.example.com" |
   .certificates.tls={name:"tls.example.com",cert:$cert,key:$key,mode:"trusted",insecure:false,
     source:{type:"snapshot",auto_sync:false}} |
   .certificates.hy2={name:"hy2.example.com",cert:$hy2cert,key:$hy2key,mode:"trusted",insecure:false,
     source:{type:"snapshot",auto_sync:false}} |
   .protocols.vmess.domain="tls.example.com" | .protocols.vmess.certificate_id="tls" |
   .protocols.hy2.domain="hy2.example.com" | .protocols.hy2.certificate_id="hy2" |
   .protocols.anytls.domain="tls.example.com" | .protocols.anytls.certificate_id="tls"
  ' <<<"$four_state")
printf '%s\n' "$cert_state" > "$STATE_FILE"
saved_certificate_matches_domain=$(declare -f certificate_matches_domain)
certificate_matches_domain(){
  [[ "$1:$2" == "$STATE_DIR/trusted.pem:tls.example.com" ||
     "$1:$2" == "$STATE_DIR/hy2.pem:hy2.example.com" ]]
}
validate_state "$cert_state"
render_config "$cert_state" "$CONFIG_FILE"
[[ $(jq -r '.inbounds[] | select(.tag=="hy2-sb") | .tls.certificate_path' "$CONFIG_FILE") == "$STATE_DIR/hy2.pem" ]]
[[ $(jq -r '.inbounds[] | select(.tag=="anytls-sb") | .tls.certificate_path' "$CONFIG_FILE") == "$STATE_DIR/trusted.pem" ]]
generate_subscriptions
trusted_anytls_link=$(grep '^anytls://' "$STATE_DIR/subscription.txt")
[[ "$trusted_anytls_link" != *'insecure='* && "$trusted_anytls_link" != *'pcs='* ]]
jq -e '
  .outbounds[] | select(.type=="anytls") |
  .tls.insecure == false and (.tls | has("certificate_public_key_sha256") | not)
' "$STATE_DIR/sing-box-client.json" >/dev/null
jq -e '[.proxies[] | select(.type=="hysteria2" or .type=="anytls") | ."skip-cert-verify"] | all(. == false)' "$STATE_DIR/mihomo.yaml" >/dev/null

# Certificate selection can atomically switch both certificate and SNI for any TLS protocol.
saved_apply_state=$(declare -f apply_state)
captured_state=
apply_state(){ captured_state=$1; }
hy2_cert_index=$(jq '.certificates | to_entries | map(.key) | index("hy2") + 1' "$STATE_FILE")
select_certificate_for_protocol anytls <<< "${hy2_cert_index}"$'\nhy2.example.com'
jq -e '.protocols.anytls.certificate_id=="hy2" and .protocols.anytls.domain=="hy2.example.com"' <<<"$captured_state" >/dev/null

# Editing a normal TLS domain must select a valid SAN-matching library entry;
# an unknown domain must leave the current protocol state untouched.
captured_state=
set_tls_domain anytls <<< $'hy2.example.com\n1'
jq -e '.protocols.anytls.certificate_id=="hy2" and .protocols.anytls.domain=="hy2.example.com"' <<<"$captured_state" >/dev/null
captured_state=
set_tls_domain anytls <<< 'missing.example.com'
[[ -z "$captured_state" ]]
eval "$saved_apply_state"

# Adding a validated domain certificate keeps protocol bindings unchanged.
single_tls_state=$(jq '
  .protocols.vless.enabled=false | .protocols.vmess.enabled=false |
  .protocols.hy2.enabled=false |
  .protocols.anytls.enabled=true | .protocols.anytls.domain="www.bing.com" |
  .protocols.anytls.certificate_id="default"
' <<<"$cert_state")
printf '%s\n' "$single_tls_state" > "$STATE_FILE"
saved_commit_certificate_library_state=$(declare -f commit_certificate_library_state)
bound_candidate_file="$STATE_DIR/bound-candidate.json"
commit_certificate_library_state(){ cat > "$bound_candidate_file"; }
store_trusted_domain_certificate tls.example.com "$STATE_DIR/trusted.pem" "$STATE_DIR/trusted.key"
jq -e '
  .protocols.anytls.domain == "www.bing.com" and
  .protocols.anytls.certificate_id == "default" and
  (.certificates | to_entries[] | select(.value.name=="tls.example.com" and .value.mode=="trusted" and .value.insecure==false))
' "$bound_candidate_file" >/dev/null
managed_cert=$(jq -r '.certificates | to_entries[] | select(.value.name=="tls.example.com") | .value.cert' "$bound_candidate_file")
managed_key=$(jq -r '.certificates | to_entries[] | select(.value.name=="tls.example.com") | .value.key' "$bound_candidate_file")
[[ -r "$managed_cert" && -r "$managed_key" && "$managed_cert" == "$STATE_DIR"/certificates/*/cert.pem ]]
eval "$saved_commit_certificate_library_state"
eval "$saved_certificate_matches_domain"

# Managed certificate synchronization validates the renewed source before an
# atomic replacement, then regenerates subscriptions and restarts the core.
set_case_dir certificate-sync
cp "$CORE_DEFAULT" "$STATE_DIR/sing-box"
saved_sync_certificate_matches_domain=$(declare -f certificate_matches_domain)
certificate_matches_domain(){ return 0; }
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj '/CN=sync.example.com' -addext 'subjectAltName=DNS:sync.example.com' \
  -keyout "$STATE_DIR/source-old.key" -out "$STATE_DIR/source-old.pem" >/dev/null 2>&1
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj '/CN=sync.example.com' -addext 'subjectAltName=DNS:sync.example.com' \
  -keyout "$STATE_DIR/source-new.key" -out "$STATE_DIR/source-new.pem" >/dev/null 2>&1
install -d -m 700 "$STATE_DIR/certificates/sync"
install -m 644 "$STATE_DIR/source-old.pem" "$STATE_DIR/certificates/sync/cert.pem"
install -m 600 "$STATE_DIR/source-old.key" "$STATE_DIR/certificates/sync/private.key"
sync_cert_state=$(make_state 1.13.14 "$CORE_DEFAULT")
sync_cert_state=$(jq --arg cert "$STATE_DIR/certificates/sync/cert.pem" --arg key "$STATE_DIR/certificates/sync/private.key" \
  --arg source_cert "$STATE_DIR/source-new.pem" --arg source_key "$STATE_DIR/source-new.key" '
  .public_address="node.example.com" |
  .protocols.vless.enabled=false | .protocols.vmess.enabled=false | .protocols.hy2.enabled=false |
  .protocols.anytls.enabled=true | .protocols.anytls.port=28123 |
  .protocols.anytls.domain="sync.example.com" | .protocols.anytls.certificate_id="sync" |
  .certificates.sync={name:"sync.example.com",cert:$cert,key:$key,mode:"trusted",insecure:false,
    source:{type:"files",cert:$source_cert,key:$source_key,auto_sync:true}}
' <<<"$sync_cert_state")
printf '%s\n' "$sync_cert_state" > "$STATE_FILE"
old_sync_der=$(certificate_der_sha256 "$STATE_DIR/certificates/sync/cert.pem")
new_sync_der=$(certificate_der_sha256 "$STATE_DIR/source-new.pem")
saved_reconcile_core_services=$(declare -f reconcile_core_services)
sync_restart_calls=0
reconcile_core_services(){ sync_restart_calls=$((sync_restart_calls + 1)); }
sync_managed_certificates true
[[ $(certificate_der_sha256 "$STATE_DIR/certificates/sync/cert.pem") == "$new_sync_der" && "$old_sync_der" != "$new_sync_der" ]]
[[ $sync_restart_calls -eq 1 ]]
eval "$saved_reconcile_core_services"
eval "$saved_sync_certificate_matches_domain"

# Subscription/config output follows runtime disable.
set_case_dir sync
cp "$CORE_DEFAULT" "$STATE_DIR/sing-box"
sync_state=$(jq '.protocols.vmess.enabled=false | .protocols.hy2.enabled=false | .protocols.anytls.enabled=false' <<<"$four_state")
printf '%s\n' "$sync_state" > "$STATE_FILE"
render_config "$sync_state" "$CONFIG_FILE"
generate_subscriptions
[[ $(jq '.inbounds|length' "$CONFIG_FILE") -eq 1 ]]
[[ $(wc -l < "$STATE_DIR/subscription.txt" | tr -d ' ') -eq 1 ]]

# Vless-Reality can move to Xray while the remaining protocols stay on Sing-box.
set_case_dir dual-xray
cp "$CORE_DEFAULT" "$STATE_DIR/sing-box"
cp "$XRAY_CORE" "$STATE_DIR/xray"
printf '%s\n' "$XRAY_DEFAULT" > "$STATE_DIR/xray-version"
printf '%s\n' "$XRAY_ARM64_SHA256" > "$STATE_DIR/xray-archive.sha256"
saved_cpu=$(declare -f cpu)
cpu(){ echo arm64; }
xray_install_valid
printf 'bad\n' > "$STATE_DIR/xray-archive.sha256"
! xray_install_valid
printf '%s\n' "$XRAY_ARM64_SHA256" > "$STATE_DIR/xray-archive.sha256"
eval "$saved_cpu"
dual_state=$(make_state 1.13.14 "$CORE_DEFAULT")
dual_state=$(enable_tags "$dual_state" vless anytls)
dual_state=$(jq '
  .public_address="node.example.com" |
  .protocols.vless.engine="xray" |
  .protocols.vless.xray.fingerprint="firefox" |
  .protocols.vless.xray.spider_x="/generated" |
  .protocols.vless.xray.fallback_profile="balanced"
' <<<"$dual_state")
printf '%s\n' "$dual_state" > "$STATE_FILE"
validate_state "$dual_state"
render_config "$dual_state" "$CONFIG_FILE"
render_xray_config "$dual_state" "$XRAY_CONFIG_FILE"
"$STATE_DIR/sing-box" check -c "$CONFIG_FILE"
"$STATE_DIR/xray" run -test -c "$XRAY_CONFIG_FILE"
[[ $(jq '.inbounds|length' "$CONFIG_FILE") -eq 1 ]]
[[ $(jq -r '.inbounds[0].tag' "$CONFIG_FILE") == anytls-sb ]]
[[ $(jq '.inbounds|length' "$XRAY_CONFIG_FILE") -eq 1 ]]
[[ $(jq -r '.inbounds[0].tag' "$XRAY_CONFIG_FILE") == vless-xray ]]
[[ $(jq -r '.inbounds[0].streamSettings.realitySettings.limitFallbackUpload.bytesPerSec' "$XRAY_CONFIG_FILE") -eq 1048576 ]]
generate_subscriptions
dual_vless_link=$(grep '^vless://' "$STATE_DIR/subscription.txt")
[[ "$dual_vless_link" == *'fp=firefox'*'spx=%2Fgenerated'* ]]
"$STATE_DIR/sing-box" check -c "$STATE_DIR/sing-box-client.json"

# ML-DSA-65 values are generated by Xray and never typed by the user.
saved_apply_state=$(declare -f apply_state)
saved_xray_mldsa_target_compatible=$(declare -f xray_mldsa_target_compatible)
captured_state=
apply_state(){ captured_state=$1; }
xray_mldsa_target_compatible(){ return 0; }
set_xray_mldsa65 <<<"1"
[[ $(jq -r '.protocols.vless.xray.mldsa65_seed|length>20' <<<"$captured_state") == true ]]
[[ $(jq -r '.protocols.vless.xray.mldsa65_verify|length>100' <<<"$captured_state") == true ]]
eval "$saved_apply_state"
eval "$saved_xray_mldsa_target_compatible"

# A failed restart restores both state and server config.
set_case_dir rollback
cp "$CORE_DEFAULT" "$STATE_DIR/sing-box"
old=$(make_state 1.13.14 "$CORE_DEFAULT")
old=$(enable_tags "$old" vless)
old=$(jq '.public_address="old.example.com"' <<<"$old")
printf '%s\n' "$old" > "$STATE_FILE"
render_config "$old" "$CONFIG_FILE"
render_xray_config "$old" "$XRAY_CONFIG_FILE"
cp "$CONFIG_FILE" "$STATE_DIR/original.json"
cp "$XRAY_CONFIG_FILE" "$STATE_DIR/original-xray.json"
write_services(){ :; }
reconcile_core_services(){ return 1; }
generate_subscriptions(){ :; }
reconcile_hy2_hop(){ :; }
reconcile_argo(){ :; }
candidate=$(jq '.public_address="new.example.com"' <<<"$old")
! commit_state <<<"$candidate"
[[ $(jq -r '.public_address' "$STATE_FILE") == old.example.com ]]
cmp "$CONFIG_FILE" "$STATE_DIR/original.json"
cmp "$XRAY_CONFIG_FILE" "$STATE_DIR/original-xray.json"

echo "all selectable-protocol tests passed"
