package crontool

import "testing"

func TestIsValidCronName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"tools-analyzer", true},
		{"my_cron_123", true},
		{"a", true},
		{"A1", true},
		{"", false},
		{"-starts-with-hyphen", false},
		{"_starts-with-underscore", false},
		{"has spaces", false},
		{"has/slash", false},
		{"has.dot", false},
		{string(make([]byte, maxCronNameLen+1)), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidCronName(tt.name)
			if got != tt.want {
				t.Errorf("isValidCronName(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}
