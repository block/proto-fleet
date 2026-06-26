package logformat

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteTextToCSVMatchesRowFormatter(t *testing.T) {
	cases := []struct {
		name        string
		logData     string
		includeType bool
	}{
		{
			name:        "with log levels",
			logData:     "2024-06-14 16:01:58.470952 | INFO  | mcdd::temp | stable\n",
			includeType: true,
		},
		{
			name:        "without log levels",
			logData:     "[2026-01-01T00:00:00Z] line one\n[2026-01-01T00:00:01Z] line two\n",
			includeType: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got bytes.Buffer
			if err := WriteTextToCSV(&got, tc.logData, tc.includeType); err != nil {
				t.Fatalf("WriteTextToCSV() error = %v", err)
			}
			want := strings.Join(FormatTextToCSV(tc.logData, tc.includeType), "\n") + "\n"
			if got.String() != want {
				t.Fatalf("WriteTextToCSV() = %q, want %q", got.String(), want)
			}
		})
	}
}
