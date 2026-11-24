// Package markdown parses note files and edits their YAML frontmatter with the
// smallest possible textual change.
//
// The only field lewnotes owns is the top-level `id:`. Everything else is
// preserved byte for byte, including key order, quoting, and unknown keys.
package markdown

import (
	"bytes"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"
	"gopkg.in/yaml.v3"
)

// Document is a parsed note. Raw and Body are byte slices into the same data.
type Document struct {
	Raw            []byte
	Body           []byte
	Frontmatter    map[string]any
	HasFrontmatter bool

	fmStart    int
	fmEnd      int
	insertAt   int
	closingEOL string
	idLine     *lineSpan
}

type lineSpan struct {
	start int
	end   int
	eol   string
}

// Parse splits frontmatter from body. A file that starts with an unterminated
// `---` block is treated as body-only rather than as an error.
func Parse(data []byte) (*Document, error) {
	doc := &Document{Raw: data, Body: data, Frontmatter: map[string]any{}}

	firstEnd := nextLine(data, 0)
	if firstEnd < 0 {
		return doc, nil
	}
	if !isDelimiter(data[:trimEOL(data, firstEnd)]) {
		return doc, nil
	}

	pos := firstEnd
	fmStart := pos
	for pos < len(data) {
		le := nextLine(data, pos)
		var line []byte
		if le < 0 {
			line = data[pos:]
		} else {
			line = data[pos:trimEOL(data, le)]
		}

		if isDelimiter(line) {
			doc.HasFrontmatter = true
			doc.fmStart = fmStart
			doc.fmEnd = pos
			doc.insertAt = pos
			doc.closingEOL = eolAt(data, pos, le)
			if err := yaml.Unmarshal(data[fmStart:pos], &doc.Frontmatter); err != nil {
				return nil, fmt.Errorf("parse frontmatter: %w", err)
			}
			if doc.Frontmatter == nil {
				doc.Frontmatter = map[string]any{}
			}
			if le >= 0 {
				doc.Body = data[le:]
			}
			doc.idLine = findIDLine(data, fmStart, pos)
			return doc, nil
		}
		if le < 0 {
			break
		}
		pos = le
	}
	return doc, nil
}

func nextLine(data []byte, pos int) int {
	if pos >= len(data) {
		return -1
	}
	i := bytes.IndexByte(data[pos:], '\n')
	if i < 0 {
		return -1
	}
	return pos + i + 1
}

// trimEOL returns the index just past the line content (excluding CR/LF).
func trimEOL(data []byte, lineEnd int) int {
	if lineEnd > 0 && data[lineEnd-1] == '\n' {
		lineEnd--
	}
	if lineEnd > 0 && data[lineEnd-1] == '\r' {
		lineEnd--
	}
	return lineEnd
}

func isDelimiter(line []byte) bool {
	return bytes.Equal(bytes.TrimRight(line, " \t\r"), []byte("---"))
}

func eolAt(data []byte, start, end int) string {
	if end > 0 && data[end-1] == '\n' {
		if end >= 2 && data[end-2] == '\r' {
			return "\r\n"
		}
		return "\n"
	}
	return "\n"
}

// findIDLine locates a top-level `id:` line inside the frontmatter block.
func findIDLine(data []byte, fmStart, fmEnd int) *lineSpan {
	pos := fmStart
	for pos < fmEnd {
		le := nextLine(data, pos)
		end := fmEnd
		if le >= 0 && le <= fmEnd {
			end = le
		}
		line := data[pos:trimEOL(data, end)]
		if isTopLevelKey(line, "id") {
			return &lineSpan{start: pos, end: end, eol: eolAt(data, pos, end)}
		}
		if le < 0 {
			break
		}
		pos = le
	}
	return nil
}

