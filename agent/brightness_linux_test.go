//go:build linux

package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadDDCBrightnessCommand(t *testing.T) {
	t.Run("percent is current over max", func(t *testing.T) {
		dir := t.TempDir()
		writeFakeDDCUtil(t, dir, "#!/bin/sh\nprintf '%s\\n' 'VCP code 0x10 (Brightness): current value =    25, max value =   200'\n")
		t.Setenv("PATH", dir)
		pct, err := readDDCBrightness()
		require.NoError(t, err)
		require.NotNil(t, pct)
		assert.Equal(t, uint8(13), *pct)
	})

	t.Run("permission denied is not a zero reading", func(t *testing.T) {
		dir := t.TempDir()
		writeFakeDDCUtil(t, dir, "#!/bin/sh\nprintf '%s\\n' 'Opening bus /dev/i2c-2: Permission denied' >&2\nexit 1\n")
		t.Setenv("PATH", dir)
		pct, err := readDDCBrightness()
		assert.Nil(t, pct)
		assert.ErrorIs(t, err, errDDCDenied)
	})

	t.Run("no display", func(t *testing.T) {
		dir := t.TempDir()
		writeFakeDDCUtil(t, dir, "#!/bin/sh\nprintf '%s\\n' 'Display not found' >&2\nexit 1\n")
		t.Setenv("PATH", dir)
		pct, err := readDDCBrightness()
		assert.Nil(t, pct)
		assert.ErrorIs(t, err, errDDCNoDisplay)
	})

	t.Run("timeout", func(t *testing.T) {
		dir := t.TempDir()
		writeFakeDDCUtil(t, dir, "#!/bin/sh\nexec sleep 5\n")
		t.Setenv("PATH", dir)
		original := ddcutilTimeout
		ddcutilTimeout = 200 * time.Millisecond
		t.Cleanup(func() { ddcutilTimeout = original })
		pct, err := readDDCBrightness()
		assert.Nil(t, pct)
		assert.ErrorIs(t, err, errDDCFailed)
		assert.NotErrorIs(t, err, errDDCDenied)
	})
}

func writeFakeDDCUtil(t *testing.T, dir, script string) {
	t.Helper()
	path := filepath.Join(dir, "ddcutil")
	require.NoError(t, os.WriteFile(path, []byte(script), 0o755))
}
