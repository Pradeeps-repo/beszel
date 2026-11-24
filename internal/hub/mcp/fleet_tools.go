package mcp

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tools/types"
	"github.com/henrygd/beszel/internal/entities/system"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func fleetRecordMatches(record *core.Record, params listSystemsParams) bool {
	if status := strings.ToLower(strings.TrimSpace(params.Status)); status != "" && !strings.EqualFold(record.GetString("status"), status) {
		return false
	}
	if name := strings.TrimSpace(params.Name); name != "" && !strings.Contains(strings.ToLower(record.GetString("name")), strings.ToLower(name)) {
		return false
	}
	var info system.Info
	_ = record.UnmarshalJSONField("info", &info)
	if orientation := strings.TrimSpace(params.Orientation); orientation != "" && !strings.Contains(strings.ToLower(info.Orientation), strings.ToLower(orientation)) {
		return false
	}
	if resolution := strings.TrimSpace(params.Resolution); resolution != "" && info.Screen != resolution {
		return false
	}
	if !versionMatches(info.SsApp, params.SsApp) || !versionMatches(info.SsIot, params.SsIot) {
		return false
	}
	if params.BrightnessMin != nil || params.BrightnessMax != nil {
		if info.Brightness == nil {
			return false
		}
		value := int(*info.Brightness)
		if params.BrightnessMin != nil && value < *params.BrightnessMin {
			return false
		}
		if params.BrightnessMax != nil && value > *params.BrightnessMax {
			return false
		}
	}
	return true
}

func versionMatches(got string, filter *versionFilter) bool {
	if filter == nil {
		return true
	}
	got = strings.TrimSpace(got)
	if eq := strings.TrimSpace(filter.Eq); eq != "" && got != eq {
		return false
	}
	if lt := strings.TrimSpace(filter.Lt); lt != "" {
		cmp, ok := compareVersion(got, lt)
		if !ok || cmp >= 0 {
			return false
		}
	}
	if gt := strings.TrimSpace(filter.Gt); gt != "" {
		cmp, ok := compareVersion(got, gt)
		if !ok || cmp <= 0 {
			return false
		}
	}
	return true
}

// compareVersion compares dotted numeric versions. A leading v is ignored.
func compareVersion(a, b string) (int, bool) {
	a = strings.TrimPrefix(strings.TrimSpace(a), "v")
	b = strings.TrimPrefix(strings.TrimSpace(b), "v")
	if a == "" || b == "" {
		return 0, false
	}
	left := strings.Split(a, ".")
	right := strings.Split(b, ".")
	n := len(left)
	if len(right) > n {
		n = len(right)
	}
	for i := 0; i < n; i++ {
		av, aok := leadingInt(left, i)
		bv, bok := leadingInt(right, i)
		if !aok || !bok {
			return 0, false
		}
		switch {
		case av < bv:
			return -1, true
		case av > bv:
			return 1, true
		}
	}
	return 0, true
}

func leadingInt(parts []string, i int) (int, bool) {
	if i >= len(parts) {
		return 0, true
	}
	digits := parts[i]
	end := 0
	for end < len(digits) && digits[end] >= '0' && digits[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, false
	}
	value, err := strconv.Atoi(digits[:end])
	if err != nil {
		return 0, false
	}
	return value, true
}

type systemHistoryParams struct {
	System  string   `json:"system" jsonschema:"system name or id"`
	Range   string   `json:"range" jsonschema:"one of 1h, 12h, 24h, 1w, 30d"`
	Metrics []string `json:"metrics" jsonschema:"cpu, mem, disk, temp, network, brightness"`
}

type historyResult struct {
	System  string         `json:"system"`
	Range   string         `json:"range"`
	Period  string         `json:"period"`
	From    string         `json:"from"`
	To      string         `json:"to"`
	Samples []historyPoint `json:"samples"`
}

