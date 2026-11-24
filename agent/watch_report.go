package agent

import (
	"encoding/json"
	"os"
	"strings"
	"time"
)

const watchReportMaxAge = 6 * time.Minute

// The root helper refreshes these every two minutes. They are mode 0644 so
// the service user can read them without access to /home/vs or the X socket.
var (
	watchDisplayPath = "/run/bright-watch/display.json"
	watchAppsPath    = "/run/bright-watch/apps.json"
)

type watchDisplayFile struct {
	Updated     time.Time `json:"updated"`
	Output      string    `json:"output"`
	Mode        string    `json:"mode"`
	Resolution  string    `json:"resolution"`
	Rotation    string    `json:"rotation"`
	Orientation string    `json:"orientation"`
	Brightness  *uint8    `json:"brightness"`
}

type watchAppsFile struct {
	Updated time.Time `json:"updated"`
	SsApp   string    `json:"ss_app"`
	SsIot   string    `json:"ss_iot"`
}

func watchReportFresh(updated time.Time) bool {
	if updated.IsZero() {
		return false
	}
	age := time.Since(updated)
	return age < watchReportMaxAge && age > -time.Minute
}

func readFreshWatchDisplay() (fleetSnapshot, *uint8, bool) {
	data, err := os.ReadFile(watchDisplayPath)
	if err != nil {
		return fleetSnapshot{}, nil, false
	}
	var file watchDisplayFile
	if err := json.Unmarshal(data, &file); err != nil || !watchReportFresh(file.Updated) {
		return fleetSnapshot{}, nil, false
	}
	snap := fleetSnapshot{
		Output:      strings.TrimSpace(file.Output),
		Mode:        normalizeMode(file.Mode),
		Resolution:  normalizeMode(file.Resolution),
		Orientation: strings.TrimSpace(file.Orientation),
	}
	if snap.Resolution == "" {
		snap.Orientation = ""
	}
	var brightness *uint8
	if file.Brightness != nil && *file.Brightness <= 100 {
		value := *file.Brightness
		brightness = &value
	}
	if snap.Resolution == "" && snap.Mode == "" && brightness == nil {
		return fleetSnapshot{}, nil, false
	}
	return snap, brightness, true
}

func helperBrightness() (*uint8, bool) {
	_, pct, ok := readFreshWatchDisplay()
	if !ok || pct == nil {
		return nil, false
	}
	return pct, true
}

func readFreshWatchApps() (appVersions, bool) {
	data, err := os.ReadFile(watchAppsPath)
	if err != nil {
		return appVersions{}, false
	}
	var file watchAppsFile
	if err := json.Unmarshal(data, &file); err != nil || !watchReportFresh(file.Updated) {
		return appVersions{}, false
	}
	versions := appVersions{
		SsApp: cleanVersion(file.SsApp),
		SsIot: cleanVersion(file.SsIot),
	}
	if versions.SsApp == "" && versions.SsIot == "" {
		return appVersions{}, false
	}
	return versions, true
}

func normalizeMode(value string) string {
	value = strings.TrimSpace(value)
	if !modePattern.MatchString(value) {
		return ""
	}
	return value
}

func cleanVersion(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 64 || strings.ContainsAny(value, "\r\n") {
		return ""
	}
	return value
}
