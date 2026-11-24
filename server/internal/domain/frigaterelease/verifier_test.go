package frigaterelease

import (
	"bytes"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestVerifyGoodSignature(t *testing.T) {
	priv, key := newKey(t)
	statement := statementBytes(t, nil)
	v := NewStaticVerifier(key)

	got, err := v.Verify(statement, sign(t, priv, statement), "")
	require.NoError(t, err)
	require.Equal(t, key.ID, got.KeyID)
	require.Equal(t, "1.4.0", got.Statement.Version)
	require.Equal(t, "arm64", got.Statement.Architecture)
	require.Equal(t, "1.0.0", got.Statement.MinInstalledVersion)
	require.Equal(t, int64(1234), got.Statement.Size)
	require.Equal(t, "2026-11-01T10:00:00Z", got.Statement.IssuedAt.Format("2006-01-02T15:04:05Z07:00"))
}

func TestVerifyAcceptsMatchingKeyID(t *testing.T) {
	priv, key := newKey(t)
	statement := statementBytes(t, nil)
	_, err := NewStaticVerifier(key).Verify(statement, sign(t, priv, statement), key.ID)
	require.NoError(t, err)
}

func TestVerifyRejectsBadSignatures(t *testing.T) {
	priv, key := newKey(t)
	otherPriv, _ := newKey(t)
	statement := statementBytes(t, nil)
	v := NewStaticVerifier(key)

	tampered := statementBytes(t, func(m *statementJSON) { m.Version = "9.9.9" })
	for name, tc := range map[string]struct {
		statement []byte
		signature []byte
	}{
		"wrong key":        {statement, sign(t, otherPriv, statement)},
		"tampered content": {tampered, sign(t, priv, statement)},
		"garbage":          {statement, []byte("not a signature")},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := v.Verify(tc.statement, tc.signature, "")
			require.ErrorIs(t, err, ErrSignatureInvalid)
		})
	}
}

func TestVerifyChecksSignatureBeforeParsing(t *testing.T) {
	_, key := newKey(t)
	otherPriv, _ := newKey(t)
	bad := []byte(`{"schema":"nope"}`)
	_, err := NewStaticVerifier(key).Verify(bad, sign(t, otherPriv, bad), "")
	require.ErrorIs(t, err, ErrSignatureInvalid)
	require.NotErrorIs(t, err, ErrStatementInvalid)
}

func TestVerifyKeyIDMismatch(t *testing.T) {
	priv, key := newKey(t)
	_, other := newKey(t)
	statement := statementBytes(t, nil)
	v := NewStaticVerifier(key, other)

	_, err := v.Verify(statement, sign(t, priv, statement), other.ID)
	require.ErrorIs(t, err, ErrKeyIDMismatch)

	_, err = v.Verify(statement, sign(t, priv, statement), "sha256:short")
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrKeyIDMismatch)
}

func TestVerifyNoTrustedKeys(t *testing.T) {
	priv, _ := newKey(t)
	statement := statementBytes(t, nil)
	_, err := NewStaticVerifier().Verify(statement, sign(t, priv, statement), "")
	require.ErrorIs(t, err, ErrNoTrustedKeys)
	_, err = NewDirVerifier("").Verify(statement, sign(t, priv, statement), "")
	require.ErrorIs(t, err, ErrNoTrustedKeys)
}

func TestVerifyBounds(t *testing.T) {
	_, key := newKey(t)
	v := NewStaticVerifier(key)
	_, err := v.Verify(nil, []byte("x"), "")
	require.ErrorIs(t, err, ErrStatementInvalid)
	_, err = v.Verify(make([]byte, MaxStatementBytes+1), []byte("x"), "")
	require.ErrorIs(t, err, ErrStatementInvalid)
	_, err = v.Verify([]byte("{}"), nil, "")
	require.ErrorIs(t, err, ErrSignatureInvalid)
	_, err = v.Verify([]byte("{}"), make([]byte, MaxSignatureBytes+1), "")
	require.ErrorIs(t, err, ErrSignatureInvalid)
}

