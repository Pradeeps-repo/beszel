package agent

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/henrygd/beszel/internal/entities/system"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBrightnessCollectionEnabled(t *testing.T) {
	t.Setenv("BESZEL_AGENT_BRIGHTNESS", "")
	require.NoError(t, os.Unsetenv("BESZEL_AGENT_BRIGHTNESS"))
	require.NoError(t, os.Unsetenv("BRIGHTNESS"))
	assert.True(t, brightnessCollectionEnabled())

	for _, value := range []string{"false", "FALSE", "0", "off", "no"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("BRIGHTNESS", value)
			assert.False(t, brightnessCollectionEnabled())
		})
	}
	t.Setenv("BRIGHTNESS", "false")
	t.Setenv("BESZEL_AGENT_BRIGHTNESS", "true")
	assert.True(t, brightnessCollectionEnabled())
}

func TestBrightnessPercent(t *testing.T) {
	pct, ok := brightnessPercent(50, 100)
	assert.True(t, ok)
	assert.Equal(t, uint8(50), pct)

	pct, ok = brightnessPercent(0, 100)
	assert.True(t, ok)
	assert.Equal(t, uint8(0), pct)

	pct, ok = brightnessPercent(1, 3)
	assert.True(t, ok)
	assert.Equal(t, uint8(33), pct)

	pct, ok = brightnessPercent(128, 255)
	assert.True(t, ok)
	assert.Equal(t, uint8(50), pct)

	_, ok = brightnessPercent(10, 0)
	assert.False(t, ok)
	_, ok = brightnessPercent(-1, 100)
	assert.False(t, ok)
}

func TestReadSysfsBrightness(t *testing.T) {
	root := t.TempDir()
	assert.Nil(t, readSysfsBrightness(filepath.Join(root, "missing")))

	writeBacklight(t, root, "acpi_video0", "firmware", "100", "100")
	writeBacklight(t, root, "intel_backlight", "raw", "32768", "65536")
	writeBacklight(t, root, "broken", "raw", "nope", "100")
	writeBacklight(t, root, "zero-max", "raw", "0", "0")

	pct := readSysfsBrightness(root)
	require.NotNil(t, pct)
	assert.Equal(t, uint8(50), *pct)

	off := t.TempDir()
	writeBacklight(t, off, "panel", "raw", "0", "100")
	pct = readSysfsBrightness(off)
	require.NotNil(t, pct)
	assert.Equal(t, uint8(0), *pct)

	actualOnly := t.TempDir()
	dir := filepath.Join(actualOnly, "panel")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "actual_brightness"), []byte("25\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "max_brightness"), []byte("100\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "type"), []byte("platform\n"), 0o644))
	pct = readSysfsBrightness(actualOnly)
	require.NotNil(t, pct)
	assert.Equal(t, uint8(25), *pct)

	tie := t.TempDir()
	writeBacklight(t, tie, "card1", "raw", "80", "100")
	writeBacklight(t, tie, "card0", "raw", "400", "1000")
	pct = readSysfsBrightness(tie)
	require.NotNil(t, pct)
	assert.Equal(t, uint8(40), *pct)
}

func TestParseDDCBrightness(t *testing.T) {
	pct, ok := parseDDCBrightness("VCP 10 C 50 100\n")
	require.True(t, ok)
	require.NotNil(t, pct)
	assert.Equal(t, uint8(50), *pct)

	pct, ok = parseDDCBrightness("VCP code 0x10 (Brightness): current value =    0, max value =   100\n")
	require.True(t, ok)
	require.NotNil(t, pct)
	assert.Equal(t, uint8(0), *pct)

	_, ok = parseDDCBrightness("VCP 10 C 50 0\n")
	assert.False(t, ok)
	_, ok = parseDDCBrightness("no display\n")
	assert.False(t, ok)
}

func TestBrightnessFromWMI(t *testing.T) {
	assert.Nil(t, brightnessFromWMI(nil))
	assert.Nil(t, brightnessFromWMI([]wmiMonitorBrightness{{InstanceName: "a", CurrentBrightness: 50, Active: false}}))
	assert.Nil(t, brightnessFromWMI([]wmiMonitorBrightness{{InstanceName: "a", CurrentBrightness: 150, Active: true}}))

	pct := brightnessFromWMI([]wmiMonitorBrightness{
		{InstanceName: "b", CurrentBrightness: 80, Active: true},
		{InstanceName: "a", CurrentBrightness: 0, Active: true},
	})
	require.NotNil(t, pct)
	assert.Equal(t, uint8(0), *pct)
}

