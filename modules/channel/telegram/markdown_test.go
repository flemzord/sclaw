package telegram

import "testing"

func TestEscapeMarkdownV2(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "plain text no special chars",
			input: "Hello world",
			want:  "Hello world",
		},
		{
			name:  "all special characters",
			input: `_*[]()~` + "`" + `>#+-=|{}.!`,
			want:  `\_\*\[\]\(\)\~` + "\\`" + `\>\#\+\-\=\|\{\}\.\!`,
		},
		{
			name:  "dots and exclamation",
			input: "Hello! How are you?",
			want:  `Hello\! How are you?`,
		},
		{
			name:  "parentheses in URL",
			input: "https://example.com/path(1)",
			want:  `https://example\.com/path\(1\)`,
		},
		{
			name:  "unicode text",
			input: "Bonjour le monde!",
			want:  `Bonjour le monde\!`,
		},
		{
			name:  "mixed special chars in sentence",
			input: "Use #hashtag and @mention + more",
			want:  `Use \#hashtag and @mention \+ more`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EscapeMarkdownV2(tt.input)
			if got != tt.want {
				t.Errorf("EscapeMarkdownV2(%q)\n  got  = %q\n  want = %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatMarkdownV2(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "plain text",
			input: "Hello world",
			want:  "Hello world",
		},
		{
			name:  "bold text",
			input: "This is **bold** text",
			want:  "This is *bold* text",
		},
		{
			name:  "bold with special chars inside",
			input: "Check **item.one** now",
			want:  `Check *item\.one* now`,
		},
		{
			name:  "inline code preserved",
			input: "Use `fmt.Println` here",
			want:  "Use `fmt.Println` here",
		},
		{
			name:  "code block preserved",
			input: "Before\n```go\nfmt.Println(\"hello\")\n```\nAfter",
			want:  "Before\n```go\nfmt.Println(\"hello\")\n```\nAfter",
		},
		{
			name:  "special chars escaped outside formatting",
			input: "Price: 10.5! (tax included)",
			want:  `Price: 10\.5\! \(tax included\)`,
		},
		{
			name:  "underline preserved",
			input: "This is __underline__ text",
			want:  "This is __underline__ text",
		},
		{
			name:  "mixed formatting",
			input: "**bold** and `code` and plain.",
			want:  "*bold* and `code` and plain\\.",
		},
		{
			name:  "multiline with code block",
			input: "Hello!\n```\ncode here\n```\nGoodbye!",
			want:  "Hello\\!\n```\ncode here\n```\nGoodbye\\!",
		},
		{
			name:  "unicode in formatted text",
			input: "**cafe** is nice",
			want:  "*cafe* is nice",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatMarkdownV2(tt.input)
			if got != tt.want {
				t.Errorf("FormatMarkdownV2(%q)\n  got  = %q\n  want = %q", tt.input, got, tt.want)
			}
		})
	}
}
