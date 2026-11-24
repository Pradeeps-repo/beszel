//go:build linux

package agent

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

func init() {
	collectDisplay = collectLinuxDisplay
}

const drmDebugRoot = "/sys/kernel/debug/dri"

// collectLinuxDisplay prefers the root helper's display.json. That file has
// the logical size after rotation. The agent's own xrandr is next, because
// the service user often cannot read the X authority file. DRM debugfs is
// only a last resort, and only for the current CRTC mode. The mode list in
// /sys/class/drm/*/modes is never the current mode.
func collectLinuxDisplay() fleetSnapshot {
	if snap, _, ok := readFreshWatchDisplay(); ok && snap.Resolution != "" {
		return snap
	}
	if snap, ok := sessionDisplay(); ok {
		return snap
	}
	if snap := readDRMCurrent(drmDebugRoot); snap.Mode != "" {
		return snap
	}
	return fleetSnapshot{}
}

func sessionDisplay() (fleetSnapshot, bool) {
	if snap, ok := queryXrandr(); ok {
		return snap, true
	}
	if snap, ok := queryWlrRandr(); ok {
		return snap, true
	}
	return fleetSnapshot{}, false
}

func queryXrandr() (fleetSnapshot, bool) {
	if _, err := exec.LookPath("xrandr"); err != nil {
		return fleetSnapshot{}, false
	}
	display := os.Getenv("DISPLAY")
	auth := os.Getenv("XAUTHORITY")
	if auth == "" {
		if foundDisplay, foundAuth, ok := findXorgAuth(procRoot); ok {
			if display == "" {
				display = foundDisplay
			}
			auth = foundAuth
		}
	}
	if display == "" {
		display = ":0"
	}
	if auth == "" {
		auth = firstExisting(xauthorityCandidates())
	}
	out, err := runDisplayCommand("xrandr", "--query", displayEnv(display, auth))
	if err != nil {
		return fleetSnapshot{}, false
	}
	snap := parseXrandr(string(out))
	return snap, snap.Resolution != ""
}

func queryWlrRandr() (fleetSnapshot, bool) {
	if _, err := exec.LookPath("wlr-randr"); err != nil {
		return fleetSnapshot{}, false
	}
	runtime := os.Getenv("XDG_RUNTIME_DIR")
	wayland := os.Getenv("WAYLAND_DISPLAY")
	if runtime == "" || wayland == "" {
		foundRuntime, foundDisplay, ok := findWaylandSession()
		if !ok {
			return fleetSnapshot{}, false
		}
		runtime, wayland = foundRuntime, foundDisplay
	}
	env := replaceEnv(os.Environ(), "XDG_RUNTIME_DIR", runtime)
	env = replaceEnv(env, "WAYLAND_DISPLAY", wayland)
	out, err := runDisplayCommand("wlr-randr", "", env)
	if err != nil {
		return fleetSnapshot{}, false
	}
	snap := parseWlrRandr(string(out))
	return snap, snap.Resolution != ""
}

func runDisplayCommand(name, arg string, env []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var cmd *exec.Cmd
	if arg == "" {
		cmd = exec.CommandContext(ctx, name)
	} else {
		cmd = exec.CommandContext(ctx, name, arg)
	}
	cmd.Env = env
	return cmd.Output()
}

func displayEnv(display, auth string) []string {
	env := replaceEnv(os.Environ(), "DISPLAY", display)
	if auth != "" {
		env = replaceEnv(env, "XAUTHORITY", auth)
	}
	return env
}

func replaceEnv(env []string, key, value string) []string {
	prefix := key + "="
	next := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			next = append(next, entry)
		}
	}
	return append(next, prefix+value)
}

func xauthorityCandidates() []string {
	candidates := []string{"/root/.Xauthority", "/var/lib/sddm/.Xauthority"}
	if home, err := os.ReadDir("/home"); err == nil {
		for _, entry := range home {
			if len(candidates) >= 6 {
				break
			}
			candidates = append(candidates, "/home/"+entry.Name()+"/.Xauthority")
		}
	}
	return candidates
}

func firstExisting(paths []string) string {
	for _, path := range paths {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

func findWaylandSession() (runtimeDir, display string, ok bool) {
	entries, err := os.ReadDir("/run/user")
	if err != nil {
		return "", "", false
	}
	for _, entry := range entries {
		runtime := "/run/user/" + entry.Name()
		sockets, err := os.ReadDir(runtime)
		if err != nil {
			continue
		}
		for _, socket := range sockets {
			if strings.HasPrefix(socket.Name(), "wayland-") {
				return runtime, socket.Name(), true
			}
		}
	}
	return "", "", false
}
