// Package psucap reads PSUCAP v1 capsules, the signed files that carry CT1
// PSU module firmware. Fleet checks a capsule's structure, digests, and the
// outer signature against the embedded leaf certificate. Frigate and the
// hashboard firmware remain authoritative for the certificate chain, the PSU
// identity a capsule targets, and the vendor images inside it.
package psucap

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
)

const (
	// Extension is the file name suffix of a capsule.
	Extension = ".psucap"
	// MaxSize is the PSUCAP v1 storage limit enforced by hashboard firmware.
	MaxSize = 512 << 10
	// MinSize is a header, leaf certificate, and signature with no payload.
	MinSize = headerSize + certificateSize + signatureSize
	// ChunkBytes is the largest upload chunk Frigate accepts.
	ChunkBytes = 1 << 20
	// MaxComponents is the number of vendor images one capsule can carry.
	MaxComponents = 3
)

const (
	headerSize      = 512
	certificateSize = 216
	signatureSize   = 64
	descriptorSize  = 64
	hashSize        = sha256.Size

	versionOffset        = 8
	headerSizeOffset     = 10
	totalSizeOffset      = 12
	payloadSizeOffset    = 16
	vendorOffset         = 20
	componentCountOffset = 21
	mfrIDOffset          = 32
	mfrModelOffset       = 48
	mfrRevisionOffset    = 72
	mfrRevisionEnd       = 88
	payloadHashOffset    = 120
	descriptorsOffset    = 192

	descriptorRoleOffset   = 0
	descriptorTargetOffset = 2
	descriptorImageOffset  = 4
	descriptorImageSize    = 8
	descriptorHashOffset   = 16

	certificateSubjectOffset   = 33
	certificateSubjectSize     = 32
	certificateCurveOffset     = 81
	certificateHashOffset      = 82
	certificatePublicKeyOffset = 87
	certificatePublicKeySize   = 65

	vendorBOCO = 1
)

var magic = [8]byte{'P', 'S', 'U', 'C', 'A', 'P', '1', 0}

// Component describes one vendor image in a capsule.
type Component struct {
	// Role is primary1, primary2, or secondary.
	Role     string
	TargetID uint8
	// SHA256 is the lowercase hex digest of the complete vendor image.
	SHA256 string
	// Image is the vendor image. It does not alias the verified input.
	Image []byte
}

// Capsule describes one verified capsule.
type Capsule struct {
	// ID is the lowercase hex SHA-256 of the sealed capsule. Frigate uses it
	// as the capsule ID.
	ID   string
	Size int64
	// Vendor is the payload vendor, for example "BOCO".
	Vendor string
	// The manufacturer fields name the PSU the capsule applies to. Hashboard
	// firmware refuses a PSU whose identity differs.
	ManufacturerID       string
	ManufacturerModel    string
	ManufacturerRevision string
	// SignerSubject is the embedded leaf certificate subject.
	SignerSubject string
	Components    []Component
}

// IsCapsule reports whether data begins with the PSUCAP v1 magic.
func IsCapsule(data []byte) bool {
	return len(data) >= len(magic) && bytes.Equal(data[:len(magic)], magic[:])
}

