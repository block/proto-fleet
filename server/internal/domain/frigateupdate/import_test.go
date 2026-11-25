package frigateupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/frigaterelease"
)

// releaseFixture signs statements for a test key the service trusts.
type releaseFixture struct {
	priv *ecdsa.PrivateKey
	key  frigaterelease.TrustedKey
}

// newReleaseSigner returns a signing key that the service does not trust.
func newReleaseSigner(t *testing.T) releaseFixture {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	key, err := frigaterelease.NewTrustedKey(&priv.PublicKey)
	require.NoError(t, err)
	return releaseFixture{priv: priv, key: key}
}

// newReleaseFixture returns a signing key and installs it on the service.
func newReleaseFixture(t *testing.T, e *env) releaseFixture {
	t.Helper()
	f := newReleaseSigner(t)
	e.svc.verifiers[PackageKindFrigate] = frigaterelease.NewStaticVerifier(f.key)
	return f
}

// publicKeyPEM is the key as an uploader supplies it: a PEM PUBLIC KEY block.
func (f releaseFixture) publicKeyPEM(t *testing.T) []byte {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(&f.priv.PublicKey)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
}

func (f releaseFixture) sign(t *testing.T, statement []byte) []byte {
	t.Helper()
	digest := sha256.Sum256(statement)
	sig, err := ecdsaSign(f.priv, digest[:])
	require.NoError(t, err)
	return sig
}

// buildDeb assembles a minimal gzip-control .deb for the given control fields.
func buildDeb(t *testing.T, control, controlName string) []byte {
	t.Helper()
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "./control", Mode: 0o644, Size: int64(len(control)), Typeflag: tar.TypeReg}))
	_, err := tw.Write([]byte(control))
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	var archive bytes.Buffer
	if controlName == "control.tar.gz" {
		zw := gzip.NewWriter(&archive)
		_, err = zw.Write(tarBuf.Bytes())
		require.NoError(t, err)
		require.NoError(t, zw.Close())
	} else {
		archive.WriteString("not really compressed")
	}
	var out bytes.Buffer
	out.WriteString("!<arch>\n")
	member := func(name string, data []byte) {
		fmt.Fprintf(&out, "%-16s%-12d%-6d%-6d%-8s%-10d`\n", name+"/", 0, 0, 0, "100644", len(data))
		out.Write(data)
		if len(data)%2 == 1 {
			out.WriteByte('\n')
		}
	}
	member("debian-binary", []byte("2.0\n"))
	member(controlName, archive.Bytes())
	member("data.tar.gz", []byte("ignored"))
	return out.Bytes()
}

func controlText(pkg, version, arch string) string {
	return fmt.Sprintf("Package: %s\nVersion: %s\nArchitecture: %s\nMaintainer: x <x@example.com>\nDescription: test\n", pkg, version, arch)
}

// statementFor returns the canonical signed encoding of a statement for deb.
func statementFor(t *testing.T, deb []byte, mutate func(s *frigaterelease.Statement)) []byte {
	t.Helper()
	sum := sha256.Sum256(deb)
	s := frigaterelease.Statement{
		Schema: frigaterelease.SchemaV1, Package: "frigate", Version: "2.1.0", Architecture: "arm64",
		Repository: "btc-mining/frigate", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(deb)),
		MinInstalledVersion: "1.0.0", IssuedAt: time.Now().UTC(),
	}
	if mutate != nil {
		mutate(&s)
	}
	raw, err := frigaterelease.MarshalCanonical(s)
	require.NoError(t, err)
	return raw
}

func (e *env) stage(t *testing.T, data []byte) string {
	t.Helper()
	path, err := e.store.NewStagingPath(fmt.Sprintf("up-%d", time.Now().UnixNano()))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return path
}

