#!/usr/bin/env bash
# Unit checks for the display helper parsers. No root, no X server, no ddcutil.
set -euo pipefail

root="$(cd "$(dirname "$0")" && pwd)"
BRIGHT_WATCH_LIB_ONLY=1
# shellcheck disable=SC1091
source "${root}/bright-watch-display.sh"

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

field() {
  local key="$1"
  awk -F= -v key="$key" '$1 == key { sub(/^[^=]*=/, ""); print; exit }'
}

sample='HDMI-A-2 connected primary 1080x1920+0+0 left (normal left inverted right x axis y axis) 600mm x 340mm
   3840x2160     30.00 +
   1920x1080     60.00*+  50.00
   1280x720      60.00
DP-1 disconnected
   3840x2160     30.00*'
parsed="$(printf '%s\n' "$sample" | parse_xrandr_text)"
[ "$(printf '%s\n' "$parsed" | field output)" = "HDMI-A-2" ] || fail "output"
[ "$(printf '%s\n' "$parsed" | field geometry)" = "1080x1920" ] || fail "logical geometry"
[ "$(printf '%s\n' "$parsed" | field rotation)" = "left" ] || fail "rotation"
[ "$(printf '%s\n' "$parsed" | field mode)" = "1920x1080" ] || fail "starred mode, not the first mode line"
[ "$(orientation_label 1080x1920 left)" = "portrait (90°)" ] || fail "portrait left"
[ "$(orientation_label 1920x1080 normal)" = "landscape" ] || fail "normal landscape"
[ "$(orientation_label 1920x1080 inverted)" = "landscape (180°)" ] || fail "inverted"
[ "$(orientation_label 1920x1080 right)" = "landscape (270°)" ] || fail "right keeps logical landscape"
[ "$(orientation_label 1080x1920 right)" = "portrait (270°)" ] || fail "right portrait"
[ "$(swap_for_rotation 1920x1080 90)" = "1080x1920" ] || fail "wlr swap"
[ "$(swap_for_rotation 1920x1080 normal)" = "1920x1080" ] || fail "wlr normal"

wlr='HDMI-A-1 "Panel"
  Enabled: yes
  Modes:
    1920x1080 px, 60.000000 Hz (current)
  Transform: 90
HDMI-A-2 "Off"
  Enabled: no
  Modes:
    1280x720 px, 60.000000 Hz (current)
  Transform: normal'
wlr_parsed="$(printf '%s\n' "$wlr" | parse_wlr_text)"
[ "$(printf '%s\n' "$wlr_parsed" | field mode)" = "1920x1080" ] || fail "wlr mode"
[ "$(printf '%s\n' "$wlr_parsed" | field transform)" = "90" ] || fail "wlr transform"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
mkdir -p "${tmp}/proc/1" "${tmp}/proc/42"
printf '%s\0' /sbin/init >"${tmp}/proc/1/cmdline"
auth="${tmp}/xauthority"
printf 'auth\n' >"$auth"
printf '%s\0' /usr/lib/xorg/Xorg :0 -seat seat0 -auth "$auth" -nolisten tcp vt7 >"${tmp}/proc/42/cmdline"
PROC_ROOT="${tmp}/proc"
session="$(find_x_session)"
[ "$(printf '%s\n' "$session" | awk 'NR==1')" = ":0" ] || fail "x display"
[ "$(printf '%s\n' "$session" | awk 'NR==2')" = "$auth" ] || fail "x auth"

WATCH_DIR="${tmp}/run"
write_reports 100 1080x1920 left 1920x1080 HDMI-A-2 "portrait (90°)" 1.2.3 4.5.6
python3 - "${WATCH_DIR}/display.json" "${WATCH_DIR}/apps.json" <<'PY'
import json, os, stat, sys
display = json.load(open(sys.argv[1], encoding="utf-8"))
apps = json.load(open(sys.argv[2], encoding="utf-8"))
assert display["resolution"] == "1080x1920"
assert display["mode"] == "1920x1080"
assert display["rotation"] == "left"
assert display["orientation"] == "portrait (90°)"
assert display["output"] == "HDMI-A-2"
assert display["brightness"] == 100
assert "updated" in display
assert apps["ss_app"] == "1.2.3"
assert apps["ss_iot"] == "4.5.6"
for path in sys.argv[1:]:
    mode = os.stat(path).st_mode
    if mode & 0o022:
        raise SystemExit(f"{path} is writable by group or other: {oct(mode & 0o777)}")
    if not mode & stat.S_IROTH:
        raise SystemExit(f"{path} is not world-readable")
directory = os.stat(os.path.dirname(sys.argv[1])).st_mode
if directory & 0o022:
    raise SystemExit("watch dir is writable by group or other")
if not directory & stat.S_IXOTH:
    raise SystemExit("watch dir is not traversable")
PY

# 0% is a real reading and must be kept. Empty strings are omitted.
write_reports 0 "" "" "" "" "" "" ""
python3 - "${WATCH_DIR}/display.json" "${WATCH_DIR}/apps.json" <<'PY'
import json, sys
display = json.load(open(sys.argv[1], encoding="utf-8"))
apps = json.load(open(sys.argv[2], encoding="utf-8"))
assert display["brightness"] == 0
assert "resolution" not in display
assert "mode" not in display
assert list(apps) == ["updated"]
PY

view="$(BRIGHT_WATCH_DDC_PERCENT=100 print_agent_view "${WATCH_DIR}/display.json" "${WATCH_DIR}/apps.json")"
printf '%s\n' "$view" | grep -q '  brightness: 100%' || fail "self-test brightness prefers ddcutil percent"
printf '%s\n' "$view" | grep -q '  ss-app: absent' || fail "self-test ss-app"
printf '%s\n' "$view" | grep -q '  ss-iot: absent' || fail "self-test ss-iot"
printf '%s\n' "$view" | grep -q '  os: ' || fail "self-test os"

unset BRIGHT_WATCH_DDC_PERCENT
write_reports 40 1080x1920 left 1920x1080 HDMI-A-2 "portrait (90°)" 9.0.1 8.0.2
view="$(print_agent_view "${WATCH_DIR}/display.json" "${WATCH_DIR}/apps.json")"
printf '%s\n' "$view" | grep -q '  brightness: 40%' || fail "self-test helper brightness"
printf '%s\n' "$view" | grep -q '  resolution: 1080x1920' || fail "self-test resolution"
printf '%s\n' "$view" | grep -q '  mode: 1920x1080' || fail "self-test mode"
printf '%s\n' "$view" | grep -q '  orientation: portrait (90°)' || fail "self-test orientation"
printf '%s\n' "$view" | grep -q '  ss-app: 9.0.1' || fail "self-test app version"
printf '%s\n' "$view" | grep -q '  ss-iot: 8.0.2' || fail "self-test iot version"

printf 'ok\n'