type historyPoint struct {
	Time       string   `json:"time"`
	Cpu        *float64 `json:"cpu,omitempty"`
	Mem        *float64 `json:"mem,omitempty"`
	Disk       *float64 `json:"disk,omitempty"`
	Temp       *float64 `json:"temp,omitempty"`
	NetSent    *float64 `json:"net_sent_bps,omitempty"`
	NetRecv    *float64 `json:"net_recv_bps,omitempty"`
	Brightness *uint8   `json:"brightness,omitempty"`
}

func historyWindow(name string) (period string, dur time.Duration, err error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "1h":
		return "1m", time.Hour, nil
	case "12h":
		return "10m", 12 * time.Hour, nil
	case "24h":
		return "20m", 24 * time.Hour, nil
	case "1w":
		return "120m", 7 * 24 * time.Hour, nil
	case "30d":
		return "480m", 30 * 24 * time.Hour, nil
	default:
		return "", 0, fmt.Errorf("range %q must be one of 1h, 12h, 24h, 1w, 30d", name)
	}
}

func (h *mcphub) getSystemHistory(ctx context.Context, _ *mcpsdk.CallToolRequest, params systemHistoryParams) (*mcpsdk.CallToolResult, historyResult, error) {
	auth, err := h.authRecord(ctx)
	if err != nil {
		return nil, historyResult{}, err
	}
	period, dur, err := historyWindow(params.Range)
	if err != nil {
		return nil, historyResult{}, err
	}
	if len(params.Metrics) == 0 {
		return nil, historyResult{}, fmt.Errorf("metrics is required")
	}
	wanted := map[string]bool{}
	for _, metric := range params.Metrics {
		name := strings.ToLower(strings.TrimSpace(metric))
		switch name {
		case "cpu", "mem", "disk", "temp", "network", "brightness":
			wanted[name] = true
		default:
			return nil, historyResult{}, fmt.Errorf("unknown metric %q", metric)
		}
	}
	record, err := h.resolveSystem(auth, params.System)
	if err != nil {
		return nil, historyResult{}, err
	}
	now := time.Now().UTC()
	from := now.Add(-dur)
	records, err := h.app.FindRecordsByFilter("system_stats",
		"system = {:system} && type = {:period} && created >= {:from} && created <= {:to}",
		"created", 0, 0, dbx.Params{
			"system": record.Id,
			"period": period,
			"from":   from.Format(types.DefaultDateLayout),
			"to":     now.Format(types.DefaultDateLayout),
		})
	if err != nil {
		return nil, historyResult{}, err
	}
	points := make([]historyPoint, 0, len(records))
	for _, statsRecord := range records {
		var stats system.Stats
		if err := statsRecord.UnmarshalJSONField("stats", &stats); err != nil {
			continue
		}
		point := historyPoint{Time: statsRecord.GetDateTime("created").Time().UTC().Format(time.RFC3339)}
		if wanted["cpu"] {
			value := stats.Cpu
			point.Cpu = &value
		}
		if wanted["mem"] {
			value := stats.MemPct
			point.Mem = &value
		}
		if wanted["disk"] {
			value := stats.DiskPct
			point.Disk = &value
		}
		if wanted["temp"] {
			var maxTemp float64
			for _, reading := range stats.Temperatures {
				if reading > maxTemp {
					maxTemp = reading
				}
			}
			point.Temp = &maxTemp
		}
		if wanted["network"] {
			sent := float64(stats.Bandwidth[0])
			recv := float64(stats.Bandwidth[1])
			if sent == 0 && recv == 0 && (stats.NetworkSent > 0 || stats.NetworkRecv > 0) {
				sent = stats.NetworkSent * 1024 * 1024
				recv = stats.NetworkRecv * 1024 * 1024
			}
			point.NetSent = &sent
			point.NetRecv = &recv
		}
		if wanted["brightness"] {
			point.Brightness = stats.Brightness
		}
		points = append(points, point)
	}
	return nil, historyResult{
		System:  record.GetString("name"),
		Range:   strings.ToLower(strings.TrimSpace(params.Range)),
		Period:  period,
		From:    from.Format(time.RFC3339),
		To:      now.Format(time.RFC3339),
		Samples: points,
	}, nil
}