func TestImportPackage_StoresAndReuses(t *testing.T) {
	e := newEnv(t)
	rel := newReleaseFixture(t, e)
	deb := buildDeb(t, controlText("frigate", "2.1.0-1", "arm64"), "control.tar.gz")
	statement := statementFor(t, deb, nil)
	sig := rel.sign(t, statement)

	res, err := e.svc.ImportPackage(e.ctx, ImportInput{StagedPath: e.stage(t, deb), Filename: "frigate_2.1.0_arm64.deb", Statement: statement, Signature: sig})
	require.NoError(t, err)
	assert.False(t, res.Reused)
	assert.Equal(t, "2.1.0", res.Package.Version)
	assert.Equal(t, "arm64", res.Package.Architecture)
	assert.Equal(t, rel.keyID(t), res.Package.ReleaseKeyID)
	assert.True(t, e.store.Exists(res.Package.ID, int64(len(deb))))

	again, err := e.svc.ImportPackage(e.ctx, ImportInput{StagedPath: e.stage(t, deb), Filename: "again.deb", Statement: statement, Signature: sig})
	require.NoError(t, err)
	assert.True(t, again.Reused)
	assert.Equal(t, res.Package.ID, again.Package.ID)

	// A restored database with no blob is repaired from the verified upload.
	require.NoError(t, e.store.Delete(res.Package.ID))
	repaired, err := e.svc.ImportPackage(e.ctx, ImportInput{StagedPath: e.stage(t, deb), Filename: "again.deb", Statement: statement, Signature: sig})
	require.NoError(t, err)
	assert.True(t, repaired.Reused)
	assert.True(t, e.store.Exists(res.Package.ID, int64(len(deb))))
}

func (f releaseFixture) keyID(t *testing.T) string {
	t.Helper()
	id, err := frigaterelease.KeyID(&f.priv.PublicKey)
	require.NoError(t, err)
	return id
}

func TestImportPackage_Rejections(t *testing.T) {
	e := newEnv(t)
	rel := newReleaseFixture(t, e)
	good := buildDeb(t, controlText("frigate", "2.1.0", "arm64"), "control.tar.gz")
	other, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	tests := []struct {
		name      string
		deb       []byte
		statement func(deb []byte) []byte
		signer    func(statement []byte) []byte
		filename  string
		contains  string
	}{
		{"untrusted signer", good, func(d []byte) []byte { return statementFor(t, d, nil) }, func(s []byte) []byte {
			digest := sha256.Sum256(s)
			sig, err := ecdsaSign(other, digest[:])
			require.NoError(t, err)
			return sig
		}, "f.deb", "release statement rejected"},
		{"size mismatch", good, func(d []byte) []byte {
			return statementFor(t, d, func(s *frigaterelease.Statement) { s.Size = int64(len(d)) + 1 })
		}, nil, "f.deb", "size"},
		{"digest mismatch", good, func(d []byte) []byte {
			return statementFor(t, d, func(s *frigaterelease.Statement) { s.SHA256 = hex.EncodeToString(make([]byte, 32)) })
		}, nil, "f.deb", "SHA-256"},
		{"control version mismatch", buildDeb(t, controlText("frigate", "9.9.9", "arm64"), "control.tar.gz"),
			func(d []byte) []byte { return statementFor(t, d, nil) }, nil, "f.deb", "does not match the signed release statement"},
		{"control package mismatch", buildDeb(t, controlText("other", "2.1.0", "arm64"), "control.tar.gz"),
			func(d []byte) []byte { return statementFor(t, d, nil) }, nil, "f.deb", "does not match the signed release statement"},
		{"unsupported compression", buildDeb(t, controlText("frigate", "2.1.0", "arm64"), "control.tar.xz"),
			func(d []byte) []byte { return statementFor(t, d, nil) }, nil, "f.deb", "gzip"},
		{"not a deb", []byte("plain text, not an archive"), func(d []byte) []byte { return statementFor(t, d, nil) }, nil, "f.deb", "not a valid controller software package"},
		{"bad filename", good, func(d []byte) []byte { return statementFor(t, d, nil) }, nil, "../f.deb", "filename"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statement := tt.statement(tt.deb)
			sig := rel.sign(t, statement)
			if tt.signer != nil {
				sig = tt.signer(statement)
			}
			path := e.stage(t, tt.deb)
			_, err := e.svc.ImportPackage(e.ctx, ImportInput{StagedPath: path, Filename: tt.filename, Statement: statement, Signature: sig})
			require.Error(t, err)
			assert.True(t, fleeterror.IsInvalidArgumentError(err), err)
			assert.Contains(t, err.Error(), tt.contains)
			_, statErr := os.Stat(path)
			assert.True(t, os.IsNotExist(statErr), "the staged file is removed on every path")
		})
	}
}

