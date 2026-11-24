package agentupdate

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const settingsFile = "agent_update.json"

// Load reads the saved policy and applies environment overrides when those
// variables are set. A missing file is the default policy, which is off.
func Load(dataDir string, getenv func(string) (string, bool)) (Settings, error) {
	settings := Defaults()
	if dataDir != "" {
		body, err := os.ReadFile(filepath.Join(dataDir, settingsFile))
		if err != nil && !os.IsNotExist(err) {
			return Settings{}, err
		}
		if err == nil && len(strings.TrimSpace(string(body))) > 0 {
			if err := json.Unmarshal(body, &settings); err != nil {
				return Settings{}, err
			}
		}
	}
	if getenv == nil {
		return normalize(settings), nil
	}
	if value, ok := getenv("AGENT_AUTO_UPDATE"); ok {
		settings.Enabled = value == "1" || strings.EqualFold(value, "true")
	}
	if value, ok := getenv("AGENT_UPDATE_VERSION"); ok {
		settings.Target = strings.TrimSpace(value)
	}
	if value, ok := getenv("AGENT_UPDATE_PERCENT"); ok {
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return Settings{}, err
		}
		settings.Percent = n
	}
	if value, ok := getenv("AGENT_UPDATE_CANARY"); ok {
		settings.Canary = splitList(value)
	}
	if value, ok := getenv("AGENT_UPDATE_WINDOW"); ok {
		start, end, ok := strings.Cut(value, "-")
		if !ok {
			return Settings{}, errClock
		}
		settings.WindowStart = strings.TrimSpace(start)
		settings.WindowEnd = strings.TrimSpace(end)
	}
	if value, ok := getenv("AGENT_UPDATE_TZ"); ok {
		settings.Timezone = strings.TrimSpace(value)
	}
	if value, ok := getenv("AGENT_UPDATE_CONCURRENCY"); ok {
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return Settings{}, err
		}
		settings.Concurrency = n
	}
	return normalize(settings), nil
}

// Save writes the policy. Environment overrides are not stored.
func Save(dataDir string, settings Settings) error {
	if dataDir == "" {
		return errors.New("data directory is required")
	}
	body, err := json.MarshalIndent(normalize(settings), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dataDir, settingsFile), append(body, '\n'), 0o600)
}

func normalize(settings Settings) Settings {
	if settings.Percent < 0 {
		settings.Percent = 0
	}
	if settings.Percent > 100 {
		settings.Percent = 100
	}
	if settings.Concurrency < 1 {
		settings.Concurrency = 1
	}
	if settings.Concurrency > 50 {
		settings.Concurrency = 50
	}
	settings.Target = strings.TrimSpace(settings.Target)
	settings.Canary = compact(settings.Canary)
	return settings
}

func splitList(value string) []string {
	value = strings.ReplaceAll(value, "\n", ",")
	return compact(strings.Split(value, ","))
}

func compact(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}
