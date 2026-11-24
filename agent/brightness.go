package agent

import (
	"errors"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/henrygd/beszel/agent/utils"
	"github.com/henrygd/beszel/internal/entities/system"
)

// backlightRoot is the sysfs class for panel backlight devices.
var backlightRoot = "/sys/class/backlight"

// platformBrightnessFn reads the built-in panel. Nil on platforms without a reader.
var platformBrightnessFn func() *uint8

// externalBrightnessFn reads an external display (DDC/CI). Nil when unsupported.
var externalBrightnessFn func() (*uint8, error)

var (
	brightnessDisabledLog sync.Once
	brightnessReadyLog    sync.Once
	errBrightnessFailed   = errors.New("brightness collection failed")
)

var (
	ddcBriefPattern = regexp.MustCompile(`(?i)\bVCP\s+10\s+C\s+(\d+)\s+(\d+)\b`)
	ddcHumanPattern = regexp.MustCompile(`(?i)current value\s*=\s*(\d+)\s*,\s*max value\s*=\s*(\d+)`)
)

// wmiMonitorBrightness is one Windows WMI brightness instance.
type wmiMonitorBrightness struct {
	InstanceName      string
	CurrentBrightness uint8
	Active            bool
}

// brightnessCollectionEnabled reports whether the collector is on.
// BRIGHTNESS=false (or BESZEL_AGENT_BRIGHTNESS=false) disables it. Unset stays on.
func brightnessCollectionEnabled() bool {
	value, ok := utils.GetEnv("BRIGHTNESS")
	if !ok {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "0", "false", "off", "no":
		brightnessDisabledLog.Do(func() {
			slog.Info("BRIGHTNESS disabled, skipping screen brightness collection")
		})
		return false
	default:
		return true
	}
}

// updateBrightness sets the current screen brightness percent.
// A miss is left nil so the hub stores it as absent, never as 0.
// The platform reader (sysfs or WMI) runs inline. DDC/CI runs in the background
// so a slow display bus cannot delay the rest of the stats snapshot.
func (a *Agent) updateBrightness(cacheTimeMs uint16, stats *system.Stats) {
	if stats == nil || !brightnessCollectionEnabled() {
		return
	}
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("brightness collection failed", "err", rec)
		}
	}()
	if platformBrightnessFn != nil {
		if pct := platformBrightnessFn(); pct != nil {
			stats.Brightness = pct
			logBrightness(*pct)
			return
		}
	}
	if a.brightnessCached {
		stats.Brightness = a.brightness
	}
	a.startExternalProbe(cacheTimeMs)
}

func (a *Agent) startExternalProbe(cacheTimeMs uint16) {
	if externalBrightnessFn == nil {
		a.brightness = nil
		a.brightnessCached = true
		return
	}
	// Realtime requests reuse the last probe. Only the minute stats interval starts one.
	if cacheTimeMs != defaultDataCacheTimeMs || !a.brightnessProbe.CompareAndSwap(false, true) {
		return
	}
	go a.probeExternalBrightness()
}

func (a *Agent) probeExternalBrightness() {
	defer a.brightnessProbe.Store(false)
	pct, err := callExternalBrightness()
	if err != nil && !errors.Is(err, errBrightnessFailed) {
		slog.Debug("Error reading display brightness", "err", err)
		pct = nil
	}
	a.Lock()
	defer a.Unlock()
	a.brightness = pct
	a.brightnessCached = true
	if pct != nil {
		logBrightness(*pct)
	}
}

// callExternalBrightness isolates DDC/CI failures, including panics, from the agent process.
func callExternalBrightness() (pct *uint8, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("brightness collection failed", "err", rec)
			pct = nil
			err = errBrightnessFailed
		}
	}()
	if externalBrightnessFn == nil {
		return nil, nil
	}
	return externalBrightnessFn()
}

func logBrightness(pct uint8) {
	brightnessReadyLog.Do(func() {
		slog.Info("Screen brightness", "percent", pct)
	})
}

