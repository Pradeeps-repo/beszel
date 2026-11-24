//go:build testing

package systems

import (
	"testing"

	"github.com/henrygd/beszel/internal/entities/system"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateRecordsBrightness(t *testing.T) {
	sys, app := newTestSystemWithHub(t)

	zero := uint8(0)
	_, err := sys.createRecords(&system.CombinedData{
		Info:  system.Info{Cpu: 1, Brightness: &zero},
		Stats: system.Stats{Cpu: 1, Brightness: &zero},
	})
	require.NoError(t, err)

	record, err := app.FindRecordById("systems", sys.Id)
	require.NoError(t, err)
	var info map[string]any
	require.NoError(t, record.UnmarshalJSONField("info", &info))
	assert.Equal(t, float64(0), info["br"])

	statsRecords, err := app.FindRecordsByFilter("system_stats", "system={:system}", "-created", 1, 0, map[string]any{"system": sys.Id})
	require.NoError(t, err)
	require.Len(t, statsRecords, 1)
	var stats map[string]any
	require.NoError(t, statsRecords[0].UnmarshalJSONField("stats", &stats))
	assert.Equal(t, float64(0), stats["br"])
	assert.Equal(t, float64(1), stats["cpu"])

	_, err = sys.createRecords(&system.CombinedData{
		Info:  system.Info{Cpu: 2},
		Stats: system.Stats{Cpu: 2},
	})
	require.NoError(t, err)

	record, err = app.FindRecordById("systems", sys.Id)
	require.NoError(t, err)
	info = map[string]any{}
	require.NoError(t, record.UnmarshalJSONField("info", &info))
	assert.NotContains(t, info, "br")
	assert.Equal(t, float64(2), info["cpu"])

	statsRecords, err = app.FindRecordsByFilter("system_stats", "system={:system}", "", 0, 0, map[string]any{"system": sys.Id})
	require.NoError(t, err)
	require.Len(t, statsRecords, 2)
	var sawAbsent bool
	for _, statsRecord := range statsRecords {
		stats = map[string]any{}
		require.NoError(t, statsRecord.UnmarshalJSONField("stats", &stats))
		if stats["cpu"] == float64(2) {
			sawAbsent = true
			assert.NotContains(t, stats, "br")
		}
	}
	assert.True(t, sawAbsent, "expected a stats record without brightness")
}