func TestImportPackage_RequiresPermission(t *testing.T) {
	e := newEnv(t)
	deb := buildDeb(t, controlText("frigate", "2.1.0", "arm64"), "control.tar.gz")

	e.auth.denyAny = true
	_, err := e.svc.ImportPackage(e.ctx, ImportInput{StagedPath: e.stage(t, deb), Filename: "f.deb"})
	assert.True(t, fleeterror.IsForbiddenError(err), err)
	_, err = e.svc.ValidateUploadStatement(e.ctx, nil, nil, nil, "f.deb", 1)
	assert.True(t, fleeterror.IsForbiddenError(err), err)
}

// versionedRelease returns a package, and its statement, for one version, so
// each case stores its own package.
func versionedRelease(t *testing.T, version string) (deb, statement []byte) {
	t.Helper()
	deb = buildDeb(t, controlText("frigate", version, "arm64"), "control.tar.gz")
	return deb, statementFor(t, deb, func(s *frigaterelease.Statement) { s.Version = version })
}

func TestImportPackage_WithoutInstalledKey(t *testing.T) {
	signer := newReleaseSigner(t)
	other := newReleaseSigner(t)
	tests := []struct {
		name      string
		publicKey []byte
		keyID     string
		warning   string
	}{
		{"no public key is stored unverified", nil, "", warnNoReleaseKey},
		{"the signing key verifies the release", signer.publicKeyPEM(t), signer.keyID(t), ""},
		{"a key that did not sign the release is stored unverified", other.publicKeyPEM(t), "", warnReleaseKeyMismatch},
	}
	for _, installed := range []struct {
		name     string
		verifier func(t *testing.T) *frigaterelease.Verifier
	}{
		{"no key directory", func(*testing.T) *frigaterelease.Verifier { return nil }},
		{"empty key directory", func(t *testing.T) *frigaterelease.Verifier { return frigaterelease.NewDirVerifier(t.TempDir()) }},
	} {
		t.Run(installed.name, func(t *testing.T) {
			e := newEnv(t)
			e.svc.verifiers[PackageKindFrigate] = installed.verifier(t)
			for i, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					deb, statement := versionedRelease(t, fmt.Sprintf("2.1.%d", i))
					res, err := e.svc.ImportPackage(e.ctx, ImportInput{
						StagedPath: e.stage(t, deb), Filename: "f.deb", Statement: statement,
						Signature: signer.sign(t, statement), PublicKey: tt.publicKey,
					})
					require.NoError(t, err)
					assert.False(t, res.Reused)
					assert.Equal(t, tt.keyID, res.Package.ReleaseKeyID)
					assert.Equal(t, tt.warning, res.Warning)
					assert.True(t, e.store.Exists(res.Package.ID, int64(len(deb))))
				})
			}
		})
	}
}