func isTopLevelKey(line []byte, key string) bool {
	if len(line) == 0 || line[0] == ' ' || line[0] == '\t' || line[0] == '#' {
		return false
	}
	prefix := key + ":"
	if !bytes.HasPrefix(line, []byte(prefix)) {
		return false
	}
	rest := line[len(prefix):]
	return len(rest) == 0 || rest[0] == ' ' || rest[0] == '\t' || rest[0] == '\r'
}

// ID returns the frontmatter id, or "" when absent.
func (d *Document) ID() string {
	for k, v := range d.Frontmatter {
		if !strings.EqualFold(k, "id") {
			continue
		}
		if s, ok := v.(string); ok {
			return strings.TrimSpace(s)
		}
		return strings.TrimSpace(fmt.Sprint(v))
	}
	return ""
}

// ValidID reports whether s is a canonical ULID.
func ValidID(s string) bool {
	if len(s) != 26 {
		return false
	}
	_, err := ulid.Parse(s)
	return err == nil
}

// EnsureID writes newID when the document has no valid id. It returns the
// resulting bytes and whether the caller must rewrite the file.
func EnsureID(data []byte, newID string) ([]byte, bool, error) {
	return writeID(data, newID, false)
}

// SetID writes newID into the frontmatter even when a different id is present.
// It is used when a duplicate id must be replaced.
func SetID(data []byte, newID string) ([]byte, bool, error) {
	return writeID(data, newID, true)
}

func writeID(data []byte, newID string, force bool) ([]byte, bool, error) {
	if !ValidID(newID) {
		return nil, false, fmt.Errorf("invalid ULID %q", newID)
	}
	doc, err := Parse(data)
	if err != nil {
		return nil, false, err
	}
	if !force && ValidID(doc.ID()) {
		return data, false, nil
	}
	if force && doc.ID() == newID {
		return data, false, nil
	}

	if doc.HasFrontmatter {
		if doc.idLine != nil {
			out := make([]byte, 0, len(data)+len(newID))
			out = append(out, data[:doc.idLine.start]...)
			out = append(out, "id: "+newID...)
			out = append(out, doc.idLine.eol...)
			out = append(out, data[doc.idLine.end:]...)
			return out, true, nil
		}
		out := make([]byte, 0, len(data)+len(newID)+8)
		out = append(out, data[:doc.insertAt]...)
		out = append(out, ("id: " + newID + doc.closingEOL)...)
		out = append(out, data[doc.insertAt:]...)
		return out, true, nil
	}

	eol := detectEOL(data)
	var buf bytes.Buffer
	buf.Grow(len(data) + len(newID) + 16)
	buf.WriteString("---" + eol)
	buf.WriteString("id: " + newID + eol)
	buf.WriteString("---" + eol)
	buf.Write(data)
	return buf.Bytes(), true, nil
}

func detectEOL(data []byte) string {
	if i := bytes.IndexByte(data, '\n'); i > 0 && data[i-1] == '\r' {
		return "\r\n"
	}
	return "\n"
}

// Title returns the frontmatter title, else the first ATX H1 outside code
// fences, else "".
func (d *Document) Title() string {
	if t, ok := d.Frontmatter["title"].(string); ok {
		if trimmed := strings.TrimSpace(t); trimmed != "" {
			return trimmed
		}
	}
	return headingTitle(d.Body)
}

func headingTitle(body []byte) string {
	inFence := false
	for _, line := range bytes.Split(body, []byte("\n")) {
		trimmed := strings.TrimSpace(string(line))
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if strings.HasPrefix(trimmed, "# ") {
			title := strings.TrimSpace(strings.TrimRight(strings.TrimPrefix(trimmed, "# "), "#"))
			if title != "" {
				return title
			}
		}
	}
	return ""
}

// Excerpt collapses whitespace and truncates to at most max characters.
func Excerpt(body []byte, max int) string {
	flat := strings.Join(strings.Fields(string(body)), " ")
	if max <= 0 || utf8.RuneCountInString(flat) <= max {
		return flat
	}
	runes := []rune(flat)
	return strings.TrimSpace(string(runes[:max]))
}
