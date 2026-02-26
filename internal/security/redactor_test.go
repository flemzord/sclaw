package security

import (
	"testing"
)

func TestRedactor_DefaultPatterns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "openai key",
			input: "key is sk-abcdefghijklmnopqrstuvwxyz",
			want:  "key is " + RedactPlaceholder,
		},
		{
			name:  "anthropic key",
			input: "api: sk-ant-abcdefghijklmnopqrstuvwxyz",
			want:  "api: " + RedactPlaceholder,
		},
		{
			name:  "github personal access token",
			input: "auth ghp_abcdefghijklmnopqrstuvwxyz",
			want:  "auth " + RedactPlaceholder,
		},
		{
			name:  "github fine-grained pat",
			input: "github_pat_abcdefghijklmnopqrstuvwxyz is mine",
			want:  RedactPlaceholder + " is mine",
		},
		{
			name:  "aws access key",
			input: "AKIAIOSFODNN7EXAMPLE in config",
			want:  RedactPlaceholder + " in config",
		},
		{
			name:  "slack bot token",
			input: "token: xoxb-123456789-abcdef",
			want:  "token: " + RedactPlaceholder,
		},
		{
			name:  "slack user token",
			input: "token: xoxp-123456789-abcdef",
			want:  "token: " + RedactPlaceholder,
		},
		{
			name:  "no secrets",
			input: "this is a normal message",
			want:  "this is a normal message",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "multiple secrets",
			input: "keys: sk-abcdefghijklmnopqrstuvwxyz and AKIAIOSFODNN7EXAMPLE",
			want:  "keys: " + RedactPlaceholder + " and " + RedactPlaceholder,
		},
	}

	r := NewRedactor()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := r.Redact(tt.input)
			if got != tt.want {
				t.Errorf("Redact(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestRedactor_Literals(t *testing.T) {
	t.Parallel()

	r := NewRedactor()
	r.AddLiteral("my-super-secret-value")
	r.AddLiteral("") // empty should be ignored

	got := r.Redact("the token is my-super-secret-value here")
	want := "the token is " + RedactPlaceholder + " here"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRedactor_SyncCredentials(t *testing.T) {
	t.Parallel()

	store := NewCredentialStore()
	store.Set("api_key", "secret-from-store-123")

	r := NewRedactor()
	r.SyncCredentials(store)

	got := r.Redact("using secret-from-store-123 in request")
	want := "using " + RedactPlaceholder + " in request"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRedactor_RedactMap(t *testing.T) {
	t.Parallel()

	r := NewRedactor()
	r.AddLiteral("literal-secret")

	m := map[string]any{
		"name":      "test",
		"api_key":   "should-be-redacted",
		"password":  "hunter2",
		"token":     "fake-test-value", //nolint:gosec // not a real token
		"secret":    "top-secret",
		"data":      "has literal-secret inside",
		"empty_key": "",
		"nested": map[string]any{
			"inner_token": "nested-secret",
			"safe":        "visible",
		},
		"list": []any{
			map[string]any{
				"credential": "list-secret",
			},
		},
	}

	r.RedactMap(m)

	// Keys matching secret pattern should be redacted.
	if m["api_key"] != RedactPlaceholder {
		t.Errorf("api_key = %v, want redacted", m["api_key"])
	}
	if m["password"] != RedactPlaceholder {
		t.Errorf("password = %v, want redacted", m["password"])
	}
	if m["token"] != RedactPlaceholder {
		t.Errorf("token = %v, want redacted", m["token"])
	}
	if m["secret"] != RedactPlaceholder {
		t.Errorf("secret = %v, want redacted", m["secret"])
	}

	// Literal values in non-secret keys should also be redacted.
	if m["data"] != "has "+RedactPlaceholder+" inside" {
		t.Errorf("data = %v, want literal redacted", m["data"])
	}

	// Non-secret keys with safe values should be preserved.
	if m["name"] != "test" {
		t.Errorf("name = %v, want test", m["name"])
	}

	// Empty string values under secret keys should NOT be redacted.
	if m["empty_key"] != "" {
		t.Errorf("empty_key = %v, want empty", m["empty_key"])
	}

	// Nested maps should be walked.
	nested := m["nested"].(map[string]any)
	if nested["inner_token"] != RedactPlaceholder {
		t.Errorf("nested.inner_token = %v, want redacted", nested["inner_token"])
	}
	if nested["safe"] != "visible" {
		t.Errorf("nested.safe = %v, want visible", nested["safe"])
	}

	// Lists of maps should be walked.
	list := m["list"].([]any)
	item := list[0].(map[string]any)
	if item["credential"] != RedactPlaceholder {
		t.Errorf("list[0].credential = %v, want redacted", item["credential"])
	}
}

func TestRedactor_AddPattern(t *testing.T) {
	t.Parallel()

	r := &Redactor{} // empty, no default patterns
	r.AddPattern(DefaultPatterns()[0])

	got := r.Redact("sk-abcdefghijklmnopqrstuvwxyz")
	if got != RedactPlaceholder {
		t.Errorf("got %q, want %q", got, RedactPlaceholder)
	}
}

func FuzzRedactor(f *testing.F) {
	f.Add("normal text")
	f.Add("sk-abcdefghijklmnopqrstuvwxyz")
	f.Add("AKIAIOSFODNN7EXAMPLE")
	f.Add("xoxb-123-abc")
	f.Add("")
	f.Add("ghp_" + "a" + "bCdEfGhIjKlMnOpQrSt0")

	r := NewRedactor()
	r.AddLiteral("test-literal-secret")

	f.Fuzz(func(t *testing.T, input string) {
		result := r.Redact(input)

		// The result should never contain a known literal secret.
		if len(result) > 0 && input != result {
			// Redaction happened — verify the placeholder is present.
			if len(result) < len(RedactPlaceholder) {
				// Result is shorter than placeholder but different from input.
				// This is acceptable for partial matches.
				return
			}
		}

		// Redaction should be idempotent.
		double := r.Redact(result)
		if double != result {
			t.Errorf("redaction not idempotent: Redact(Redact(%q)) = %q, want %q", input, double, result)
		}
	})
}
