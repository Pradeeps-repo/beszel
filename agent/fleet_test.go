package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/henrygd/beszel/internal/entities/system"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadDRMDisplay(t *testing.T) {
	root := t.TempDir()
	writeDRM(t, root, "card0-HDMI-A-1", "connected", "1920x1080\n1280x720\n")
	writeDRM(t, root, "card0-eDP-1", "connected", "1080x1920\n")
	writeDRM(t, root, "card0-DP-1", "disconnected", "1920x1080\n")
	writeDRM(t, root, "card0", "", "")

	snap := readDRMDisplay(root)
	assert.Equal(t, "1080x1920", snap.Resolution)
	assert.Equal(t, "portrait", snap.Orientation)

	empty := t.TempDir()
	assert.Empty(t, readDRMDisplay(empty).Resolution)
	assert.Empty(t, readDRMDisplay(filepath.Join(root, "missing")).Resolution)
}

func TestParseXrandr(t *testing.T) {
	output := "" +
		"Screen 0: minimum 8 x 8, current 1080 x 1920, maximum 32767 x 32767\n" +
		"HDMI-1 connected primary 1080x1920+0+0 right (normal left inverted right x axis y axis) 509mm x 286mm\n" +
		"   1920x1080     60.00*+\n"
	snap := parseXrandr(output)
	assert.Equal(t, "1080x1920", snap.Resolution)
	assert.Equal(t, "portrait (270°)", snap.Orientation)

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
	packageSearchRoots = []string{filepath.Join(root, "opt")}
	t.Cleanup(func() { packageSearchRoots = orig })

	versions := readAppVersions()
	assert.Equal(t, "2.0.0", versions.SsApp, "an explicit path wins over discovery")
	assert.Empty(t, versions.SsIot, "a package.json without a version is omitted")
}

func TestUpdateFleetInfo(t *testing.T) {
	origDisplay := collectDisplay
	origRoots := packageSearchRoots
	t.Cleanup(func() {
		collectDisplay = origDisplay
		packageSearchRoots = origRoots
	})
	collectDisplay = func() fleetSnapshot {
		return fleetSnapshot{Resolution: "1080x1920", Orientation: "portrait"}
	}
	root := t.TempDir()
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
	assert.Equal(t, "Debian GNU/Linux 12 (bookworm)", agent.systemInfo.OsRelease)
	assert.Equal(t, "6.8.0-1", agent.systemInfo.KernelVersion)

	collectDisplay = func() fleetSnapshot { panic("display") }
	agent.fleetAt = time.Time{}
	assert.NotPanics(t, agent.updateFleetInfo)
	assert.Equal(t, 12.5, agent.systemInfo.Cpu)
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
