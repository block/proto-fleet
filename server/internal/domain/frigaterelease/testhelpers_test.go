package frigaterelease

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func newKey(t *testing.T) (*ecdsa.PrivateKey, TrustedKey) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	key, err := NewTrustedKey(&priv.PublicKey)
	require.NoError(t, err)
	return priv, key
}

func sign(t *testing.T, priv *ecdsa.PrivateKey, statement []byte) []byte {
	t.Helper()
	digest := sha256.Sum256(statement)
	sig, err := ecdsa.SignASN1(rand.Reader, priv, digest[:])
	require.NoError(t, err)
	return sig
}

func pemOf(t *testing.T, pub *ecdsa.PublicKey) []byte {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(pub)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
}

func validStatement() statementJSON {
	return statementJSON{
		Schema:              SchemaV1,
		Package:             "frigate",
		Version:             "1.4.0",
		Architecture:        "arm64",
		Repository:          "btc-mining/frigate",
		SHA256:              "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Size:                1234,
		MinInstalledVersion: "1.0.0",
		IssuedAt:            "2026-11-01T10:00:00Z",
	}
}

// statementBytes encodes the valid statement with the optional mutation applied.
// The struct's field order is the canonical order, so an unmutated statement is
// canonical and a mutated one is only ever invalid in the field that changed.
func statementBytes(t *testing.T, mutate func(*statementJSON)) []byte {
	t.Helper()
	m := validStatement()
	if mutate != nil {
		mutate(&m)
	}
	raw, err := json.Marshal(m)
	require.NoError(t, err)
	return raw
}

// buildDeb assembles a minimal .deb: debian-binary, a control archive, and a data archive.
func buildDeb(t *testing.T, controlName string, controlArchive []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	out.WriteString(arMagic)
	member := func(name string, data []byte) {
		fmt.Fprintf(&out, "%-16s%-12d%-6d%-6d%-8s%-10d`\n", name+"/", 0, 0, 0, "100644", len(data))
		out.Write(data)
		if len(data)%2 == 1 {
			out.WriteByte('\n')
		}
	}
	member("debian-binary", []byte("2.0\n"))
	member(controlName, controlArchive)
	member("data.tar.gz", []byte("ignored"))
	return out.Bytes()
}

func controlTar(t *testing.T, name, control string, gzipped bool) []byte {
	t.Helper()
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(control)), Typeflag: tar.TypeReg}))
	_, err := tw.Write([]byte(control))
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	if !gzipped {
		return tarBuf.Bytes()
	}
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	_, err = zw.Write(tarBuf.Bytes())
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	return gz.Bytes()
}

func bytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }
