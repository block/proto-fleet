package frigateupdate

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/frigaterelease"
	domain "github.com/block/proto-fleet/server/internal/domain/frigateupdate"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

const (
	protoOSTag     = "v0.6.2-internal.14"
	protoOSVersion = "0.6.2~internal.14"
	protoOSControl = "Package: proto-os\nVersion: 0.6.2~internal.14\nArchitecture: all\nMaintainer: x <x@example.com>\nDescription: t\n"
)

// newProtoOSUploadEnv returns a Fleet whose ProtoOS key directory holds the key
// that signs the release, and whose Frigate key directory holds another key.
func newProtoOSUploadEnv(t *testing.T) (*uploadEnv, *ecdsa.PrivateKey) {
	t.Helper()
	frigatePriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	frigateKey, err := frigaterelease.NewTrustedKey(&frigatePriv.PublicKey)
	require.NoError(t, err)
	e := newUploadEnvWithVerifiers(t, func(signer frigaterelease.TrustedKey) map[domain.PackageKind]*frigaterelease.Verifier {
		return map[domain.PackageKind]*frigaterelease.Verifier{
			domain.PackageKindFrigate: frigaterelease.NewStaticVerifier(frigateKey),
			domain.PackageKindProtoOS: frigaterelease.NewStaticVerifier(signer),
		}
	})
	e.useProtoOSRelease(protoOSControl, nil)
	return e, frigatePriv
}

// useProtoOSRelease makes the env's release a proto-os package with control
// as its control file, signed by the env's key.
func (e *uploadEnv) useProtoOSRelease(control string, mutate func(*frigaterelease.Statement)) {
	e.t.Helper()
	e.deb = buildDebWithControl(e.t, 1<<20+77, control)
	sum := sha256.Sum256(e.deb)
	e.statemnt = e.statement(func(s *frigaterelease.Statement) {
		s.Package, s.Version, s.Architecture, s.Repository, s.MinInstalledVersion = "proto-os", protoOSVersion, "all", "block/proto-fleet", ""
		s.SHA256, s.Size = hex.EncodeToString(sum[:]), int64(len(e.deb))
		if mutate != nil {
			mutate(s)
		}
	})
	e.sig = e.sign(e.statemnt)
}

func (e *uploadEnv) protoOSBody() initiateRequest {
	body := e.validBody()
	body.Filename = sdk.ProtoOSAssetName(protoOSTag)
	return body
}

func TestUpload_ProtoOSPackage(t *testing.T) {
	e, _ := newProtoOSUploadEnv(t)

	init, done := e.uploadBody("alice", e.protoOSBody())

	assert.Equal(t, "proto_os", init.Kind)
	assert.Empty(t, init.Warning)
	assert.Equal(t, "proto_os", done.Package.Kind)
	assert.Equal(t, protoOSVersion, done.Package.Version)
	assert.Equal(t, "all", done.Package.Architecture)
	assert.Equal(t, sdk.ProtoOSAssetName(protoOSTag), done.Package.Filename)
	assert.Regexp(t, `^sha256:[0-9a-f]{64}$`, done.Package.ReleaseKeyID, "the ProtoOS key verified it")
	assert.False(t, done.Reused)
}

func TestUpload_ProtoOSRulesAtInitiate(t *testing.T) {
	for name, tc := range map[string]struct {
		mutate   func(*frigaterelease.Statement)
		filename string
		want     string
	}{
		"architecture":          {mutate: func(s *frigaterelease.Statement) { s.Architecture = "arm64" }, want: "architecture"},
		"min_installed_version": {mutate: func(s *frigaterelease.Statement) { s.MinInstalledVersion = "0.6.0" }, want: "min_installed_version"},
		"version without a tag": {mutate: func(s *frigaterelease.Statement) { s.Version = "0.6.2-1" }, want: "does not come from a release tag"},
		"file name":             {filename: "proto-os_0.6.2_all.deb", want: sdk.ProtoOSAssetName(protoOSTag)},
		"size":                  {mutate: func(s *frigaterelease.Statement) { s.Size = 64<<20 + 1 }, want: "maximum allowed size (67108864 bytes)"},
	} {
		t.Run(name, func(t *testing.T) {
			e, _ := newProtoOSUploadEnv(t)
			e.useProtoOSRelease(protoOSControl, tc.mutate)
			body := e.protoOSBody()
			if tc.filename != "" {
				body.Filename = tc.filename
			}
			statement, err := frigaterelease.ParseStatement(e.statemnt)
			require.NoError(t, err)
			body.Size = statement.Size

			rec := e.initiate("alice", body)

			assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			assert.Contains(t, rec.Body.String(), tc.want)
		})
	}
}

