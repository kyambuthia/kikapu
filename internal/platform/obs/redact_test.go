package obs

import (
	"testing"
)

func TestRedactPhoneNumber(t *testing.T) {
	tests := []struct {
		name     string
		phone    string
		expected string
	}{
		{
			name:     "standard Kenyan phone number",
			phone:    "+254712345678",
			expected: "+254******678",
		},
		{
			name:     "phone number without plus",
			phone:    "254712345678",
			expected: "2547*****678",
		},
		{
			name:     "phone number with spaces",
			phone:    "+254 712 345 678",
			expected: "+254******678",
		},
		{
			name:     "phone number with dashes",
			phone:    "+254-712-345-678",
			expected: "+254******678",
		},
		{
			name:     "local format 07XX",
			phone:    "0712345678",
			expected: "0712***678",
		},
		{
			name:     "empty string",
			phone:    "",
			expected: "",
		},
		{
			name:     "too short",
			phone:    "123",
			expected: "123",
		},
		{
			name:     "with spaces and different format",
			phone:    "+1 234 567 8901",
			expected: "+123*****901",
		},
		{
			name:     "UK phone number",
			phone:    "+447912345678",
			expected: "+447******678",
		},
		{
			name:     "US phone number",
			phone:    "+12125551234",
			expected: "+121*****234",
		},
		{
			name:     "phone in text",
			phone:    "Call +254712345678 for support",
			expected: "Call +254******678 for support",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RedactPhoneNumber(tt.phone)
			if result != tt.expected {
				t.Errorf("RedactPhoneNumber(%q) = %q, want %q", tt.phone, result, tt.expected)
			}
		})
	}
}
