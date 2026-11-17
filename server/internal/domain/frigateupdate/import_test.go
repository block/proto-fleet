package frigateupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/frigaterelease"
)

// releaseFixture signs statements for a test key the service trusts.
type releaseFixture struct {
	priv *ecdsa.PrivateKey
	key  frigaterelease.TrustedKey
}

func newReleaseFixture(t *testing.T, e *env) releaseFixture {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	key, err := frigaterelease.NewTrustedKey(&priv.PublicKey)
	require.NoError(t, err)
	e.svc.verifier = frigaterelease.NewStaticVerifier(key)
	return releaseFixture{priv: priv, key: key}
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

func TestImportPackage_RequiresPermissionAndVerifier(t *testing.T) {
	e := newEnv(t)
	deb := buildDeb(t, controlText("frigate", "2.1.0", "arm64"), "control.tar.gz")

	_, err := e.svc.ImportPackage(e.ctx, ImportInput{StagedPath: e.stage(t, deb), Filename: "f.deb", Statement: []byte("{}"), Signature: []byte{1}})
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err), "no verifier configured disables upload")

	e.auth.denyAny = true
	_, err = e.svc.ImportPackage(e.ctx, ImportInput{StagedPath: e.stage(t, deb), Filename: "f.deb"})
	assert.True(t, fleeterror.IsForbiddenError(err), err)
	_, err = e.svc.ValidateUploadStatement(e.ctx, nil, nil, "f.deb", 1)
	assert.True(t, fleeterror.IsForbiddenError(err), err)
}

func TestValidateUploadStatement(t *testing.T) {
	e := newEnv(t)
	rel := newReleaseFixture(t, e)
	deb := buildDeb(t, controlText("frigate", "2.1.0", "arm64"), "control.tar.gz")
	statement := statementFor(t, deb, nil)
	sig := rel.sign(t, statement)

	v, err := e.svc.ValidateUploadStatement(e.ctx, statement, sig, "f.deb", int64(len(deb)))
	require.NoError(t, err)
	assert.Equal(t, "2.1.0", v.Statement.Version)

	_, err = e.svc.ValidateUploadStatement(e.ctx, statement, sig, "f.deb", int64(len(deb))+1)
	assert.True(t, fleeterror.IsInvalidArgumentError(err), err)
	_, err = e.svc.ValidateUploadStatement(e.ctx, statement, sig, "f.deb", 0)
	assert.True(t, fleeterror.IsInvalidArgumentError(err), err)
	_, err = e.svc.ValidateUploadStatement(e.ctx, statement, []byte{1, 2}, "f.deb", int64(len(deb)))
	assert.True(t, fleeterror.IsInvalidArgumentError(err), err)

	e.svc.cfg.MaxPackageSize = int64(len(deb)) - 1
	_, err = e.svc.ValidateUploadStatement(e.ctx, statement, sig, "f.deb", int64(len(deb)))
	assert.True(t, fleeterror.IsInvalidArgumentError(err), err)
}

func ecdsaSign(priv *ecdsa.PrivateKey, digest []byte) ([]byte, error) {
	sig, err := ecdsa.SignASN1(rand.Reader, priv, digest)
	if err != nil {
		return nil, fmt.Errorf("sign: %w", err)
	}
	return sig, nil
}
