package logformat

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
)

const csvLogHeaderWithType = "Time,Type,Message"
const csvLogHeaderNoType = "Time,Message"
const MaxArtifactBytes int64 = 4 * 1024 * 1024

// logLevelSeparators maps Proto miner log-level separators to their display labels.
// Format: "{prefix}: {timestamp} | LEVEL | {message}"
var logLevelSeparators = []struct {
	separator string
	label     string
}{
	{" | ERROR | ", "ERROR"},
	{" | WARN  | ", "WARN"},
	{" | INFO  | ", "INFO"},
	{" | DEBUG | ", "DEBUG"},
}

// FormatTextToCSV converts raw newline-delimited miner logs into CSV rows.
func FormatTextToCSV(logData string, includeType bool) []string {
	return FormatLinesToCSV(strings.Split(strings.TrimRight(logData, "\n"), "\n"), includeType)
}

// FormatLinesToCSV converts raw log lines into CSV rows.
// When includeType is true, the header is "Time,Type,Message" for logs that emit
// levels. When false, the header is "Time,Message".
func FormatLinesToCSV(logLines []string, includeType bool) []string {
	header := csvLogHeaderWithType
	if !includeType {
		header = csvLogHeaderNoType
	}
	rows := make([]string, 0, len(logLines)+1)
	rows = append(rows, header)
	for _, line := range logLines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		rows = append(rows, FormatLineToCSVRow(line, includeType))
	}
	return rows
}

// WriteTextToCSV converts raw newline-delimited miner logs into CSV rows without
// materializing every formatted row.
func WriteTextToCSV(w io.Writer, logData string, includeType bool) error {
	header := csvLogHeaderWithType
	if !includeType {
		header = csvLogHeaderNoType
	}
	if _, err := fmt.Fprintln(w, header); err != nil {
		return fmt.Errorf("write csv header: %w", err)
	}

	remaining := strings.TrimRight(logData, "\n")
	for {
		line, rest, found := strings.Cut(remaining, "\n")
		if strings.TrimSpace(line) != "" {
			if _, err := fmt.Fprintln(w, FormatLineToCSVRow(line, includeType)); err != nil {
				return fmt.Errorf("write csv row: %w", err)
			}
		}
		if !found {
			return nil
		}
		remaining = rest
	}
}

// WriteSanitizedCSV parses an uploaded miner-log CSV, validates that it has one
// of the expected miner-log headers, and rewrites all data cells with the same
// spreadsheet-formula neutralization used by direct miner log formatting.
func WriteSanitizedCSV(w io.Writer, r io.Reader) error {
	reader := csv.NewReader(r)
	header, err := reader.Read()
	if errors.Is(err, io.EOF) {
		return fmt.Errorf("empty miner log csv")
	}
	if err != nil {
		return fmt.Errorf("read csv header: %w", err)
	}
	if !isMinerLogCSVHeader(header) {
		return fmt.Errorf("unexpected miner log csv header")
	}
	if _, err := fmt.Fprintln(w, strings.Join(header, ",")); err != nil {
		return fmt.Errorf("write csv header: %w", err)
	}

	for {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read csv row: %w", err)
		}
		for i := range row {
			row[i] = neutralizeCSVFormula(row[i])
		}
		if _, err := fmt.Fprintln(w, formatCSVFields(row)); err != nil {
			return fmt.Errorf("write csv row: %w", err)
		}
	}
}

// FormatLineToCSVRow parses a single log line into a CSV row.
func FormatLineToCSVRow(line string, includeType bool) string {
	csvRow := func(ts, logType, message string) string {
		if includeType {
			return formatCSVFields([]string{ts, logType, message})
		}
		return formatCSVFields([]string{ts, message})
	}

	for _, level := range logLevelSeparators {
		idx := strings.Index(line, level.separator)
		if idx < 0 {
			continue
		}
		prefix := line[:idx]
		message := line[idx+len(level.separator):]

		ts := prefix
		if parts := strings.SplitN(prefix, ": ", 2); len(parts) == 2 {
			ts = parts[1]
		} else if fields := strings.Fields(prefix); len(fields) >= 3 {
			ts = fields[0] + " " + fields[1] + " " + fields[2]
		}
		ts = strings.TrimSpace(ts)
		if dotIdx := strings.Index(ts, "."); dotIdx >= 0 {
			ts = ts[:dotIdx]
		}

		return csvRow(ts, level.label, message)
	}

	// Antminer bracketed calendar timestamps look like "[2026-01-01T00:00:00Z] message".
	// Boot counters such as "[258.894452@1]" intentionally fall through.
	if strings.HasPrefix(line, "[") {
		if closeBracket := strings.Index(line, "]"); closeBracket > 0 {
			potentialTS := strings.TrimSpace(line[1:closeBracket])
			if strings.ContainsAny(potentialTS, "0123456789") && strings.ContainsAny(potentialTS, "T-/") {
				message := strings.TrimPrefix(line[closeBracket+1:], " ")
				return csvRow(potentialTS, "", message)
			}
		}
	}

	if len(line) > 19 && line[4] == '-' && line[7] == '-' && line[10] == ' ' && line[13] == ':' && line[16] == ':' {
		timestamp := line[:19]
		message := strings.TrimPrefix(line[19:], " ")
		return csvRow(timestamp, "", message)
	}

	return csvRow("", "", line)
}

func isMinerLogCSVHeader(header []string) bool {
	if len(header) == 2 {
		return header[0] == "Time" && header[1] == "Message"
	}
	if len(header) == 3 {
		return header[0] == "Time" && header[1] == "Type" && header[2] == "Message"
	}
	return false
}

func formatCSVFields(fields []string) string {
	escaped := make([]string, 0, len(fields))
	for _, field := range fields {
		field = neutralizeCSVFormula(field)
		field = strings.ReplaceAll(field, `"`, `""`)
		escaped = append(escaped, `"`+field+`"`)
	}
	return strings.Join(escaped, ",")
}

func neutralizeCSVFormula(s string) string {
	trimmed := strings.TrimLeftFunc(s, unicode.IsSpace)
	if trimmed == "" {
		return s
	}
	switch trimmed[0] {
	case '=', '+', '-', '@':
		return "'" + s
	default:
		return s
	}
}
