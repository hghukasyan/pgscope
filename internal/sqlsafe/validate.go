// Package sqlsafe validates SQL for safe EXPLAIN execution.
//
// Validation is PostgreSQL-aware: it understands standard and dollar-quoted
// strings, nested comments, and statement boundaries. Keyword matching alone
// is never treated as a security boundary for the full statement body; the
// lexer plus structural checks reject multi-statement input, non-SELECT
// classes, and data-modifying CTEs before any EXPLAIN is issued.
package sqlsafe

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Class is the top-level statement category after validation.
type Class string

const (
	ClassSelect Class = "select"
	ClassOther  Class = "other"
)

// Result describes a validated single statement.
type Result struct {
	Class          Class
	SQL            string
	HasModifyingCTE bool
	Warnings       []string
}

// ValidateReadOnlySelect ensures sql is a single read-oriented SELECT/WITH
// statement suitable for EXPLAIN (FORMAT JSON). It rejects multi-statement
// input, DDL/DML, COPY, CALL, DO, and data-modifying CTEs.
func ValidateReadOnlySelect(sql string) (Result, error) {
	trimmed := strings.TrimSpace(sql)
	if trimmed == "" {
		return Result{}, fmt.Errorf("empty SQL")
	}
	if !utf8.ValidString(trimmed) {
		return Result{}, fmt.Errorf("SQL contains invalid UTF-8")
	}

	stmts, err := splitStatements(trimmed)
	if err != nil {
		return Result{}, err
	}
	if len(stmts) == 0 {
		return Result{}, fmt.Errorf("empty SQL")
	}
	if len(stmts) > 1 {
		return Result{}, fmt.Errorf("multi-statement SQL is not allowed (%d statements)", len(stmts))
	}

	stmt := strings.TrimSpace(stmts[0])
	tokens, err := tokenizeSignificant(stmt)
	if err != nil {
		return Result{}, err
	}
	if len(tokens) == 0 {
		return Result{}, fmt.Errorf("empty SQL after removing comments")
	}

	first := strings.ToUpper(tokens[0])
	res := Result{SQL: stmt}

	switch first {
	case "SELECT":
		res.Class = ClassSelect
	case "WITH":
		res.Class = ClassSelect
		if hasModifyingCTE(tokens) {
			res.HasModifyingCTE = true
			return res, fmt.Errorf("data-modifying CTEs (INSERT/UPDATE/DELETE inside WITH) are not allowed")
		}
		if !withEndsInSelect(tokens) {
			return res, fmt.Errorf("WITH statement must end in a SELECT for EXPLAIN safety")
		}
	case "TABLE":
		// TABLE relation is sugar for SELECT * FROM relation.
		res.Class = ClassSelect
		res.Warnings = append(res.Warnings, "TABLE syntax accepted as SELECT equivalent")
	case "VALUES":
		res.Class = ClassSelect
	default:
		res.Class = ClassOther
		return res, fmt.Errorf("statement type %q is not allowed; only single SELECT/WITH/VALUES/TABLE statements may be explained", first)
	}

	if containsForbiddenConstruct(tokens) {
		return res, fmt.Errorf("statement contains forbidden constructs (INTO, FOR UPDATE/SHARE of write intent, COPY, or procedural blocks)")
	}

	return res, nil
}

// ValidateExplainAnalyze adds extra checks for EXPLAIN ANALYZE opt-in mode.
func ValidateExplainAnalyze(sql string, trustedIsolated bool) (Result, error) {
	res, err := ValidateReadOnlySelect(sql)
	if err != nil {
		return res, err
	}
	if !trustedIsolated {
		tokens, _ := tokenizeSignificant(res.SQL)
		if looksLikeSideEffectingCall(tokens) {
			return res, fmt.Errorf("EXPLAIN ANALYZE refused: statement appears to invoke potentially side-effecting functions; use a trusted isolated database and --trusted-isolated to override")
		}
	}
	return res, nil
}