func TestUpload_ProtoOSControlFileAtComplete(t *testing.T) {
	for name, control := range map[string]string{
		"package":      "Package: frigate\nVersion: 0.6.2~internal.14\nArchitecture: all\n",
		"architecture": "Package: proto-os\nVersion: 0.6.2~internal.14\nArchitecture: arm64\n",
		"revision":     "Package: proto-os\nVersion: 0.6.2~internal.14-1\nArchitecture: all\n",
	} {
		t.Run(name, func(t *testing.T) {
			e, _ := newProtoOSUploadEnv(t)
			e.useProtoOSRelease(control, nil)
			init := decode[initiateResponse](t, e.initiate("alice", e.protoOSBody()))
			require.Equal(t, http.StatusOK, e.put("alice", init.UploadID, 0, len(e.deb)-1, len(e.deb), e.deb).Code)

			rec := e.complete("alice", init.UploadID)

			assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			assert.Contains(t, rec.Body.String(), "statement")
		})
	}
}

func TestUpload_ReleaseKeysAreSeparatePerKind(t *testing.T) {
	t.Run("a Frigate release key does not verify a ProtoOS package", func(t *testing.T) {
		e, frigatePriv := newProtoOSUploadEnv(t)
		body := e.protoOSBody()
		digest := sha256.Sum256(e.statemnt)
		sig, err := ecdsa.SignASN1(rand.Reader, frigatePriv, digest[:])
		require.NoError(t, err)
		body.ReleaseSignatureB64 = base64.StdEncoding.EncodeToString(sig)

		rec := e.initiate("alice", body)

		assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), "signature")
	})

	t.Run("a ProtoOS release key does not verify a Frigate package", func(t *testing.T) {
		e, _ := newProtoOSUploadEnv(t)
		e.deb = buildDeb(t, 1<<20)
		sum := sha256.Sum256(e.deb)
		e.statemnt = e.statement(func(s *frigaterelease.Statement) { s.SHA256 = hex.EncodeToString(sum[:]); s.Size = int64(len(e.deb)) })
		e.sig = e.sign(e.statemnt)

		rec := e.initiate("alice", e.validBody())

		assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), "signature")
	})

	t.Run("without a ProtoOS key the Frigate key is not used", func(t *testing.T) {
		e := newUploadEnvWithVerifiers(t, func(signer frigaterelease.TrustedKey) map[domain.PackageKind]*frigaterelease.Verifier {
			return map[domain.PackageKind]*frigaterelease.Verifier{domain.PackageKindFrigate: frigaterelease.NewStaticVerifier(signer)}
		})
		e.useProtoOSRelease(protoOSControl, nil)

		init, unverified := e.uploadBody("alice", e.protoOSBody())
		assert.Contains(t, init.Warning, "no release key to verify the signature")
		assert.Empty(t, unverified.Package.ReleaseKeyID)

		// The key in the upload verifies it, as for a Frigate package.
		body := e.protoOSBody()
		body.ReleasePublicKeyB64 = e.publicKeyB64()
		_, verified := e.uploadBody("alice", body)
		assert.Empty(t, verified.Warning)
		assert.Equal(t, unverified.Package.ID, verified.Package.ID)
		assert.NotEmpty(t, verified.Package.ReleaseKeyID)
	})
}

func TestUpload_FrigatePackageIsControllerSoftware(t *testing.T) {
	e := newUploadEnv(t)
	init, done := e.uploadBody("alice", e.validBody())
	assert.Equal(t, "frigate", init.Kind)
	assert.Equal(t, "frigate", done.Package.Kind)
}