func TestImportPackage_WithoutInstalledKey_Rejections(t *testing.T) {
	e := newEnv(t)
	signer := newReleaseSigner(t)
	good := buildDeb(t, controlText("frigate", "2.1.0", "arm64"), "control.tar.gz")
	goodStatement := statementFor(t, good, nil)
	privateKey := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: []byte("not a public key")})

	tests := []struct {
		name      string
		statement []byte
		signature []byte
		publicKey []byte
		contains  string
	}{
		{"statement that is not canonical", []byte("{}"), nil, nil, "release statement rejected"},
		{"empty signature", goodStatement, []byte{}, nil, errSignatureFile},
		{"oversize signature", goodStatement, make([]byte, frigaterelease.MaxSignatureBytes+1), nil, errSignatureFile},
		{"public key that is not PEM", goodStatement, nil, []byte("not a key"), errPublicKeyFile},
		{"PEM block that is not a public key", goodStatement, nil, privateKey, errPublicKeyFile},
		{"oversize public key", goodStatement, nil, make([]byte, MaxReleasePublicKeyBytes+1), errPublicKeyFile},
		// The package is still checked against the statement without a key.
		{"size mismatch", statementFor(t, good, func(s *frigaterelease.Statement) { s.Size++ }), nil, nil, "size"},
		{"digest mismatch", statementFor(t, good, func(s *frigaterelease.Statement) { s.SHA256 = hex.EncodeToString(make([]byte, 32)) }), nil, nil, "SHA-256"},
		{"control mismatch", statementFor(t, good, func(s *frigaterelease.Statement) { s.Version = "9.9.9" }), nil, nil, "does not match the signed release statement"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signature := tt.signature
			if signature == nil {
				signature = signer.sign(t, tt.statement)
			}
			path := e.stage(t, good)
			_, err := e.svc.ImportPackage(e.ctx, ImportInput{StagedPath: path, Filename: "f.deb", Statement: tt.statement, Signature: signature, PublicKey: tt.publicKey})
			require.Error(t, err)
			assert.True(t, fleeterror.IsInvalidArgumentError(err), err)
			assert.Contains(t, err.Error(), tt.contains)
			_, statErr := os.Stat(path)
			assert.True(t, os.IsNotExist(statErr), "the staged file is removed on every path")
		})
	}
}

// An uploader's key never replaces the keys an operator installed.
func TestImportPackage_InstalledKeyIgnoresUploadedKey(t *testing.T) {
	e := newEnv(t)
	newReleaseFixture(t, e)
	other := newReleaseSigner(t)
	deb := buildDeb(t, controlText("frigate", "2.1.0", "arm64"), "control.tar.gz")
	statement := statementFor(t, deb, nil)

	_, err := e.svc.ImportPackage(e.ctx, ImportInput{
		StagedPath: e.stage(t, deb), Filename: "f.deb", Statement: statement,
		Signature: other.sign(t, statement), PublicKey: other.publicKeyPEM(t),
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err), err)
	assert.Contains(t, err.Error(), "release statement rejected")
}

// A key directory that holds a file Fleet cannot read as a key is an error. It
// must not turn into "no installed key", which would accept the upload.
func TestImportPackage_BrokenKeyDirectoryFailsClosed(t *testing.T) {
	e := newEnv(t)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "release.pem"), []byte("not a key"), 0o600))
	e.svc.verifiers[PackageKindFrigate] = frigaterelease.NewDirVerifier(dir)
	signer := newReleaseSigner(t)
	deb := buildDeb(t, controlText("frigate", "2.1.0", "arm64"), "control.tar.gz")
	statement := statementFor(t, deb, nil)

	_, err := e.svc.ImportPackage(e.ctx, ImportInput{
		StagedPath: e.stage(t, deb), Filename: "f.deb", Statement: statement,
		Signature: signer.sign(t, statement), PublicKey: signer.publicKeyPEM(t),
	})
	require.Error(t, err)
	assert.False(t, fleeterror.IsInvalidArgumentError(err), err)
	assert.Contains(t, err.Error(), "failed to verify the release statement")
}