// readSysfsBrightness returns the built-in panel percent from a backlight class directory.
// Several devices for one panel are common (raw plus a firmware mirror). The raw
// device wins, then the finest scale, then the name.
func readSysfsBrightness(root string) *uint8 {
	if root == "" {
		return nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Debug("Error reading backlight", "path", root, "err", err)
		}
		return nil
	}
	var best *backlightCandidate
	for _, entry := range entries {
		candidate, ok := readBacklightDevice(filepath.Join(root, entry.Name()))
		if !ok {
			continue
		}
		if best == nil || betterBacklight(candidate, *best) {
			best = &candidate
		}
	}
	if best == nil {
		return nil
	}
	pct := best.pct
	return &pct
}

type backlightCandidate struct {
	name string
	rank int
	max  int64
	pct  uint8
}

func betterBacklight(a, b backlightCandidate) bool {
	if a.rank != b.rank {
		return a.rank < b.rank
	}
	if a.max != b.max {
		return a.max > b.max
	}
	return a.name < b.name
}

func readBacklightDevice(dir string) (backlightCandidate, bool) {
	current, ok := readBacklightValue(filepath.Join(dir, "brightness"))
	if !ok {
		current, ok = readBacklightValue(filepath.Join(dir, "actual_brightness"))
	}
	if !ok {
		return backlightCandidate{}, false
	}
	maxBrightness, ok := readBacklightValue(filepath.Join(dir, "max_brightness"))
	if !ok {
		return backlightCandidate{}, false
	}
	pct, ok := brightnessPercent(current, maxBrightness)
	if !ok {
		return backlightCandidate{}, false
	}
	kind, _ := os.ReadFile(filepath.Join(dir, "type"))
	return backlightCandidate{
		name: filepath.Base(dir),
		rank: backlightTypeRank(string(kind)),
		max:  maxBrightness,
		pct:  pct,
	}, true
}

func backlightTypeRank(kind string) int {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "raw":
		return 0
	case "platform":
		return 1
	case "firmware":
		return 2
	default:
		return 3
	}
}

func readBacklightValue(path string) (int64, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	value, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil || value < 0 {
		return 0, false
	}
	return value, true
}

// brightnessPercent rounds current/max to a 0-100 percent.
// A non-positive max is unreadable, which is distinct from a current of 0.
func brightnessPercent(current, max int64) (uint8, bool) {
	if current < 0 || max <= 0 || max > math.MaxInt64/100 {
		return 0, false
	}
	if current > max {
		current = max
	}
	pct := (current*100 + max/2) / max
	if pct > 100 {
		pct = 100
	}
	return uint8(pct), true
}

// parseDDCBrightness reads a ddcutil getvcp 10 report.
// Brief output looks like "VCP 10 C 50 100". Human output names current and max.
func parseDDCBrightness(output string) (*uint8, bool) {
	if pct, ok := firstDDCPercent(ddcBriefPattern, output); ok {
		return pct, true
	}
	return firstDDCPercent(ddcHumanPattern, output)
}

func firstDDCPercent(pattern *regexp.Regexp, output string) (*uint8, bool) {
	for _, match := range pattern.FindAllStringSubmatch(output, -1) {
		if len(match) != 3 {
			continue
		}
		current, currentErr := strconv.ParseInt(match[1], 10, 64)
		maxBrightness, maxErr := strconv.ParseInt(match[2], 10, 64)
		if currentErr != nil || maxErr != nil {
			continue
		}
		pct, ok := brightnessPercent(current, maxBrightness)
		if !ok {
			continue
		}
		return &pct, true
	}
	return nil, false
}

// brightnessFromWMI returns the first active monitor in instance-name order.
// Inactive or out-of-range readings are skipped. No active monitor is absent, not 0.
func brightnessFromWMI(monitors []wmiMonitorBrightness) *uint8 {
	ordered := slices.Clone(monitors)
	slices.SortFunc(ordered, func(a, b wmiMonitorBrightness) int {
		return strings.Compare(a.InstanceName, b.InstanceName)
	})
	for _, monitor := range ordered {
		if !monitor.Active || monitor.CurrentBrightness > 100 {
			continue
		}
		pct := monitor.CurrentBrightness
		return &pct
	}
	return nil
}
