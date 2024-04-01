package database

import (
	"strings"
	"unicode"
)

// SanitizeQueryPreview returns a single-line, truncated query string suitable
// for reports. It never attempts to recover bound parameter values.
func SanitizeQueryPreview(query string, maxLen int) string {
	if maxLen <= 0 {
		maxLen = 120
	}
	q := strings.TrimSpace(query)
	if q == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(q))
	prevSpace := false
	for _, r := range q {
		if unicode.IsSpace(r) {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		prevSpace = false
		b.WriteRune(r)
	}
	out := b.String()
	if len(out) > maxLen {
		if maxLen <= 3 {
			return out[:maxLen]
		}
		return out[:maxLen-3] + "..."
	}
	return out
}