func TestImportPackage_ReuploadWithoutInstalledKey(t *testing.T) {
	e := newEnv(t)
	signer := newReleaseSigner(t)
	deb := buildDeb(t, controlText("frigate", "2.1.0", "arm64"), "control.tar.gz")
	statement := statementFor(t, deb, nil)
	stored := func() sqlc.FrigateUpdatePackage {
		sum := sha256.Sum256(deb)
		row, err := sqlc.New(e.conn).GetFrigateUpdatePackageBySHA256(e.ctx, sqlc.GetFrigateUpdatePackageBySHA256Params{OrgID: e.orgID, Sha256: hex.EncodeToString(sum[:])})
		require.NoError(t, err)
		return row
	}
	upload := func(statement, signature, publicKey []byte) (ImportResult, error) {
		return e.svc.ImportPackage(e.ctx, ImportInput{StagedPath: e.stage(t, deb), Filename: "f.deb", Statement: statement, Signature: signature, PublicKey: publicKey})
	}

	firstSig := signer.sign(t, statement)
	first, err := upload(statement, firstSig, nil)
	require.NoError(t, err)
	assert.Empty(t, first.Package.ReleaseKeyID)

	// Fleet verified neither signature, so the newer one replaces the stored one.
	secondSig := signer.sign(t, statement)
	second, err := upload(statement, secondSig, nil)
	require.NoError(t, err)
	assert.True(t, second.Reused)
	assert.Equal(t, warnNoReleaseKey, second.Warning)
	assert.Equal(t, secondSig, stored().Signature)

	// The same package with its key becomes a verified package.
	verifiedSig := signer.sign(t, statement)
	verified, err := upload(statement, verifiedSig, signer.publicKeyPEM(t))
	require.NoError(t, err)
	assert.True(t, verified.Reused)
	assert.Equal(t, first.Package.ID, verified.Package.ID)
	assert.Equal(t, signer.keyID(t), verified.Package.ReleaseKeyID)
	assert.Empty(t, verified.Warning)

	// An upload without the key keeps the verified record and its signature.
	kept, err := upload(statement, signer.sign(t, statement), nil)
	require.NoError(t, err)
	assert.Equal(t, signer.keyID(t), kept.Package.ReleaseKeyID)
	assert.Empty(t, kept.Warning, "the stored package is verified")
	assert.Equal(t, verifiedSig, stored().Signature)

	// A different statement that Fleet cannot verify does not replace it.
	resigned := statementFor(t, deb, func(s *frigaterelease.Statement) { s.MinInstalledVersion = "1.5.0" })
	_, err = upload(resigned, signer.sign(t, resigned), nil)
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err), err)
	assert.Contains(t, err.Error(), errVerifiedStored)
	assert.Equal(t, "1.0.0", stored().MinInstalledVersion)

	// With the key, the new statement is verified and replaces the stored one.
	replaced, err := upload(resigned, signer.sign(t, resigned), signer.publicKeyPEM(t))
	require.NoError(t, err)
	assert.True(t, replaced.Reused)
	assert.Equal(t, "1.5.0", replaced.Package.MinInstalledVersion)
}

// An unverified upload that read the row before a concurrent verified upload
// committed must not replace the verified release.
func TestImportPackage_StaleUnverifiedRefreshKeepsVerifiedRelease(t *testing.T) {
	e := newEnv(t)
	signer := newReleaseSigner(t)
	deb := buildDeb(t, controlText("frigate", "2.1.0", "arm64"), "control.tar.gz")
	statement := statementFor(t, deb, nil)
	sum := sha256.Sum256(deb)
	stored := func() sqlc.FrigateUpdatePackage {
		row, err := sqlc.New(e.conn).GetFrigateUpdatePackageBySHA256(e.ctx, sqlc.GetFrigateUpdatePackageBySHA256Params{OrgID: e.orgID, Sha256: hex.EncodeToString(sum[:])})
		require.NoError(t, err)
		return row
	}

	_, err := e.svc.ImportPackage(e.ctx, ImportInput{StagedPath: e.stage(t, deb), Filename: "f.deb", Statement: statement, Signature: signer.sign(t, statement)})
	require.NoError(t, err)
	stale := stored()
	require.Empty(t, stale.ReleaseKeyID)

	verifiedSig := signer.sign(t, statement)
	_, err = e.svc.ImportPackage(e.ctx, ImportInput{
		StagedPath: e.stage(t, deb), Filename: "f.deb", Statement: statement, Signature: verifiedSig, PublicKey: signer.publicKeyPEM(t),
	})
	require.NoError(t, err)
	require.Equal(t, signer.keyID(t), stored().ReleaseKeyID)

	refresh := func(statement []byte) error {
		in := ImportInput{Statement: statement, Signature: signer.sign(t, statement)}
		check, err := e.svc.checkRelease(in.Statement, in.Signature, nil, "f.deb")
		require.NoError(t, err)
		_, err = e.svc.refreshRelease(e.ctx, stale, check, in)
		return err
	}

	require.NoError(t, refresh(statement))
	resigned := statementFor(t, deb, func(s *frigaterelease.Statement) { s.MinInstalledVersion = "1.5.0" })
	err = refresh(resigned)
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err), err)
	assert.Contains(t, err.Error(), errVerifiedStored)

	row := stored()
	assert.Equal(t, signer.keyID(t), row.ReleaseKeyID)
	assert.Equal(t, verifiedSig, row.Signature)
	assert.Equal(t, statement, row.Statement)
}

