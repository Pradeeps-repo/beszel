package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/henrygd/beszel/internal/entities/system"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadDRMDisplay(t *testing.T) {
	// The first line of modes is the panel maximum, not the live mode.
	root := t.TempDir()
	writeDRM(t, root, "card0-HDMI-A-2", "connected", "3840x2160\n1920x1080\n")
	writeDRM(t, root, "card0-eDP-1", "connected", "1080x1920\n")

	snap := readDRMDisplay(root)
	assert.Empty(t, snap.Resolution)
	assert.Empty(t, snap.Orientation)
	assert.Empty(t, snap.Mode)

	state := "" +
		"crtc[51]: crtc-0\n" +
		"	enable=yes\n" +
		"	active=yes\n" +
		"	mode: \"1920x1080\": 60 148500 1920 2008 2052 2200 1080 1084 1089 1125 0x40 0x5\n" +
		"crtc[52]: crtc-1\n" +
		"	mode: \"3840x2160\": 30 0\n" +
		"connector[67]: HDMI-A-2\n" +
		"	status: connected\n" +
		"	crtc=crtc-0\n" +
		"connector[68]: DP-1\n" +
		"	status: disconnected\n" +
		"	crtc=crtc-1\n"
	mode, output, ok := parseDRMDebugState(state)
	require.True(t, ok)
	assert.Equal(t, "1920x1080", mode)
	assert.Equal(t, "HDMI-A-2", output)

	require.NoError(t, os.MkdirAll(filepath.Join(root, "0"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "0", "state"), []byte(state), 0o644))
	snap = readDRMCurrent(root)
	assert.Equal(t, "1920x1080", snap.Mode)
	assert.Equal(t, "HDMI-A-2", snap.Output)
	assert.Empty(t, snap.Resolution, "DRM state has no rotation, so the logical size stays unknown")
	assert.Empty(t, snap.Orientation)

	assert.Empty(t, readDRMDisplay(t.TempDir()).Resolution)
	assert.Empty(t, readDRMDisplay(filepath.Join(root, "missing")).Mode)
}

func TestParseXrandr(t *testing.T) {
	output := "" +
		"Screen 0: minimum 8 x 8, current 1080 x 1920, maximum 32767 x 32767\n" +
		"HDMI-1 connected primary 1080x1920+0+0 right (normal left inverted right x axis y axis) 509mm x 286mm\n" +
		"   1920x1080     60.00*+\n"
	snap := parseXrandr(output)
	assert.Equal(t, "1080x1920", snap.Resolution)
	assert.Equal(t, "1920x1080", snap.Mode)
	assert.Equal(t, "HDMI-1", snap.Output)
	assert.Equal(t, "portrait (270°)", snap.Orientation)

	portrait := "" +
		"HDMI-A-2 connected primary 1080x1920+0+0 left (normal left inverted right x axis y axis) 600mm x 340mm\n" +
		"   3840x2160     30.00 +\n" +
		"   1920x1080     60.00*+\n"
	snap = parseXrandr(portrait)
	assert.Equal(t, "1080x1920", snap.Resolution)
	assert.Equal(t, "1920x1080", snap.Mode)
	assert.Equal(t, "portrait (90°)", snap.Orientation)

	xwayland := "XWAYLAND0 connected 1080x1920+0+0 (normal left inverted right x axis y axis) 340mm x 620mm\n"
	snap = parseXrandr(xwayland)
	assert.Equal(t, "XWAYLAND0", snap.Output)
	assert.Equal(t, "1080x1920", snap.Resolution)
	assert.Equal(t, "1080x1920", snap.Mode, "XWayland geometry is the current resolution when there is no starred mode")
	assert.Equal(t, "portrait", snap.Orientation)
	assert.NotContains(t, snap.Orientation, "°")

	normal := "eDP-1 connected 1920x1080+0+0 normal (normal left inverted right)\n"
	snap = parseXrandr(normal)
	assert.Equal(t, "1920x1080", snap.Resolution)
	assert.Equal(t, "landscape", snap.Orientation)
	assert.Empty(t, parseXrandr("no outputs\n").Resolution)
}

func TestParseWlrRandr(t *testing.T) {
	output := "" +
		"HDMI-A-1 \"Panel\"\n" +
		"  Enabled: yes\n" +
		"  Modes:\n" +
		"    1920x1080 px, 60.000000 Hz (current)\n" +
		"  Transform: 90\n"
	snap := parseWlrRandr(output)
	assert.Equal(t, "1080x1920", snap.Resolution)
	assert.Equal(t, "portrait (90°)", snap.Orientation)

	disabled := "HDMI-A-1\n  Enabled: no\n  Modes:\n    1920x1080 px (current)\n  Transform: normal\n"
	assert.Empty(t, parseWlrRandr(disabled).Resolution)
}