// Verify checks a sealed capsule's structure, digests, and outer signature,
// and returns its description.
func Verify(data []byte) (*Capsule, error) {
	if len(data) < MinSize || len(data) > MaxSize {
		return nil, fmt.Errorf("capsule is %d bytes; expected %d to %d", len(data), MinSize, MaxSize)
	}
	if !IsCapsule(data) {
		return nil, errors.New("not a PSUCAP capsule")
	}
	if got := binary.LittleEndian.Uint16(data[versionOffset:]); got != 1 {
		return nil, fmt.Errorf("unsupported PSUCAP version %d", got)
	}
	if got := binary.LittleEndian.Uint16(data[headerSizeOffset:]); got != headerSize {
		return nil, fmt.Errorf("invalid capsule header size %d", got)
	}
	if total := binary.LittleEndian.Uint32(data[totalSizeOffset:]); uint64(total) != uint64(len(data)) {
		return nil, fmt.Errorf("capsule size field %d does not match its %d bytes", total, len(data))
	}
	payloadSize := uint64(binary.LittleEndian.Uint32(data[payloadSizeOffset:]))
	if headerSize+payloadSize+certificateSize+signatureSize != uint64(len(data)) {
		return nil, fmt.Errorf("capsule payload size %d is inconsistent with its %d bytes", payloadSize, len(data))
	}
	count := int(data[componentCountOffset])
	if count < 1 || count > MaxComponents {
		return nil, fmt.Errorf("capsule has %d components; expected 1 to %d", count, MaxComponents)
	}

	data = bytes.Clone(data)
	digest := sha256.Sum256(data)
	capsule := &Capsule{ID: hex.EncodeToString(digest[:]), Size: int64(len(data)), Vendor: vendorName(data[vendorOffset])}
	var err error
	if capsule.ManufacturerID, err = paddedString(data[mfrIDOffset:mfrModelOffset], "manufacturer ID"); err != nil {
		return nil, err
	}
	if capsule.ManufacturerModel, err = paddedString(data[mfrModelOffset:mfrRevisionOffset], "manufacturer model"); err != nil {
		return nil, err
	}
	if capsule.ManufacturerRevision, err = paddedString(data[mfrRevisionOffset:mfrRevisionEnd], "manufacturer revision"); err != nil {
		return nil, err
	}

	payloadEnd := headerSize + payloadSize
	next := uint64(headerSize)
	previousRole := uint8(0)
	for i := range count {
		descriptor := data[descriptorsOffset+i*descriptorSize:][:descriptorSize]
		role := descriptor[descriptorRoleOffset]
		name, ok := roleName(role)
		if !ok || role <= previousRole {
			return nil, fmt.Errorf("capsule component %d has an invalid, repeated, or misordered role %d", i, role)
		}
		previousRole = role
		offset := uint64(binary.LittleEndian.Uint32(descriptor[descriptorImageOffset:]))
		size := uint64(binary.LittleEndian.Uint32(descriptor[descriptorImageSize:]))
		if offset != next || size == 0 || offset+size > payloadEnd {
			return nil, fmt.Errorf("capsule component %s is not inside the payload", name)
		}
		image := data[offset : offset+size]
		sum := sha256.Sum256(image)
		if subtle.ConstantTimeCompare(sum[:], descriptor[descriptorHashOffset:descriptorHashOffset+hashSize]) != 1 {
			return nil, fmt.Errorf("capsule component %s does not match its SHA-256", name)
		}
		capsule.Components = append(capsule.Components, Component{Role: name, TargetID: descriptor[descriptorTargetOffset], SHA256: hex.EncodeToString(sum[:]), Image: image})
		next += size
	}
	if next != payloadEnd {
		return nil, errors.New("capsule components do not fill the payload")
	}
	payloadDigest := sha256.Sum256(data[headerSize:payloadEnd])
	if subtle.ConstantTimeCompare(payloadDigest[:], data[payloadHashOffset:payloadHashOffset+hashSize]) != 1 {
		return nil, errors.New("capsule payload does not match its SHA-256")
	}

	certificate := data[payloadEnd : payloadEnd+certificateSize]
	if certificate[0] != 1 || certificate[certificateCurveOffset] != 0 || certificate[certificateHashOffset] != 0 {
		return nil, errors.New("capsule certificate is not a version 1 P-256/SHA-256 certificate")
	}
	if capsule.SignerSubject, err = paddedString(certificate[certificateSubjectOffset:certificateSubjectOffset+certificateSubjectSize], "certificate subject"); err != nil {
		return nil, err
	}
	key, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), certificate[certificatePublicKeyOffset:certificatePublicKeyOffset+certificatePublicKeySize])
	if err != nil {
		return nil, errors.New("capsule certificate has an invalid P-256 public key")
	}
	signature := data[payloadEnd+certificateSize:]
	signed := sha256.Sum256(data[:payloadEnd+certificateSize])
	r := new(big.Int).SetBytes(signature[:signatureSize/2])
	s := new(big.Int).SetBytes(signature[signatureSize/2:])
	if !ecdsa.Verify(key, signed[:], r, s) {
		return nil, errors.New("capsule signature does not verify against its certificate")
	}
	return capsule, nil
}

func vendorName(code byte) string {
	if code == vendorBOCO {
		return "BOCO"
	}
	return fmt.Sprintf("unknown(%d)", code)
}

func roleName(role uint8) (string, bool) {
	switch role {
	case 1:
		return "primary1", true
	case 2:
		return "primary2", true
	case 3:
		return "secondary", true
	}
	return "", false
}

// paddedString decodes a NUL-padded printable ASCII field.
func paddedString(field []byte, name string) (string, error) {
	end := bytes.IndexByte(field, 0)
	if end <= 0 {
		return "", fmt.Errorf("capsule %s is empty or unterminated", name)
	}
	for _, b := range field[end:] {
		if b != 0 {
			return "", fmt.Errorf("capsule %s has data after its terminator", name)
		}
	}
	for _, b := range field[:end] {
		if b < 0x20 || b > 0x7e {
			return "", fmt.Errorf("capsule %s is not printable ASCII", name)
		}
	}
	return string(field[:end]), nil
}
