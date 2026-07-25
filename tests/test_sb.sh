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

# Existing systemd services must always restart after a validated config replacement.
systemctl_calls=
systemctl(){ systemctl_calls+="${*}"$'\n'; }
systemd_enable_restart sing-box.service
[[ "$systemctl_calls" == $'enable sing-box.service\nrestart sing-box.service\n' ]]
unset -f systemctl

# Missing/failing optional Argo state must not prevent stopping the main service or vice versa.
saved_systemd_stop_disable=$(declare -f systemd_stop_disable)
stopped_units=
systemctl(){ :; }
systemd_stop_disable(){ stopped_units+="${1}"$'\n'; [[ "$1" != sing-box.service ]]; }
stop_systemd_services
[[ "$stopped_units" == $'sing-box.service\nsing-box-argo.service\n' ]]
eval "$saved_systemd_stop_disable"
unset -f systemctl

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
selected=$(select_protocols 1.13.14 <<<"1,3,4")
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
check_case four 1.13.14 "$CORE_DEFAULT" 4 vless vmess hy2 anytls
check_case legacy 1.10.7 "$CORE_110" 3 vless vmess hy2

# New self-signed certificates are leaf certificates with SAN and both client-specific pins.
four_state=$(cat "$TEST_ROOT/four/protocols.json")
[[ $(jq -r '.certificates.default.mode' <<<"$four_state") == pinned ]]
four_cert=$(jq -r '.certificates.default.cert' <<<"$four_state")
four_cert_text=$(openssl x509 -in "$four_cert" -noout -text)
grep -q 'DNS:www.bing.com' <<<"$four_cert_text"
grep -q 'CA:FALSE' <<<"$four_cert_text"
expected_der_pin=$(certificate_der_sha256 "$four_cert")
expected_spki_pin=$(certificate_spki_sha256 "$four_cert")
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

# Schema 1 states migrate the global certificate into the library and remove TUIC completely.
schema1_state=$(jq '
  .schema=1 | .certificate=.certificates.default | del(.certificates) |
  .protocols.tuic={enabled:true,name:"tuic-v5",port:29999,uuid:.protocols.vless.uuid,password:"legacy",domain:"www.bing.com"}
' <<<"$four_state")
migrated_state=$(normalize_state <<<"$schema1_state")
jq -e '
  .schema==2 and (.protocols|has("tuic")|not) and
  .protocols.vmess.certificate_id=="default" and
  .protocols.hy2.certificate_id=="default" and
  .protocols.anytls.certificate_id=="default" and
  .certificates.default.cert != null
' <<<"$migrated_state" >/dev/null
validate_state "$migrated_state"
ss(){ printf 'udp UNCONN 0 0 [::]:28000 [::]:*\n'; }
! port_available 28000
port_available 28001
unset -f ss
valid_host 203.0.113.10
valid_host 2001:db8::10
valid_host node.example.com
! valid_host 999.1.1.1
! valid_host 'bad host!'

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
    if ((mock_ufw_numbered_count >= 1)); then printf '[ 1] 25001/tcp ALLOW IN Anywhere # sing-box-yg:vless\n'; fi
    if ((mock_ufw_numbered_count >= 2)); then printf '[12] 25005/tcp ALLOW IN Anywhere # sing-box-yg:anytls\n'; fi
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
set_uuid vless <<<"0"
set_bool vmess tls <<<"0"
set_number hy2 up_mbps '上行 Mbps：' <<<"0"
set_port vless <<<"0"
set_address <<<"0"
set_certificate <<<"0"
set_tls_domain anytls <<<"0"
certificate_menu <<<"0"
domain_certificate_wizard anytls <<<"0"
domain_certificate_target_menu <<<"0"
set_hy2_hop <<<"0"
set_argo <<<"0"
set_reality_sni <<<"0"
rotate_reality_keys <<<"0"
toggle_protocol vless <<<"0"
update_core <<<"0"
protocol_menu <<< $'1\n5\n0\n0\n0'
if uninstall <<<"0"; then
  echo 'cancelled uninstall unexpectedly succeeded' >&2
  exit 1
fi
[[ $apply_calls -eq 0 ]]

# Reality scans all ten 3x-ui v3.4.2 defaults, ranks stability/latency, and applies a selected top-five result.
[[ ${#REALITY_CANDIDATES[@]} -eq 10 ]]
[[ ${REALITY_CANDIDATES[0]} == www.cloudflare.com && ${REALITY_CANDIDATES[9]} == dl.google.com ]]
# Binary/NUL bytes in openssl output stay in a temporary file and never enter a Bash variable.
timeout(){ printf 'Verify return code: 0 (ok)\0binary\nALPN protocol: h2\n'; }
probe_latency=$(probe_reality_once binary.example 1)
[[ "$probe_latency" =~ ^[0-9]+$ ]]
unset -f timeout
saved_probe_reality_once=$(declare -f probe_reality_once)
probe_reality_once(){
  local host=$1 sample=$2
  case "$host" in
    fast.example) echo "$((20 + sample))";;
    slow.example) echo "$((80 + sample * 2))";;
    flaky.example) [[ $sample -lt 3 ]] && echo "$((30 + sample))" || return 1;;
    *) return 1;;
  esac
}
ranked=$(scan_reality_candidates slow.example flaky.example bad.example fast.example)
[[ $(awk -F '\t' 'NR==1{print $1}' <<<"$ranked") == fast.example ]]
[[ $(awk -F '\t' 'NR==2{print $1}' <<<"$ranked") == slow.example ]]
[[ $(awk -F '\t' 'NR==3{print $1}' <<<"$ranked") == flaky.example ]]
eval "$saved_probe_reality_once"

