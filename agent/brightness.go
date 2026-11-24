package agent

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/henrygd/beszel/agent/utils"
	"github.com/henrygd/beszel/internal/entities/system"
)

// backlightRoot is the sysfs class for panel backlight devices.
var backlightRoot = "/sys/class/backlight"

// platformBrightnessFn reads the non-DDC source: Linux sysfs backlight, or Windows WMI.
var platformBrightnessFn func() *uint8

// externalBrightnessFn runs `ddcutil getvcp 10`. Nil where DDC/CI is not implemented.
var externalBrightnessFn func() (*uint8, error)

// ddcutilAvailableFn reports whether the ddcutil binary can be executed.
// Nil means DDC/CI is not offered on this platform.
var ddcutilAvailableFn func() bool

// brightnessRefreshInterval is how long a DDC reading is reused.
// DDC/CI takes about a second, so this stays on the minute stats cadence.
const brightnessRefreshInterval = time.Minute

// ddcutilTimeout stops a stuck I2C read. The stats snapshot does not wait for it.
var ddcutilTimeout = 3 * time.Second

type brightnessSource uint8

const (
	brightnessSourceAuto brightnessSource = iota
	brightnessSourceDDC
	brightnessSourceBacklight
)

var (
	brightnessDisabledLog sync.Once
	brightnessReadyLog    sync.Once
	brightnessSourceLog   sync.Once
	brightnessDeniedLog   sync.Once
	errBrightnessFailed   = errors.New("brightness collection failed")
	errDDCMissing         = errors.New("ddcutil not found")
	errDDCNoDisplay       = errors.New("no DDC display responded")
	errDDCDenied          = errors.New("ddcutil permission denied")
	errDDCFailed          = errors.New("ddcutil failed")
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

// brightnessReadSource selects the Linux reader.
// BRIGHTNESS_SOURCE=auto (default) uses ddcutil when it is installed and a display
// answers, and /sys/class/backlight otherwise. ddc and backlight force one source.
func brightnessReadSource() brightnessSource {
	value, ok := utils.GetEnv("BRIGHTNESS_SOURCE")
	if !ok {
		return brightnessSourceAuto
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "auto":
		return brightnessSourceAuto
	case "ddc", "ddcutil":
		return brightnessSourceDDC
	case "backlight", "sysfs":
		return brightnessSourceBacklight
	default:
		brightnessSourceLog.Do(func() {
			slog.Warn("Invalid BRIGHTNESS_SOURCE, using auto", "value", value)
		})
		return brightnessSourceAuto
	}
}

func ddcutilIsAvailable() bool {
	if ddcutilAvailableFn == nil {
		return false
	}
	return ddcutilAvailableFn()
}

// updateBrightness sets the current screen brightness percent.
// A miss is left nil so the hub stores it as absent, never as 0.
// DDC/CI is read in the background at most once per brightnessRefreshInterval.
func (a *Agent) updateBrightness(stats *system.Stats) {
	if stats == nil || !brightnessCollectionEnabled() {
		return
	}
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("brightness collection failed", "err", rec)
		}
	}()
	source := brightnessReadSource()
	// Backlight and Windows WMI are local and fast. DDC is not, so auto mode
	// only takes this path when ddcutil itself is not installed.
	if source == brightnessSourceBacklight || (source == brightnessSourceAuto && !ddcutilIsAvailable()) {
		if platformBrightnessFn != nil {
			stats.Brightness = platformBrightnessFn()
			if stats.Brightness != nil {
				logBrightness(*stats.Brightness)
			}
		}
		return
	}
	if !ddcutilIsAvailable() {
		return
	}
	a.serveCachedBrightness(stats)
	a.maybeRefreshBrightness(source)
}

func (a *Agent) serveCachedBrightness(stats *system.Stats) {
	a.brightnessMu.Lock()
	defer a.brightnessMu.Unlock()
	if a.brightnessCached {
		stats.Brightness = a.brightness
	}
}

