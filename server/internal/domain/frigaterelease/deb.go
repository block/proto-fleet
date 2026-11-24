package frigaterelease

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
)

const (
	arMagic = "!<arch>\n"
	// arHeaderLen is the fixed size of an ar member header.
	arHeaderLen = 60
	// maxControlBytes bounds the compressed and decompressed control archive
	// and the control file itself.
	maxControlBytes = 1 << 20
	// maxARMembers bounds the members scanned before the control archive.
	maxARMembers = 16
)

var (
	// ErrPackageInvalid is returned when a file is not a readable Debian package.
	ErrPackageInvalid = errors.New("invalid Debian package")
	// ErrUnsupportedCompression is returned when the control archive uses a
	// compression the Go standard library cannot read (xz or zstd).
	ErrUnsupportedCompression = errors.New("unsupported Debian control archive compression")
)

// Control is the identity data from a Debian package's control file.
type Control struct {
	Package      string
	Version      string
	Architecture string
}

// ReadDebControl reads the control file of the Debian package at filePath. It
// supports control archives that are plain or gzip-compressed tar files.
func ReadDebControl(filePath string) (Control, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return Control{}, fmt.Errorf("open package: %w", err)
	}
	defer f.Close()
	return ParseDeb(f)
}

// ParseDeb reads the control file from a Debian package stream.
func ParseDeb(r io.Reader) (Control, error) {
	br := bufio.NewReader(r)
	magic := make([]byte, len(arMagic))
	if _, err := io.ReadFull(br, magic); err != nil || string(magic) != arMagic {
		return Control{}, fmt.Errorf("%w: not an ar archive", ErrPackageInvalid)
	}
	for range maxARMembers {
		header := make([]byte, arHeaderLen)
		if _, err := io.ReadFull(br, header); err != nil {
			return Control{}, fmt.Errorf("%w: control archive not found", ErrPackageInvalid)
		}
		if string(header[58:60]) != "`\n" {
			return Control{}, fmt.Errorf("%w: corrupt ar header", ErrPackageInvalid)
		}
		name := strings.TrimSuffix(strings.TrimSpace(string(header[0:16])), "/")
		size, err := strconv.ParseInt(strings.TrimSpace(string(header[48:58])), 10, 64)
		if err != nil || size < 0 {
			return Control{}, fmt.Errorf("%w: corrupt ar member size", ErrPackageInvalid)
		}
		padded := size + size%2
		if !strings.HasPrefix(name, "control.tar") {
			if _, err := io.CopyN(io.Discard, br, padded); err != nil {
				return Control{}, fmt.Errorf("%w: truncated package", ErrPackageInvalid)
			}
			continue
		}
		if size > maxControlBytes {
			return Control{}, fmt.Errorf("%w: control archive is too large", ErrPackageInvalid)
		}
		data := make([]byte, size)
		if _, err := io.ReadFull(br, data); err != nil {
			return Control{}, fmt.Errorf("%w: truncated control archive", ErrPackageInvalid)
		}
		return controlFromArchive(name, data)
	}
	return Control{}, fmt.Errorf("%w: control archive not found", ErrPackageInvalid)
}

func controlFromArchive(name string, data []byte) (Control, error) {
	var tarStream io.Reader
	switch name {
	case "control.tar":
		tarStream = bytes.NewReader(data)
	case "control.tar.gz":
		gz, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return Control{}, fmt.Errorf("%w: %w", ErrPackageInvalid, err)
		}
		defer gz.Close()
		tarStream = io.LimitReader(gz, maxControlBytes+1)
	default:
		return Control{}, fmt.Errorf("%w: %s", ErrUnsupportedCompression, name)
	}
	tr := tar.NewReader(tarStream)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return Control{}, fmt.Errorf("%w: no control file in the control archive", ErrPackageInvalid)
		}
		if err != nil {
			return Control{}, fmt.Errorf("%w: %w", ErrPackageInvalid, err)
		}
		if hdr.Typeflag != tar.TypeReg || path.Clean(hdr.Name) != "control" {
			continue
		}
		body, err := io.ReadAll(io.LimitReader(tr, maxControlBytes+1))
		if err != nil {
			return Control{}, fmt.Errorf("%w: %w", ErrPackageInvalid, err)
		}
		if len(body) > maxControlBytes {
			return Control{}, fmt.Errorf("%w: control file is too large", ErrPackageInvalid)
		}
		return parseControlFields(body)
	}
}

// parseControlFields reads the first paragraph of a Debian control file.
func parseControlFields(body []byte) (Control, error) {
	var c Control
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 0, 4096), maxControlBytes)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			break
		}
		if line[0] == ' ' || line[0] == '\t' {
			continue // Continuation of a multi-line field.
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return Control{}, fmt.Errorf("%w: malformed control line", ErrPackageInvalid)
		}
		value = strings.TrimSpace(value)
		switch strings.ToLower(key) {
		case "package":
			c.Package = value
		case "version":
			c.Version = value
		case "architecture":
			c.Architecture = value
		}
	}
	if err := scanner.Err(); err != nil {
		return Control{}, fmt.Errorf("%w: %w", ErrPackageInvalid, err)
	}
	if c.Package == "" || c.Version == "" || c.Architecture == "" {
		return Control{}, fmt.Errorf("%w: control file lacks Package, Version or Architecture", ErrPackageInvalid)
	}
	return c, nil
}

// versionMatches reports whether a control version is the statement version,
// optionally with one Debian revision: "1.2.3-1" matches "1.2.3", but
// "1.2.3-rc1-1" does not, because the revision is whatever follows the last
// hyphen and so the upstream part there is "1.2.3-rc1". It is Frigate's rule.
func versionMatches(statementVersion, controlVersion string) bool {
	if controlVersion == statementVersion {
		return true
	}
	index := strings.LastIndexByte(controlVersion, '-')
	return index >= 0 && index < len(controlVersion)-1 && controlVersion[:index] == statementVersion
}

// Matches reports whether the package's control data agrees with a verified
// statement. A Debian revision suffix ("1.2.3-1") on the control version is accepted.
func (c Control) Matches(s Statement) error {
	if c.Package != s.Package {
		return fmt.Errorf("%w: package is %q, statement says %q", ErrPackageInvalid, c.Package, s.Package)
	}
	if c.Architecture != s.Architecture {
		return fmt.Errorf("%w: architecture is %q, statement says %q", ErrPackageInvalid, c.Architecture, s.Architecture)
	}
	if !versionMatches(s.Version, c.Version) {
		return fmt.Errorf("%w: version is %q, statement says %q", ErrPackageInvalid, c.Version, s.Version)
	}
	return nil
}