func TestDiscoverPackageVersions(t *testing.T) {
	root := t.TempDir()
	origApp := defaultAppPackage
	origIot := defaultIotPackage
	t.Cleanup(func() {
		defaultAppPackage = origApp
		defaultIotPackage = origIot
	})
	defaultAppPackage = filepath.Join(root, "missing-app", "package.json")
	defaultIotPackage = filepath.Join(root, "missing-iot", "package.json")
	writePackage(t, filepath.Join(root, "opt", "kiosk", "ss-app"), "ss-app", "1.4.2")
	writePackage(t, filepath.Join(root, "opt", "other"), "unrelated", "9.9.9")
	writePackage(t, filepath.Join(root, "srv", "ss-iot"), "ss-iot", "0.3.1")

	found := discoverPackageVersions([]string{filepath.Join(root, "opt"), filepath.Join(root, "srv")})
	assert.Equal(t, "1.4.2", found.SsApp)
	assert.Equal(t, "0.3.1", found.SsIot)

	assert.Empty(t, versionFromPackageJSON(filepath.Join(root, "opt", "missing", "package.json"), ""))
	assert.Empty(t, versionFromPackageJSON(filepath.Join(root, "opt", "other", "package.json"), "ss-app"))
}

func TestReadAppVersionsEnv(t *testing.T) {
	root := t.TempDir()
	app := writePackage(t, filepath.Join(root, "explicit-app"), "ignored", "2.0.0")
	iot := writePackage(t, filepath.Join(root, "explicit-iot"), "ignored", "")
	writePackage(t, filepath.Join(root, "opt", "ss-app"), "ss-app", "9.0.0")

	t.Setenv("SS_APP_PACKAGE_JSON", app)
	t.Setenv("SS_IOT_PACKAGE_JSON", iot)
	orig := packageSearchRoots
	origApps := watchAppsPath
	packageSearchRoots = []string{filepath.Join(root, "opt")}
	watchAppsPath = filepath.Join(root, "apps.json")
	t.Cleanup(func() {
		packageSearchRoots = orig
		watchAppsPath = origApps
	})

	versions := readAppVersions()
	assert.Equal(t, "2.0.0", versions.SsApp, "an explicit path wins over discovery")
	assert.Empty(t, versions.SsIot, "a package.json without a version is omitted")
}

func TestUpdateFleetInfo(t *testing.T) {
	origDisplay := collectDisplay
	origRoots := packageSearchRoots
	origApp := defaultAppPackage
	origIot := defaultIotPackage
	origApps := watchAppsPath
	t.Cleanup(func() {
		collectDisplay = origDisplay
		packageSearchRoots = origRoots
		defaultAppPackage = origApp
		defaultIotPackage = origIot
		watchAppsPath = origApps
	})
	watchAppsPath = filepath.Join(t.TempDir(), "apps.json")
	collectDisplay = func() fleetSnapshot {
		return fleetSnapshot{Resolution: "1080x1920", Orientation: "portrait"}
	}
	root := t.TempDir()
	defaultAppPackage = filepath.Join(root, "missing-app", "package.json")
	defaultIotPackage = filepath.Join(root, "missing-iot", "package.json")
	writePackage(t, filepath.Join(root, "ss-app"), "ss-app", "1.2.3")
	packageSearchRoots = []string{root}
	t.Setenv("SS_APP_PACKAGE_JSON", "")
	require.NoError(t, os.Unsetenv("SS_APP_PACKAGE_JSON"))
	require.NoError(t, os.Unsetenv("SS_IOT_PACKAGE_JSON"))
	require.NoError(t, os.Unsetenv("BESZEL_AGENT_SS_APP_PACKAGE_JSON"))
	require.NoError(t, os.Unsetenv("BESZEL_AGENT_SS_IOT_PACKAGE_JSON"))

	agent := &Agent{systemDetails: system.Details{OsName: "Debian GNU/Linux 12 (bookworm)", Kernel: "6.8.0-1"}}
	agent.systemInfo.Cpu = 12.5
	agent.updateFleetInfo()
	assert.Equal(t, 12.5, agent.systemInfo.Cpu)
	assert.Equal(t, "1080x1920", agent.systemInfo.Screen)
	assert.Equal(t, "portrait", agent.systemInfo.Orientation)
	assert.Equal(t, "1.2.3", agent.systemInfo.SsApp)
	assert.Empty(t, agent.systemInfo.SsIot)
	assert.Empty(t, agent.systemInfo.Mode)
	assert.Equal(t, "Debian GNU/Linux 12 (bookworm)", agent.systemInfo.OsRelease)
	assert.Equal(t, "6.8.0-1", agent.systemInfo.KernelVersion)

	collectDisplay = func() fleetSnapshot { panic("display") }
	agent.fleetAt = time.Time{}
	assert.NotPanics(t, agent.updateFleetInfo)
	assert.Equal(t, 12.5, agent.systemInfo.Cpu)
}

