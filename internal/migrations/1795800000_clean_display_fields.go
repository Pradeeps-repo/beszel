package migrations

import (
	"strings"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
	m "ganigeorgiev.com/pocketbase/pocketbase/migrations"
	"github.com/henrygd/beszel/internal/entities/system"
)

func init() {
	m.Register(func(app core.App) error {
		records, err := app.FindRecordsByFilter("systems", "", "", 0, 0, nil)
		if err != nil {
			return err
		}
		for _, record := range records {
			var info map[string]any
			if err := record.UnmarshalJSONField("info", &info); err != nil || info == nil {
				continue
			}
			changed := false
			if cleanDisplayField(info, "res", system.NormalizeDisplay) {
				changed = true
			}
			if cleanDisplayField(info, "mod", system.NormalizeDisplay) {
				changed = true
			}
			if cleanDisplayField(info, "ori", system.NormalizeLabel) {
				changed = true
			}
			if !changed {
				continue
			}
			record.Set("info", info)
			if err := app.SaveNoValidate(record); err != nil {
				return err
			}
		}
		return nil
	}, func(_ core.App) error {
		return nil
	})
}

func cleanDisplayField(info map[string]any, key string, normalize func(string) string) bool {
	raw, ok := info[key]
	if !ok || raw == nil {
		return false
	}
	text := displayText(raw)
	cleaned := normalize(text)
	if cleaned == text && !displayList(raw) {
		return false
	}
	if cleaned == "" {
		delete(info, key)
	} else {
		info[key] = cleaned
	}
	return true
}

func displayList(raw any) bool {
	_, ok := raw.([]any)
	return ok
}

func displayText(raw any) string {
	switch value := raw.(type) {
	case string:
		return value
	case []any:
		parts := make([]string, 0, len(value))
		for _, item := range value {
			text, ok := item.(string)
			if !ok {
				continue
			}
			text = strings.TrimSpace(text)
			if text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, " ")
	default:
		return ""
	}
}
