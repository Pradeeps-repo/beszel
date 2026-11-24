package agent

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/henrygd/beszel/agent/utils"
)

const fleetRefreshInterval = 5 * time.Minute

const (
	packageNameApp = "ss-app"
	packageNameIot = "ss-iot"
)

// fleetSnapshot is inventory that changes rarely. Empty strings are omitted
// from the system record so a miss is absent, not a blank value.
type fleetSnapshot struct {
	Resolution  string
	Orientation string
	SsApp       string
	SsIot       string
}

// collectDisplay reads the connected screen. Nil-safe default is no display.
var collectDisplay = func() fleetSnapshot { return fleetSnapshot{} }

// packageSearchRoots are walked a few levels deep for ss-app and ss-iot package.json files.
var packageSearchRoots = []string{
	"/opt",
	"/srv",
	"/home",
	"/usr/lib/node_modules",
	"/usr/local/lib/node_modules",
}

var modePattern = regexp.MustCompile(`^([0-9]+)x([0-9]+)$`)

// updateFleetInfo copies OS strings Beszel already collects and refreshes
// screen and app versions a few times an hour. Failures stay inside this
// function so the rest of the stats snapshot is unchanged.
func (a *Agent) updateFleetInfo() {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("fleet info collection failed", "err", rec)
			a.fleetCached = true
			a.fleetAt = time.Now()
		}
	}()

	// PRETTY_NAME and kernel are collected once in refreshSystemDetails.
	if a.systemDetails.OsName != "" {
		a.systemInfo.OsRelease = a.systemDetails.OsName
	}
	if a.systemDetails.Kernel != "" {
		a.systemInfo.KernelVersion = a.systemDetails.Kernel
	}

	if !a.fleetCached || time.Since(a.fleetAt) >= fleetRefreshInterval {
		snap := collectDisplay()
		apps := readAppVersions()
		snap.SsApp = apps.SsApp
		snap.SsIot = apps.SsIot
		a.fleet = snap
		a.fleetAt = time.Now()
		a.fleetCached = true
	}
	a.systemInfo.Screen = a.fleet.Resolution
	a.systemInfo.Orientation = a.fleet.Orientation
	a.systemInfo.SsApp = a.fleet.SsApp
	a.systemInfo.SsIot = a.fleet.SsIot
}

type appVersions struct {
	SsApp string
	SsIot string
}

func readAppVersions() appVersions {
	var versions appVersions
	appPath, appSet := utils.GetEnv("SS_APP_PACKAGE_JSON")
	iotPath, iotSet := utils.GetEnv("SS_IOT_PACKAGE_JSON")
	if appSet {
		versions.SsApp = versionFromPackageJSON(appPath, "")
	}
	if iotSet {
		versions.SsIot = versionFromPackageJSON(iotPath, "")
	}
	if appSet && iotSet {
		return versions
	}
	found := discoverPackageVersions(packageSearchRoots)
	if !appSet {
		versions.SsApp = found.SsApp
	}
	if !iotSet {
		versions.SsIot = found.SsIot
	}
	return versions
}

// versionFromPackageJSON reads the version field. When wantName is set, the
// file is ignored unless its name matches. A missing file or version is empty.
func versionFromPackageJSON(path, wantName string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 512*1024))
	if err != nil {
		return ""
	}
	var pkg struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return ""
	}
	if wantName != "" && pkg.Name != wantName {
		return ""
	}
	return strings.TrimSpace(pkg.Version)
}

func discoverPackageVersions(roots []string) appVersions {
	var versions appVersions
	budget := 48
	for _, root := range roots {
		maxDepth := 3
		if strings.Contains(root, "node_modules") {
			maxDepth = 1
		}
		walkPackages(root, 0, maxDepth, &budget, &versions)
		if versions.SsApp != "" && versions.SsIot != "" || budget <= 0 {
			break
		}
	}
	return versions
}

func walkPackages(dir string, depth, maxDepth int, budget *int, found *appVersions) {
	if *budget <= 0 || (found.SsApp != "" && found.SsIot != "") {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if *budget <= 0 || (found.SsApp != "" && found.SsIot != "") {
			return
		}
		name := entry.Name()
		path := filepath.Join(dir, name)
		if name == "package.json" {
			*budget--
			switch {
			case found.SsApp == "":
				if version := versionFromPackageJSON(path, packageNameApp); version != "" {
					found.SsApp = version
				}
			}
			if found.SsIot == "" {
				if version := versionFromPackageJSON(path, packageNameIot); version != "" {
					found.SsIot = version
				}
			}
			continue
		}
		if depth >= maxDepth || skipPackageDir(name, depth) {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			continue
		}
		walkPackages(path, depth+1, maxDepth, budget, found)
	}
}

func skipPackageDir(name string, depth int) bool {
	switch name {
	case ".git", ".cache", "proc", "sys", "dev":
		return true
	case "node_modules":
		return depth > 0
	default:
		return strings.HasPrefix(name, ".")
	}
}

