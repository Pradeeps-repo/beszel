package system

import (
	"regexp"
	"strings"
)

const maxDisplays = 4

var displayModePattern = regexp.MustCompile(`[0-9]+x[0-9]+`)

// NormalizeDisplay collapses a resolution or mode that was stored more than once.
// One value stays as-is. Distinct sizes are kept, in order, up to four.
func NormalizeDisplay(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if prefix, ok := repeatedPrefix(value); ok {
		value = prefix
	}
	matches := displayModePattern.FindAllString(value, -1)
	if len(matches) == 0 {
		return collapseRepeat(value)
	}
	unique := make([]string, 0, len(matches))
	seen := map[string]struct{}{}
	for _, match := range matches {
		if _, ok := seen[match]; ok {
			continue
		}
		seen[match] = struct{}{}
		unique = append(unique, match)
		if len(unique) == maxDisplays {
			break
		}
	}
	if len(unique) == 1 {
		return unique[0]
	}
	return strings.Join(unique, ", ")
}

// NormalizeLabel collapses an orientation or other short label that was repeated.
func NormalizeLabel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return collapseRepeat(value)
}

func collapseRepeat(value string) string {
	if prefix, ok := repeatedPrefix(value); ok {
		return prefix
	}
	if len(value) > 80 {
		return value[:80]
	}
	return value
}

func repeatedPrefix(value string) (string, bool) {
	compact := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\n', '\t', ',', ';':
			return -1
		default:
			return r
		}
	}, value)
	if compact == "" {
		return "", false
	}
	for size := 1; size <= len(compact)/2; size++ {
		if len(compact)%size != 0 {
			continue
		}
		copies := len(compact) / size
		if copies < 2 {
			continue
		}
		prefix := compact[:size]
		if strings.Repeat(prefix, copies) == compact {
			return strings.TrimSpace(prefix), true
		}
	}
	return "", false
}
