package frigaterelease

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"time"
)

const (
	// SchemaV1 is the only statement schema Fleet understands.
	SchemaV1 = "frigate-release-statement/v1"
	// PackageName is the Debian package name of the controller software.
	PackageName = "frigate"
	// ProtoOSPackageName is the Debian package name of the ProtoOS browser UI.
	ProtoOSPackageName = "proto-os"
	// MaxStatementBytes bounds a statement. It matches the controller manifest limit.
	MaxStatementBytes = 4 << 10
	// MaxSignatureBytes bounds a DER signature. It matches Frigate's verifier: an
	// ASN.1 DER ECDSA P-256 signature is at most 72 bytes.
	MaxSignatureBytes = 128
	// MaxPackageBytes bounds a package. It matches the controller manifest limit.
	MaxPackageBytes int64 = 1 << 30

	// issuedAtLayout is the only issued_at encoding: UTC with whole seconds.
	issuedAtLayout = "2006-01-02T15:04:05Z"
)

var (
	// ErrStatementInvalid is returned when a statement is malformed or not acceptable.
	ErrStatementInvalid = errors.New("invalid release statement")

	debianVersionRe = regexp.MustCompile(`^[0-9][0-9A-Za-z.+~:-]*$`)
	architectureRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
	repositoryRe    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9._-]+$`)
	sha256HexRe     = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Statement is a decoded release statement.
type Statement struct {
	Schema       string
	Package      string
	Version      string
	Architecture string
	Repository   string
	// SHA256 is the lowercase hex SHA-256 of the .deb file.
	SHA256 string
	// Size is the .deb file size in bytes.
	Size int64
	// MinInstalledVersion is the oldest installed version allowed to upgrade
	// to this release. Empty means no floor.
	MinInstalledVersion string
	IssuedAt            time.Time
}

// statementJSON is the wire shape. Its field order is the canonical field
// order, and every field is always emitted, including an empty
// min_installed_version, exactly as Frigate's releasesig package encodes it.
type statementJSON struct {
	Schema              string `json:"schema"`
	Package             string `json:"package"`
	Version             string `json:"version"`
	Architecture        string `json:"architecture"`
	Repository          string `json:"repository"`
	SHA256              string `json:"sha256"`
	Size                int64  `json:"size"`
	MinInstalledVersion string `json:"min_installed_version"`
	IssuedAt            string `json:"issued_at"`
}

// MarshalCanonical returns the canonical signed encoding of a statement: the v1
// fields in schema order, compact, without a trailing newline, with issued_at in
// UTC at whole-second precision. It is byte-for-byte what Frigate's
// releasesig.MarshalCanonical produces.
func MarshalCanonical(s Statement) ([]byte, error) {
	s.IssuedAt = s.IssuedAt.UTC().Truncate(time.Second)
	if err := s.Validate(); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(statementJSON{
		Schema: s.Schema, Package: s.Package, Version: s.Version, Architecture: s.Architecture,
		Repository: s.Repository, SHA256: s.SHA256, Size: s.Size, MinInstalledVersion: s.MinInstalledVersion,
		IssuedAt: s.IssuedAt.Format(issuedAtLayout),
	})
	if err != nil {
		return nil, fmt.Errorf("encode release statement: %w", err)
	}
	return encoded, nil
}

// ParseStatement strictly decodes and validates raw statement bytes. Like
// Frigate's parser it rejects oversize input, unknown fields, trailing data and
// any encoding other than the canonical one, so a statement has exactly one
// valid byte representation: reordered, padded, duplicated, differently cased or
// differently timestamped fields all fail. It does not check a signature; use
// Verifier.Verify for that.
func ParseStatement(raw []byte) (Statement, error) {
	if len(raw) == 0 || len(raw) > MaxStatementBytes {
		return Statement{}, fmt.Errorf("%w: statement must be between 1 and %d bytes", ErrStatementInvalid, MaxStatementBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var wire statementJSON
	if err := dec.Decode(&wire); err != nil {
		return Statement{}, fmt.Errorf("%w: %w", ErrStatementInvalid, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Statement{}, fmt.Errorf("%w: unexpected data after the JSON document", ErrStatementInvalid)
	}
	issuedAt, err := time.Parse(issuedAtLayout, wire.IssuedAt)
	if err != nil {
		return Statement{}, fmt.Errorf("%w: issued_at must be UTC RFC 3339 with whole seconds", ErrStatementInvalid)
	}
	s := Statement{
		Schema: wire.Schema, Package: wire.Package, Version: wire.Version, Architecture: wire.Architecture,
		Repository: wire.Repository, SHA256: wire.SHA256, Size: wire.Size, MinInstalledVersion: wire.MinInstalledVersion,
		IssuedAt: issuedAt,
	}
	canonical, err := MarshalCanonical(s)
	if err != nil {
		return Statement{}, err
	}
	if !bytes.Equal(canonical, raw) {
		return Statement{}, fmt.Errorf("%w: statement is not in canonical form", ErrStatementInvalid)
	}
	return s, nil
}

// Validate checks every field against the v1 schema and Fleet's limits.
func (s Statement) Validate() error {
	switch {
	case s.Schema != SchemaV1:
		return fmt.Errorf("%w: unsupported schema %q", ErrStatementInvalid, s.Schema)
	case s.Package != PackageName && s.Package != ProtoOSPackageName:
		return fmt.Errorf("%w: package must be %q or %q", ErrStatementInvalid, PackageName, ProtoOSPackageName)
	case !debianVersionRe.MatchString(s.Version):
		return fmt.Errorf("%w: invalid version", ErrStatementInvalid)
	case !architectureRe.MatchString(s.Architecture):
		return fmt.Errorf("%w: invalid architecture", ErrStatementInvalid)
	case !repositoryRe.MatchString(s.Repository):
		return fmt.Errorf("%w: invalid repository", ErrStatementInvalid)
	case !sha256HexRe.MatchString(s.SHA256):
		return fmt.Errorf("%w: sha256 must be 64 lowercase hex characters", ErrStatementInvalid)
	case s.Size <= 0 || s.Size > MaxPackageBytes:
		return fmt.Errorf("%w: size must be between 1 and %d bytes", ErrStatementInvalid, MaxPackageBytes)
	case s.IssuedAt.IsZero():
		return fmt.Errorf("%w: issued_at is required", ErrStatementInvalid)
	case s.MinInstalledVersion != "" && !debianVersionRe.MatchString(s.MinInstalledVersion):
		return fmt.Errorf("%w: invalid min_installed_version", ErrStatementInvalid)
	}
	return nil
}
