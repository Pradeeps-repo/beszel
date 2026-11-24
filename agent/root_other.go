//go:build !unix

package agent

func runningAsRoot() bool {
	return false
}
