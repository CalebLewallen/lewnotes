// Package skills embeds the agent skill shipped with the lewnotes binary.
package skills

import _ "embed"

// Content is the lewnotes-cli SKILL.md.
//
//go:embed lewnotes-cli/SKILL.md
var Content string
