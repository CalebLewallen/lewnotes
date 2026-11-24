// Package links parses Obsidian-style wikilinks and resolves their targets
// against the set of note paths.
package links

import (
	"path"
	"sort"
	"strings"
)

// Kind distinguishes a link from an embed.
type Kind string

const (
	KindLink  Kind = "link"
	KindEmbed Kind = "embed"
)

// Ref is one wikilink occurrence.
type Ref struct {
	Target   string
	Alias    string
	Fragment string
	Kind     Kind
	Line     int
}

// Candidate is one note in the vault, used for resolution.
type Candidate struct {
	ID   string
	Path string
}

// Resolution is the outcome of resolving one target.
type Resolution struct {
	Status     string // exact | suffix | unresolved | ambiguous
	ID         string
	Path       string
	Candidates []string // sorted paths, only for ambiguous
}

// Parse finds every [[wikilink]] and ![[embed]] in a markdown body.
// Links may not span a newline. Escaped openers (`\[[`) are ignored.
func Parse(body []byte) []Ref {
	refs := []Ref{}
	line := 1
	for i := 0; i < len(body); {
		switch body[i] {
		case '\n':
			line++
			i++
			continue
		case '[':
			if i+1 >= len(body) || body[i+1] != '[' || (i > 0 && body[i-1] == '\\') {
				i++
				continue
			}
			end := -1
			for j := i + 2; j+1 < len(body); j++ {
				if body[j] == '\n' {
					break
				}
				if body[j] == ']' && body[j+1] == ']' {
					end = j
					break
				}
			}
			if end < 0 {
				i++
				continue
			}
			kind := KindLink
			if i > 0 && body[i-1] == '!' {
				kind = KindEmbed
			}
			if ref, ok := parseInner(string(body[i+2:end]), line, kind); ok {
				refs = append(refs, ref)
			}
			i = end + 2
			continue
		}
		i++
	}
	return refs
}

func parseInner(inner string, line int, kind Kind) (Ref, bool) {
	inner = strings.TrimSpace(inner)
	if inner == "" {
		return Ref{}, false
	}

	targetPart := inner
	alias := ""
	if before, after, ok := cutUnescaped(inner, '|'); ok {
		targetPart = strings.TrimSpace(before)
		alias = strings.TrimSpace(strings.ReplaceAll(after, `\|`, "|"))
	}
	if targetPart == "" {
		return Ref{}, false
	}

	fragment := ""
	if before, after, ok := cutUnescaped(targetPart, '#'); ok {
		targetPart = strings.TrimSpace(before)
		fragment = strings.TrimSpace(after)
	}
	targetPart = strings.TrimSpace(strings.ReplaceAll(targetPart, `\|`, "|"))
	if targetPart == "" {
		return Ref{}, false
	}
	return Ref{Target: targetPart, Alias: alias, Fragment: fragment, Kind: kind, Line: line}, true
}

func cutUnescaped(s string, sep byte) (string, string, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] != sep {
			continue
		}
		if i > 0 && s[i-1] == '\\' {
			continue
		}
		return s[:i], s[i+1:], true
	}
	return s, "", false
}

// Normalize canonicalizes a link target or note path for comparison: POSIX
// separators, lowercase, no leading slash or ./, and no .md extension.
func Normalize(target string) string {
	target = strings.TrimSpace(strings.ReplaceAll(target, `\`, "/"))
	target = strings.TrimPrefix(target, "./")
	target = strings.TrimPrefix(target, "/")
	target = path.Clean(target)
	if target == "." || target == ".." || strings.HasPrefix(target, "../") {
		return ""
	}
	target = strings.ToLower(target)
	return strings.TrimSuffix(target, ".md")
}

// Resolve applies Obsidian-compatible resolution: exact full path first, then
// a unique trailing-segment match, else unresolved or ambiguous.
func Resolve(target string, candidates []Candidate) Resolution {
	norm := Normalize(target)
	if norm == "" {
		return Resolution{Status: "unresolved"}
	}

	var exact []Candidate
	for _, c := range candidates {
		if Normalize(c.Path) == norm {
			exact = append(exact, c)
		}
	}
	switch len(exact) {
	case 1:
		return Resolution{Status: "exact", ID: exact[0].ID, Path: exact[0].Path}
	case 0:
	default:
		return ambiguous(exact)
	}

	var suffix []Candidate
	for _, c := range candidates {
		candidate := Normalize(c.Path)
		if candidate == "" {
			continue
		}
		if strings.HasSuffix(candidate, "/"+norm) {
			suffix = append(suffix, c)
		}
	}
	switch len(suffix) {
	case 0:
		return Resolution{Status: "unresolved"}
	case 1:
		return Resolution{Status: "suffix", ID: suffix[0].ID, Path: suffix[0].Path}
	default:
		return ambiguous(suffix)
	}
}

func ambiguous(candidates []Candidate) Resolution {
	seen := map[string]bool{}
	paths := make([]string, 0, len(candidates))
	for _, c := range candidates {
		if seen[c.Path] {
			continue
		}
		seen[c.Path] = true
		paths = append(paths, c.Path)
	}
	sort.Strings(paths)
	return Resolution{Status: "ambiguous", Candidates: paths}
}

// Resolved reports whether a resolution points at a note.
func (r Resolution) Resolved() bool {
	return r.Status == "exact" || r.Status == "suffix"
}
