#!/usr/bin/env bash
# Upgrade a systemd beszel-agent installed by upstream install-agent.sh.
# Safe to run unattended: the live binary is replaced only after sha256
# verification, the previous binary is kept as beszel-agent.bak, and a
# failed restart restores that backup.
#
# Usage (as root):
#   scripts/upgrade-agent.sh
set -euo pipefail

RELEASE_BASE="${BESZEL_AGENT_RELEASE_BASE:-https://github.com/Pradeeps-repo/beszel/releases/download/agent-latest}"
BIN_PATH="${BESZEL_AGENT_BIN:-/opt/beszel-agent/beszel-agent}"
SERVICE="${BESZEL_AGENT_SERVICE:-beszel-agent.service}"
LOCK_FILE="${BESZEL_AGENT_LOCK:-/var/lock/beszel-upgrade-agent.lock}"
HELPER_PATH="${BESZEL_DISPLAY_HELPER:-/opt/beszel-agent/bright-watch-display.sh}"
NEED_RESTART=0
NEED_DAEMON_RELOAD=0

log() {
  printf '%s\n' "$*"
}

die() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

asset_for_machine() {
  local machine="${1:-$(uname -m)}"
  case "$machine" in
    x86_64 | amd64) printf '%s\n' beszel-agent_linux_amd64 ;;
    aarch64 | arm64) printf '%s\n' beszel-agent_linux_arm64 ;;
    armv7l | armv7 | armhf) printf '%s\n' beszel-agent_linux_armv7 ;;
    *)
      printf 'unsupported architecture: %s (published assets: amd64, arm64, armv7)\n' "$machine" >&2
      return 1
      ;;
  esac
}

# sha256sum files look like "<hash>  <name>". A bare hash is also accepted.
expected_sha256() {
  local file="$1"
  awk 'NF { print $1; exit }' "$file"
}

verify_sha256() {
  local file="$1" expected="$2" actual
  [ -n "$expected" ] || return 1
  actual="$(sha256sum "$file" | awk '{ print $1 }')"
  [ "$actual" = "$expected" ]
}

# Percent from `ddcutil getvcp 10` output. Fails when current or max is missing.
# A max of 0 is unreadable, which is not the same as a current of 0.
brightness_percent() {
  awk '
    {
      if (match($0, /[Cc]urrent value *= *[0-9]+/)) {
        s = substr($0, RSTART, RLENGTH)
        sub(/.*= */, "", s)
        current = s + 0
        have_c = 1
      }
      if (match($0, /[Mm]ax value *= *[0-9]+/)) {
        s = substr($0, RSTART, RLENGTH)
        sub(/.*= */, "", s)
        max = s + 0
        have_m = 1
      }
    }
    END {
      if (!have_c || !have_m || max + 0 == 0) exit 1
      printf "%d\n", int((current * 100 + max / 2) / max)
    }
  ' <<<"$1"
}

service_user() {
  local user
  user="$(systemctl show -p User --value "$SERVICE" 2>/dev/null || true)"
  if [ -z "$user" ]; then
    printf '%s\n' root
  else
    printf '%s\n' "$user"
  fi
}

print_rollback() {
  cat <<EOF

Rollback (only if ${BIN_PATH}.bak exists):
  systemctl stop ${SERVICE}
  cp -a ${BIN_PATH}.bak ${BIN_PATH}
  systemctl start ${SERVICE}
  systemctl --no-pager --full status ${SERVICE}
EOF
}

ensure_ddcutil() {
  if command -v ddcutil >/dev/null 2>&1; then
    log "ddcutil already installed: $(command -v ddcutil)"
    return 0
  fi
  command -v apt-get >/dev/null 2>&1 || die "ddcutil is missing and apt-get is not available"
  log "Installing ddcutil"
  export DEBIAN_FRONTEND=noninteractive
  apt-get update
  apt-get install -y \
    -o Dpkg::Options::=--force-confdef \
    -o Dpkg::Options::=--force-confold \
    ddcutil
}

