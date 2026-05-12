package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// secretReader serializes one or more secret prompts off a single stdin.
// On a TTY the prompt is printed and term.ReadPassword reads without echo.
// For piped/scripted input, a shared bufio.Scanner reads one line per
// prompt, so callers can feed multiple secrets via one stdin stream
// (e.g. `printf '%s\n%s\n' "$CODE" "$KEY" | fleetnode enroll ...`).
type secretReader struct {
	stdin   io.Reader
	stderr  io.Writer
	scanner *bufio.Scanner
	tty     *os.File
}

const scannerMaxLine = 1024 * 1024

func newSecretReader(stdin io.Reader, stderr io.Writer) *secretReader {
	sr := &secretReader{stdin: stdin, stderr: stderr}
	if f, ok := stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		sr.tty = f
	}
	return sr
}

func (sr *secretReader) read(prompt string) (string, error) {
	_, _ = fmt.Fprint(sr.stderr, prompt)
	if sr.tty != nil {
		b, err := term.ReadPassword(int(sr.tty.Fd()))
		_, _ = fmt.Fprintln(sr.stderr)
		if err != nil {
			return "", fmt.Errorf("read from terminal: %w", err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	if sr.scanner == nil {
		sr.scanner = bufio.NewScanner(sr.stdin)
		sr.scanner.Buffer(make([]byte, 0, 1024), scannerMaxLine)
	}
	if !sr.scanner.Scan() {
		if err := sr.scanner.Err(); err != nil {
			return "", fmt.Errorf("scan stdin: %w", err)
		}
		return "", errors.New("no input on stdin")
	}
	return strings.TrimSpace(sr.scanner.Text()), nil
}
