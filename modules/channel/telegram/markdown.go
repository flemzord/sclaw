package telegram

import "strings"

// markdownV2SpecialChars lists all characters that must be escaped in Telegram MarkdownV2.
var markdownV2SpecialChars = strings.NewReplacer(
	`_`, `\_`,
	`*`, `\*`,
	`[`, `\[`,
	`]`, `\]`,
	`(`, `\(`,
	`)`, `\)`,
	`~`, `\~`,
	"`", "\\`",
	`>`, `\>`,
	`#`, `\#`,
	`+`, `\+`,
	`-`, `\-`,
	`=`, `\=`,
	`|`, `\|`,
	`{`, `\{`,
	`}`, `\}`,
	`.`, `\.`,
	`!`, `\!`,
)

// EscapeMarkdownV2 escapes all special characters for Telegram MarkdownV2 format.
// Special chars: _ * [ ] ( ) ~ ` > # + - = | { } . !
func EscapeMarkdownV2(text string) string {
	return markdownV2SpecialChars.Replace(text)
}

// FormatMarkdownV2 converts standard markdown to Telegram MarkdownV2 format.
// It preserves **bold**, `code`, ```code blocks```, and escapes everything else.
func FormatMarkdownV2(text string) string {
	lines := strings.Split(text, "\n")
	var result strings.Builder
	inCodeBlock := false

	for i, line := range lines {
		if i > 0 {
			result.WriteByte('\n')
		}

		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			result.WriteString(line)
			continue
		}

		if inCodeBlock {
			result.WriteString(line)
			continue
		}

		result.WriteString(formatLine(line))
	}

	return result.String()
}

// formatLine processes a single line of standard markdown, converting it
// to Telegram MarkdownV2 format.
func formatLine(line string) string {
	var result strings.Builder
	runes := []rune(line)
	n := len(runes)
	i := 0

	for i < n {
		// Inline code: ` ... `
		if runes[i] == '`' {
			end := findClosing(runes, i+1, '`')
			if end > 0 {
				// Write inline code as-is (no escaping inside).
				result.WriteString(string(runes[i : end+1]))
				i = end + 1
				continue
			}
		}

		// Bold: **text** → *text* (Telegram uses single asterisk for bold).
		if i+1 < n && runes[i] == '*' && runes[i+1] == '*' {
			end := findDoubleClosing(runes, i+2, '*')
			if end > 0 {
				inner := string(runes[i+2 : end])
				result.WriteByte('*')
				result.WriteString(EscapeMarkdownV2(inner))
				result.WriteByte('*')
				i = end + 2
				continue
			}
		}

		// Underline: __text__ (Telegram uses double underscore for underline).
		if i+1 < n && runes[i] == '_' && runes[i+1] == '_' {
			end := findDoubleClosing(runes, i+2, '_')
			if end > 0 {
				inner := string(runes[i+2 : end])
				result.WriteString("__")
				result.WriteString(EscapeMarkdownV2(inner))
				result.WriteString("__")
				i = end + 2
				continue
			}
		}

		// Everything else: escape.
		result.WriteString(EscapeMarkdownV2(string(runes[i])))
		i++
	}

	return result.String()
}

// findClosing finds the index of the closing delimiter starting from pos.
// Returns -1 if not found.
func findClosing(runes []rune, start int, delim rune) int {
	for i := start; i < len(runes); i++ {
		if runes[i] == delim {
			return i
		}
	}
	return -1
}

// findDoubleClosing finds the index of a double-character closing delimiter
// (e.g., ** or __) starting from pos. Returns the index of the first character
// of the closing pair, or -1 if not found.
func findDoubleClosing(runes []rune, start int, delim rune) int {
	for i := start; i < len(runes)-1; i++ {
		if runes[i] == delim && runes[i+1] == delim {
			return i
		}
	}
	return -1
}
