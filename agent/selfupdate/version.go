package selfupdate

import (
	"os/exec"
	"strings"
)

func readBinaryVersion(path string) (string, error) {
	out, err := exec.Command(path, "--version").CombinedOutput()
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		if err != nil {
			return "", err
		}
		return "", exec.ErrNotFound
	}
	return fields[len(fields)-1], nil
}