func TestWatchDisplayAndApps(t *testing.T) {
	dir := t.TempDir()
	origDisplay := watchDisplayPath
	origApps := watchAppsPath
	origApp := defaultAppPackage
	origIot := defaultIotPackage
	origRoots := packageSearchRoots
	t.Cleanup(func() {
		watchDisplayPath = origDisplay
		watchAppsPath = origApps
		defaultAppPackage = origApp
		defaultIotPackage = origIot
		packageSearchRoots = origRoots
	})
	watchDisplayPath = filepath.Join(dir, "display.json")
	watchAppsPath = filepath.Join(dir, "apps.json")
	require.NoError(t, os.Unsetenv("SS_APP_PACKAGE_JSON"))
	require.NoError(t, os.Unsetenv("SS_IOT_PACKAGE_JSON"))
	require.NoError(t, os.Unsetenv("BESZEL_AGENT_SS_APP_PACKAGE_JSON"))
	require.NoError(t, os.Unsetenv("BESZEL_AGENT_SS_IOT_PACKAGE_JSON"))

	writeWatch(t, watchDisplayPath, map[string]any{
		"updated":     time.Now().UTC().Format(time.RFC3339),
		"output":      "HDMI-A-2",
		"mode":        "1920x1080",
		"resolution":  "1080x1920",
		"orientation": "portrait (90°)",
		"brightness":  0,
	})
	writeWatch(t, watchAppsPath, map[string]any{
		"updated": time.Now().UTC().Format(time.RFC3339),
		"ss_app":  "1.8.0",
		"ss_iot":  "0.4.2",
	})

	snap, brightness, ok := readFreshWatchDisplay()
	require.True(t, ok)
	assert.Equal(t, "1080x1920", snap.Resolution)
	assert.Equal(t, "1920x1080", snap.Mode)
	assert.Equal(t, "portrait (90°)", snap.Orientation)
	require.NotNil(t, brightness)
	assert.Equal(t, uint8(0), *brightness)

	versions := readAppVersions()
	assert.Equal(t, "1.8.0", versions.SsApp)
	assert.Equal(t, "0.4.2", versions.SsIot)

	stale := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	writeWatch(t, watchAppsPath, map[string]any{"updated": stale, "ss_app": "9.9.9", "ss_iot": "9.9.9"})
	defaultAppPackage = writePackage(t, filepath.Join(dir, "smartselect"), "other-name", "3.1.0")
	defaultIotPackage = writePackage(t, filepath.Join(dir, "smartiot"), "other-name", "3.2.0")
	packageSearchRoots = []string{filepath.Join(dir, "search")}
	writePackage(t, filepath.Join(dir, "search", "ss-app"), "ss-app", "8.0.0")
	versions = readAppVersions()
	assert.Equal(t, "3.1.0", versions.SsApp, "the field package.json paths are the first discovery defaults")
	assert.Equal(t, "3.2.0", versions.SsIot)

	explicit := writePackage(t, filepath.Join(dir, "explicit"), "ignored", "2.2.2")
	t.Setenv("SS_APP_PACKAGE_JSON", explicit)
	versions = readAppVersions()
	assert.Equal(t, "2.2.2", versions.SsApp, "an explicit path wins over the helper and the defaults")
}

func TestParseXorgArgs(t *testing.T) {
	display, auth, ok := parseXorgArgs([]string{
		"/usr/lib/xorg/Xorg", ":0", "-seat", "seat0", "-auth", "/var/run/lightdm/root/:0", "-nolisten", "tcp",
	})
	require.True(t, ok)
	assert.Equal(t, ":0", display)
	assert.Equal(t, "/var/run/lightdm/root/:0", auth)

	_, _, ok = parseXorgArgs([]string{"/usr/sbin/sshd", "-D"})
	assert.False(t, ok)

	root := t.TempDir()
	authFile := filepath.Join(root, "auth")
	require.NoError(t, os.WriteFile(authFile, []byte("x"), 0o600))
	pid := filepath.Join(root, "42")
	require.NoError(t, os.MkdirAll(pid, 0o755))
	cmdline := append([]byte("/usr/lib/xorg/Xorg"), 0)
	cmdline = append(cmdline, []byte(":1")...)
	cmdline = append(cmdline, 0)
	cmdline = append(cmdline, []byte("-auth")...)
	cmdline = append(cmdline, 0)
	cmdline = append(cmdline, []byte(authFile)...)
	cmdline = append(cmdline, 0)
	require.NoError(t, os.WriteFile(filepath.Join(pid, "cmdline"), cmdline, 0o644))
	display, auth, ok = findXorgAuth(root)
	require.True(t, ok)
	assert.Equal(t, ":1", display)
	assert.Equal(t, authFile, auth)
}

func writeWatch(t *testing.T, path string, payload map[string]any) {
	t.Helper()
	data, err := json.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o644))
}

func writeDRM(t *testing.T, root, name, status, modes string) {
	t.Helper()
	dir := filepath.Join(root, name)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	if status != "" {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "status"), []byte(status+"\n"), 0o644))
	}
	if modes != "" {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "modes"), []byte(modes), 0o644))
	}
}

func writePackage(t *testing.T, dir, name, version string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, "package.json")
	body := `{"name":"` + name + `","version":"` + version + `"}`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return path
}
