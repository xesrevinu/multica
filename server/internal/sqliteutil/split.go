package sqliteutil

import (
	"fmt"
	"strings"
	"unicode"
)

// splitSQL splits a SQLite script into statements database/sql can Exec one
// at a time. Semicolons inside quotes, comments, and trigger BEGIN/END bodies
// (including CASE/END) are not separators. BEGIN TRANSACTION and the other
// transaction-start forms are not treated as blocks.
func splitSQL(script string) ([]string, error) {
	var stmts []string
	var b strings.Builder
	depth := 0
	inSingle, inDouble, inBacktick, inBracket := false, false, false, false
	inLineComment, inBlockComment := false, false

	flush := func() {
		stmt := strings.TrimSpace(b.String())
		b.Reset()
		if stmt != "" {
			stmts = append(stmts, stmt)
		}
	}

	for i := 0; i < len(script); i++ {
		c := script[i]
		var next byte
		if i+1 < len(script) {
			next = script[i+1]
		}

		switch {
		case inLineComment:
			b.WriteByte(c)
			if c == '\n' {
				inLineComment = false
			}
		case inBlockComment:
			b.WriteByte(c)
			if c == '*' && next == '/' {
				b.WriteByte(next)
				i++
				inBlockComment = false
			}
		case inSingle:
			b.WriteByte(c)
			if c == '\'' {
				if next == '\'' {
					b.WriteByte(next)
					i++
					continue
				}
				inSingle = false
			}
		case inDouble:
			b.WriteByte(c)
			if c == '"' {
				if next == '"' {
					b.WriteByte(next)
					i++
					continue
				}
				inDouble = false
			}
		case inBacktick:
			b.WriteByte(c)
			if c == '`' {
				if next == '`' {
					b.WriteByte(next)
					i++
					continue
				}
				inBacktick = false
			}
		case inBracket:
			b.WriteByte(c)
			if c == ']' {
				inBracket = false
			}
		case c == '-' && next == '-':
			b.WriteByte(c)
			b.WriteByte(next)
			i++
			inLineComment = true
		case c == '/' && next == '*':
			b.WriteByte(c)
			b.WriteByte(next)
			i++
			inBlockComment = true
		case c == '\'':
			inSingle = true
			b.WriteByte(c)
		case c == '"':
			inDouble = true
			b.WriteByte(c)
		case c == '`':
			inBacktick = true
			b.WriteByte(c)
		case c == '[':
			inBracket = true
			b.WriteByte(c)
		case c == ';' && depth == 0:
			flush()
		case isIdentStart(c):
			j := i + 1
			for j < len(script) && isIdentCont(script[j]) {
				j++
			}
			word := script[i:j]
			b.WriteString(word)
			switch strings.ToUpper(word) {
			case "BEGIN":
				if !isTransactionBegin(script, j) {
					depth++
				}
			case "CASE":
				depth++
			case "END":
				if depth > 0 {
					depth--
				}
			}
			i = j - 1
		default:
			b.WriteByte(c)
		}
	}

	switch {
	case inSingle || inDouble || inBacktick || inBracket:
		return nil, fmt.Errorf("unterminated SQL literal")
	case inBlockComment:
		return nil, fmt.Errorf("unterminated block comment")
	case depth != 0:
		return nil, fmt.Errorf("unbalanced BEGIN/CASE/END (depth %d)", depth)
	}
	flush()
	return stmts, nil
}

// isTransactionBegin reports whether the BEGIN keyword at the preceding
// position starts a transaction (BEGIN, BEGIN TRANSACTION, BEGIN IMMEDIATE,
// BEGIN DEFERRED, BEGIN EXCLUSIVE) rather than a trigger body.
func isTransactionBegin(script string, i int) bool {
	for i < len(script) {
		switch script[i] {
		case ' ', '\t', '\n', '\r':
			i++
			continue
		case '-':
			if i+1 < len(script) && script[i+1] == '-' {
				for i < len(script) && script[i] != '\n' {
					i++
				}
				continue
			}
		case '/':
			if i+1 < len(script) && script[i+1] == '*' {
				i += 2
				for i+1 < len(script) && !(script[i] == '*' && script[i+1] == '/') {
					i++
				}
				if i+1 < len(script) {
					i += 2
				}
				continue
			}
		}
		break
	}
	if i >= len(script) || script[i] == ';' {
		return true
	}
	if !isIdentStart(script[i]) {
		return false
	}
	j := i + 1
	for j < len(script) && isIdentCont(script[j]) {
		j++
	}
	switch strings.ToUpper(script[i:j]) {
	case "TRANSACTION", "DEFERRED", "IMMEDIATE", "EXCLUSIVE":
		return true
	default:
		return false
	}
}

func isIdentStart(c byte) bool {
	return c == '_' || unicode.IsLetter(rune(c))
}

func isIdentCont(c byte) bool {
	return c == '_' || unicode.IsLetter(rune(c)) || unicode.IsDigit(rune(c))
}
