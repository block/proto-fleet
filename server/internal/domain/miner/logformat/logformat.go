package logformat

import (
	"fmt"
	"strings"
)

const csvLogHeaderWithType = "Time,Type,Message"
const csvLogHeaderNoType = "Time,Message"

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

// FormatLineToCSVRow parses a single log line into a CSV row.
func FormatLineToCSVRow(line string, includeType bool) string {
	csvRow := func(ts, logType, message string) string {
		esc := func(s string) string { return strings.ReplaceAll(s, `"`, `""`) }
		if includeType {
			return fmt.Sprintf(`"%s","%s","%s"`, esc(ts), esc(logType), esc(message))
		}
		return fmt.Sprintf(`"%s","%s"`, esc(ts), esc(message))
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
