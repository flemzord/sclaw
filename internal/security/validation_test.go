package security

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateMessageSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		size    int
		max     int
		wantErr error
	}{
		{name: "within limit", size: 100, max: 1024, wantErr: nil},
		{name: "at limit", size: 1024, max: 1024, wantErr: nil},
		{name: "over limit", size: 1025, max: 1024, wantErr: ErrMessageTooLarge},
		{name: "zero max uses default", size: 100, max: 0, wantErr: nil},
		{name: "empty data", size: 0, max: 100, wantErr: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data := make([]byte, tt.size)
			err := ValidateMessageSize(data, tt.max)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("ValidateMessageSize(size=%d, max=%d) = %v, want %v",
					tt.size, tt.max, err, tt.wantErr)
			}
		})
	}
}

func TestValidateJSONDepth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		json    string
		max     int
		wantErr error
	}{
		{
			name:    "flat object",
			json:    `{"key": "value"}`,
			max:     2,
			wantErr: nil,
		},
		{
			name:    "nested within limit",
			json:    `{"a": {"b": {"c": 1}}}`,
			max:     3,
			wantErr: nil,
		},
		{
			name:    "nested over limit",
			json:    `{"a": {"b": {"c": {"d": 1}}}}`,
			max:     3,
			wantErr: ErrJSONTooDeep,
		},
		{
			name:    "array nesting",
			json:    `[[[1]]]`,
			max:     3,
			wantErr: nil,
		},
		{
			name:    "array over limit",
			json:    `[[[[1]]]]`,
			max:     3,
			wantErr: ErrJSONTooDeep,
		},
		{
			name:    "empty data",
			json:    "",
			max:     1,
			wantErr: nil,
		},
		{
			name:    "simple string",
			json:    `"hello"`,
			max:     1,
			wantErr: nil,
		},
		{
			name:    "zero max uses default",
			json:    `{"key": "value"}`,
			max:     0,
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateJSONDepth([]byte(tt.json), tt.max)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("ValidateJSONDepth(%q, %d) = %v, want %v",
					tt.json, tt.max, err, tt.wantErr)
			}
		})
	}
}

func TestValidateJSONDepth_DeepNesting(t *testing.T) {
	t.Parallel()

	// Build a deeply nested JSON: {"a":{"a":{"a":...}}}
	depth := 50
	var sb strings.Builder
	for range depth {
		sb.WriteString(`{"a":`)
	}
	sb.WriteString("1")
	for range depth {
		sb.WriteString("}")
	}

	err := ValidateJSONDepth([]byte(sb.String()), 32)
	if !errors.Is(err, ErrJSONTooDeep) {
		t.Errorf("expected ErrJSONTooDeep for depth %d, got %v", depth, err)
	}
}

func BenchmarkValidateJSONDepth(b *testing.B) {
	// Moderately nested JSON.
	data := []byte(`{"users": [{"name": "Alice", "profile": {"age": 30, "address": {"city": "NYC"}}}]}`)
	b.ResetTimer()
	for range b.N {
		_ = ValidateJSONDepth(data, 32)
	}
}

func BenchmarkValidateMessageSize(b *testing.B) {
	data := make([]byte, 4096)
	b.ResetTimer()
	for range b.N {
		_ = ValidateMessageSize(data, DefaultMaxMessageSize)
	}
}
