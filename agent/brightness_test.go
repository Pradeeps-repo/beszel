package agent

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
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

	// The fleet's displays do not always use a max of 100.
	pct, ok = parseDDCBrightness("VCP code 0x10 (Brightness): current value =    25, max value =   200\n")
	require.True(t, ok)
	require.NotNil(t, pct)
	assert.Equal(t, uint8(13), *pct)

	pct, ok = parseDDCBrightness("VCP 10 C 25 200\n")
	require.True(t, ok)
	require.NotNil(t, pct)
	assert.Equal(t, uint8(13), *pct)

	_, ok = parseDDCBrightness("VCP 10 C 50 0\n")
	assert.False(t, ok)
	_, ok = parseDDCBrightness("current value = 50\n")
	assert.False(t, ok, "a missing max must not be treated as 100")
	_, ok = parseDDCBrightness("no display\n")
	assert.False(t, ok)
}

func TestBrightnessReadSource(t *testing.T) {
	t.Setenv("BRIGHTNESS_SOURCE", "")
	require.NoError(t, os.Unsetenv("BRIGHTNESS_SOURCE"))
	require.NoError(t, os.Unsetenv("BESZEL_AGENT_BRIGHTNESS_SOURCE"))
	assert.Equal(t, brightnessSourceAuto, brightnessReadSource())

	t.Setenv("BRIGHTNESS_SOURCE", "backlight")
	assert.Equal(t, brightnessSourceBacklight, brightnessReadSource())
	t.Setenv("BESZEL_AGENT_BRIGHTNESS_SOURCE", "ddc")
	assert.Equal(t, brightnessSourceDDC, brightnessReadSource())
	t.Setenv("BESZEL_AGENT_BRIGHTNESS_SOURCE", "nope")
	assert.Equal(t, brightnessSourceAuto, brightnessReadSource())
}

