// Package search parses lewnotes' shared search grammar into an index filter.
package search

import (
	"fmt"
	"strings"
	"unicode"
)

// Query is a parsed search expression.
type Query struct {
	Text     []string
	NegText  []string
	Tags     []string
	NegTags  []string
	Paths    []string
	NegPaths []string
	Is       []string
	Has      []string
	NegHas   []string
	Raw      string
}

// Parse reads the grammar shared by the web palette, CLI, and agents:
//
//	term | "quoted phrase" | tag:v | path:prefix | is:unresolved|ambiguous
//	| has:embed | -term | -tag:v
//
// Unknown or unavailable filters are errors so agents never get silent
// no-op filters.
func Parse(input string) (Query, error) {
	q := Query{Raw: strings.TrimSpace(input)}
	for _, token := range tokenize(input) {
		if token == "" {
			continue
		}
		neg := false
		if strings.HasPrefix(token, "-") && len(token) > 1 {
			neg = true
			token = token[1:]
		}

		key, value, isFilter := splitFilter(token)
		if !isFilter {
			if neg {
				q.NegText = append(q.NegText, token)
			} else {
				q.Text = append(q.Text, token)
			}
			continue
		}
		value = strings.TrimSpace(unquote(value))
		if value == "" {
			return Query{}, fmt.Errorf("%s: needs a value", key)
		}

		switch key {
		case "tag":
			if neg {
				q.NegTags = append(q.NegTags, value)
			} else {
				q.Tags = append(q.Tags, value)
			}
		case "path":
			if neg {
				q.NegPaths = append(q.NegPaths, value)
			} else {
				q.Paths = append(q.Paths, value)
			}
		case "is":
			switch value {
			case "unresolved", "ambiguous":
				q.Is = append(q.Is, value)
			default:
				return Query{}, fmt.Errorf("is:%s: only unresolved and ambiguous are supported", value)
			}
		case "has":
			switch value {
			case "embed":
				if neg {
					q.NegHas = append(q.NegHas, value)
				} else {
					q.Has = append(q.Has, value)
				}
			default:
				return Query{}, fmt.Errorf("has:%s: only embed is available in this build", value)
			}
		case "todo", "project", "context":
			return Query{}, fmt.Errorf("%s: search arrives with the todos slice", key)
		default:
			return Query{}, fmt.Errorf("unknown filter %q", key)
		}
	}

	if len(q.NegText) > 0 && len(q.Text) == 0 {
		return Query{}, fmt.Errorf("negative text needs at least one positive term")
	}
	return q, nil
}

func splitFilter(token string) (key, value string, ok bool) {
	i := strings.IndexByte(token, ':')
	if i <= 0 {
		return "", "", false
	}
	key = strings.ToLower(token[:i])
	for _, r := range key {
		if (r < 'a' || r > 'z') && r != '_' && r != '-' {
			return "", "", false
		}
	}
	return key, token[i+1:], true
}

func unquote(s string) string {
	if len(s) >= 2 && strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`) {
		return strings.ReplaceAll(s[1:len(s)-1], `\"`, `"`)
	}
	return s
}

// FTSMatch builds an FTS5 expression from the free-text terms.
func (q Query) FTSMatch() string {
	parts := make([]string, 0, len(q.Text)+len(q.NegText))
	for _, term := range q.Text {
		parts = append(parts, ftsTerm(term))
	}
	if len(parts) == 0 {
		return ""
	}
	expr := strings.Join(parts, " ")
	for _, term := range q.NegText {
		expr += " NOT " + ftsTerm(term)
	}
	return expr
}

func ftsTerm(term string) string {
	phrase := strings.HasPrefix(term, `"`) && strings.HasSuffix(term, `"`) && len(term) >= 2
	inner := term
	prefix := "*"
	if phrase {
		inner = term[1 : len(term)-1]
		prefix = ""
	}
	// FTS5 allows a prefix marker on a single token, not on a phrase. Terms
	// with punctuation tokenize into several words, so index them as a phrase.
	for _, r := range inner {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			prefix = ""
			break
		}
	}
	inner = strings.ReplaceAll(inner, `"`, `""`)
	return `"` + inner + `"` + prefix
}

// tokenize splits on whitespace while keeping quoted phrases intact.
func tokenize(input string) []string {
	var tokens []string
	var current strings.Builder
	inQuote := false
	escaped := false

	flush := func() {
		if current.Len() > 0 {
			tokens = append(tokens, current.String())
			current.Reset()
		}
	}

	for _, r := range input {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}
		switch {
		case r == '\\':
			escaped = true
		case r == '"':
			inQuote = !inQuote
			current.WriteRune(r)
		case (r == ' ' || r == '\t' || r == '\n') && !inQuote:
			flush()
		default:
			current.WriteRune(r)
		}
	}
	flush()
	return tokens
}
