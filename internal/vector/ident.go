package vector

import (
	"fmt"
	"strings"
	"unicode"
)

// QuoteIdent safely double-quotes a PostgreSQL identifier.
func QuoteIdent(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("empty identifier")
	}
	if strings.ContainsRune(name, '\x00') {
		return "", fmt.Errorf("identifier contains NUL")
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`, nil
}

// QuoteQualified quotes schema.table or schema.table.column parts.
func QuoteQualified(parts ...string) (string, error) {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		q, err := QuoteIdent(p)
		if err != nil {
			return "", err
		}
		out = append(out, q)
	}
	return strings.Join(out, "."), nil
}

// ValidateSimpleIdent allows only letters, digits, underscore (for whitelist keys).
func ValidateSimpleIdent(name string) error {
	if name == "" {
		return fmt.Errorf("empty identifier")
	}
	for i, r := range name {
		if r == '_' || unicode.IsLetter(r) {
			continue
		}
		if i > 0 && unicode.IsDigit(r) {
			continue
		}
		return fmt.Errorf("identifier %q contains unsupported characters", name)
	}
	return nil
}
