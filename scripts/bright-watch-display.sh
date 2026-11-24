#!/usr/bin/env bash
# Root helper for the Beszel agent. The agent runs as the service user
# (beszel) and cannot read a mode-700 home or the X authority file.
# This writes world-readable JSON under /run/bright-watch:
#   display.json  current output, hardware mode, logical resolution, rotation, brightness
#   apps.json     ss-app and ss-iot versions from the real package.json paths
# A missing X session or a failed ddcutil read is omitted, not stored as a guess.
set -euo pipefail

WATCH_DIR="${BRIGHT_WATCH_DIR:-/run/bright-watch}"
PROC_ROOT="${BRIGHT_WATCH_PROC:-/proc}"
APP_PACKAGE="${SS_APP_PACKAGE_JSON:-${BESZEL_AGENT_SS_APP_PACKAGE_JSON:-/home/vs/smartselect/package.json}}"
IOT_PACKAGE="${SS_IOT_PACKAGE_JSON:-${BESZEL_AGENT_SS_IOT_PACKAGE_JSON:-/home/vs/smartiot/package.json}}"

log() {
  printf 'bright-watch: %s\n' "$*" >&2
}

run_timeout() {
  local seconds="$1"
  shift
  if command -v timeout >/dev/null 2>&1; then
    timeout "$seconds" "$@"
  else
    "$@"
  fi
}

# Percent from `ddcutil getvcp 10`. Fails when current or max is missing.
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

# Xorg, X, and Xwayland. Mutter's XWayland is not named Xorg.
is_x_server() {
  case "$1" in
    Xorg | X | Xorg.bin | Xwayland) return 0 ;;
    *) return 1 ;;
  esac
}

# True when xrandr refused the caller (root is often refused on XWayland).
xrandr_was_refused() {
  grep -Eqi 'no protocol specified|authorization required|can.?t open display|invalid mit-magic-cookie|not authorized|permission denied' <<<"$1"
}

