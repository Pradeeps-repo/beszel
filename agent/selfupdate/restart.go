package selfupdate

import (
	"errors"
	"os/exec"
)

func restartSystemd() error {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return errors.New("systemctl is not available")
	}
	if err := exec.Command("systemctl", "is-active", "beszel-agent.service").Run(); err != nil {
		return errors.New("beszel-agent.service is not active")
	}
	return exec.Command("systemctl", "restart", "beszel-agent.service").Run()
}