func TestUpdateBrightness(t *testing.T) {
	origPlatform := platformBrightnessFn
	origExternal := externalBrightnessFn
	t.Cleanup(func() {
		platformBrightnessFn = origPlatform
		externalBrightnessFn = origExternal
	})

	t.Run("disabled leaves stats unchanged", func(t *testing.T) {
		t.Setenv("BRIGHTNESS", "false")
		calls := 0
		platformBrightnessFn = func() *uint8 {
			calls++
			pct := uint8(50)
			return &pct
		}
		agent := &Agent{}
		stats := system.Stats{Cpu: 12.5}
		agent.updateBrightness(defaultDataCacheTimeMs, &stats)
		assert.Equal(t, 0, calls)
		assert.Nil(t, stats.Brightness)
		assert.Equal(t, 12.5, stats.Cpu)
	})

	t.Run("platform value and zero are kept", func(t *testing.T) {
		t.Setenv("BESZEL_AGENT_BRIGHTNESS", "true")
		externalCalls := 0
		externalBrightnessFn = func() (*uint8, error) {
			externalCalls++
			return nil, errors.New("should not run")
		}
		for _, want := range []uint8{50, 0} {
			pct := want
			platformBrightnessFn = func() *uint8 { return &pct }
			agent := &Agent{}
			stats := system.Stats{Cpu: 3}
			agent.updateBrightness(defaultDataCacheTimeMs, &stats)
			require.NotNil(t, stats.Brightness)
			assert.Equal(t, want, *stats.Brightness)
			assert.Equal(t, 3.0, stats.Cpu)
			assert.Equal(t, 0, externalCalls)
		}
	})

	t.Run("platform panic does not escape", func(t *testing.T) {
		t.Setenv("BRIGHTNESS", "true")
		platformBrightnessFn = func() *uint8 { panic("backlight") }
		externalBrightnessFn = func() (*uint8, error) { return nil, nil }
		agent := &Agent{}
		stats := system.Stats{Cpu: 9}
		assert.NotPanics(t, func() {
			agent.updateBrightness(defaultDataCacheTimeMs, &stats)
		})
		assert.Nil(t, stats.Brightness)
		assert.Equal(t, 9.0, stats.Cpu)
	})

	t.Run("external fallback is absent until the probe finishes", func(t *testing.T) {
		t.Setenv("BRIGHTNESS", "true")
		platformBrightnessFn = func() *uint8 { return nil }
		externalBrightnessFn = func() (*uint8, error) {
			pct := uint8(50)
			return &pct, nil
		}
		agent := &Agent{}
		stats := system.Stats{Cpu: 4}
		agent.updateBrightness(defaultDataCacheTimeMs, &stats)
		assert.Nil(t, stats.Brightness, "the stats snapshot must not wait on DDC/CI")
		assert.Equal(t, 4.0, stats.Cpu)
		require.Eventually(t, func() bool {
			agent.Lock()
			defer agent.Unlock()
			return agent.brightnessCached && agent.brightness != nil && *agent.brightness == 50 && !agent.brightnessProbe.Load()
		}, time.Second, 10*time.Millisecond)

		externalBrightnessFn = func() (*uint8, error) {
			t.Error("short interval must reuse the cached reading")
			return nil, errors.New("again")
		}
		stats = system.Stats{}
		agent.updateBrightness(1000, &stats)
		require.NotNil(t, stats.Brightness)
		assert.Equal(t, uint8(50), *stats.Brightness)
	})

	t.Run("external panic stays in the probe", func(t *testing.T) {
		t.Setenv("BRIGHTNESS", "true")
		platformBrightnessFn = func() *uint8 { return nil }
		externalBrightnessFn = func() (*uint8, error) { panic("ddc") }
		agent := &Agent{}
		stats := system.Stats{Cpu: 6}
		assert.NotPanics(t, func() {
			agent.updateBrightness(defaultDataCacheTimeMs, &stats)
		})
		assert.Equal(t, 6.0, stats.Cpu)
		require.Eventually(t, func() bool {
			agent.Lock()
			defer agent.Unlock()
			return agent.brightnessCached && agent.brightness == nil && !agent.brightnessProbe.Load()
		}, time.Second, 10*time.Millisecond)
	})

	t.Run("external error stays absent", func(t *testing.T) {
		t.Setenv("BRIGHTNESS", "true")
		platformBrightnessFn = func() *uint8 { return nil }
		externalBrightnessFn = func() (*uint8, error) { return nil, errors.New("ddc failed") }
		agent := &Agent{}
		stats := system.Stats{Cpu: 8}
		agent.updateBrightness(defaultDataCacheTimeMs, &stats)
		assert.Equal(t, 8.0, stats.Cpu)
		require.Eventually(t, func() bool {
			agent.Lock()
			defer agent.Unlock()
			return agent.brightnessCached && agent.brightness == nil && !agent.brightnessProbe.Load()
		}, time.Second, 10*time.Millisecond)
	})
}

func writeBacklight(t *testing.T, root, name, kind, current, max string) {
	t.Helper()
	dir := filepath.Join(root, name)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "type"), []byte(kind+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "brightness"), []byte(current+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "max_brightness"), []byte(max+"\n"), 0o644))
}