func TestVerifyStatementSchemaAndFields(t *testing.T) {
	priv, key := newKey(t)
	v := NewStaticVerifier(key)
	for name, mutate := range map[string]func(*statementJSON){
		"unknown schema":   func(m *statementJSON) { m.Schema = "frigate-release-statement/v2" },
		"wrong package":    func(m *statementJSON) { m.Package = "flotilla" },
		"bad architecture": func(m *statementJSON) { m.Architecture = "ARM 64" },
		"long architecture": func(m *statementJSON) {
			m.Architecture = "a" + strings.Repeat("b", 32)
		},
		"bad repository": func(m *statementJSON) { m.Repository = "no-slash" },
		"short sha":      func(m *statementJSON) { m.SHA256 = "abc" },
		"uppercase sha": func(m *statementJSON) {
			m.SHA256 = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
		},
		"zero size":       func(m *statementJSON) { m.Size = 0 },
		"oversize":        func(m *statementJSON) { m.Size = MaxPackageBytes + 1 },
		"bad version":     func(m *statementJSON) { m.Version = "v1.4.0" },
		"bad min version": func(m *statementJSON) { m.MinInstalledVersion = "has space" },
		"bad issued_at":   func(m *statementJSON) { m.IssuedAt = "yesterday" },
		"empty version":   func(m *statementJSON) { m.Version = "" },
	} {
		t.Run(name, func(t *testing.T) {
			statement := statementBytes(t, mutate)
			_, err := v.Verify(statement, sign(t, priv, statement), "")
			require.ErrorIs(t, err, ErrStatementInvalid)
		})
	}

	canonical := string(statementBytes(t, nil))
	for name, statement := range map[string]string{
		"unknown field":       strings.Replace(canonical, `"issued_at"`, `"extra":true,"issued_at"`, 1),
		"missing sha":         strings.Replace(canonical, `"sha256":"`+validStatement().SHA256+`",`, "", 1),
		"wrong type for size": strings.Replace(canonical, `"size":1234`, `"size":"12"`, 1),
		"null field":          strings.Replace(canonical, `"version":"1.4.0"`, `"version":null`, 1),
		"not an object":       `["frigate"]`,
	} {
		t.Run(name, func(t *testing.T) {
			raw := []byte(statement)
			_, err := v.Verify(raw, sign(t, priv, raw), "")
			require.ErrorIs(t, err, ErrStatementInvalid)
		})
	}
}

// frigateStatement is what Frigate's releasesig.MarshalCanonical produces for
// the fixture statement, captured verbatim, with and without a version floor.
const (
	frigateStatement = `{"schema":"frigate-release-statement/v1","package":"frigate","version":"1.4.0","architecture":"arm64","repository":"btc-mining/frigate","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","size":1234,"min_installed_version":"1.0.0","issued_at":"2026-11-01T10:00:00Z"}`
	// frigateStatementNoFloor shows that Frigate emits an empty min_installed_version.
	frigateStatementNoFloor = `{"schema":"frigate-release-statement/v1","package":"frigate","version":"1.4.0","architecture":"arm64","repository":"btc-mining/frigate","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","size":1234,"min_installed_version":"","issued_at":"2026-11-01T10:00:00Z"}`
)

func TestParseStatementAcceptsFrigateOutputVerbatim(t *testing.T) {
	priv, key := newKey(t)
	v := NewStaticVerifier(key)
	for name, statement := range map[string]string{"with floor": frigateStatement, "without floor": frigateStatementNoFloor} {
		t.Run(name, func(t *testing.T) {
			raw := []byte(statement)
			got, err := v.Verify(raw, sign(t, priv, raw), "")
			require.NoError(t, err)
			again, err := MarshalCanonical(got.Statement)
			require.NoError(t, err)
			require.Equal(t, statement, string(again), "Fleet re-encodes the statement exactly as Frigate does")
		})
	}
	require.Equal(t, frigateStatement, string(statementBytes(t, nil)))
}

func TestParseStatementRejectsNonCanonicalForms(t *testing.T) {
	pretty := new(bytes.Buffer)
	require.NoError(t, json.Indent(pretty, []byte(frigateStatement), "", "  "))
	for name, statement := range map[string]string{
		"trailing newline":  frigateStatement + "\n",
		"leading space":     " " + frigateStatement,
		"pretty printed":    pretty.String(),
		"space after colon": strings.Replace(frigateStatement, `"package":"frigate"`, `"package": "frigate"`, 1),
		"reordered fields": `{"package":"frigate","schema":"frigate-release-statement/v1","version":"1.4.0","architecture":"arm64","repository":"btc-mining/frigate",` +
			`"sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","size":1234,"min_installed_version":"1.0.0","issued_at":"2026-11-01T10:00:00Z"}`,
		"duplicate key":          strings.Replace(frigateStatement, `"size":1234,`, `"size":1,"size":1234,`, 1),
		"differently cased key":  strings.Replace(frigateStatement, `"schema"`, `"Schema"`, 1),
		"escaped character":      strings.Replace(frigateStatement, `"frigate","version"`, `"\u0066rigate","version"`, 1),
		"floor omitted":          strings.Replace(frigateStatement, `"min_installed_version":"1.0.0",`, "", 1),
		"offset timestamp":       strings.Replace(frigateStatement, `10:00:00Z`, `10:00:00+00:00`, 1),
		"fractional seconds":     strings.Replace(frigateStatement, `10:00:00Z`, `10:00:00.5Z`, 1),
		"lowercase timestamp":    strings.Replace(frigateStatement, `T10:00:00Z`, `t10:00:00z`, 1),
		"size with exponent":     strings.Replace(frigateStatement, `"size":1234`, `"size":1.234e3`, 1),
		"size with leading zero": strings.Replace(frigateStatement, `"size":1234`, `"size":01234`, 1),
		"trailing document":      frigateStatement + frigateStatement,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseStatement([]byte(statement))
			require.ErrorIs(t, err, ErrStatementInvalid)
		})
	}
}

