// Package skills provides embedded skill files compiled into the binary.
package skills

import "embed"

// BuiltinFS embeds all skill files shipped with the binary.
// Non-.md files (like this .go file) are included but ignored by the skill parser.
//
//go:embed all:*
var BuiltinFS embed.FS
