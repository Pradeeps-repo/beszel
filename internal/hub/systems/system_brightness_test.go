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

func TestCreateRecordsStoresPackageUpdates(t *testing.T) {
	sys, app := newTestSystemWithHub(t)

	_, err := sys.createRecords(&system.CombinedData{
		Info: system.Info{Cpu: 1, PackageUpdates: []uint16{1, 1}},
		PackageUpdates: &system.PackageUpdates{
			Manager:       "apt",
			CheckedAt:     1700000000,
			SecurityKnown: true,
			Packages: []system.PackageUpdate{{
				Name: "openssl", Current: "3.0.2", Available: "3.0.13", Security: true,
			}},
		},
	})
	require.NoError(t, err)

	record, err := app.FindRecordById("systems", sys.Id)
	require.NoError(t, err)
	var stored system.PackageUpdates
	require.NoError(t, record.UnmarshalJSONField("package_updates", &stored))
	require.Equal(t, "openssl", stored.Packages[0].Name)
	require.Equal(t, "3.0.13", stored.Packages[0].Available)
	require.True(t, stored.Packages[0].Security)

	_, err = sys.createRecords(&system.CombinedData{Info: system.Info{Cpu: 2}})
	require.NoError(t, err)
	record, err = app.FindRecordById("systems", sys.Id)
	require.NoError(t, err)
	stored = system.PackageUpdates{}
	require.NoError(t, record.UnmarshalJSONField("package_updates", &stored))
	require.Equal(t, "openssl", stored.Packages[0].Name, "a payload without the list must keep the stored packages")

	_, err = sys.createRecords(&system.CombinedData{
		Info:           system.Info{Cpu: 3},
		PackageUpdates: &system.PackageUpdates{Manager: "apt", CheckedAt: 1700000100, Packages: []system.PackageUpdate{}},
	})
	require.NoError(t, err)
	record, err = app.FindRecordById("systems", sys.Id)
	require.NoError(t, err)
	stored = system.PackageUpdates{}
	require.NoError(t, record.UnmarshalJSONField("package_updates", &stored))
	require.Empty(t, stored.Packages)
}