func TestClassifyDDCFailure(t *testing.T) {
	err := classifyDDCFailure("Opening bus /dev/i2c-2: Permission denied\n", errors.New("exit status 1"))
	assert.ErrorIs(t, err, errDDCDenied)

	err = classifyDDCFailure("Access to I2C device /dev/i2c-2 is restricted. See https://www.ddcutil.com/i2c_permissions/\n", errors.New("exit status 1"))
	assert.ErrorIs(t, err, errDDCDenied)

	err = classifyDDCFailure("Display not found\n", errors.New("exit status 1"))
	assert.ErrorIs(t, err, errDDCNoDisplay)
	assert.NotErrorIs(t, err, errDDCDenied)

	err = classifyDDCFailure("", errors.New("exit status 1"))
	assert.ErrorIs(t, err, errDDCFailed)
	assert.NotErrorIs(t, err, errDDCNoDisplay)
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
	origAvailable := ddcutilAvailableFn
	origWatch := watchDisplayPath
	watchDisplayPath = filepath.Join(t.TempDir(), "missing.json")
	t.Cleanup(func() {
		platformBrightnessFn = origPlatform
		externalBrightnessFn = origExternal
		ddcutilAvailableFn = origAvailable
		watchDisplayPath = origWatch
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
		agent.updateBrightness(&stats)
		assert.Equal(t, 0, calls)
		assert.Nil(t, stats.Brightness)
		assert.Equal(t, 12.5, stats.Cpu)
	})

	t.Run("backlight source does not call ddcutil", func(t *testing.T) {
		t.Setenv("BRIGHTNESS_SOURCE", "backlight")
		ddcutilAvailableFn = func() bool { return true }
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
			agent.updateBrightness(&stats)
			require.NotNil(t, stats.Brightness)
			assert.Equal(t, want, *stats.Brightness)
			assert.Equal(t, 3.0, stats.Cpu)
			assert.Equal(t, 0, externalCalls)
		}
	})

	t.Run("auto uses ddcutil when a display responds", func(t *testing.T) {
		t.Setenv("BRIGHTNESS_SOURCE", "auto")
		ddcutilAvailableFn = func() bool { return true }
		platformCalls := 0
		platformBrightnessFn = func() *uint8 {
			platformCalls++
			pct := uint8(80)
			return &pct
		}
		externalBrightnessFn = func() (*uint8, error) {
			pct := uint8(0)
			return &pct, nil
		}
		agent := &Agent{}
		stats := system.Stats{Cpu: 4}
		agent.updateBrightness(&stats)
		assert.Nil(t, stats.Brightness, "the stats snapshot must not wait on DDC/CI")
		assert.Equal(t, 4.0, stats.Cpu)
		require.Eventually(t, func() bool {
			agent.brightnessMu.Lock()
			defer agent.brightnessMu.Unlock()
			return agent.brightnessCached && agent.brightness != nil && *agent.brightness == 0 && !agent.brightnessProbe.Load()
		}, time.Second, 10*time.Millisecond)
		assert.Equal(t, 0, platformCalls, "a DDC reading of 0% must not fall back to backlight")

		calls := 0
		externalBrightnessFn = func() (*uint8, error) {
			calls++
			t.Error("cached reading must be reused")
			return nil, errDDCFailed
		}
		stats = system.Stats{}
		agent.updateBrightness(&stats)
		require.NotNil(t, stats.Brightness)
		assert.Equal(t, uint8(0), *stats.Brightness)
		assert.Equal(t, 0, calls)
	})

	t.Run("auto falls back to backlight when no display responds", func(t *testing.T) {
		t.Setenv("BRIGHTNESS_SOURCE", "auto")
		ddcutilAvailableFn = func() bool { return true }
		externalBrightnessFn = func() (*uint8, error) { return nil, errDDCNoDisplay }
		platformBrightnessFn = func() *uint8 {
			pct := uint8(25)
			return &pct
		}
		agent := &Agent{}
		stats := system.Stats{}
		agent.updateBrightness(&stats)
		assert.Nil(t, stats.Brightness)
		require.Eventually(t, func() bool {
			agent.brightnessMu.Lock()
			defer agent.brightnessMu.Unlock()
			return agent.brightnessCached && agent.brightness != nil && *agent.brightness == 25 && !agent.brightnessProbe.Load()
		}, time.Second, 10*time.Millisecond)
	})

	t.Run("auto without ddcutil reads backlight inline", func(t *testing.T) {
		t.Setenv("BRIGHTNESS_SOURCE", "auto")
		ddcutilAvailableFn = func() bool { return false }
		externalCalls := 0
		externalBrightnessFn = func() (*uint8, error) {
			externalCalls++
			return nil, errDDCMissing
		}
		platformBrightnessFn = func() *uint8 {
			pct := uint8(33)
			return &pct
		}
		agent := &Agent{}
		stats := system.Stats{Cpu: 2}
		agent.updateBrightness(&stats)
		require.NotNil(t, stats.Brightness)
		assert.Equal(t, uint8(33), *stats.Brightness)
		assert.Equal(t, 2.0, stats.Cpu)
		assert.Equal(t, 0, externalCalls)
	})

	t.Run("permission failure uses a fresh helper brightness", func(t *testing.T) {
		t.Setenv("BRIGHTNESS_SOURCE", "auto")
		dir := t.TempDir()
		orig := watchDisplayPath
		watchDisplayPath = filepath.Join(dir, "display.json")
		t.Cleanup(func() { watchDisplayPath = orig })
		payload, err := json.Marshal(map[string]any{
			"updated":    time.Now().UTC().Format(time.RFC3339),
			"brightness": 100,
		})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(watchDisplayPath, payload, 0o644))
		ddcutilAvailableFn = func() bool { return true }
		platformCalls := 0
		platformBrightnessFn = func() *uint8 {
			platformCalls++
			pct := uint8(50)
			return &pct
		}
		externalBrightnessFn = func() (*uint8, error) {
			return nil, classifyDDCFailure("Opening /dev/i2c-2: Permission denied", errors.New("exit status 1"))
		}
		agent := &Agent{}
		agent.updateBrightness(&system.Stats{})
		require.Eventually(t, func() bool {
			agent.brightnessMu.Lock()
			defer agent.brightnessMu.Unlock()
			return agent.brightnessCached && agent.brightness != nil && *agent.brightness == 100 && !agent.brightnessProbe.Load()
		}, time.Second, 10*time.Millisecond)
		assert.Equal(t, 0, platformCalls)
	})

	t.Run("permission failure is absent and does not use backlight", func(t *testing.T) {
		t.Setenv("BRIGHTNESS_SOURCE", "auto")
		orig := watchDisplayPath
		watchDisplayPath = filepath.Join(t.TempDir(), "missing.json")
		t.Cleanup(func() { watchDisplayPath = orig })
		ddcutilAvailableFn = func() bool { return true }
		platformCalls := 0
		platformBrightnessFn = func() *uint8 {
			platformCalls++
			pct := uint8(50)
			return &pct
		}
		externalBrightnessFn = func() (*uint8, error) {
			return nil, classifyDDCFailure("Opening /dev/i2c-2: Permission denied", errors.New("exit status 1"))
		}
		agent := &Agent{}
		stats := system.Stats{Cpu: 8}
		agent.updateBrightness(&stats)
		assert.Equal(t, 8.0, stats.Cpu)
		require.Eventually(t, func() bool {
			agent.brightnessMu.Lock()
			defer agent.brightnessMu.Unlock()
			return agent.brightnessCached && agent.brightness == nil && !agent.brightnessProbe.Load()
		}, time.Second, 10*time.Millisecond)
		assert.Nil(t, stats.Brightness)
		assert.Equal(t, 0, platformCalls)
	})

	t.Run("forced ddc does not fall back", func(t *testing.T) {
		t.Setenv("BRIGHTNESS_SOURCE", "ddc")
		ddcutilAvailableFn = func() bool { return true }
		platformCalls := 0
		platformBrightnessFn = func() *uint8 {
			platformCalls++
			pct := uint8(50)
			return &pct
		}
		externalBrightnessFn = func() (*uint8, error) { return nil, errDDCNoDisplay }
		agent := &Agent{}
		stats := system.Stats{}
		agent.updateBrightness(&stats)
		require.Eventually(t, func() bool {
			agent.brightnessMu.Lock()
			defer agent.brightnessMu.Unlock()
			return agent.brightnessCached && agent.brightness == nil && !agent.brightnessProbe.Load()
		}, time.Second, 10*time.Millisecond)
		assert.Equal(t, 0, platformCalls)
	})

	t.Run("ddc probe is not concurrent and a failure keeps the last reading", func(t *testing.T) {
		t.Setenv("BRIGHTNESS_SOURCE", "ddc")
		ddcutilAvailableFn = func() bool { return true }
		started := make(chan struct{})
		release := make(chan struct{})
		var calls atomicInt
		externalBrightnessFn = func() (*uint8, error) {
			if calls.add() == 1 {
				close(started)
			}
			<-release
			pct := uint8(40)
			return &pct, nil
		}
		agent := &Agent{}
		agent.updateBrightness(&system.Stats{})
		agent.updateBrightness(&system.Stats{})
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("probe did not start")
		}
		assert.Equal(t, 1, calls.value())
		close(release)
		require.Eventually(t, func() bool {
			agent.brightnessMu.Lock()
			defer agent.brightnessMu.Unlock()
			return agent.brightnessCached && agent.brightness != nil && *agent.brightness == 40 && !agent.brightnessProbe.Load()
		}, time.Second, 10*time.Millisecond)

		externalBrightnessFn = func() (*uint8, error) { return nil, errDDCFailed }
		agent.brightnessMu.Lock()
		agent.brightnessAt = time.Now().Add(-brightnessRefreshInterval - time.Second)
		agent.brightnessMu.Unlock()
		agent.updateBrightness(&system.Stats{})
		require.Eventually(t, func() bool {
			agent.brightnessMu.Lock()
			defer agent.brightnessMu.Unlock()
			return !agent.brightnessProbe.Load() && agent.brightness != nil && *agent.brightness == 40
		}, time.Second, 10*time.Millisecond)
	})

	t.Run("platform panic does not escape", func(t *testing.T) {
		t.Setenv("BRIGHTNESS_SOURCE", "backlight")
		platformBrightnessFn = func() *uint8 { panic("backlight") }
		agent := &Agent{}
		stats := system.Stats{Cpu: 9}
		assert.NotPanics(t, func() {
			agent.updateBrightness(&stats)
		})
		assert.Nil(t, stats.Brightness)
		assert.Equal(t, 9.0, stats.Cpu)
	})

	t.Run("external panic stays in the probe", func(t *testing.T) {
		t.Setenv("BRIGHTNESS_SOURCE", "ddc")
		ddcutilAvailableFn = func() bool { return true }
		platformBrightnessFn = func() *uint8 { return nil }
		externalBrightnessFn = func() (*uint8, error) { panic("ddc") }
		agent := &Agent{}
		stats := system.Stats{Cpu: 6}
		assert.NotPanics(t, func() {
			agent.updateBrightness(&stats)
		})
		assert.Equal(t, 6.0, stats.Cpu)
		require.Eventually(t, func() bool {
			agent.brightnessMu.Lock()
			defer agent.brightnessMu.Unlock()
			return agent.brightnessCached && agent.brightness == nil && !agent.brightnessProbe.Load()
		}, time.Second, 10*time.Millisecond)
	})
}

// atomicInt is a tiny counter for tests. The agent package's race-detector run
// shares the probe function across goroutines.
type atomicInt struct {
	mu sync.Mutex
	n  int
}

func (a *atomicInt) add() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.n++
	return a.n
}

func (a *atomicInt) value() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.n
}

func writeBacklight(t *testing.T, root, name, kind, current, max string) {
	t.Helper()
	dir := filepath.Join(root, name)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "type"), []byte(kind+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "brightness"), []byte(current+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "max_brightness"), []byte(max+"\n"), 0o644))
}
