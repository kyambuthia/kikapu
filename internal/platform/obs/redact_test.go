package obs

import (
	"testing"
)

func TestRedactPhone(t *testing.T) {
	tests := []struct {
		name  string
		phone string
		want  string
	}{
		{
			name:  "standard Kenyan phone",
			phone: "+254712345678",
			want:  "+254****678",
		},
		{
			name:  "phone without plus",
			phone: "254712345678",
			want:  "254****678",
		},
		{
			name:  "phone with spaces",
			phone: "+254 712 345 678",
			want:  "+254****678",
		},
		{
			name:  "phone with dashes",
			phone: "+254-712-345-678",
			want:  "+254****678",
		},
		{
			name:  "short phone",
			phone: "12345",
			want:  "12345",
		},
		{
			name:  "empty string",
			phone: "",
			want:  "",
		},
		{
			name:  "non-numeric",
			phone: "abc123",
			want:  "abc123",
		},
		{
			name:  "US phone format",
			phone: "+12025551234",
			want:  "+120****234",
		},
		{
			name:  "Nigerian phone",
			phone: "+234801234567",
			want:  "+234****567",
		},
		{
			name:  "phone with letter",
			phone: "+2547123A5678",
			want:  "+2547123A5678",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RedactPhone(tt.phone); got != tt.want {
				t.Errorf("RedactPhone() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRedactPAN(t *testing.T) {
	tests := []struct {
		name string
		pan  string
		want string
	}{
		{
			name: "standard PAN",
			pan:  "4111111111111111",
			want: "4111****1111",
		},
		{
			name: "short PAN",
			pan:  "123456",
			want: "123456",
		},
		{
			name: "PAN with spaces",
			pan:  "4111 1111 1111 1111",
			want: "4111****1111",
		},
		{
			name: "empty PAN",
			pan:  "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RedactPAN(tt.pan); got != tt.want {
				t.Errorf("RedactPAN() = %v, want %v", got, tt.want)
			}
		})
	}
}
