//go:build unix

package agent

import "os"

func runningAsRoot() bool {
	return os.Geteuid() == 0
}
