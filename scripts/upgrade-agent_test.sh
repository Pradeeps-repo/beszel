#!/usr/bin/env bash
# Unit checks for the pure helpers in upgrade-agent.sh. No root and no network.
set -euo pipefail

root="$(cd "$(dirname "$0")" && pwd)"
UPGRADE_AGENT_LIB_ONLY=1
# shellcheck disable=SC1091
source "${root}/upgrade-agent.sh"

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

[ "$(asset_for_machine x86_64)" = "beszel-agent_linux_amd64" ] || fail "amd64"
[ "$(asset_for_machine aarch64)" = "beszel-agent_linux_arm64" ] || fail "arm64"
[ "$(asset_for_machine armv7l)" = "beszel-agent_linux_armv7" ] || fail "armv7"
if asset_for_machine armv6l >/dev/null 2>&1; then
  fail "armv6 should be rejected"
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
printf 'hello\n' >"${tmp}/file"
sha256sum "${tmp}/file" >"${tmp}/file.sha256"
hash="$(expected_sha256 "${tmp}/file.sha256")"
verify_sha256 "${tmp}/file" "$hash" || fail "checksum"
printf '%s\n' "$hash" >"${tmp}/bare.sha256"
[ "$(expected_sha256 "${tmp}/bare.sha256")" = "$hash" ] || fail "bare hash"
printf '00\n' >"${tmp}/bad"
verify_sha256 "${tmp}/file" 00 && fail "bad checksum accepted"

sample='VCP code 0x10 (Brightness): current value =    25, max value =   200'
[ "$(brightness_percent "$sample")" = "13" ] || fail "percent 25/200"
zero='current value =    0, max value =   100'
[ "$(brightness_percent "$zero")" = "0" ] || fail "zero is a real reading"
if brightness_percent 'current value = 50' >/dev/null 2>&1; then
  fail "missing max should not invent 100"
fi

[ "$(supplementary_groups_value '')" = "i2c" ] || fail "empty groups"
[ "$(supplementary_groups_value 'systemd-journal')" = "systemd-journal i2c" ] || fail "keep journal"
[ "$(supplementary_groups_value 'systemd-journal i2c')" = "systemd-journal i2c" ] || fail "i2c already present"

unit='# /lib/systemd/system/beszel-agent.service
[Service]
User=beszel
SupplementaryGroups=systemd-journal
# /etc/systemd/system/beszel-agent.service.d/10-i2c.conf
[Service]
SupplementaryGroups=i2c'
[ "$(printf '%s\n' "$unit" | groups_from_unit_text)" = "systemd-journal" ] || fail "ignore own drop-in"

printf 'ok\n'
