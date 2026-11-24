//go:build testing

package systems

import (
	"testing"

	"github.com/henrygd/beszel/internal/entities/system"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateRecordsFleetInfo(t *testing.T) {
	sys, app := newTestSystemWithHub(t)
	_, err := sys.createRecords(&system.CombinedData{
		Info: system.Info{
			Cpu:           1,
			Screen:        "1080x1920",
			Orientation:   "portrait",
			SsApp:         "1.2.3",
			SsIot:         "0.4.0",
			OsRelease:     "Debian GNU/Linux 12 (bookworm)",
			KernelVersion: "6.1.0",
		},
		Stats: system.Stats{Cpu: 1},
	})
	require.NoError(t, err)

	record, err := app.FindRecordById("systems", sys.Id)
	require.NoError(t, err)
	var info map[string]any
	require.NoError(t, record.UnmarshalJSONField("info", &info))
	assert.Equal(t, "1080x1920", info["res"])
	assert.Equal(t, "portrait", info["ori"])
	assert.Equal(t, "1.2.3", info["ssa"])
	assert.Equal(t, "0.4.0", info["ssi"])
	assert.Equal(t, "Debian GNU/Linux 12 (bookworm)", info["osr"])
	assert.Equal(t, "6.1.0", info["k"])
	assert.Equal(t, float64(1), info["cpu"])

	_, err = sys.createRecords(&system.CombinedData{
		Info:  system.Info{Cpu: 2},
		Stats: system.Stats{Cpu: 2},
	})
	require.NoError(t, err)
	record, err = app.FindRecordById("systems", sys.Id)
	require.NoError(t, err)
	info = map[string]any{}
	require.NoError(t, record.UnmarshalJSONField("info", &info))
	assert.NotContains(t, info, "res")
	assert.NotContains(t, info, "ori")
	assert.NotContains(t, info, "ssa")
	assert.NotContains(t, info, "ssi")
	assert.NotContains(t, info, "osr")
	assert.Equal(t, float64(2), info["cpu"])
}
