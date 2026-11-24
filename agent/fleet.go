package agent

import (
	"bytes"
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

// Default package.json paths on Bright.Blue machines. The home directory may
// be mode 700, so the agent also reads the root helper's apps.json first.
var (
	defaultAppPackage = "/home/vs/smartselect/package.json"
	defaultIotPackage = "/home/vs/smartiot/package.json"
)

// procRoot is where Xorg's command line is read to find its -auth file.
var procRoot = "/proc"

// fleetSnapshot is inventory that changes rarely. Empty strings are omitted
// from the system record so a miss is absent, not a blank value.
type fleetSnapshot struct {
	Resolution  string
	Orientation string
	// Mode is the current hardware mode before rotation, e.g. "1920x1080".
	// Resolution is the logical size after rotation. Empty means unknown.
	Mode   string
	Output string
	SsApp  string
	SsIot  string
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
	a.systemInfo.Mode = a.fleet.Mode
	a.systemInfo.SsApp = a.fleet.SsApp
	a.systemInfo.SsIot = a.fleet.SsIot
}

type appVersions struct {
	SsApp string
	SsIot string
}

func readAppVersions() appVersions {
	appPath, appSet := utils.GetEnv("SS_APP_PACKAGE_JSON")
	iotPath, iotSet := utils.GetEnv("SS_IOT_PACKAGE_JSON")
	var versions appVersions
	appDenied := false
	iotDenied := false
	if appSet {
		versions.SsApp, appDenied = versionFromPackage(appPath)
	}
	if iotSet {
		versions.SsIot, iotDenied = versionFromPackage(iotPath)
	}
	// The service user often cannot read /home/vs. The root helper copies the
	// versions into apps.json. An explicit path that can be read still wins.
	helper, helperOK := readFreshWatchApps()
	if helperOK {
		if versions.SsApp == "" && (!appSet || appDenied) {
			versions.SsApp = helper.SsApp
		}
		if versions.SsIot == "" && (!iotSet || iotDenied) {
			versions.SsIot = helper.SsIot
		}
	}
	if versions.SsApp != "" && versions.SsIot != "" {
		return versions
	}
	needApp := (!appSet || appDenied) && versions.SsApp == ""
	needIot := (!iotSet || iotDenied) && versions.SsIot == ""
	if !needApp && !needIot {
		return versions
	}
	found := discoverPackageVersions(packageSearchRoots)
	if needApp {
		versions.SsApp = found.SsApp
	}
	if needIot {
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
	version, _ := versionFromReader(file, wantName)
	return version
}

// versionFromPackage reports the version and whether the open was denied.
// A readable file with no version is not denied, so discovery does not replace it.
func versionFromPackage(path string) (version string, denied bool) {
	file, err := os.Open(path)
	if err != nil {
		return "", os.IsPermission(err)
	}
	defer file.Close()
	version, _ = versionFromReader(file, "")
	return version, false
}

func versionFromReader(file io.Reader, wantName string) (string, bool) {
	data, err := io.ReadAll(io.LimitReader(file, 512*1024))
	if err != nil {
		return "", false
	}
	var pkg struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return "", false
	}
	if wantName != "" && pkg.Name != wantName {
		return "", true
	}
	return strings.TrimSpace(pkg.Version), true
}

func discoverPackageVersions(roots []string) appVersions {
	// Field installs keep the apps here. Try them before the wider search.
	versions := appVersions{
		SsApp: versionFromPackageJSON(defaultAppPackage, ""),
		SsIot: versionFromPackageJSON(defaultIotPackage, ""),
	}
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
// The starred mode under that output is the current hardware mode.
func parseXrandr(output string) fleetSnapshot {
	lines := strings.Split(output, "\n")
	for i, line := range lines {
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
		snap := displayFromMode(geometry, rotation, true)
		snap.Mode = currentXrandrMode(lines[i+1:])
		snap.Output = fields[0]
		return snap
	}
	return fleetSnapshot{}
}

func currentXrandrMode(lines []string) string {
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			break
		}
		if !strings.Contains(line, "*") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) > 0 && modePattern.MatchString(fields[0]) {
			return fields[0]
		}
	}
	return ""
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

var (
	drmConnectorHeader = regexp.MustCompile(`^connector(?:\[[0-9]+\])?:\s+(\S+)`)
	drmCrtcHeader      = regexp.MustCompile(`^crtc(?:\[[0-9]+\])?:\s+(\S+)|^((?:crtc-)?[0-9]+):\s*$`)
	drmModeLine        = regexp.MustCompile(`^mode:\s+"?([0-9]+x[0-9]+)"?`)
)

