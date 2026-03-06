package config

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
)

// stubOP replaces both opLookPathFunc and opRunnerFunc for the duration of a test.
// It returns a cleanup function that restores the originals.
func stubOP(t *testing.T, runner func(string, string) (string, error)) {
	t.Helper()
	origLookPath := opLookPathFunc
	origRunner := opRunnerFunc
	t.Cleanup(func() {
		opLookPathFunc = origLookPath
		opRunnerFunc = origRunner
	})
	opLookPathFunc = func() error { return nil }
	opRunnerFunc = runner
}

func TestResolveOnePassword_NoRefs(t *testing.T) {
	input := []byte("token: plain_value\nkey: another")
	result, err := resolveOnePassword(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result) != string(input) {
		t.Errorf("got %q, want %q", string(result), string(input))
	}
}

func TestResolveOnePassword_SingleRef(t *testing.T) {
	stubOP(t, func(ref, _ string) (string, error) {
		if ref == "op://vault/item/field" {
			return "secret123", nil
		}
		return "", fmt.Errorf("unexpected ref: %s", ref)
	})

	input := []byte(`token: "op://vault/item/field"`)
	result, err := resolveOnePassword(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `token: "secret123"`
	if string(result) != want {
		t.Errorf("got %q, want %q", string(result), want)
	}
}

func TestResolveOnePassword_MultipleRefs(t *testing.T) {
	secrets := map[string]string{
		"op://vault/item/token":   "tok_abc",
		"op://vault/item/api-key": "key_xyz",
	}
	stubOP(t, func(ref, _ string) (string, error) {
		v, ok := secrets[ref]
		if !ok {
			return "", fmt.Errorf("unexpected ref: %s", ref)
		}
		return v, nil
	})

	input := []byte("token: op://vault/item/token\napi_key: op://vault/item/api-key")
	result, err := resolveOnePassword(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := string(result)
	if !strings.Contains(got, "tok_abc") {
		t.Errorf("result should contain tok_abc: %s", got)
	}
	if !strings.Contains(got, "key_xyz") {
		t.Errorf("result should contain key_xyz: %s", got)
	}
}

func TestResolveOnePassword_Deduplication(t *testing.T) {
	var callCount atomic.Int32
	stubOP(t, func(_, _ string) (string, error) {
		callCount.Add(1)
		return "resolved", nil
	})

	input := []byte("a: op://vault/item/field\nb: op://vault/item/field\nc: op://vault/item/field")
	result, err := resolveOnePassword(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount.Load() != 1 {
		t.Errorf("op runner called %d times, want 1 (deduplication)", callCount.Load())
	}
	if strings.Contains(string(result), "op://") {
		t.Errorf("result still contains op:// references: %s", string(result))
	}
}

func TestResolveOnePassword_ErrorAccumulation(t *testing.T) {
	stubOP(t, func(ref, _ string) (string, error) {
		return "", fmt.Errorf("failed: %s", ref)
	})

	input := []byte("a: op://vault/item/one\nb: op://vault/item/two")
	_, err := resolveOnePassword(input)
	if err == nil {
		t.Fatal("expected error for failed references")
	}
	if !strings.Contains(err.Error(), "op://vault/item/one") {
		t.Errorf("error should mention first ref: %v", err)
	}
	if !strings.Contains(err.Error(), "op://vault/item/two") {
		t.Errorf("error should mention second ref: %v", err)
	}
}

func TestResolveOnePassword_TwoSegmentError(t *testing.T) {
	stubOP(t, func(ref, _ string) (string, error) {
		t.Fatalf("op runner should not be called for invalid ref, got: %s", ref)
		return "", nil
	})

	input := []byte(`token: "op://vault/item"`)
	_, err := resolveOnePassword(input)
	if err == nil {
		t.Fatal("expected error for 2-segment reference")
	}
	if !strings.Contains(err.Error(), "op://vault/item") {
		t.Errorf("error should mention the invalid ref: %v", err)
	}
	if !strings.Contains(err.Error(), "at least 3") {
		t.Errorf("error should mention segment requirement: %v", err)
	}
}

func TestResolveOnePassword_AccountPassedToRunner(t *testing.T) {
	stubOP(t, func(ref, account string) (string, error) {
		if account != "MYACCOUNT123" {
			t.Errorf("expected account MYACCOUNT123, got %q", account)
		}
		return "resolved", nil
	})

	input := []byte("onepassword:\n  account: MYACCOUNT123\ntoken: op://vault/item/field")
	result, err := resolveOnePassword(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(string(result), "op://") {
		t.Errorf("result still contains op:// references: %s", string(result))
	}
}

func TestResolveOnePassword_NoAccountDefault(t *testing.T) {
	stubOP(t, func(_, account string) (string, error) {
		if account != "" {
			t.Errorf("expected empty account, got %q", account)
		}
		return "resolved", nil
	})

	input := []byte("token: op://vault/item/field")
	_, err := resolveOnePassword(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExtractOPAccount(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"with account", "onepassword:\n  account: ABC123\nmodules:", "ABC123"},
		{"quoted account", "onepassword:\n  account: \"ABC123\"\nmodules:", "ABC123"},
		{"no onepassword section", "version: \"1\"\nmodules:", ""},
		{"empty account", "onepassword:\n  account: \nmodules:", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractOPAccount([]byte(tt.input))
			if got != tt.want {
				t.Errorf("extractOPAccount() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveOnePassword_FourSegmentPath(t *testing.T) {
	stubOP(t, func(ref, _ string) (string, error) {
		if ref == "op://vault/item/section/field" {
			return "deep_secret", nil
		}
		return "", fmt.Errorf("unexpected ref: %s", ref)
	})

	input := []byte(`key: "op://vault/item/section/field"`)
	result, err := resolveOnePassword(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `key: "deep_secret"`
	if string(result) != want {
		t.Errorf("got %q, want %q", string(result), want)
	}
}
