// Package psucaptest builds PSUCAP v1 capsules for tests and the CT1
// simulator. They carry placeholder images signed with a throwaway key and
// must never be sent to real hardware.
package psucaptest

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

const (
	headerSize      = 512
	certificateSize = 216
	signatureSize   = 64
	descriptorSize  = 64
	// A vendor image is a host header, which is not sent to the PSU, then an
	// update subject and the program body.
	hostHeaderSize = 64
	subjectSize    = 64
	bodySize       = 256

	// versionMarker precedes the firmware version a placeholder image
	// installs on a simulated PSU. Real images carry no such field.
	versionMarker = "FLEET-SIMULATED-PSU-VERSION:"
	maxVersion    = 64
)

// Options selects what Build writes. The zero value is a BOCO capsule with
// one secondary image that installs DefaultVersion.
type Options struct {
	// Revision is the PSU manufacturer revision the capsule targets.
	Revision string
	// Version is the firmware version the simulator reports once the capsule
	// is applied.
	Version string
	// Roles lists the component roles in ascending order (1 primary1,
	// 2 primary2, 3 secondary).
	Roles []uint8
	// Subject is the leaf certificate subject.
	Subject string
}

const (
	DefaultRevision = "HA1200000"
	DefaultVersion  = "app=1410;bl=1110"
)

// Build returns a sealed capsule.
func Build(options Options) ([]byte, error) {
	if options.Revision == "" {
		options.Revision = DefaultRevision
	}
	if options.Version == "" {
		options.Version = DefaultVersion
	}
	if len(options.Roles) == 0 {
		options.Roles = []uint8{3}
	}
	if options.Subject == "" {
		options.Subject = "dev-leaf"
	}
	if len(options.Roles) > 3 || len(options.Revision) > 15 || len(options.Subject) > 31 || len(options.Version) > maxVersion {
		return nil, fmt.Errorf("capsule options exceed the PSUCAP v1 field sizes")
	}

	header := make([]byte, headerSize)
	copy(header, "PSUCAP1\x00")
	binary.LittleEndian.PutUint16(header[8:], 1)
	binary.LittleEndian.PutUint16(header[10:], headerSize)
	header[20] = 1
	header[21] = byte(len(options.Roles))
	copy(header[32:48], "BOCO")
	copy(header[48:72], "BC802A27-01-02")
	copy(header[72:88], options.Revision)

	var payload []byte
	for i, role := range options.Roles {
		image := placeholderImage(role, options.Version)
		descriptor := header[192+i*descriptorSize:][:descriptorSize]
		descriptor[0], descriptor[1], descriptor[2] = role, 1, image[26]
		binary.LittleEndian.PutUint32(descriptor[4:], uint32(headerSize+len(payload)))
		binary.LittleEndian.PutUint32(descriptor[8:], uint32(len(image)))
		binary.LittleEndian.PutUint32(descriptor[12:], uint32(len(image)-hostHeaderSize))
		digest := sha256.Sum256(image)
		copy(descriptor[16:48], digest[:])
		payload = append(payload, image...)
	}
	binary.LittleEndian.PutUint32(header[12:], uint32(headerSize+len(payload)+certificateSize+signatureSize))
	binary.LittleEndian.PutUint32(header[16:], uint32(len(payload)))
	payloadDigest := sha256.Sum256(payload)
	copy(header[120:152], payloadDigest[:])

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	public, err := key.PublicKey.Bytes()
	if err != nil {
		return nil, err
	}
	certificate := make([]byte, certificateSize)
	certificate[0] = 1
	copy(certificate[33:65], options.Subject)
	copy(certificate[87:152], public)

	sealed := append(append(header, payload...), certificate...)
	signed := sha256.Sum256(sealed)
	r, s, err := ecdsa.Sign(rand.Reader, key, signed[:])
	if err != nil {
		return nil, err
	}
	signature := make([]byte, signatureSize)
	r.FillBytes(signature[:signatureSize/2])
	s.FillBytes(signature[signatureSize/2:])
	return append(sealed, signature...), nil
}

// placeholderImage returns a vendor image with the BOCO AA55 host header and
// a body that names the version it installs on a simulated PSU.
func placeholderImage(role uint8, version string) []byte {
	out := make([]byte, hostHeaderSize+subjectSize+bodySize)
	out[0], out[1] = 0xaa, 0x55
	binary.LittleEndian.PutUint16(out[7:], 64)
	for i, delay := range []uint16{3000, 60, 60, 2000} {
		binary.LittleEndian.PutUint16(out[9+2*i:], delay)
	}
	out[22], out[23], out[25] = 0xd4, 0x58, 0xd5
	out[26] = map[uint8]byte{1: 0x10, 2: 0x11, 3: 0x20}[role]
	copy(out[hostHeaderSize:], "Erase")
	body := out[hostHeaderSize+subjectSize:]
	for i := range body {
		body[i] = 0xcd
	}
	copy(body, versionMarker+version+"\x00")
	return out
}

// SimulatedVersion returns the firmware version a placeholder image installs
// on a simulated PSU, or "" for any other image.
func SimulatedVersion(image []byte) string {
	_, rest, found := bytes.Cut(image, []byte(versionMarker))
	if !found {
		return ""
	}
	version, _, terminated := bytes.Cut(rest, []byte{0})
	if !terminated {
		return ""
	}
	return string(version)
}