ensure_i2c() {
  local user="$1" rule="/etc/udev/rules.d/60-beszel-i2c.rules" modules="/etc/modules-load.d/i2c-dev.conf"
  local rule_body='KERNEL=="i2c-[0-9]*", GROUP="i2c", MODE="0660"'

  if [ ! -f "$modules" ] || ! grep -qx 'i2c-dev' "$modules"; then
    log "Loading i2c-dev at boot via ${modules}"
    printf 'i2c-dev\n' >"$modules"
  fi
  if ! modprobe i2c-dev; then
    log "WARNING: modprobe i2c-dev failed. The module is still configured for next boot."
  fi

  if ! getent group i2c >/dev/null 2>&1; then
    groupadd --system i2c
  fi

  if [ "$user" != "root" ]; then
    case " $(id -nG "$user") " in
      *" i2c "*) ;;
      *)
        log "Adding ${user} to group i2c"
        usermod -aG i2c "$user"
        NEED_RESTART=1
        ;;
    esac
  fi

  if [ ! -f "$rule" ] || [ "$(cat "$rule")" != "$rule_body" ]; then
    log "Writing ${rule}"
    printf '%s\n' "$rule_body" >"$rule"
    if command -v udevadm >/dev/null 2>&1; then
      udevadm control --reload
      udevadm trigger --subsystem-match=i2c || true
    fi
  fi
}

# Drop-in SupplementaryGroups replaces the unit's list, so keep groups that
# are already set (systemd-journal) and add i2c.
supplementary_groups_value() {
  local existing="$1" token have_i2c=0
  if [ -z "$existing" ]; then
    printf '%s\n' i2c
    return 0
  fi
  for token in $existing; do
    if [ "$token" = "i2c" ]; then
      have_i2c=1
    fi
  done
  if [ "$have_i2c" -eq 1 ]; then
    printf '%s\n' "$existing"
  else
    printf '%s\n' "${existing} i2c"
  fi
}

# Last SupplementaryGroups= line outside this script's own drop-in.
groups_from_unit_text() {
  awk '
    /^# / {
      file = $0
      sub(/^# /, "", file)
      skip = (file ~ /(^|\/)10-i2c\.conf$/)
      next
    }
    skip { next }
    /^SupplementaryGroups=/ {
      value = $0
      sub(/^SupplementaryGroups=/, "", value)
      last = value
    }
    END { if (last != "") print last }
  '
}

ensure_python3() {
  if command -v python3 >/dev/null 2>&1; then
    return 0
  fi
  command -v apt-get >/dev/null 2>&1 || die "python3 is required for the display helper"
  log "Installing python3"
  export DEBIAN_FRONTEND=noninteractive
  apt-get update
  apt-get install -y \
    -o Dpkg::Options::=--force-confdef \
    -o Dpkg::Options::=--force-confold \
    python3
}

ensure_i2c_dropin() {
  local dir="/etc/systemd/system/${SERVICE}.d"
  local file="${dir}/10-i2c.conf"
  local existing desired body current=""
  mkdir -p "$dir"
  existing="$(systemctl cat "$SERVICE" 2>/dev/null | groups_from_unit_text || true)"
  desired="$(supplementary_groups_value "$existing")"
  body="$(printf '[Service]\nSupplementaryGroups=%s\n' "$desired")"
  if [ -f "$file" ]; then
    current="$(cat "$file")"
  fi
  if [ "$current" != "$body" ]; then
    log "Writing ${file} (SupplementaryGroups=${desired})"
    printf '%s' "$body" >"$file"
    NEED_RESTART=1
    NEED_DAEMON_RELOAD=1
  fi
}

script_dir() {
  dirname "$(readlink -f "${BASH_SOURCE[0]}")"
}

write_if_changed() {
  local path="$1" body="$2" current=""
  case "$body" in
    *$'\n') ;;
    *) body="${body}"$'\n' ;;
  esac
  if [ -f "$path" ]; then
    current="$(cat "$path")"
  fi
  if [ "$current" != "$body" ]; then
    log "Writing ${path}"
    printf '%s' "$body" >"$path"
    NEED_DAEMON_RELOAD=1
  fi
}

install_display_helper() {
  local dest="$1" src url staged
  mkdir -p "$(dirname "$dest")"
  src="$(script_dir)/bright-watch-display.sh"
  if [ -f "$src" ]; then
    install -m 0755 "$src" "$dest"
    return 0
  fi
  url="${BESZEL_DISPLAY_HELPER_URL:-https://raw.githubusercontent.com/Pradeeps-repo/beszel/main/scripts/bright-watch-display.sh}"
  staged="${dest}.new"
  log "Downloading ${url}"
  curl -fsSL --retry 5 --retry-delay 2 --retry-connrefused -o "$staged" "$url"
  grep -q 'display.json' "$staged" || die "downloaded display helper is not bright-watch-display.sh"
  install -m 0755 "$staged" "$dest"
  rm -f "$staged"
}

