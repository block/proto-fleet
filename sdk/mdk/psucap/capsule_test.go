package psucap_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/block/proto-fleet/sdk/mdk/psucap"
	"github.com/block/proto-fleet/sdk/mdk/psucap/psucaptest"
)

func build(t *testing.T, options psucaptest.Options) []byte {
	t.Helper()
	data, err := psucaptest.Build(options)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestVerifyDescribesCapsule(t *testing.T) {
	data := build(t, psucaptest.Options{Revision: "HA1200000", Version: "app=1410;bl=1110", Roles: []uint8{1, 3}})
	capsule, err := psucap.Verify(data)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	if capsule.ID != hex.EncodeToString(digest[:]) || capsule.Size != int64(len(data)) {
		t.Fatalf("unexpected identity: %s, %d bytes", capsule.ID, capsule.Size)
	}
	if capsule.Vendor != "BOCO" || capsule.ManufacturerID != "BOCO" || capsule.ManufacturerModel != "BC802A27-01-02" ||
		capsule.ManufacturerRevision != "HA1200000" || capsule.SignerSubject != "dev-leaf" {
		t.Fatalf("unexpected description: %+v", capsule)
	}
	if len(capsule.Components) != 2 || capsule.Components[0].Role != "primary1" || capsule.Components[0].TargetID != 0x10 ||
		capsule.Components[1].Role != "secondary" || capsule.Components[1].TargetID != 0x20 {
		t.Fatalf("unexpected components: %+v", capsule.Components)
	}
	for _, component := range capsule.Components {
		sum := sha256.Sum256(component.Image)
		if component.SHA256 != hex.EncodeToString(sum[:]) || psucaptest.SimulatedVersion(component.Image) != "app=1410;bl=1110" {
			t.Fatalf("unexpected component image: %+v", component)
		}
	}

	// The description must not alias the caller's buffer.
	image := bytes.Clone(capsule.Components[0].Image)
	clear(data)
	if !bytes.Equal(image, capsule.Components[0].Image) {
		t.Fatal("component image aliases the verified input")
	}
}

func TestVerifyRejectsDamagedCapsules(t *testing.T) {
	valid := build(t, psucaptest.Options{})
	payloadEnd := len(valid) - 216 - 64
	tests := []struct {
		name   string
		mutate func([]byte) []byte
		want   string
	}{
		{"too short", func(d []byte) []byte { return d[:791] }, "expected 792 to"},
		{"too long", func(d []byte) []byte { return append(d, make([]byte, psucap.MaxSize)...) }, "expected 792 to"},
		{"wrong magic", func(d []byte) []byte { d[0] = 'X'; return d }, "not a PSUCAP capsule"},
		{"unknown version", func(d []byte) []byte { binary.LittleEndian.PutUint16(d[8:], 2); return d }, "unsupported PSUCAP version 2"},
		{"wrong header size", func(d []byte) []byte { binary.LittleEndian.PutUint16(d[10:], 256); return d }, "header size"},
		{"truncated", func(d []byte) []byte { return d[:len(d)-1] }, "size field"},
		{"payload size", func(d []byte) []byte { binary.LittleEndian.PutUint32(d[16:], 1); return d }, "payload size"},
		{"no components", func(d []byte) []byte { d[21] = 0; return d }, "0 components"},
		{"too many components", func(d []byte) []byte { d[21] = 4; return d }, "4 components"},
		{"unknown role", func(d []byte) []byte { d[192] = 9; return d }, "role 9"},
		{"image outside payload", func(d []byte) []byte { binary.LittleEndian.PutUint32(d[192+8:], 1<<20); return d }, "not inside the payload"},
		{"short image", func(d []byte) []byte { binary.LittleEndian.PutUint32(d[192+8:], 16); return d }, "does not match its SHA-256"},
		{"damaged image", func(d []byte) []byte { d[600] ^= 1; return d }, "component secondary does not match its SHA-256"},
		{"damaged payload digest", func(d []byte) []byte { d[120] ^= 1; return d }, "payload does not match its SHA-256"},
		{"unterminated revision", func(d []byte) []byte { copy(d[72:88], strings.Repeat("R", 16)); return d }, "manufacturer revision"},
		{"unprintable model", func(d []byte) []byte { d[48] = 0x01; return d }, "manufacturer model is not printable"},
		{"changed revision", func(d []byte) []byte { d[72] = 'Z'; return d }, "signature does not verify"},
		{"unknown certificate", func(d []byte) []byte { d[payloadEnd] = 2; return d }, "version 1 P-256"},
		{"invalid key", func(d []byte) []byte { d[payloadEnd+87] = 0; return d }, "invalid P-256 public key"},
		{"damaged signature", func(d []byte) []byte { d[len(d)-1] ^= 1; return d }, "signature does not verify"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := psucap.Verify(tt.mutate(bytes.Clone(valid)))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want an error containing %q", err, tt.want)
			}
		})
	}
}

func TestVerifyRejectsMisorderedRoles(t *testing.T) {
	// Swap the two descriptors' roles so they no longer ascend; the check
	// runs before any digest is compared.
	data := build(t, psucaptest.Options{Roles: []uint8{1, 3}})
	data[192], data[192+64] = 3, 1
	if _, err := psucap.Verify(data); err == nil || !strings.Contains(err.Error(), "misordered role 1") {
		t.Fatalf("got %v", err)
	}
}

func TestIsCapsule(t *testing.T) {
	if !psucap.IsCapsule(build(t, psucaptest.Options{})) {
		t.Fatal("capsule not recognized")
	}
	for _, data := range [][]byte{nil, []byte("PSUCAP1"), []byte("PK\x03\x04 archive")} {
		if psucap.IsCapsule(data) {
			t.Fatalf("%q recognized as a capsule", data)
		}
	}
}

func TestSimulatedVersionIgnoresOtherImages(t *testing.T) {
	if got := psucaptest.SimulatedVersion([]byte("vendor image")); got != "" {
		t.Fatalf("got %q", got)
	}
}
