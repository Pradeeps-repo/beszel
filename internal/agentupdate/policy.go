package agentupdate

import (
	"hash/fnv"
	"strings"
	"time"
)

// Settings is the hub-wide agent update policy. Enabled defaults to false.
type Settings struct {
	Enabled     bool     `json:"enabled"`
	Target      string   `json:"target,omitempty"`
	Percent     int      `json:"percent"`
	Canary      []string `json:"canary,omitempty"`
	WindowStart string   `json:"window_start,omitempty"`
	WindowEnd   string   `json:"window_end,omitempty"`
	Timezone    string   `json:"timezone,omitempty"`
	Concurrency int      `json:"concurrency"`
}

// Defaults is the policy used when nothing has been saved.
func Defaults() Settings {
	return Settings{
		Enabled:     false,
		Percent:     10,
		WindowStart: "01:00",
		WindowEnd:   "05:00",
		Concurrency: 5,
	}
}

// System is the slice of a system record the policy needs.
type System struct {
	ID      string
	Name    string
	Status  string
	Version string
	State   string
	Forced  bool
}

// InWindow reports whether now falls in the fleet-local window.
// An empty window is always open. An overnight window crosses midnight.
func InWindow(now time.Time, start, end string, loc *time.Location) bool {
	if strings.TrimSpace(start) == "" || strings.TrimSpace(end) == "" {
		return true
	}
	startMin, okStart := clockMinutes(start)
	endMin, okEnd := clockMinutes(end)
	if !okStart || !okEnd || startMin == endMin {
		return true
	}
	if loc == nil {
		loc = time.Local
	}
	local := now.In(loc)
	nowMin := local.Hour()*60 + local.Minute()
	if startMin < endMin {
		return nowMin >= startMin && nowMin < endMin
	}
	return nowMin >= startMin || nowMin < endMin
}

func clockMinutes(value string) (int, bool) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 {
		return 0, false
	}
	hour, errH := parseClock(parts[0], 23)
	min, errM := parseClock(parts[1], 59)
	if errH != nil || errM != nil {
		return 0, false
	}
	return hour*60 + min, true
}

func parseClock(value string, max int) (int, error) {
	n := 0
	if value == "" {
		return 0, errClock
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0, errClock
		}
		n = n*10 + int(r-'0')
	}
	if n > max {
		return 0, errClock
	}
	return n, nil
}

var errClock = clockError{}

type clockError struct{}

func (clockError) Error() string { return "invalid clock" }

// InRollout is true for canary names or ids when a canary list is set.
// Otherwise it is a stable percentage of system ids. Percent 0 matches nobody.
func InRollout(id, name string, percent int, canary []string) bool {
	if len(canary) > 0 {
		for _, item := range canary {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			if strings.EqualFold(item, id) || strings.EqualFold(item, name) {
				return true
			}
		}
		return false
	}
	if percent >= 100 {
		return true
	}
	if percent <= 0 {
		return false
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(id))
	return int(hash.Sum32()%100) < percent
}

// AutoEligible is a connected system that is behind the target and inside the
// rollout and the window. Force bypasses the switch, the window, and the rollout.
func AutoEligible(sys System, settings Settings, target string, now time.Time, force bool) bool {
	if !strings.EqualFold(sys.Status, "up") {
		return false
	}
	if strings.TrimSpace(target) == "" || Compare(sys.Version, target) >= 0 {
		return false
	}
	state := strings.ToLower(sys.State)
	if state == "pending" || state == "updating" {
		return false
	}
	if force {
		return true
	}
	if !settings.Enabled {
		return false
	}
	loc := time.Local
	if tz := strings.TrimSpace(settings.Timezone); tz != "" {
		if loaded, err := time.LoadLocation(tz); err == nil {
			loc = loaded
		}
	}
	if !InWindow(now, settings.WindowStart, settings.WindowEnd, loc) {
		return false
	}
	return InRollout(sys.ID, sys.Name, settings.Percent, settings.Canary)
}

// Batch returns at most slots pending systems, forced ones first.
func Batch(pending []System, slots int) []System {
	if slots <= 0 || len(pending) == 0 {
		return nil
	}
	ordered := append([]System(nil), pending...)
	for i := 1; i < len(ordered); i++ {
		for j := i; j > 0; j-- {
			if lessPending(ordered[j], ordered[j-1]) {
				ordered[j], ordered[j-1] = ordered[j-1], ordered[j]
			}
		}
	}
	if slots > len(ordered) {
		slots = len(ordered)
	}
	return ordered[:slots]
}

func lessPending(a, b System) bool {
	if a.Forced != b.Forced {
		return a.Forced
	}
	return strings.ToLower(a.Name) < strings.ToLower(b.Name)
}
