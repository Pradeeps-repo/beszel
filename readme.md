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

The percent is `current / max * 100` from the getvcp line (`current value = N, max value = M`). Max is not assumed to be 100. A failed read is omitted, never stored as 0%. DDC is slow (about a second), so the agent caches the result for one minute, runs at most one `ddcutil` at a time, and does not wait for it before sending the other stats. The value shows up on a later sample. A permission failure logs one warning and stays omitted.

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

Install `ddcutil`, then restart the agent service so it picks up the new group. Confirm with that same user (not only as root):

```bash
sudo -u BESZEL_AGENT_USER ddcutil getvcp 10
```

A working read contains `current value =` and `max value =`. `Permission denied` or `i2c_permissions` means the device node is still root-only.

The systemd unit's `User=` must be that group member. Root works without the group, but it is not required.

### Docker

The default scratch agent image does not contain `ddcutil`. Use a host install (systemd) or an image that has `ddcutil` on `PATH`. Give the container the host I2C devices and the host `i2c` group's numeric id (`getent group i2c | cut -d: -f3`):

```yaml
group_add:
  - "994" # host i2c GID, not the name
devices:
  - /dev/i2c-0
  - /dev/i2c-1
```

List `/dev/i2c-*` on the host and pass every bus the panel uses. `privileged: true` also works and is broader than this needs. Mounting `/sys` is only the backlight fallback (`BRIGHTNESS_SOURCE=backlight`, or `auto` when `ddcutil` is absent).

- **Windows:** WMI `WmiMonitorBrightness` in `root\wmi`. The first active monitor, by instance name, is reported. `BRIGHTNESS_SOURCE` does not apply.

## Help and discussion

Please search existing issues and discussions before opening a new one. I try my best to respond, but may not always have time to do so.

#### Bug reports and feature requests

Bug reports and feature requests can be posted on [GitHub issues](https://github.com/henrygd/beszel/issues).

#### Support and general discussion

Support requests and general discussion can be posted on [GitHub discussions](https://github.com/henrygd/beszel/discussions) or the community-run [Matrix room](https://matrix.to/#/#beszel:matrix.org): `#beszel:matrix.org`.

## License

Beszel is licensed under the MIT License. See the [LICENSE](LICENSE) file for more details.