saved_scan_reality_candidates=$(declare -f scan_reality_candidates)
captured_state=
apply_state(){ captured_state=$1; }
scan_reality_candidates(){ printf 'www.cloudflare.com\t3\t42\t6\nwww.microsoft.com\t3\t55\t9\n'; }
set_reality_sni <<< $'1\n1'
[[ $(jq -r '.protocols.vless.sni' <<<"$captured_state") == www.cloudflare.com ]]
validate_state "$captured_state"
render_config "$captured_state" "$STATE_DIR/reality-candidate.json"
"$STATE_DIR/sing-box" check -c "$STATE_DIR/reality-candidate.json"
eval "$saved_apply_state"
eval "$saved_scan_reality_candidates"

# The domain-certificate wizard reuses a matching certificate or validates ACME output before binding.
saved_find_domain_certificate=$(declare -f find_domain_certificate)
saved_bind_trusted_domain_certificate=$(declare -f bind_trusted_domain_certificate)
saved_acme=$(declare -f acme)
wizard_bind=
wizard_acme_calls=0
find_domain_certificate(){
  FOUND_CERT=/tmp/existing-cert.crt
  FOUND_KEY=/tmp/existing-private.key
  return 0
}
bind_trusted_domain_certificate(){ wizard_bind="${1}"$'\t'"${2}"$'\t'"${3}"$'\t'"${4}"; }
domain_certificate_wizard anytls <<<"tls.example.com"
[[ "$wizard_bind" == $'anytls\ttls.example.com\t/tmp/existing-cert.crt\t/tmp/existing-private.key' ]]
wizard_bind=
find_domain_certificate(){
  if [[ "${2:-false}" == true && $wizard_acme_calls -eq 1 ]]; then
    FOUND_CERT=/root/ygkkkca/cert.crt
    FOUND_KEY=/root/ygkkkca/private.key
    return 0
  fi
  return 1
}
acme(){ wizard_acme_calls=$((wizard_acme_calls + 1)); }
domain_certificate_wizard anytls <<< $'tls.example.com\n1'
[[ $wizard_acme_calls -eq 1 ]]
[[ "$wizard_bind" == $'anytls\ttls.example.com\t/root/ygkkkca/cert.crt\t/root/ygkkkca/private.key' ]]
eval "$saved_find_domain_certificate"
eval "$saved_bind_trusted_domain_certificate"
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
   .certificates.tls={name:"tls.example.com",cert:$cert,key:$key,mode:"trusted",insecure:false} |
   .certificates.hy2={name:"hy2.example.com",cert:$hy2cert,key:$hy2key,mode:"trusted",insecure:false} |
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
select_certificate_for_protocol <<< $'3\n'"${hy2_cert_index}"$'\nhy2.example.com'
jq -e '.protocols.anytls.certificate_id=="hy2" and .protocols.anytls.domain=="hy2.example.com"' <<<"$captured_state" >/dev/null
eval "$saved_apply_state"

# Binding a validated domain certificate passes one atomic candidate to commit_state.
single_tls_state=$(jq '
  .protocols.vless.enabled=false | .protocols.vmess.enabled=false |
  .protocols.hy2.enabled=false |
  .protocols.anytls.enabled=true | .protocols.anytls.domain="www.bing.com" |
  .protocols.anytls.certificate_id="default"
' <<<"$cert_state")
printf '%s\n' "$single_tls_state" > "$STATE_FILE"
saved_commit_state=$(declare -f commit_state)
bound_candidate_file="$STATE_DIR/bound-candidate.json"
commit_state(){ cat > "$bound_candidate_file"; }
bind_trusted_domain_certificate anytls tls.example.com "$STATE_DIR/trusted.pem" "$STATE_DIR/trusted.key"
jq -e '
  .protocols.anytls.domain == "tls.example.com" and
  (.protocols.anytls.certificate_id | startswith("cert_")) and
  (.certificates[.protocols.anytls.certificate_id] |
    .name=="tls.example.com" and .mode=="trusted" and .insecure==false)
' "$bound_candidate_file" >/dev/null
managed_cert=$(jq -r '.certificates[.protocols.anytls.certificate_id].cert' "$bound_candidate_file")
managed_key=$(jq -r '.certificates[.protocols.anytls.certificate_id].key' "$bound_candidate_file")
[[ -r "$managed_cert" && -r "$managed_key" && "$managed_cert" == "$STATE_DIR"/certificates/*/cert.pem ]]
eval "$saved_commit_state"
eval "$saved_certificate_matches_domain"

# Subscription/config output follows runtime disable.
set_case_dir sync
cp "$CORE_DEFAULT" "$STATE_DIR/sing-box"
sync_state=$(jq '.protocols.vmess.enabled=false | .protocols.hy2.enabled=false | .protocols.anytls.enabled=false' <<<"$four_state")
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
