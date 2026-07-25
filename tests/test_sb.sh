#!/usr/bin/env bash
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
CORE_DEFAULT=${SB_TEST_CORE_DEFAULT:-}
CORE_110=${SB_TEST_CORE_110:-}
[[ -x "$CORE_DEFAULT" && -x "$CORE_110" ]] || {
  echo "set SB_TEST_CORE_DEFAULT and SB_TEST_CORE_110 to executable test cores" >&2
  exit 77
}

TEST_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/sbyg-tests.XXXXXX")
export SBYG_LIB_ONLY=1
export SBYG_STATE_DIR="$TEST_ROOT/initial"
source "$ROOT/sb.sh"
trap 'cleanup_tmp; rm -rf "$TEST_ROOT"' EXIT

set_case_dir(){
  STATE_DIR="$TEST_ROOT/$1"
  STATE_FILE="$STATE_DIR/protocols.json"
  CONFIG_FILE="$STATE_DIR/sb.json"
  mkdir -p "$STATE_DIR"
}

make_state(){
  local version=$1 core=$2
  cp "$core" "$STATE_DIR/sing-box"
  new_state "$version"
}

enable_tags(){
  local state=$1 tag port=25000
  shift
  for tag in "$@"; do
    port=$((port + 1))
    state=$(jq --arg tag "$tag" --argjson port "$port" '.protocols[$tag].enabled=true | .protocols[$tag].port=$port' <<<"$state")
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
selected=$(select_protocols 1.13.14 <<<"1,3,5")
eval "$saved_port_available"
jq -e '[.protocols|to_entries[]|select(.value.enabled)|.key] == ["vless","hy2","anytls"]' <<<"$selected" >/dev/null
set_case_dir selection-cancel
cp "$CORE_DEFAULT" "$STATE_DIR/sing-box"
if select_protocols 1.13.14 <<<"0" >/dev/null; then
  echo 'protocol selection cancellation unexpectedly succeeded' >&2
  exit 1
fi
[[ ! -e "$STATE_DIR/cert.pem" && ! -e "$STATE_DIR/private.key" ]]

check_case single 1.13.14 "$CORE_DEFAULT" 1 vless
check_case multi 1.13.14 "$CORE_DEFAULT" 3 vless hy2 anytls
check_case five 1.13.14 "$CORE_DEFAULT" 5 vless vmess hy2 tuic anytls
check_case legacy 1.10.7 "$CORE_110" 4 vless vmess hy2 tuic

# 1.10 never accepts AnyTLS.
legacy_state=$(cat "$TEST_ROOT/legacy/protocols.json")
legacy_invalid=$(jq '.protocols.anytls.enabled=true | .protocols.anytls.port=26000' <<<"$legacy_state")
! validate_state "$legacy_invalid"

# Duplicate/invalid ports and a malformed WS path are rejected.
five_state=$(cat "$TEST_ROOT/five/protocols.json")
duplicate=$(jq '.protocols.vmess.port=.protocols.vless.port' <<<"$five_state")
! validate_state "$duplicate"
bad_path=$(jq '.protocols.vmess.path="missing-slash"' <<<"$five_state")
! validate_state "$bad_path"
bad_port=$(jq '.protocols.hy2.port=70000' <<<"$five_state")
! validate_state "$bad_port"
ss(){ printf 'udp UNCONN 0 0 [::]:28000 [::]:*\n'; }
! port_available 28000
port_available 28001
unset -f ss
valid_host 203.0.113.10
valid_host 2001:db8::10
valid_host node.example.com
! valid_host 999.1.1.1
! valid_host 'bad host!'

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
set_uuid vless <<<"0"
set_bool vmess tls <<<"0"
set_number hy2 up_mbps '上行 Mbps：' <<<"0"
set_port vless <<<"0"
set_address <<<"0"
set_certificate <<<"0"
set_hy2_hop <<<"0"
set_argo <<<"0"
set_reality_sni <<<"0"
rotate_reality_keys <<<"0"
toggle_protocol vless <<<"0"
update_core <<<"0"
protocol_menu <<< $'1\n5\n0\n0\n0'
[[ $apply_calls -eq 0 ]]

# Reality exposes the first five 3x-ui v3.4.2 defaults and validates before apply.
saved_probe_reality_sni=$(declare -f probe_reality_sni)
captured_state=
apply_state(){ captured_state=$1; }
probe_reality_sni(){ [[ "$1" == www.cloudflare.com ]]; }
set_reality_sni <<<"1"
[[ $(jq -r '.protocols.vless.sni' <<<"$captured_state") == www.cloudflare.com ]]
validate_state "$captured_state"
render_config "$captured_state" "$STATE_DIR/reality-candidate.json"
"$STATE_DIR/sing-box" check -c "$STATE_DIR/reality-candidate.json"
eval "$saved_apply_state"
eval "$saved_probe_reality_sni"

# Subscription/config output follows runtime disable.
set_case_dir sync
cp "$CORE_DEFAULT" "$STATE_DIR/sing-box"
sync_state=$(jq '.protocols.vmess.enabled=false | .protocols.hy2.enabled=false | .protocols.tuic.enabled=false | .protocols.anytls.enabled=false' <<<"$five_state")
printf '%s\n' "$sync_state" > "$STATE_FILE"
render_config "$sync_state" "$CONFIG_FILE"
generate_subscriptions
[[ $(jq '.inbounds|length' "$CONFIG_FILE") -eq 1 ]]
[[ $(wc -l < "$STATE_DIR/subscription.txt" | tr -d ' ') -eq 1 ]]

# A failed restart restores both state and server config.
set_case_dir rollback
cp "$CORE_DEFAULT" "$STATE_DIR/sing-box"
old=$(make_state 1.13.14 "$CORE_DEFAULT")
old=$(enable_tags "$old" vless)
old=$(jq '.public_address="old.example.com"' <<<"$old")
printf '%s\n' "$old" > "$STATE_FILE"
render_config "$old" "$CONFIG_FILE"
cp "$CONFIG_FILE" "$STATE_DIR/original.json"
write_service(){ :; }
restart_service(){ return 1; }
generate_subscriptions(){ :; }
reconcile_hy2_hop(){ :; }
reconcile_argo(){ :; }
candidate=$(jq '.public_address="new.example.com"' <<<"$old")
! commit_state <<<"$candidate"
[[ $(jq -r '.public_address' "$STATE_FILE") == old.example.com ]]
cmp "$CONFIG_FILE" "$STATE_DIR/original.json"

echo "all selectable-protocol tests passed"