func TestMarshalCanonicalNormalizesIssuedAt(t *testing.T) {
	s, err := ParseStatement([]byte(frigateStatement))
	require.NoError(t, err)
	s.IssuedAt = s.IssuedAt.In(time.FixedZone("x", 3600)).Add(500 * time.Millisecond)
	got, err := MarshalCanonical(s)
	require.NoError(t, err)
	require.Equal(t, frigateStatement, string(got))
}

func TestParseStatementRejectsTrailingData(t *testing.T) {
	_, err := ParseStatement(append(statementBytes(t, nil), []byte(` {"a":1}`)...))
	require.ErrorIs(t, err, ErrStatementInvalid)
}

func TestParseStatementMinVersionMayBeEmpty(t *testing.T) {
	s, err := ParseStatement(statementBytes(t, func(m *statementJSON) { m.MinInstalledVersion = "" }))
	require.NoError(t, err)
	require.Empty(t, s.MinInstalledVersion)
}

func TestKeyIDIsSPKIHash(t *testing.T) {
	_, key := newKey(t)
	require.Regexp(t, `^sha256:[0-9a-f]{64}$`, key.ID)
	again, err := KeyID(key.Key)
	require.NoError(t, err)
	require.Equal(t, key.ID, again)
}

func TestLoadKeysDirAndRotation(t *testing.T) {
	dir := t.TempDir()
	privA, keyA := newKey(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.pem"), pemOf(t, keyA.Key), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignored"), 0o600))
	v := NewDirVerifier(dir)

	statement := statementBytes(t, nil)
	_, err := v.Verify(statement, sign(t, privA, statement), keyA.ID)
	require.NoError(t, err)

	// A key added later is trusted without recreating the verifier.
	privB, keyB := newKey(t)
	_, err = v.Verify(statement, sign(t, privB, statement), "")
	require.ErrorIs(t, err, ErrSignatureInvalid)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.pem"), pemOf(t, keyB.Key), 0o600))
	_, err = v.Verify(statement, sign(t, privB, statement), keyB.ID)
	require.NoError(t, err)
}

func TestLoadKeysDirRejectsBadFilesAndCurves(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.pem"), []byte("-----BEGIN PUBLIC KEY-----\nAAAA\n-----END PUBLIC KEY-----\n"), 0o600))
	_, err := LoadKeysDir(dir)
	require.Error(t, err)

	_, err = LoadKeysDir(filepath.Join(dir, "missing"))
	require.Error(t, err)
}

func TestLoadKeysDirFollowsSymlinks(t *testing.T) {
	dir := t.TempDir()
	// A Kubernetes secret mount exposes key.pem as a link into a ..data directory.
	data := filepath.Join(dir, "..2026_11_10")
	require.NoError(t, os.Mkdir(data, 0o700))
	priv, key := newKey(t)
	require.NoError(t, os.WriteFile(filepath.Join(data, "release.pem"), pemOf(t, key.Key), 0o600))
	require.NoError(t, os.Symlink("..2026_11_10", filepath.Join(dir, "..data")))
	require.NoError(t, os.Symlink(filepath.Join("..data", "release.pem"), filepath.Join(dir, "release.pem")))

	statement := statementBytes(t, nil)
	_, err := NewDirVerifier(dir).Verify(statement, sign(t, priv, statement), key.ID)
	require.NoError(t, err)
}

func TestLoadKeysDirIgnoresDirectoriesAndRejectsDanglingLinks(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "folder.pem"), 0o700))
	keys, err := LoadKeysDir(dir)
	require.NoError(t, err)
	require.Empty(t, keys)

	require.NoError(t, os.Symlink(filepath.Join(dir, "gone"), filepath.Join(dir, "dangling.pem")))
	_, err = LoadKeysDir(dir)
	require.Error(t, err, "a link that resolves to nothing must not silently shrink the trusted set")
}

func TestParsePublicKeysRejectsOtherBlocksAndEmptyFiles(t *testing.T) {
	_, key := newKey(t)
	good := pemOf(t, key.Key)
	privateBlock := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: []byte{1, 2, 3}})

	_, err := ParsePublicKeys(append(append([]byte(nil), good...), privateBlock...))
	require.ErrorContains(t, err, "unexpected PEM block type")
	_, err = ParsePublicKeys(append(append([]byte(nil), privateBlock...), good...))
	require.ErrorContains(t, err, "unexpected PEM block type")

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "good.pem"), good, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "empty.pem"), []byte("not a pem file\n"), 0o600))
	_, err = LoadKeysDir(dir)
	require.ErrorContains(t, err, "no PEM public key found")
}