func (a *Agent) maybeRefreshBrightness(source brightnessSource) {
	a.brightnessMu.Lock()
	fresh := a.brightnessCached && time.Since(a.brightnessAt) < brightnessRefreshInterval
	a.brightnessMu.Unlock()
	if fresh || !a.brightnessProbe.CompareAndSwap(false, true) {
		return
	}
	go a.probeBrightness(source)
}

func (a *Agent) probeBrightness(source brightnessSource) {
	defer a.brightnessProbe.Store(false)
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("brightness collection failed", "err", rec)
			a.storeBrightness(nil, true)
		}
	}()

	pct, err := callExternalBrightness()
	if err == nil && pct == nil {
		err = errDDCNoDisplay
	}
	switch {
	case err == nil:
		a.storeBrightness(pct, true)
		logBrightness(*pct)
	case errors.Is(err, errDDCDenied):
		warnBrightnessPermission(err)
		a.storeBrightness(nil, true)
	case errors.Is(err, errDDCMissing) || errors.Is(err, errDDCNoDisplay):
		if source == brightnessSourceDDC {
			a.storeBrightness(nil, true)
			break
		}
		fallback := readPlatformBrightness()
		a.storeBrightness(fallback, true)
		if fallback != nil {
			logBrightness(*fallback)
		}
	default:
		// Timeout or other bus error. Keep the last good percent when we have
		// one. A value we never read stays absent, not 0.
		slog.Debug("Error reading display brightness", "err", err)
		a.brightnessMu.Lock()
		a.brightnessCached = true
		a.brightnessAt = time.Now()
		a.brightnessMu.Unlock()
	}
}

func readPlatformBrightness() (pct *uint8) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("brightness collection failed", "err", rec)
			pct = nil
		}
	}()
	if platformBrightnessFn == nil {
		return nil
	}
	return platformBrightnessFn()
}

func (a *Agent) storeBrightness(pct *uint8, cached bool) {
	a.brightnessMu.Lock()
	a.brightness = pct
	a.brightnessCached = cached
	a.brightnessAt = time.Now()
	a.brightnessMu.Unlock()
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
		return nil, errDDCMissing
	}
	return externalBrightnessFn()
}

func warnBrightnessPermission(err error) {
	brightnessDeniedLog.Do(func() {
		slog.Warn("ddcutil cannot read screen brightness (permission denied on /dev/i2c-*). Load i2c-dev (modprobe i2c-dev), install ddcutil, and grant the agent access to /dev/i2c-* via the i2c group or a udev rule: KERNEL==\"i2c-[0-9]*\", GROUP=\"i2c\", MODE=\"0660\". Restart the agent after adding the group. Brightness is omitted, not reported as 0.", "err", err)
	})
}

// classifyDDCFailure distinguishes a permission problem from "no display" and other failures.
// output is the combined stdout and stderr of `ddcutil getvcp 10`.
func classifyDDCFailure(output string, err error) error {
	text := strings.ToLower(output)
	if err != nil {
		text += "\n" + strings.ToLower(err.Error())
	}
	detail := strings.TrimSpace(output)
	if detail == "" && err != nil {
		detail = err.Error()
	}
	if len(detail) > 300 {
		detail = detail[:300]
	}
	switch {
	case ddcPermissionDenied(text):
		return fmt.Errorf("%w: %s", errDDCDenied, detail)
	case ddcNoDisplay(text) || err == nil:
		return errDDCNoDisplay
	default:
		return fmt.Errorf("%w: %s", errDDCFailed, detail)
	}
}

func ddcPermissionDenied(text string) bool {
	return strings.Contains(text, "permission denied") ||
		strings.Contains(text, "operation not permitted") ||
		strings.Contains(text, "i2c_permissions") ||
		strings.Contains(text, "access to i2c") ||
		strings.Contains(text, "eacces")
}

func ddcNoDisplay(text string) bool {
	return strings.Contains(text, "no display") ||
		strings.Contains(text, "display not found") ||
		strings.Contains(text, "unable to detect")
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