// orientationFromSize infers portrait or landscape. Equal sides are omitted.
func orientationFromSize(width, height int) string {
	switch {
	case width <= 0 || height <= 0 || width == height:
		return ""
	case height > width:
		return "portrait"
	default:
		return "landscape"
	}
}

// orientationWithRotation appends a degree value when a session reports one.
func orientationWithRotation(base string, degrees int) string {
	if base == "" {
		return ""
	}
	switch degrees {
	case 0, 90, 180, 270:
		if degrees == 0 {
			return base
		}
		return base + " (" + strconv.Itoa(degrees) + "°)"
	default:
		return base
	}
}

func rotationDegrees(name string) (int, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "normal", "0":
		return 0, true
	case "left", "90":
		return 90, true
	case "inverted", "180":
		return 180, true
	case "right", "270":
		return 270, true
	default:
		return 0, false
	}
}

// parseXrandr reads `xrandr --query` output. The connected geometry is the
// transformed size; the following word is the rotation when xrandr prints one.
func parseXrandr(output string) fleetSnapshot {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[1] != "connected" {
			continue
		}
		geometry := ""
		rotation := ""
		for _, field := range fields[2:] {
			if modePattern.MatchString(strings.Split(field, "+")[0]) && strings.Contains(field, "+") {
				geometry = strings.Split(field, "+")[0]
				continue
			}
			if geometry != "" && rotation == "" {
				if _, ok := rotationDegrees(field); ok {
					rotation = field
				}
				break
			}
		}
		if geometry == "" {
			continue
		}
		return displayFromMode(geometry, rotation, true)
	}
	return fleetSnapshot{}
}

// parseWlrRandr reads `wlr-randr` output. Transform is the compositor rotation.
func parseWlrRandr(output string) fleetSnapshot {
	var currentMode string
	var transform string
	enabled := false
	best := fleetSnapshot{}
	flush := func() {
		if !enabled || currentMode == "" {
			return
		}
		snap := displayFromMode(currentMode, transform, false)
		if snap.Resolution != "" && best.Resolution == "" {
			best = snap
		}
	}
	for _, line := range strings.Split(output, "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			flush()
			currentMode = ""
			transform = ""
			enabled = false
			continue
		}
		switch {
		case strings.HasPrefix(trim, "Enabled:"):
			enabled = strings.Contains(trim, "yes")
		case strings.HasPrefix(trim, "Transform:"):
			transform = strings.TrimSpace(strings.TrimPrefix(trim, "Transform:"))
		case strings.Contains(trim, "(current)") && len(strings.Fields(trim)) > 0 && modePattern.MatchString(strings.Fields(trim)[0]):
			currentMode = strings.Fields(trim)[0]
		}
	}
	flush()
	return best
}

// displayFromMode builds the logical resolution and orientation.
// transformed is true when mode is already the rotated geometry (xrandr).
// DRM and wlr-randr report the panel mode, so a 90° or 270° transform swaps it.
func displayFromMode(mode, rotation string, transformed bool) fleetSnapshot {
	parts := strings.Split(mode, "x")
	if len(parts) != 2 {
		return fleetSnapshot{}
	}
	width, widthErr := strconv.Atoi(parts[0])
	height, heightErr := strconv.Atoi(parts[1])
	if widthErr != nil || heightErr != nil {
		return fleetSnapshot{}
	}
	degrees, hasRotation := rotationDegrees(rotation)
	if hasRotation && !transformed && (degrees == 90 || degrees == 270) {
		width, height = height, width
	}
	shown := strconv.Itoa(width) + "x" + strconv.Itoa(height)
	base := orientationFromSize(width, height)
	if !hasRotation {
		return fleetSnapshot{Resolution: shown, Orientation: base}
	}
	return fleetSnapshot{Resolution: shown, Orientation: orientationWithRotation(base, degrees)}
}

func connectorRank(name string) int {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "edp"), strings.Contains(lower, "dsi"), strings.Contains(lower, "lvds"):
		return 0
	case strings.Contains(lower, "dp"):
		return 1
	default:
		return 2
	}
}

// readDRMDisplay returns the preferred connected connector under a DRM class directory.
func readDRMDisplay(root string) fleetSnapshot {
	entries, err := os.ReadDir(root)
	if err != nil {
		return fleetSnapshot{}
	}
	bestRank := 99
	var best fleetSnapshot
	for _, entry := range entries {
		name := entry.Name()
		if !strings.Contains(name, "-") {
			continue
		}
		dir := filepath.Join(root, name)
		status, err := os.ReadFile(filepath.Join(dir, "status"))
		if err != nil || strings.TrimSpace(string(status)) != "connected" {
			continue
		}
		modes, err := os.ReadFile(filepath.Join(dir, "modes"))
		if err != nil {
			continue
		}
		var mode string
		for _, line := range strings.Split(string(modes), "\n") {
			line = strings.TrimSpace(line)
			if modePattern.MatchString(line) {
				mode = line
				break
			}
		}
		if mode == "" {
			continue
		}
		rank := connectorRank(name)
		if best.Resolution != "" && rank >= bestRank {
			continue
		}
		bestRank = rank
		best = displayFromMode(mode, "", false)
	}
	return best
}
