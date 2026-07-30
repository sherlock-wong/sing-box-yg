#!/usr/bin/env bash
set -euo pipefail

repo='sherlock-wong/vps-net-manager'
state_dir='/etc/vps-net-manager'
target='/usr/local/bin/vm'

fail() {
  printf 'vm 安装失败：%s\n' "$*" >&2
  exit 1
}

require_root() {
  [ "${EUID:-$(id -u)}" -eq 0 ] || fail '请以 root 运行安装命令。'
}

require_host() {
  [ -r /etc/os-release ] || fail '无法读取 /etc/os-release。'
  . /etc/os-release
  [ "${ID:-}" = debian ] || [ "${ID:-}" = ubuntu ] || fail '仅支持 Debian 或 Ubuntu。'
  command -v systemctl >/dev/null 2>&1 || fail '需要 systemd。'
  [ -d /run/systemd/system ] || fail '当前环境未运行 systemd。'
}

architecture() {
  case "$(uname -m)" in
    x86_64|amd64) printf 'amd64\n' ;;
    aarch64|arm64) printf 'arm64\n' ;;
    *) fail '仅支持 amd64 或 arm64。' ;;
  esac
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    fail '缺少 SHA-256 校验工具。'
  fi
}

reject_legacy_install() {
  [ ! -e "$state_dir/protocols.json" ] || fail '检测到旧版 Bash 状态；请先在旧 vpnm 菜单执行卸载。'
  if [ -x /usr/local/bin/vpnm ] && head -n 1 /usr/local/bin/vpnm 2>/dev/null | grep -Eq '^#!.*(ba)?sh'; then
    fail '检测到旧版 Bash vpnm；请先在旧 vpnm 菜单执行卸载。'
  fi
}

resolve_build_commit() {
  curl -fsSL --proto '=https' --tlsv1.2 "https://api.github.com/repos/$repo/git/ref/heads/main-build" \
    | sed -nE 's/.*"sha"[[:space:]]*:[[:space:]]*"([0-9a-f]{40})".*/\1/p' \
    | head -n 1
}

download() {
  curl -fsSL --proto '=https' --tlsv1.2 --retry 2 -o "$2" "$1"
}

main() {
  require_root
  require_host
  reject_legacy_install
  command -v curl >/dev/null 2>&1 || fail '缺少 curl。'
  arch=$(architecture)
  commit=$(resolve_build_commit)
  [ "${#commit}" -eq 40 ] || fail '无法解析 main-build 的不可变提交。'
  temp=$(mktemp -d)
  trap 'rm -rf "$temp"' EXIT
  base="https://raw.githubusercontent.com/$repo/$commit"
  binary="vpnm-linux-$arch"
  download "$base/manifest.json" "$temp/manifest.json" || fail '下载 manifest 失败。'
  download "$base/checksums.txt" "$temp/checksums.txt" || fail '下载 checksums 失败。'
  expected=$(awk -v name="$binary" '$2==name {print $1}' "$temp/checksums.txt")
  [ "${#expected}" -eq 64 ] || fail 'checksums 中缺少当前架构二进制。'
  download "$base/$binary" "$temp/$binary" || fail '下载 vm 二进制失败。'
  actual=$(sha256_file "$temp/$binary")
  [ "$actual" = "$expected" ] || fail 'vm 二进制 SHA-256 校验失败。'
  mkdir -p /usr/local/bin
  staged=$(mktemp /usr/local/bin/.vm.XXXXXX)
  chmod 0755 "$temp/$binary"
  cp "$temp/$binary" "$staged"
  chmod 0755 "$staged"
  mv -f "$staged" "$target"
  printf '已安装 main 成功构建（%s）。\n' "$commit"
  exec "$target" install
}

main "$@"
