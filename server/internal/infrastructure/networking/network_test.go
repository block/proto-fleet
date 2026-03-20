package networking

import "testing"

func TestNormalizeMAC(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "colon separated",
			input:    "AA:BB:CC:DD:EE:FF",
			expected: "AA:BB:CC:DD:EE:FF",
		},
		{
			name:     "dash separated lowercase",
			input:    "aa-bb-cc-dd-ee-ff",
			expected: "AA:BB:CC:DD:EE:FF",
		},
		{
			name:     "dot separated",
			input:    "aabb.ccdd.eeff",
			expected: "AA:BB:CC:DD:EE:FF",
		},
		{
			name:     "bare uppercase",
			input:    "AABBCCDDEEFF",
			expected: "AA:BB:CC:DD:EE:FF",
		},
		{
			name:     "bare lowercase",
			input:    "aabbccddeeff",
			expected: "AA:BB:CC:DD:EE:FF",
		},
		{
			name:     "trim whitespace",
			input:    "  AABBCCDDEEFF  ",
			expected: "AA:BB:CC:DD:EE:FF",
		},
		{
			name:     "empty",
			input:    "",
			expected: "",
		},
		{
			name:     "invalid length",
			input:    "AABBCCDDEEF",
			expected: "",
		},
		{
			name:     "invalid characters",
			input:    "AABBCCDDEEFG",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := NormalizeMAC(tt.input); got != tt.expected {
				t.Fatalf("NormalizeMAC(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