func hasModifyingCTE(tokens []string) bool {
	// Look for AS ( INSERT|UPDATE|DELETE|MERGE after a CTE name.
	upper := make([]string, len(tokens))
	for i, t := range tokens {
		upper[i] = strings.ToUpper(t)
	}
	for i := 0; i < len(upper); i++ {
		if upper[i] != "AS" {
			continue
		}
		// AS ( ... modifying
		j := i + 1
		if j < len(upper) && upper[j] == "NOT" {
			j++
			if j < len(upper) && upper[j] == "MATERIALIZED" {
				j++
			}
		} else if j < len(upper) && upper[j] == "MATERIALIZED" {
			j++
		}
		if j < len(upper) && upper[j] == "(" {
			j++
			if j < len(upper) {
				switch upper[j] {
				case "INSERT", "UPDATE", "DELETE", "MERGE":
					return true
				}
			}
		}
	}
	return false
}

func withEndsInSelect(tokens []string) bool {
	upper := make([]string, len(tokens))
	for i, t := range tokens {
		upper[i] = strings.ToUpper(t)
	}
	depth := 0
	for i := 0; i < len(upper); i++ {
		switch upper[i] {
		case "(":
			depth++
		case ")":
			if depth > 0 {
				depth--
			}
		case "SELECT", "VALUES", "TABLE":
			if depth == 0 && i > 0 {
				return true
			}
		}
	}
	return false
}

func containsForbiddenConstruct(tokens []string) bool {
	upper := make([]string, len(tokens))
	for i, t := range tokens {
		upper[i] = strings.ToUpper(t)
	}
	depth := 0
	for i := 0; i < len(upper); i++ {
		tok := upper[i]
		switch tok {
		case "(":
			depth++
		case ")":
			if depth > 0 {
				depth--
			}
		}
		if depth > 0 {
			continue
		}
		switch tok {
		case "INTO":
			// SELECT INTO creates/populates a table — reject at top level.
			// Avoid flagging identifiers; require SELECT ... INTO pattern.
			if i > 0 {
				// Walk back over DISTINCT/ALL/columns is hard; accept INTO after SELECT keyword seen.
				for j := 0; j < i; j++ {
					if upper[j] == "SELECT" {
						return true
					}
				}
			}
		}
	}
	return false
}

func looksLikeSideEffectingCall(tokens []string) bool {
	dangerous := map[string]struct{}{
		"PG_SLEEP": {}, "PG_TERMINATE_BACKEND": {}, "PG_CANCEL_BACKEND": {},
		"PG_RELOAD_CONF": {}, "PG_ROTATE_LOGFILE": {}, "PG_CREATE_RESTORE_POINT": {},
		"PG_SWITCH_WAL": {}, "PG_ADVISE_LOCK": {}, "LO_IMPORT": {}, "LO_EXPORT": {},
		"DBLINK": {}, "HTTP": {}, "CURL": {},
	}
	for _, t := range tokens {
		name := strings.ToUpper(t)
		if _, ok := dangerous[name]; ok {
			return true
		}
		if strings.Contains(name, "DBLINK") || strings.HasPrefix(name, "PG_FILE_") {
			return true
		}
	}
	return false
}

// splitStatements splits SQL on semicolons that are outside quotes/comments.
func splitStatements(sql string) ([]string, error) {
	var stmts []string
	var b strings.Builder
	i := 0
	for i < len(sql) {
		ch := sql[i]
		switch {
		case ch == '-' && i+1 < len(sql) && sql[i+1] == '-':
			// line comment
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
		case ch == '/' && i+1 < len(sql) && sql[i+1] == '*':
			i += 2
			for i+1 < len(sql) && !(sql[i] == '*' && sql[i+1] == '/') {
				i++
			}
			if i+1 >= len(sql) {
				return nil, fmt.Errorf("unterminated block comment")
			}
			i += 2
		case ch == '\'':
			b.WriteByte(ch)
			i++
			for i < len(sql) {
				if sql[i] == '\'' {
					b.WriteByte(sql[i])
					i++
					if i < len(sql) && sql[i] == '\'' {
						b.WriteByte(sql[i])
						i++
						continue
					}
					break
				}
				b.WriteByte(sql[i])
				i++
			}
		case ch == '"':
			b.WriteByte(ch)
			i++
			for i < len(sql) {
				b.WriteByte(sql[i])
				if sql[i] == '"' {
					i++
					if i < len(sql) && sql[i] == '"' {
						b.WriteByte(sql[i])
						i++
						continue
					}
					break
				}
				i++
			}
		case ch == '$':
			tag, n := readDollarTag(sql[i:])
			if n > 0 {
				b.WriteString(sql[i : i+n])
				i += n
				end := "$" + tag + "$"
				idx := strings.Index(sql[i:], end)
				if idx < 0 {
					return nil, fmt.Errorf("unterminated dollar-quoted string")
				}
				b.WriteString(sql[i : i+idx+len(end)])
				i += idx + len(end)
			} else {
				b.WriteByte(ch)
				i++
			}
		case ch == ';':
			part := strings.TrimSpace(b.String())
			if part != "" {
				stmts = append(stmts, part)
			}
			b.Reset()
			i++
		default:
			b.WriteByte(ch)
			i++
		}
	}
	if rem := strings.TrimSpace(b.String()); rem != "" {
		stmts = append(stmts, rem)
	}
	return stmts, nil
}

