package security

import (
	"strings"
	"testing"
)

func TestSanitizedEnv_RemovesSensitiveVars(t *testing.T) {
	t.Parallel()

	// We can't reliably set env vars in parallel tests, so we test
	// the isSensitiveEnvVar helper directly.
	tests := []struct {
		name      string
		sensitive bool
	}{
		{"OPENAI_API_KEY", true},
		{"ANTHROPIC_API_KEY", true},
		{"AWS_SECRET_ACCESS_KEY", true},
		{"AWS_SESSION_TOKEN_STUFF", true},
		{"GITHUB_TOKEN", true},
		{"GH_TOKEN", true},
		{"SLACK_TOKEN", true},
		{"SLACK_BOT_TOKEN", true},
		{"DISCORD_TOKEN", true},
		{"TELEGRAM_BOT_TOKEN", true},
		{"PATH", false},
		{"HOME", false},
		{"USER", false},
		{"GOPATH", false},
		{"SHELL", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isSensitiveEnvVar(tt.name)
			if got != tt.sensitive {
				t.Errorf("isSensitiveEnvVar(%q) = %v, want %v", tt.name, got, tt.sensitive)
			}
		})
	}
}

func TestSanitizedEnv_CaseInsensitive(t *testing.T) {
	t.Parallel()

	// The function uppercases before checking, so mixed case should work.
	if !isSensitiveEnvVar("openai_api_key") {
		t.Error("expected lower case openai_api_key to be sensitive")
	}
	if !isSensitiveEnvVar("Github_Token") {
		t.Error("expected mixed case Github_Token to be sensitive")
	}
}

func TestSanitizedEnv_ResultExcludesSensitive(t *testing.T) {
	// Non-parallel: calls os.Environ() which is read-only.
	env := SanitizedEnv(nil)
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		if isSensitiveEnvVar(key) {
			t.Errorf("sensitive var %q found in sanitized env", key)
		}
	}
}

func TestSanitizedEnv_RedactsCredentialValues(t *testing.T) {
	t.Parallel()

	store := NewCredentialStore()
	store.Set("test", "super-secret-123")

	// SanitizedEnv reads os.Environ(), so we can't inject specific vars.
	// Instead, verify the function runs without panic.
	env := SanitizedEnv(store)
	if len(env) == 0 {
		t.Log("no env vars to test (unusual but not an error)")
	}

	// None of the values should contain the secret.
	for _, entry := range env {
		if strings.Contains(entry, "super-secret-123") {
			t.Errorf("credential value found in env: %s", entry)
		}
	}
}

func TestValidatePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path    string
		wantErr bool
	}{
		{"/proc/self/environ", true},
		{"/proc/1234/environ", true},
		{"/proc/self/maps", true},
		{"/proc/self/status", true},
		{"/home/user/file.txt", false},
		{"/tmp/data", false},
		{"/etc/passwd", false},
		{"/proc/version", false}, // /proc but not self or environ
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			err := ValidatePath(tt.path)
			if tt.wantErr && err == nil {
				t.Errorf("ValidatePath(%q) = nil, want error", tt.path)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidatePath(%q) = %v, want nil", tt.path, err)
			}
		})
	}
}

func TestEscapeShellArg(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"hello", "'hello'"},
		{"hello world", "'hello world'"},
		{"it's", `'it'\''s'`},
		{"", "''"},
		{"$HOME", "'$HOME'"},
		{"; rm -rf /", "'; rm -rf /'"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := EscapeShellArg(tt.input)
			if got != tt.want {
				t.Errorf("EscapeShellArg(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
