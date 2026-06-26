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

func TestFormatLineToCSVRowNeutralizesFormulaCells(t *testing.T) {
	tests := []struct {
		name        string
		line        string
		includeType bool
		want        string
	}{
		{
			name:        "message formula without timestamp",
			line:        `=HYPERLINK("https://example.invalid","open")`,
			includeType: false,
			want:        `"","'=HYPERLINK(""https://example.invalid"",""open"")"`,
		},
		{
			name:        "typed message formula",
			line:        "2024-06-14 16:01:58.470952 | INFO  | +cmd",
			includeType: true,
			want:        `"2024-06-14 16:01:58","INFO","'+cmd"`,
		},
		{
			name:        "timestamp formula",
			line:        "-2026-01-01T00:00:00Z message",
			includeType: false,
			want:        `"","'-2026-01-01T00:00:00Z message"`,
		},
		{
			name:        "message formula after tab",
			line:        "\t=cmd",
			includeType: false,
			want:        "\"\",\"'\t=cmd\"",
		},
		{
			name:        "message formula after carriage return",
			line:        "\r+cmd",
			includeType: false,
			want:        "\"\",\"'\r+cmd\"",
		},
		{
			name:        "message formula after newline",
			line:        "\n-cmd",
			includeType: false,
			want:        "\"\",\"'\n-cmd\"",
		},
		{
			name:        "message formula after leading spaces",
			line:        "   @cmd",
			includeType: false,
			want:        `"","'   @cmd"`,
		},
		{
			name:        "typed message formula after tab",
			line:        "2024-06-14 16:01:58.470952 | INFO  | \t=cmd",
			includeType: true,
			want:        "\"2024-06-14 16:01:58\",\"INFO\",\"'\t=cmd\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatLineToCSVRow(tt.line, tt.includeType); got != tt.want {
				t.Fatalf("FormatLineToCSVRow() = %q, want %q", got, tt.want)
			}
		})
	}
}