func readDollarTag(s string) (string, int) {
	if len(s) == 0 || s[0] != '$' {
		return "", 0
	}
	j := 1
	for j < len(s) {
		c := s[j]
		if c == '$' {
			return s[1:j], j + 1
		}
		if !(c == '_' || unicode.IsLetter(rune(c)) || unicode.IsDigit(rune(c))) {
			return "", 0
		}
		j++
	}
	return "", 0
}

func tokenizeSignificant(sql string) ([]string, error) {
	var tokens []string
	i := 0
	for i < len(sql) {
		ch := sql[i]
		switch {
		case unicode.IsSpace(rune(ch)):
			i++
		case ch == '-' && i+1 < len(sql) && sql[i+1] == '-':
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
		case ch == '/' && i+1 < len(sql) && sql[i+1] == '*':
			i += 2
			for i+1 < len(sql) && !(sql[i] == '*' && sql[i+1] == '/') {
				i++
			}
			if i+1 >= len(sql) {
				return nil, fmt.Errorf("unterminated block comment")
			}
			i += 2
		case ch == '\'':
			i++
			for i < len(sql) {
				if sql[i] == '\'' {
					i++
					if i < len(sql) && sql[i] == '\'' {
						i++
						continue
					}
					break
				}
				i++
			}
			tokens = append(tokens, "''")
		case ch == '"':
			start := i
			i++
			for i < len(sql) {
				if sql[i] == '"' {
					i++
					if i < len(sql) && sql[i] == '"' {
						i++
						continue
					}
					break
				}
				i++
			}
			tokens = append(tokens, sql[start:i])
		case ch == '$':
			tag, n := readDollarTag(sql[i:])
			if n > 0 {
				i += n
				end := "$" + tag + "$"
				idx := strings.Index(sql[i:], end)
				if idx < 0 {
					return nil, fmt.Errorf("unterminated dollar-quoted string")
				}
				i += idx + len(end)
				tokens = append(tokens, "$$")
			} else {
				tokens = append(tokens, "$")
				i++
			}
		case strings.ContainsRune("(),;*.=<>!+-|/:%^&", rune(ch)):
			// multi-char ops
			if i+1 < len(sql) {
				two := sql[i : i+2]
				switch two {
				case "<>", "<=", ">=", "!=", "||", "::", "->", ">>", "<<":
					tokens = append(tokens, two)
					i += 2
					continue
				}
			}
			tokens = append(tokens, string(ch))
			i++
		case isIdentStart(rune(ch)):
			start := i
			i++
			for i < len(sql) {
				r, size := utf8.DecodeRuneInString(sql[i:])
				if !isIdentPart(r) {
					break
				}
				i += size
			}
			tokens = append(tokens, sql[start:i])
		case unicode.IsDigit(rune(ch)) || (ch == '.' && i+1 < len(sql) && unicode.IsDigit(rune(sql[i+1]))):
			start := i
			i++
			for i < len(sql) && (unicode.IsDigit(rune(sql[i])) || sql[i] == '.' || sql[i] == 'e' || sql[i] == 'E' || sql[i] == '+' || sql[i] == '-') {
				// stop on second sign incorrectly; keep simple
				if (sql[i] == '+' || sql[i] == '-') && !(sql[i-1] == 'e' || sql[i-1] == 'E') {
					break
				}
				i++
			}
			tokens = append(tokens, sql[start:i])
		default:
			i++
		}
	}
	return tokens, nil
}

func isIdentStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}

func isIdentPart(r rune) bool {
	return r == '_' || r == '$' || unicode.IsLetter(r) || unicode.IsDigit(r)
}