helper_package_env() {
  local key="$1" value
  value="$(systemctl show -p Environment --value "$SERVICE" 2>/dev/null | tr ' ' '\n' | awk -F= -v key="$key" '$1 == key { sub(/^[^=]*=/, ""); print; exit }' || true)"
  printf '%s\n' "$value"
}

ensure_display_helper() {
  local dest="$HELPER_PATH"
  local unit="/etc/systemd/system/bright-watch-display.service"
  local timer="/etc/systemd/system/bright-watch-display.timer"
  local dropin="/etc/systemd/system/bright-watch-display.service.d/paths.conf"
  local unit_body timer_body drop_body="" app iot
  mkdir -p /etc/systemd/system/bright-watch-display.service.d
  install_display_helper "$dest"
  unit_body="$(cat <<EOF
[Unit]
Description=Record display mode, brightness, and app versions for Beszel
After=systemd-user-sessions.service

[Service]
Type=oneshot
TimeoutStartSec=30
ExecStart=${dest}
EOF
)"
  timer_body="$(cat <<EOF
[Unit]
Description=Record display mode, brightness, and app versions for Beszel

[Timer]
OnBootSec=30s
OnUnitActiveSec=2min
AccuracySec=30s
Persistent=true

[Install]
WantedBy=timers.target
EOF
)"
  app="$(helper_package_env SS_APP_PACKAGE_JSON)"
  if [ -z "$app" ]; then
    app="$(helper_package_env BESZEL_AGENT_SS_APP_PACKAGE_JSON)"
  fi
  iot="$(helper_package_env SS_IOT_PACKAGE_JSON)"
  if [ -z "$iot" ]; then
    iot="$(helper_package_env BESZEL_AGENT_SS_IOT_PACKAGE_JSON)"
  fi
  if [ -n "$app" ] || [ -n "$iot" ]; then
    drop_body="[Service]"$'\n'
    if [ -n "$app" ]; then
      drop_body+="Environment=SS_APP_PACKAGE_JSON=${app}"$'\n'
    fi
    if [ -n "$iot" ]; then
      drop_body+="Environment=SS_IOT_PACKAGE_JSON=${iot}"$'\n'
    fi
  fi
  write_if_changed "$unit" "$unit_body"
  write_if_changed "$timer" "$timer_body"
  if [ -n "$drop_body" ]; then
    write_if_changed "$dropin" "$drop_body"
  fi
}

self_test() {
  local user="$1" output="" percent=""
  log "self-test as ${user}:"
  if [ ! -x "$HELPER_PATH" ]; then
    log "  display helper is not installed"
    return 0
  fi
  if command -v ddcutil >/dev/null 2>&1; then
    if output="$(runuser -u "$user" -- ddcutil getvcp 10 2>&1)"; then
      percent="$(brightness_percent "$output" || true)"
    else
      log "  ddcutil as ${user} failed:"
      printf '%s\n' "$output" | awk 'NR<=30'
      log "  brightness below is from /run/bright-watch/display.json when ddcutil cannot open i2c"
    fi
  else
    log "  ddcutil as ${user}: not installed"
  fi
  if ! BRIGHT_WATCH_DDC_PERCENT="$percent" runuser -u "$user" -- env BRIGHT_WATCH_DDC_PERCENT="$percent" "$HELPER_PATH" --print; then
    log "  could not read /run/bright-watch as ${user}"
  fi
}

download_verified() {
  local asset="$1" dest="$2" sumfile url
  sumfile="${dest}.sha256"
  url="${RELEASE_BASE}/${asset}"
  log "Downloading ${url}"
  curl -fsSL --retry 5 --retry-delay 2 --retry-connrefused -o "$dest" "$url"
  curl -fsSL --retry 5 --retry-delay 2 --retry-connrefused -o "$sumfile" "${url}.sha256"
  expected="$(expected_sha256 "$sumfile")"
  verify_sha256 "$dest" "$expected" || die "sha256 mismatch for ${asset}"
  log "sha256 ok ${expected}"
}