# source<TAB>display<TAB>auth, in the order the field machines need:
#   1. Xorg/Xwayland -auth from /proc/PID/cmdline
#   2. /home/*/.Xauthority, with vs first
#   3. mutter and gdm XWayland cookies under /run/user
list_xauth_candidates() {
  local cmdline bin display="" auth="" arg i name path
  local -a args
  local seen=""
  shopt -s nullglob
  emit_auth() {
    local source="$1" disp="$2" file="$3"
    [ -n "$file" ] || return 0
    case "$seen" in
      *"|${file}|"*) return 0 ;;
    esac
    seen="${seen}|${file}|"
    printf '%s\t%s\t%s\n' "$source" "$disp" "$file"
  }
  if [ -d "$PROC_ROOT" ]; then
    for cmdline in "$PROC_ROOT"/[0-9]*/cmdline; do
      [ -r "$cmdline" ] || continue
      args=()
      mapfile -d '' -t args <"$cmdline" || true
      [ "${#args[@]}" -gt 0 ] || continue
      bin="${args[0]##*/}"
      is_x_server "$bin" || continue
      display=""
      auth=""
      for ((i = 1; i < ${#args[@]}; i++)); do
        arg="${args[$i]}"
        if [ "$arg" = "-auth" ] && [ $((i + 1)) -lt ${#args[@]} ]; then
          auth="${args[$((i + 1))]}"
          i=$((i + 1))
          continue
        fi
        case "$arg" in
          :*)
            case "$arg" in
              *=*) ;;
              *) display="$arg" ;;
            esac
            ;;
        esac
      done
      if [ -z "$auth" ]; then
        log "auth proc:${bin} skipped: no -auth in ${cmdline}"
        continue
      fi
      [ -n "$display" ] || display=":0"
      if [ ! -f "$auth" ]; then
        log "auth proc:${bin} skipped: ${auth} is not a file"
        continue
      fi
      emit_auth "proc:${bin}" "$display" "$auth"
    done
  fi
  local home_root="${BRIGHT_WATCH_HOME:-/home}"
  if [ -f "${home_root}/vs/.Xauthority" ]; then
    emit_auth "home" ":0" "${home_root}/vs/.Xauthority"
  fi
  for path in "${home_root}"/*/.Xauthority; do
    [ -f "$path" ] || continue
    name="$(basename "$(dirname "$path")")"
    [ "$name" = "vs" ] && continue
    emit_auth "home" ":0" "$path"
  done
  local runtime_root="${BRIGHT_WATCH_RUNTIME:-/run/user}"
  for path in "${runtime_root}"/*/.mutter-Xwaylandauth.*; do
    [ -f "$path" ] || continue
    emit_auth "mutter" ":0" "$path"
  done
  for path in "${runtime_root}"/*/gdm/Xauthority; do
    [ -f "$path" ] || continue
    emit_auth "gdm" ":0" "$path"
  done
}

# Print "DISPLAY\nXAUTHORITY" for the first X server -auth file.
find_x_session() {
  local line display auth
  line="$(list_xauth_candidates | awk -F '\t' '$1 ~ /^proc:/ { print; exit }')"
  [ -n "$line" ] || return 1
  display="$(printf '%s\n' "$line" | awk -F '\t' '{ print $2 }')"
  auth="$(printf '%s\n' "$line" | awk -F '\t' '{ print $3 }')"
  printf '%s\n%s\n' "$display" "$auth"
}

auth_owner() {
  stat -c '%U' "$1" 2>/dev/null || true
}

# xrandr --query. Geometry on the connected line is already rotated.
# The starred mode line is the current hardware mode, not the first mode.
parse_xrandr_text() {
  awk '
    function ismode(s) { return s ~ /^[0-9]+x[0-9]+$/ }
    $2 == "connected" {
      if (geo != "") {
        capture = 0
        next
      }
      output = $1
      geo = ""
      rot = ""
      mode = ""
      capture = 1
      for (i = 3; i <= NF; i++) {
        split($i, parts, "+")
        if (ismode(parts[1]) && index($i, "+") > 0) {
          geo = parts[1]
          continue
        }
        if (geo != "" && rot == "") {
          if ($i == "normal" || $i == "left" || $i == "inverted" || $i == "right") {
            rot = $i
          }
          break
        }
      }
      next
    }
    capture && $0 ~ /^[ \t]/ {
      if (mode == "" && index($0, "*") > 0 && ismode($1)) {
        mode = $1
      }
      next
    }
    capture && $0 !~ /^[ \t]/ && NF {
      capture = 0
    }
    END {
      # XWayland often has no starred mode. The connected geometry is the live size.
      if (mode == "" && geo != "") {
        mode = geo
      }
      printf "output=%s\ngeometry=%s\nrotation=%s\nmode=%s\n", output, geo, rot, mode
    }
  '
}

# wlr-randr reports the panel mode. A 90 or 270 transform is swapped later.
parse_wlr_text() {
  awk '
    function flush() {
      if (enabled && mode != "" && best == "") {
        best = mode
        bestt = transform
      }
    }
    /^[^ \t]/ { flush(); enabled = 0; mode = ""; transform = ""; next }
    $1 == "Enabled:" && $2 == "yes" { enabled = 1; next }
    $1 == "Transform:" { transform = $2; next }
    index($0, "(current)") && $1 ~ /^[0-9]+x[0-9]+$/ { mode = $1; next }
    END {
      flush()
      printf "mode=%s\ntransform=%s\n", best, bestt
    }
  '
}

swap_for_rotation() {
  local resolution="$1" rotation="$2" width height
  case "$rotation" in
    90 | 270 | left | right)
      width="${resolution%x*}"
      height="${resolution#*x}"
      printf '%s\n' "${height}x${width}"
      ;;
    *) printf '%s\n' "$resolution" ;;
  esac
}

# Matches the agent: logical size, then portrait/landscape, with degrees when rotated.
orientation_label() {
  local resolution="$1" rotation="$2" width height base degrees=""
  width="${resolution%%x*}"
  height="${resolution#*x}"
  case "$width" in
    "" | *[!0-9]*) printf '\n'; return 0 ;;
  esac
  case "$height" in
    "" | *[!0-9]*) printf '\n'; return 0 ;;
  esac
  if [ "$width" -le 0 ] || [ "$height" -le 0 ] || [ "$width" -eq "$height" ]; then
    base=""
  elif [ "$height" -gt "$width" ]; then
    base="portrait"
  else
    base="landscape"
  fi
  case "$rotation" in
    normal | 0) degrees=0 ;;
    left | 90) degrees=90 ;;
    inverted | 180) degrees=180 ;;
    right | 270) degrees=270 ;;
    *)
      printf '%s\n' "$base"
      return 0
      ;;
  esac
  if [ -z "$base" ]; then
    printf '\n'
    return 0
  fi
  if [ "$degrees" -eq 0 ]; then
    printf '%s\n' "$base"
  else
    printf '%s (%s°)\n' "$base" "$degrees"
  fi
}

package_version() {
  local path="$1"
  [ -f "$path" ] || return 0
  python3 - "$path" <<'PY'
import json, sys
try:
    with open(sys.argv[1], encoding="utf-8") as handle:
        data = json.load(handle)
except Exception:
    raise SystemExit(0)
if not isinstance(data, dict):
    raise SystemExit(0)
version = str(data.get("version", "")).strip()
if version:
    print(version)
PY
}

field_value() {
  local key="$1"
  awk -F= -v key="$key" '$1 == key { sub(/^[^=]*=/, ""); print; exit }'
}

# Run xrandr. If root is refused, retry as the user who owns the authority file.
run_xrandr_query() {
  local display="$1" auth="$2" user="$3"
  local err out user_error
  XRR_ERROR=""
  XRR_USER=""
  err="$(mktemp)"
  if out="$(DISPLAY="$display" XAUTHORITY="$auth" run_timeout 3 xrandr --query 2>"$err")"; then
    rm -f "$err"
    printf '%s\n' "$out"
    return 0
  fi
  XRR_ERROR="$(tr '\n' ' ' <"$err")"
  XRR_ERROR="${XRR_ERROR%"${XRR_ERROR##*[![:space:]]}"}"
  rm -f "$err"
  if [ "$(id -u)" -eq 0 ] && [ -n "$user" ] && [ "$user" != "root" ] && xrandr_was_refused "$XRR_ERROR"; then
    if ! command -v runuser >/dev/null 2>&1; then
      XRR_ERROR="root refused (${XRR_ERROR}); runuser is not available for ${user}"
      return 1
    fi
    err="$(mktemp)"
    if command -v timeout >/dev/null 2>&1; then
      out="$(runuser -u "$user" -- env DISPLAY="$display" XAUTHORITY="$auth" timeout 3 xrandr --query 2>"$err")" && {
        rm -f "$err"
        XRR_USER="$user"
        printf '%s\n' "$out"
        return 0
      }
    else
      out="$(runuser -u "$user" -- env DISPLAY="$display" XAUTHORITY="$auth" xrandr --query 2>"$err")" && {
        rm -f "$err"
        XRR_USER="$user"
        printf '%s\n' "$out"
        return 0
      }
    fi
    user_error="$(tr '\n' ' ' <"$err")"
    user_error="${user_error%"${user_error##*[![:space:]]}"}"
    rm -f "$err"
    XRR_ERROR="root refused (${XRR_ERROR}); ${user} also failed (${user_error})"
  fi
  return 1
}

read_xrandr() {
  local source display auth parsed out user any=0
  command -v xrandr >/dev/null 2>&1 || {
    log "xrandr is not installed"
    return 1
  }
  while IFS=$'\t' read -r source display auth; do
    any=1
    if [ ! -f "$auth" ]; then
      log "auth ${source} skipped: ${auth} is not a file"
      continue
    fi
    user="$(auth_owner "$auth")"
    if out="$(run_xrandr_query "$display" "$auth" "$user")"; then
      if [ -n "$XRR_USER" ]; then
        log "xrandr auth source=${source} display=${display} file=${auth} as user ${XRR_USER} (root was refused)"
        X_AUTH_SOURCE="${source}:${auth} as ${XRR_USER}"
      else
        log "xrandr auth source=${source} display=${display} file=${auth}"
        X_AUTH_SOURCE="${source}:${auth}"
      fi
      parsed="$(printf '%s\n' "$out" | parse_xrandr_text)"
      X_OUTPUT="$(printf '%s\n' "$parsed" | field_value output)"
      X_GEOMETRY="$(printf '%s\n' "$parsed" | field_value geometry)"
      X_ROTATION="$(printf '%s\n' "$parsed" | field_value rotation)"
      X_MODE="$(printf '%s\n' "$parsed" | field_value mode)"
      if [ -n "$X_GEOMETRY" ]; then
        return 0
      fi
      log "auth ${source} file=${auth} failed: xrandr ran but reported no connected geometry"
      continue
    fi
    log "auth ${source} display=${display} file=${auth} failed: ${XRR_ERROR:-xrandr failed}"
  done < <(list_xauth_candidates)
  if [ "$any" -eq 0 ]; then
    log "xrandr: no X authority candidates (no Xorg/Xwayland -auth, no home .Xauthority, no mutter or gdm cookie)"
  fi
  return 1
}

read_wlr() {
  local runtime socket out parsed mode transform
  command -v wlr-randr >/dev/null 2>&1 || return 1
  shopt -s nullglob
  for socket in /run/user/*/wayland-*; do
    [ -S "$socket" ] || continue
    runtime="$(dirname "$socket")"
    if ! out="$(XDG_RUNTIME_DIR="$runtime" WAYLAND_DISPLAY="$(basename "$socket")" run_timeout 3 wlr-randr 2>/dev/null)"; then
      continue
    fi
    parsed="$(printf '%s\n' "$out" | parse_wlr_text)"
    mode="$(printf '%s\n' "$parsed" | field_value mode)"
    transform="$(printf '%s\n' "$parsed" | field_value transform)"
    [ -n "$mode" ] || continue
    X_OUTPUT=""
    X_MODE="$mode"
    X_ROTATION="$transform"
    X_GEOMETRY="$(swap_for_rotation "$mode" "$transform")"
    return 0
  done
  return 1
}

read_brightness_percent() {
  local out pct
  command -v ddcutil >/dev/null 2>&1 || return 1
  if ! out="$(run_timeout 8 ddcutil getvcp 10 2>&1)"; then
    log "ddcutil failed: ${out}"
    return 1
  fi
  pct="$(brightness_percent "$out")" || return 1
  printf '%s\n' "$pct"
}

write_reports() {
  local brightness="$1" resolution="$2" rotation="$3" mode="$4" output="$5" orientation="$6" ss_app="$7" ss_iot="$8" auth="${9:-}"
  mkdir -p "$WATCH_DIR"
  chmod 0755 "$WATCH_DIR"
  BRIGHTNESS="$brightness" \
    RESOLUTION="$resolution" \
    ROTATION="$rotation" \
    MODE="$mode" \
    OUTPUT="$output" \
    ORIENTATION="$orientation" \
    SS_APP="$ss_app" \
    SS_IOT="$ss_iot" \
    AUTH="$auth" \
    WATCH_DIR="$WATCH_DIR" \
    python3 - <<'PY'
import json, os
directory = os.environ["WATCH_DIR"]
os.makedirs(directory, 0o755, exist_ok=True)
os.chmod(directory, 0o755)

def clean(value):
    value = value.strip()
    return value or None

def write(name, payload):
    path = os.path.join(directory, name)
    temporary = path + ".tmp"
    with open(temporary, "w", encoding="utf-8") as handle:
        json.dump(payload, handle, ensure_ascii=False)
        handle.write("\n")
    os.chmod(temporary, 0o644)
    os.replace(temporary, path)

from datetime import datetime, timezone
updated = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
display = {
    "updated": updated,
    "output": clean(os.environ.get("OUTPUT", "")),
    "mode": clean(os.environ.get("MODE", "")),
    "resolution": clean(os.environ.get("RESOLUTION", "")),
    "rotation": clean(os.environ.get("ROTATION", "")),
    "orientation": clean(os.environ.get("ORIENTATION", "")),
    "auth": clean(os.environ.get("AUTH", "")),
}
brightness = os.environ.get("BRIGHTNESS", "").strip()
if brightness.isdigit():
    value = int(brightness)
    if 0 <= value <= 100:
        display["brightness"] = value
display = {key: value for key, value in display.items() if value is not None}
write("display.json", display)
apps = {
    "updated": updated,
    "ss_app": clean(os.environ.get("SS_APP", "")),
    "ss_iot": clean(os.environ.get("SS_IOT", "")),
}
apps = {key: value for key, value in apps.items() if value is not None}
write("apps.json", apps)
PY
}

# What the agent will report. A successful ddcutil read as the service user
# wins, because the agent tries that first. Otherwise the helper file is used.
print_agent_view() {
  local display="$1" apps="$2"
  python3 - "$display" "$apps" <<'PY'
import json, os, sys

def load(path):
    try:
        with open(path, encoding="utf-8") as handle:
            data = json.load(handle)
    except Exception:
        return {}
    return data if isinstance(data, dict) else {}

display = load(sys.argv[1])
apps = load(sys.argv[2])

def text(value):
    if value is None:
        return "absent"
    rendered = str(value).strip()
    return rendered or "absent"

percent = os.environ.get("BRIGHT_WATCH_DDC_PERCENT", "").strip()
if percent.isdigit():
    brightness = f"{int(percent)}%"
else:
    raw = display.get("brightness")
    if isinstance(raw, bool) or not isinstance(raw, (int, float)):
        brightness = "absent"
    else:
        value = int(raw)
        brightness = f"{value}%" if 0 <= value <= 100 else "absent"

pretty = ""
try:
    with open("/etc/os-release", encoding="utf-8") as handle:
        for line in handle:
            if line.startswith("PRETTY_NAME="):
                pretty = line.split("=", 1)[1].strip().strip('"').strip("'")
                break
except Exception:
    pretty = ""
kernel = os.uname().release
os_text = " · ".join(part for part in (pretty, kernel) if part) or "absent"
print(f"  brightness: {brightness}")
print(f"  resolution: {text(display.get('resolution'))}")
print(f"  mode: {text(display.get('mode'))}")
print(f"  orientation: {text(display.get('orientation'))}")
print(f"  output: {text(display.get('output'))}")
print(f"  auth: {text(display.get('auth'))}")
print(f"  os: {os_text}")
print(f"  ss-app: {text(apps.get('ss_app'))}")
print(f"  ss-iot: {text(apps.get('ss_iot'))}")
PY
}

collect_and_write() {
  local brightness="" resolution="" rotation="" mode="" output="" orientation="" ss_app="" ss_iot=""
  X_OUTPUT=""
  X_GEOMETRY=""
  X_ROTATION=""
  X_MODE=""
  X_AUTH_SOURCE=""
  if read_xrandr || read_wlr; then
    output="$X_OUTPUT"
    resolution="$X_GEOMETRY"
    rotation="$X_ROTATION"
    mode="$X_MODE"
    orientation="$(orientation_label "$resolution" "$rotation")"
  else
    log "no readable X or Wayland session; resolution omitted"
  fi
  if brightness="$(read_brightness_percent)"; then
    :
  else
    brightness=""
  fi
  ss_app="$(package_version "$APP_PACKAGE")"
  ss_iot="$(package_version "$IOT_PACKAGE")"
  write_reports "$brightness" "$resolution" "$rotation" "$mode" "$output" "$orientation" "$ss_app" "$ss_iot" "$X_AUTH_SOURCE"
  log "auth=${X_AUTH_SOURCE:-absent} output=${output:-absent} resolution=${resolution:-absent} mode=${mode:-absent} orientation=${orientation:-absent} brightness=${brightness:-absent} ss-app=${ss_app:-absent} ss-iot=${ss_iot:-absent}"
}

if [ "${BRIGHT_WATCH_LIB_ONLY:-}" = "1" ]; then
  return 0 2>/dev/null || exit 0
fi

main() {
  case "${1:-}" in
    --print)
      print_agent_view "${WATCH_DIR}/display.json" "${WATCH_DIR}/apps.json"
      ;;
    *)
      [ "$(id -u)" -eq 0 ] || {
        printf 'bright-watch: must run as root\n' >&2
        exit 1
      }
      command -v python3 >/dev/null 2>&1 || {
        printf 'bright-watch: python3 is required\n' >&2
        exit 1
      }
      collect_and_write
      ;;
  esac
}

main "$@"