func TestValidateUploadStatement(t *testing.T) {
	e := newEnv(t)
	rel := newReleaseFixture(t, e)
	deb := buildDeb(t, controlText("frigate", "2.1.0", "arm64"), "control.tar.gz")
	statement := statementFor(t, deb, nil)
	sig := rel.sign(t, statement)

	v, err := e.svc.ValidateUploadStatement(e.ctx, statement, sig, nil, "f.deb", int64(len(deb)))
	require.NoError(t, err)
	assert.Equal(t, "2.1.0", v.Statement.Version)
	assert.Equal(t, SignatureInstalledKey, v.Signature)
	assert.Equal(t, rel.keyID(t), v.KeyID)
	assert.Empty(t, v.Warning)

	_, err = e.svc.ValidateUploadStatement(e.ctx, statement, sig, nil, "f.deb", int64(len(deb))+1)
	assert.True(t, fleeterror.IsInvalidArgumentError(err), err)
	_, err = e.svc.ValidateUploadStatement(e.ctx, statement, sig, nil, "f.deb", 0)
	assert.True(t, fleeterror.IsInvalidArgumentError(err), err)
	_, err = e.svc.ValidateUploadStatement(e.ctx, statement, []byte{1, 2}, nil, "f.deb", int64(len(deb)))
	assert.True(t, fleeterror.IsInvalidArgumentError(err), err)

	e.svc.cfg.MaxPackageSize = int64(len(deb)) - 1
	_, err = e.svc.ValidateUploadStatement(e.ctx, statement, sig, nil, "f.deb", int64(len(deb)))
	assert.True(t, fleeterror.IsInvalidArgumentError(err), err)
}

func TestValidateUploadStatement_WithoutInstalledKey(t *testing.T) {
	e := newEnv(t)
	signer := newReleaseSigner(t)
	deb := buildDeb(t, controlText("frigate", "2.1.0", "arm64"), "control.tar.gz")
	statement := statementFor(t, deb, nil)
	sig := signer.sign(t, statement)

	unverified, err := e.svc.ValidateUploadStatement(e.ctx, statement, sig, nil, "f.deb", int64(len(deb)))
	require.NoError(t, err)
	assert.Equal(t, SignatureUnverified, unverified.Signature)
	assert.Empty(t, unverified.KeyID)
	assert.Equal(t, warnNoReleaseKey, unverified.Warning)

	verified, err := e.svc.ValidateUploadStatement(e.ctx, statement, sig, signer.publicKeyPEM(t), "f.deb", int64(len(deb)))
	require.NoError(t, err)
	assert.Equal(t, SignatureUploadedKey, verified.Signature)
	assert.Equal(t, signer.keyID(t), verified.KeyID)
	assert.Empty(t, verified.Warning)

	// The size check does not depend on a key.
	_, err = e.svc.ValidateUploadStatement(e.ctx, statement, sig, nil, "f.deb", int64(len(deb))+1)
	assert.True(t, fleeterror.IsInvalidArgumentError(err), err)
}

func ecdsaSign(priv *ecdsa.PrivateKey, digest []byte) ([]byte, error) {
	sig, err := ecdsa.SignASN1(rand.Reader, priv, digest)
	if err != nil {
		return nil, fmt.Errorf("sign: %w", err)
	}
	return sig, nil
}
