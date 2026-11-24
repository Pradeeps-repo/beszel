package selfupdate

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// HelperOptions installs the same host setup as scripts/upgrade-agent.sh.
// Commands are fixed. Nothing in here comes from the hub.
type HelperOptions struct {
	Runner        func(name string, args ...string) error
	DdcutilExists bool
	Helper        []byte
	ServiceUser   string
}

// InstallFleetHelpers writes the i2c and display-helper files under root.
// An empty root means the real filesystem root. A second call with the same
// inputs does not rewrite files that already match.
func InstallFleetHelpers(root string, opts HelperOptions) error {
	if opts.Runner == nil {
		opts.Runner = func(name string, args ...string) error {
			return exec.Command(name, args...).Run()
		}
	}
	if !opts.DdcutilExists {
		if err := opts.Runner("apt-get", "update"); err != nil {
			return err
		}
		if err := opts.Runner("apt-get", "install", "-y", "ddcutil"); err != nil {
			return err
		}
	}
	modules := filepath.Join(root, "etc", "modules-load.d", "i2c-dev.conf")
	if err := writeIfChanged(modules, "i2c-dev\n"); err != nil {
		return err
	}
	rule := filepath.Join(root, "etc", "udev", "rules.d", "60-beszel-i2c.rules")
	if err := writeIfChanged(rule, "KERNEL==\"i2c-[0-9]*\", GROUP=\"i2c\", MODE=\"0660\"\n"); err != nil {
		return err
	}
	_ = opts.Runner("groupadd", "--system", "i2c")
	user := opts.ServiceUser
	if user == "" {
		user = "beszel"
	}
	if user != "root" {
		_ = opts.Runner("usermod", "-aG", "i2c", user)
	}
	dropin := filepath.Join(root, "etc", "systemd", "system", "beszel-agent.service.d", "10-i2c.conf")
	if err := writeIfChanged(dropin, "[Service]\nSupplementaryGroups=i2c\n"); err != nil {
		return err
	}
	if len(opts.Helper) > 0 {
		helperPath := filepath.Join(root, "opt", "beszel-agent", "bright-watch-display.sh")
		if !bytesContain(opts.Helper, "display.json") {
			return errors.New("display helper is not bright-watch-display.sh")
		}
		if err := writeIfChanged(helperPath, string(opts.Helper)); err != nil {
			return err
		}
		_ = os.Chmod(helperPath, 0o755)
	}
	unit := filepath.Join(root, "etc", "systemd", "system", "bright-watch-display.service")
	timer := filepath.Join(root, "etc", "systemd", "system", "bright-watch-display.timer")
	unitBody := "[Unit]\nDescription=Record display mode, brightness, and app versions for Beszel\n\n[Service]\nType=oneshot\nTimeoutStartSec=30\nExecStart=/opt/beszel-agent/bright-watch-display.sh\n"
	timerBody := "[Unit]\nDescription=Record display mode, brightness, and app versions for Beszel\n\n[Timer]\nOnBootSec=30s\nOnUnitActiveSec=2min\nAccuracySec=30s\nPersistent=true\n\n[Install]\nWantedBy=timers.target\n"
	changed, err := writeIfChangedReport(unit, unitBody)
	if err != nil {
		return err
	}
	timerChanged, err := writeIfChangedReport(timer, timerBody)
	if err != nil {
		return err
	}
	if changed || timerChanged {
		if err := opts.Runner("systemctl", "daemon-reload"); err != nil {
			return err
		}
		if err := opts.Runner("systemctl", "enable", "--now", "bright-watch-display.timer"); err != nil {
			return err
		}
	}
	return nil
}

func bytesContain(body []byte, needle string) bool {
	return strings.Contains(string(body), needle)
}

func writeIfChanged(path, body string) error {
	_, err := writeIfChangedReport(path, body)
	return err
}

func writeIfChangedReport(path, body string) (bool, error) {
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	current, err := os.ReadFile(path)
	if err == nil && string(current) == body {
		return false, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, []byte(body), 0o644)
}