install_binary() {
  local staged="$1" previous_owner
  if [ -f "$BIN_PATH" ]; then
    if verify_sha256 "$BIN_PATH" "$(expected_sha256 "${staged}.sha256")"; then
      log "Installed binary already matches ${staged}; not replacing it"
      return 1
    fi
    cp -a "$BIN_PATH" "${BIN_PATH}.bak"
    log "Previous binary saved at ${BIN_PATH}.bak"
  fi
  previous_owner="$(stat -c '%U:%G' "$BIN_PATH" 2>/dev/null || printf '%s\n' root:root)"
  install -m 0755 "$staged" "${BIN_PATH}.new"
  chown "$previous_owner" "${BIN_PATH}.new"
  mv -f "${BIN_PATH}.new" "$BIN_PATH"
  if command -v restorecon >/dev/null 2>&1; then
    restorecon "$BIN_PATH" || true
  fi
  log "Installed ${BIN_PATH}"
  return 0
}

rollback_binary() {
  if [ -f "${BIN_PATH}.bak" ]; then
    log "Restoring ${BIN_PATH}.bak"
    cp -a "${BIN_PATH}.bak" "$BIN_PATH"
    systemctl start "$SERVICE" || true
  fi
}

print_status() {
  systemctl --no-pager --full status "$SERVICE" || true
}

main() {
  local asset user staged="" replaced=0 stopped=0

  [ "$(id -u)" -eq 0 ] || die "run as root"
  command -v systemctl >/dev/null 2>&1 || die "systemctl is required"
  systemctl cat "$SERVICE" >/dev/null 2>&1 || die "${SERVICE} is not installed"
  command -v curl >/dev/null 2>&1 || die "curl is required"
  command -v sha256sum >/dev/null 2>&1 || die "sha256sum is required"

  mkdir -p "$(dirname "$LOCK_FILE")"
  exec 9>"$LOCK_FILE"
  flock -n 9 || die "another upgrade-agent.sh is running"

  asset="$(asset_for_machine)" || exit 1
  user="$(service_user)"
  log "arch asset=${asset} service_user=${user}"

  # Prepare DDC access before swapping the binary. A package failure then
  # leaves the running agent untouched.
  ensure_ddcutil
  ensure_python3
  ensure_i2c "$user"
  ensure_i2c_dropin
  ensure_display_helper
  if [ "$NEED_DAEMON_RELOAD" -eq 1 ]; then
    systemctl daemon-reload
  fi
  systemctl enable --now bright-watch-display.timer
  if ! systemctl start bright-watch-display.service; then
    log "WARNING: display helper did not complete. Resolution and app versions may be absent."
  fi

  staged="$(mktemp -d)"
  trap 'status=$?; [ -n "${staged:-}" ] && rm -rf "$staged"; if [ "$status" -ne 0 ] && [ "${stopped:-0}" -eq 1 ] && [ "$(systemctl is-active "$SERVICE" 2>/dev/null || true)" != "active" ]; then if [ "${replaced:-0}" -eq 1 ]; then rollback_binary; else systemctl start "$SERVICE" || true; fi; print_rollback; fi; exit "$status"' EXIT
  download_verified "$asset" "${staged}/beszel-agent"

  # Re-running across the fleet should not restart a service that is already
  # on this binary and already in the i2c group.
  if [ -f "$BIN_PATH" ] && verify_sha256 "$BIN_PATH" "$(expected_sha256 "${staged}/beszel-agent.sha256")" && [ "$NEED_RESTART" -eq 0 ]; then
    log "Already up to date; leaving ${SERVICE} running"
    print_status
    self_test "$user"
    print_rollback
    log "RESULT asset=${asset} replaced=0 service=active"
    exit 0
  fi

  systemctl stop "$SERVICE"
  stopped=1
  if install_binary "${staged}/beszel-agent"; then
    replaced=1
  fi

  if ! systemctl restart "$SERVICE"; then
    log "ERROR: ${SERVICE} did not restart"
    if [ "$replaced" -eq 1 ]; then
      rollback_binary
    fi
    print_status
    print_rollback
    exit 1
  fi

  if [ "$(systemctl is-active "$SERVICE")" != "active" ]; then
    log "ERROR: ${SERVICE} is not active"
    if [ "$replaced" -eq 1 ]; then
      rollback_binary
    fi
    print_status
    print_rollback
    exit 1
  fi

  print_status
  self_test "$user"
  print_rollback
  log "RESULT asset=${asset} replaced=${replaced} service=active"
}

if [ "${UPGRADE_AGENT_LIB_ONLY:-}" = "1" ]; then
  return 0 2>/dev/null || exit 0
fi

main "$@"