// parseDRMDebugState reads a DRM debugfs state file. The current mode is the
// CRTC mode attached to a connected connector. The modes file under
// /sys/class/drm is the panel's mode list, and its first line is the maximum,
// so this parser never uses that list.
func parseDRMDebugState(text string) (mode, output string, ok bool) {
	type block struct {
		kind   string
		name   string
		crtc   string
		status string
		mode   string
	}
	var blocks []block
	var cur *block
	flush := func() {
		if cur == nil {
			return
		}
		blocks = append(blocks, *cur)
		cur = nil
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if line[0] != ' ' && line[0] != '\t' {
			flush()
			switch {
			case drmConnectorHeader.MatchString(line):
				match := drmConnectorHeader.FindStringSubmatch(line)
				cur = &block{kind: "connector", name: match[1]}
			case drmCrtcHeader.MatchString(line):
				match := drmCrtcHeader.FindStringSubmatch(line)
				name := match[1]
				if name == "" {
					name = match[2]
				}
				cur = &block{kind: "crtc", name: name}
			}
			continue
		}
		if cur == nil {
			continue
		}
		trim := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trim, "crtc=") || strings.HasPrefix(trim, "crtc:"):
			value := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(trim, "crtc="), "crtc:"))
			if value != "" && value != "0" && !strings.EqualFold(value, "none") {
				cur.crtc = value
			}
		case strings.HasPrefix(trim, "status:"):
			cur.status = strings.TrimSpace(strings.TrimPrefix(trim, "status:"))
		case strings.HasPrefix(trim, "connector state:"):
			cur.status = strings.TrimSpace(strings.TrimPrefix(trim, "connector state:"))
		default:
			if cur.kind != "crtc" {
				continue
			}
			if match := drmModeLine.FindStringSubmatch(trim); match != nil {
				cur.mode = match[1]
			}
		}
	}
	flush()

	modes := map[string]string{}
	for _, item := range blocks {
		if item.kind == "crtc" && item.mode != "" {
			modes[item.name] = item.mode
		}
	}
	bestRank := 99
	for _, item := range blocks {
		if item.kind != "connector" || item.crtc == "" {
			continue
		}
		status := strings.ToLower(item.status)
		if status != "" && status != "connected" {
			continue
		}
		current := modes[item.crtc]
		if current == "" {
			continue
		}
		rank := connectorRank(item.name)
		if ok && rank >= bestRank {
			continue
		}
		bestRank = rank
		mode = current
		output = item.name
		ok = true
	}
	return mode, output, ok
}

// readDRMDisplay returns the current CRTC mode. A modes-only sysfs tree is
// unknown: the first line of modes is the panel maximum, not the live mode.
// Rotation is not in DRM state, so resolution and orientation stay empty.
func readDRMDisplay(root string) fleetSnapshot {
	return readDRMCurrent(root)
}

func readDRMCurrent(root string) fleetSnapshot {
	entries, err := os.ReadDir(root)
	if err != nil {
		return fleetSnapshot{}
	}
	best := fleetSnapshot{}
	bestRank := 99
	found := false
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, entry.Name(), "state"))
		if err != nil {
			continue
		}
		mode, output, ok := parseDRMDebugState(string(data))
		if !ok {
			continue
		}
		rank := connectorRank(output)
		if found && rank >= bestRank {
			continue
		}
		found = true
		bestRank = rank
		best = fleetSnapshot{Mode: mode, Output: output}
	}
	return best
}

// parseXorgArgs reads an Xorg/X command line. The -auth path is the file
// xrandr must use; guessing ~/.Xauthority is not enough on these machines.
func parseXorgArgs(args []string) (display, auth string, ok bool) {
	if len(args) == 0 || args[0] == "" {
		return "", "", false
	}
	switch filepath.Base(args[0]) {
	case "Xorg", "X", "Xorg.bin":
	default:
		return "", "", false
	}
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if arg == "-auth" && i+1 < len(args) {
			auth = args[i+1]
			i++
			continue
		}
		if strings.HasPrefix(arg, ":") && !strings.Contains(arg, "=") {
			display = arg
		}
	}
	if auth == "" {
		return "", "", false
	}
	if display == "" {
		display = ":0"
	}
	return display, auth, true
}

func findXorgAuth(root string) (display, auth string, ok bool) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", "", false
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == "" || name[0] < '0' || name[0] > '9' {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, name, "cmdline"))
		if err != nil || len(data) == 0 {
			continue
		}
		display, auth, ok = parseXorgArgs(splitCmdline(data))
		if !ok {
			continue
		}
		info, err := os.Stat(auth)
		if err != nil || info.IsDir() {
			continue
		}
		return display, auth, true
	}
	return "", "", false
}

func splitCmdline(data []byte) []string {
	parts := bytes.Split(data, []byte{0})
	args := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		args = append(args, string(part))
	}
	return args
}
