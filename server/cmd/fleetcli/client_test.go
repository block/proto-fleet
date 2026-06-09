package main

import "testing"

func TestNormalizeBaseURL(t *testing.T) {
	tests := []struct {
		name     string
		server   string
		insecure bool
		want     string
	}{
		{
			name:   "adds https scheme and api proxy path",
			server: "fleet.example.com",
			want:   "https://fleet.example.com/api-proxy",
		},
		{
			name:     "uses http for insecure host without scheme",
			server:   "localhost:8080",
			insecure: true,
			want:     "http://localhost:8080/api-proxy",
		},
		{
			name:   "preserves explicit path",
			server: "https://fleet.example.com/custom/",
			want:   "https://fleet.example.com/custom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeBaseURL(tt.server, tt.insecure)
			if err != nil {
				t.Fatalf("normalizeBaseURL() error = %v", err)
			}
			if got.String() != tt.want {
				t.Fatalf("normalizeBaseURL() = %q, want %q", got.String(), tt.want)
			}
		})
	}
}

func TestNormalizeBaseURLRejectsMissingHost(t *testing.T) {
	if _, err := normalizeBaseURL("https:///api-proxy", false); err == nil {
		t.Fatal("normalizeBaseURL() error = nil, want missing host error")
	}
}