type fleetSummaryParams struct {
	StaleMinutes int `json:"stale_minutes,omitempty" jsonschema:"systems not updated within this many minutes are listed as stale (default 10)"`
}

type fleetSummary struct {
	Total         int            `json:"total"`
	ByStatus      map[string]int `json:"by_status"`
	BySsApp       map[string]int `json:"by_ss_app"`
	BySsIot       map[string]int `json:"by_ss_iot"`
	ByOs          map[string]int `json:"by_os"`
	ByOrientation map[string]int `json:"by_orientation"`
	ByResolution  map[string]int `json:"by_resolution"`
	ByBrightness  map[string]int `json:"by_brightness"`
	StaleMinutes  int            `json:"stale_minutes"`
	Stale         []string       `json:"stale"`
}

func (h *mcphub) fleetSummary(ctx context.Context, _ *mcpsdk.CallToolRequest, params fleetSummaryParams) (*mcpsdk.CallToolResult, fleetSummary, error) {
	auth, err := h.authRecord(ctx)
	if err != nil {
		return nil, fleetSummary{}, err
	}
	records, err := h.listAllVisibleSystems(auth)
	if err != nil {
		return nil, fleetSummary{}, err
	}
	minutes := params.StaleMinutes
	if minutes <= 0 {
		minutes = 10
	}
	cutoff := time.Now().UTC().Add(-time.Duration(minutes) * time.Minute)
	summary := fleetSummary{
		Total:         len(records),
		ByStatus:      map[string]int{},
		BySsApp:       map[string]int{},
		BySsIot:       map[string]int{},
		ByOs:          map[string]int{},
		ByOrientation: map[string]int{},
		ByResolution:  map[string]int{},
		ByBrightness:  map[string]int{},
		StaleMinutes:  minutes,
		Stale:         []string{},
	}
	for _, record := range records {
		status := record.GetString("status")
		if status == "" {
			status = "unknown"
		}
		summary.ByStatus[status]++
		var info system.Info
		_ = record.UnmarshalJSONField("info", &info)
		summary.BySsApp[labelOrUnknown(info.SsApp)]++
		summary.BySsIot[labelOrUnknown(info.SsIot)]++
		summary.ByOs[labelOrUnknown(info.OsRelease)]++
		summary.ByOrientation[orientationBucket(info.Orientation)]++
		summary.ByResolution[labelOrUnknown(info.Screen)]++
		summary.ByBrightness[brightnessBucket(info.Brightness)]++
		updated := record.GetDateTime("updated").Time()
		if updated.IsZero() || updated.Before(cutoff) {
			summary.Stale = append(summary.Stale, record.GetString("name"))
		}
	}
	return nil, summary, nil
}

func labelOrUnknown(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return value
}

func orientationBucket(value string) string {
	lower := strings.ToLower(value)
	switch {
	case strings.Contains(lower, "portrait"):
		return "portrait"
	case strings.Contains(lower, "landscape"):
		return "landscape"
	default:
		return "unknown"
	}
}

func brightnessBucket(value *uint8) string {
	if value == nil {
		return "unknown"
	}
	switch n := int(*value); {
	case n < 25:
		return "0-24"
	case n < 50:
		return "25-49"
	case n < 75:
		return "50-74"
	default:
		return "75-100"
	}
}

func isFleetTextName(name string) bool {
	switch name {
	case "Resolution", "Orientation", "SsApp", "SsIot", "Os":
		return true
	default:
		return false
	}
}

func fleetModeLabel(value float64) string {
	switch int(value) {
	case 1:
		return "mismatch"
	case 2:
		return "missing"
	default:
		return "change"
	}
}
