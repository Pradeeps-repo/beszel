# Installing as a Linux systemd service

This is useful if you want to run the hub or agent in the background continuously, including after a reboot.

## Install script (recommended)

There are two scripts, one for the hub and one for the agent. You can run either one, or both.

The install script creates a dedicated user for the service (`beszel`), downloads the latest release, and installs the service.

If you need to edit the service -- for instance, to change an environment variable -- you can edit the file(s) in `/etc/systemd/system/`. Then reload the systemd daemon and restart the service.

> [!NOTE]
> You need system administrator privileges to run the install script. If you encounter a problem, please [open an issue](https://github.com/henrygd/beszel/issues/new).

### Hub

Download the script:

```bash
curl -sL https://raw.githubusercontent.com/henrygd/beszel/main/supplemental/scripts/install-hub.sh -o install-hub.sh && chmod +x install-hub.sh
```

#### Install

You may specify a port number with the `-p` flag. The default port is `8090`.

```bash
./install-hub.sh
```

#### Uninstall

```bash
./install-hub.sh -u
```

#### Update

```bash
sudo /opt/beszel/beszel update && sudo systemctl restart beszel-hub
```

### Agent

Download the script:

```bash
curl -sL https://raw.githubusercontent.com/henrygd/beszel/main/supplemental/scripts/install-agent.sh -o install-agent.sh && chmod +x install-agent.sh
```

#### Install

You may optionally include the SSH key and port as arguments. Run `./install-agent.sh -h` for more info.

If specifying your key with `-k`, please make sure to enclose it in quotes.

```bash
./install-agent.sh
```

#### Uninstall

```bash
./install-agent.sh -u
```

#### Update

```bash
sudo /opt/beszel-agent/beszel-agent update && sudo systemctl restart beszel-agent
```

## Manual install

### Hub

1. Create the system service at `/etc/systemd/system/beszel.service`

```bash
[Unit]
Description=Beszel Hub Service
After=network.target

[Service]
# update the values in the curly braces below (remove the braces)
ExecStart={/path/to/working/directory}/beszel serve
WorkingDirectory={/path/to/working/directory}
User={YOUR_USERNAME}
Restart=always

[Install]
WantedBy=multi-user.target
```

2. Start and enable the service to let it run after system boot

```bash
sudo systemctl daemon-reload
sudo systemctl enable beszel.service
sudo systemctl start beszel.service
```

### Agent

1. Create the system service at `/etc/systemd/system/beszel-agent.service`

```bash
[Unit]
Description=Beszel Agent Service
After=network.target

[Service]
# update the values in curly braces below (remove the braces)
Environment="PORT={PASTE_YOUR_PORT_HERE}"
Environment="KEY={PASTE_YOUR_KEY_HERE}"
# Environment="EXTRA_FILESYSTEMS={sdb}"
# Environment="BRIGHTNESS=false"
# Environment="BRIGHTNESS_SOURCE=auto"   # auto, ddc, or backlight
ExecStart={/path/to/directory}/beszel-agent
# DDC brightness: this user must be in the i2c group. See the README.
User={YOUR_USERNAME}
Restart=always

[Install]
WantedBy=multi-user.target
```

2. Start and enable the service to let it run after system boot

```bash
sudo systemctl daemon-reload
sudo systemctl enable beszel-agent.service
sudo systemctl start beszel-agent.service
```

3. Screen brightness over DDC/CI (`ddcutil getvcp 10`) needs `i2c-dev` and access to `/dev/i2c-*` for `User=`. Root is not required.

```bash
sudo modprobe i2c-dev
echo i2c-dev | sudo tee /etc/modules-load.d/i2c-dev.conf
echo 'KERNEL=="i2c-[0-9]*", GROUP="i2c", MODE="0660"' | sudo tee /etc/udev/rules.d/60-ddcutil-i2c.rules
sudo udevadm control --reload && sudo udevadm trigger
getent group i2c >/dev/null || sudo groupadd i2c
sudo usermod -aG i2c {YOUR_USERNAME}
sudo systemctl restart beszel-agent.service
```

Install `ddcutil` and check as that user: `sudo -u {YOUR_USERNAME} ddcutil getvcp 10`. `Permission denied` means the device node is still not in the `i2c` group. The agent logs that once and omits brightness instead of sending 0%.
