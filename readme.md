# Beszel

Beszel is a lightweight server monitoring platform that includes Docker statistics, historical data, and alert functions.

It has a friendly web interface, simple configuration, and is ready to use out of the box. It supports automatic backup, multi-user, OAuth authentication, and API access.

[![agent Docker Image Size](https://img.shields.io/docker/image-size/henrygd/beszel-agent/latest?logo=docker&label=agent%20image%20size)](https://hub.docker.com/r/henrygd/beszel-agent)
[![hub Docker Image Size](https://img.shields.io/docker/image-size/henrygd/beszel/latest?logo=docker&label=hub%20image%20size)](https://hub.docker.com/r/henrygd/beszel)
[![MIT license](https://img.shields.io/github/license/henrygd/beszel?color=%239944ee)](https://github.com/henrygd/beszel/blob/main/LICENSE)
[![Crowdin](https://badges.crowdin.net/beszel/localized.svg)](https://crowdin.com/project/beszel)

![Screenshot of Beszel dashboard and system page, side by side. The dashboard shows metrics from multiple connected systems, while the system page shows detailed metrics for a single system.](https://henrygd-assets.b-cdn.net/beszel/screenshot-new.png)

## Features

- **Lightweight**: Smaller and less resource-intensive than leading solutions.
- **Simple**: Easy setup with little manual configuration required.
- **Alerts**: Configurable alerts for most metrics. Supports many notification services.
- **Docker stats**: Tracks CPU, memory, and network usage history for each container.
- **Network monitoring**: Monitor response time and interruptions directly from agents.
- **S.M.A.R.T.**: Disk health data and notifications on drive failure.
- **Multi-user**: Users manage their own systems. Admins can share systems across users.
- **OAuth / OIDC**: Supports many OAuth2 providers. Password auth can be disabled.
- **Automatic backups**: Save to and restore from disk or S3-compatible storage.
<!-- - **REST API**: Use or update your data in your own scripts and applications. -->

## Architecture

Beszel consists of two main components: the **hub** and the **agent**.

- **Hub**: A web application built on [PocketBase](https://pocketbase.io/) that provides a dashboard for viewing and managing connected systems.
- **Agent**: Runs on each system you want to monitor and communicates system metrics to the hub.

## Getting started

The [quick start guide](https://beszel.dev/guide/getting-started) and other documentation is available on our website, [beszel.dev](https://beszel.dev). You'll be up and running in a few minutes.

## Screenshots

![Dashboard](https://beszel.dev/image/dashboard.png)
![System page](https://beszel.dev/image/system-full.png)
![Notification Settings](https://beszel.dev/image/settings-notifications.png)

## Supported metrics

- **CPU usage** - Host system and Docker / Podman containers.
- **Memory usage** - Host system and containers. Includes swap and ZFS ARC.
- **Disk usage** - Host system. Supports multiple partitions and devices.
- **Disk I/O** - Host system. Supports multiple partitions and devices.
- **Network usage** - Host system and containers.
- **Load average** - Host system.
- **Temperature** - Host system sensors.
- **Fan speed** - Host system sensors (Linux, via `/sys/class/hwmon`).
- **GPU usage / power draw** - Nvidia, AMD, and Intel.
- **Battery charge** - Host system and some peripherals.
- **Screen brightness** - Current panel brightness, as a percent. Omitted when it cannot be read (it is never reported as 0% unless the screen is actually at 0%).
- **Containers** - Status and metrics of all running Docker / Podman containers.
- **S.M.A.R.T.** - Host system disk health (includes eMMC wear/EOL and Linux mdraid array health via sysfs when available).
- **ZFS** - Pool capacity, usage, health, I/O throughput, scrub status, and per-dataset usage.

## Screen brightness

The agent adds the current screen brightness to the same stats payload it already sends. The hub stores it on the system record (`info.br`) and in `system_stats` (`stats.br`). Both are existing JSON fields, so no database migration is required. The All Systems table has a sortable **Brightness** column (machines with no reading sort last, so they are not mixed up with 0%). Each system page shows a brightness chart once a reading exists.

Collection is on by default. Set `BRIGHTNESS=false` (or `BESZEL_AGENT_BRIGHTNESS=false`) to turn it off.

`BRIGHTNESS_SOURCE` (or `BESZEL_AGENT_BRIGHTNESS_SOURCE`) chooses the Linux reader:

| Value | Behavior |
| --- | --- |
| `auto` (default) | `ddcutil getvcp 10` when `ddcutil` is installed and a display answers. Otherwise `/sys/class/backlight`. |
| `ddc` | DDC/CI only. No backlight fallback. |
| `backlight` | `/sys/class/backlight` only. |

The percent is `current / max * 100` from the getvcp line (`current value = N, max value = M`). Max is not assumed to be 100. A failed read is omitted, never stored as 0%. DDC is slow (about a second), so the agent caches the result for one minute, runs at most one `ddcutil` at a time, and does not wait for it before sending the other stats. The value shows up on a later sample. A permission failure or other `ddcutil` error is logged once at warning level. If the root helper's `/run/bright-watch/display.json` has a brightness reading from the last few minutes, that percent is used instead of leaving the value absent.

### Screen, apps, and OS

These are sent on the system record (not charted). A value that cannot be read is omitted, not sent as an empty string.

- **Resolution and orientation.** `scripts/upgrade-agent.sh` installs a root helper, `bright-watch-display.timer`, which runs every two minutes. It tries X authority files in order: the running Xorg or Xwayland `-auth` path from `/proc/<pid>/cmdline`, then `/home/*/.Xauthority` (`vs` first), then `/run/user/*/.mutter-Xwaylandauth.*` and `/run/user/*/gdm/Xauthority`. If root is refused, it reruns `xrandr` as the user who owns that file. It logs the source it used and why each earlier one failed. A Wayland socket is the fallback when `xrandr` finds nothing. The resolution in `/run/bright-watch/display.json` is the logical size (`1080x1920` for a portrait panel). On XWayland the rotation word is often missing and there is no starred mode, so that geometry is the current resolution and orientation comes from the aspect ratio (taller than wide is portrait). When a rotation word is present it is still used: `normal` 0°, `left` 90°, `inverted` 180°, and `right` 270°. A starred mode, when one exists, stays the hardware mode. The agent uses that file when it is less than six minutes old. If it is missing, the agent tries its own `xrandr` or `wlr-randr`. The last resort is the current CRTC mode in `/sys/kernel/debug/dri/*/state`. That mode has no rotation, so resolution and orientation stay omitted rather than guessed. `/sys/class/drm/*/modes` is not the current mode; its first line is the panel maximum.
- **ss-app and ss-iot.** Versions come from each app's `package.json` `version`. Set `SS_APP_PACKAGE_JSON` and `SS_IOT_PACKAGE_JSON` (or the `BESZEL_AGENT_` prefixed names) to override the paths. When those are unset, the agent reads `/run/bright-watch/apps.json` from the root helper, then `/home/vs/smartselect/package.json` (`ss-app`) and `/home/vs/smartiot/package.json` (`ss-iot`). Those homes are often mode 700, so the service user cannot open them; the helper runs as root and copies the versions. After that it still searches a few levels under `/opt`, `/srv`, `/home`, and the `node_modules` directories in `/usr/lib` and `/usr/local/lib` for a package whose `name` is `ss-app` or `ss-iot`. An explicit path that can be read and has no version stays omitted. An explicit path that cannot be read falls back to the helper file.
- **OS.** Beszel already reads `PRETTY_NAME` and the kernel for the system details record. Those strings are copied onto the system record (`osr` and the existing `k` kernel field) so the All Systems table can show them.

Screen and package versions are cached for five minutes. The All Systems columns are Resolution, Orientation, ss-app, ss-iot, and OS. They sort with the other columns, match the table filter, and can be hidden from Visible Fields. The same strings appear on the system page header.

## MCP

The hub serves a read-only [Model Context Protocol](https://modelcontextprotocol.io) endpoint at `/mcp` on the same port as the UI (8090 in Coolify). No extra container is required. The route is off until you set a token or turn the existing API-key mode on.

Set a hub-wide bearer secret:

```bash
MCP_TOKEN=choose-a-long-random-string
```

`BESZEL_HUB_MCP_TOKEN` is the same variable. Leave it unset and the secret is disabled. A client that sends `Authorization: Bearer <MCP_TOKEN>` can read every system. PocketBase user, superuser, and API-key tokens are also accepted, and those calls only see the systems that account can see. To use API keys without `MCP_TOKEN`, set `BESZEL_HUB_MCP=true`.

Example client config:

```json
{
  "mcpServers": {
    "bright-watch": {
      "url": "http://HOST:8090/mcp",
      "headers": {
        "Authorization": "Bearer choose-a-long-random-string"
      }
    }
  }
}
```

Tools are read-only: `list_systems` (status, name, ss-app, ss-iot, orientation, resolution, brightness, with a limit), `get_system`, `get_system_history` (`1h`, `12h`, `24h`, `1w`, `30d`), `fleet_summary`, and `list_alerts` (rules and recent history).

### Linux DDC/CI permissions

`ddcutil` needs the `i2c-dev` module and read/write access to `/dev/i2c-*`. The agent does not have to run as root.

On the host, as root:

```bash
# load the I2C device interface now and on boot
modprobe i2c-dev
printf 'i2c-dev\n' > /etc/modules-load.d/i2c-dev.conf

# let the i2c group open the buses
printf 'KERNEL=="i2c-[0-9]*", GROUP="i2c", MODE="0660"\n' > /etc/udev/rules.d/60-ddcutil-i2c.rules
udevadm control --reload
udevadm trigger

# group name is i2c; create it if the distro did not
getent group i2c >/dev/null || groupadd i2c
usermod -aG i2c BESZEL_AGENT_USER
```

Group membership is applied when the process starts. `scripts/upgrade-agent.sh` also writes `/etc/systemd/system/beszel-agent.service.d/10-i2c.conf` with `SupplementaryGroups=` set to the unit's existing groups plus `i2c` (so `systemd-journal` is kept), runs `daemon-reload`, and restarts the service.

Install `ddcutil`, then restart the agent service so it picks up the new group. Confirm with that same user (not only as root):

```bash
sudo -u BESZEL_AGENT_USER ddcutil getvcp 10
```

A working read contains `current value =` and `max value =`. `Permission denied` or `i2c_permissions` means the device node is still root-only.

The systemd unit's `User=` must be that group member. Root works without the group, but it is not required.

### Upgrade systemd agents

Machines installed with upstream `install-agent.sh` keep the binary at `/opt/beszel-agent/beszel-agent` and the unit `beszel-agent.service` (drop-ins in `/etc/systemd/system/beszel-agent.service.d`). They are not the Docker image.

Every push to `main` publishes static binaries for `linux/amd64`, `linux/arm64`, and `linux/arm` v7 on the rolling release tag `agent-latest`:

- https://github.com/Pradeeps-repo/beszel/releases/download/agent-latest/beszel-agent_linux_amd64
- https://github.com/Pradeeps-repo/beszel/releases/download/agent-latest/beszel-agent_linux_amd64.sha256

The arm64 and armv7 assets use the same URL shape. On each machine, as root:

```bash
curl -fsSL -o /tmp/upgrade-agent.sh https://raw.githubusercontent.com/Pradeeps-repo/beszel/main/scripts/upgrade-agent.sh
bash /tmp/upgrade-agent.sh
```

The script is idempotent. It detects the architecture, checks sha256 before touching the live binary, keeps the previous binary as `/opt/beszel-agent/beszel-agent.bak`, installs `ddcutil` with apt when it is missing, loads `i2c-dev` at boot, and gives the service user access to `/dev/i2c-*` (i2c group, a udev rule, and a `SupplementaryGroups=i2c` drop-in). It installs `/opt/beszel-agent/bright-watch-display.sh` plus a systemd timer that rewrites `/run/bright-watch/display.json` and `apps.json` every two minutes. It then restarts the service when the binary or the i2c drop-in changed. The self-test at the end runs as the service user and prints brightness, resolution, mode, orientation, OS, ss-app, and ss-iot as the agent will report them. A failed `ddcutil` as that user is printed, and brightness then comes from the helper file. A failed restart restores the `.bak` binary. Rollback is also printed at the end:

```bash
systemctl stop beszel-agent.service
cp -a /opt/beszel-agent/beszel-agent.bak /opt/beszel-agent/beszel-agent
systemctl start beszel-agent.service
```

### Docker

Pushes to `main` and version tags (`v*`) publish multi-arch (`linux/amd64`, `linux/arm64`) images to GHCR:

- Hub: `ghcr.io/pradeeps-repo/beszel` (`latest`, plus the short commit SHA on `main` or the version tag)
- Agent: `ghcr.io/pradeeps-repo/beszel-agent` (Alpine image from `internal/dockerfile_agent_alpine`, with `ddcutil` installed)

The scratch Dockerfile (`internal/dockerfile_agent`) still has no `ddcutil`. Use the GHCR agent image, or a host install, when brightness is read over DDC/CI.

Give the container the host I2C devices and the host `i2c` group's numeric id (`getent group i2c | cut -d: -f3`):

```yaml
group_add:
  - "994" # host i2c GID, not the name
devices:
  - /dev/i2c-0
  - /dev/i2c-1
```

List `/dev/i2c-*` on the host and pass every bus the panel uses. `privileged: true` also works and is broader than this needs. Mounting `/sys` is only the backlight fallback (`BRIGHTNESS_SOURCE=backlight`, or `auto` when `ddcutil` is absent).

Coolify hub: change the image from `henrygd/beszel` to `ghcr.io/pradeeps-repo/beszel:latest`. Keep the `/beszel_data` volume and port `8090`. New GHCR packages are private. Either make `beszel` and `beszel-agent` public (package settings → Change visibility) or add a Coolify registry credential for `ghcr.io` (GitHub username + a PAT with `read:packages`).

- **Windows:** WMI `WmiMonitorBrightness` in `root\wmi`. The first active monitor, by instance name, is reported. `BRIGHTNESS_SOURCE` does not apply.

## Help and discussion

Please search existing issues and discussions before opening a new one. I try my best to respond, but may not always have time to do so.

#### Bug reports and feature requests

Bug reports and feature requests can be posted on [GitHub issues](https://github.com/henrygd/beszel/issues).

#### Support and general discussion

Support requests and general discussion can be posted on [GitHub discussions](https://github.com/henrygd/beszel/discussions) or the community-run [Matrix room](https://matrix.to/#/#beszel:matrix.org): `#beszel:matrix.org`.

## License

Beszel is licensed under the MIT License. See the [LICENSE](LICENSE) file for more details.
