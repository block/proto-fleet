package credentialblob

import (
	"encoding/base64"
	"errors"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
)

const (
	version  = byte(1)
	magic    = "PFNC"
	nonceLen = 12
	tagLen   = 16
	maxLen   = 4096
)

var (
	// ErrMissingCredentials means the node did not return both encrypted username and password blobs.
	ErrMissingCredentials = errors.New("encrypted credentials must include username and password")
	// ErrMalformedCredentials means a returned blob does not match the node-local credential envelope shape.
	ErrMalformedCredentials = errors.New("encrypted credentials are malformed")
)

// IsValid reports whether blob has the fleet-node credential envelope shape.
func IsValid(blob []byte) bool {
	minLen := 1 + len(magic) + nonceLen + tagLen
	magicStart := 1
	magicEnd := magicStart + len(magic)
	return len(blob) >= minLen &&
		len(blob) <= maxLen &&
		blob[0] == version &&
		string(blob[magicStart:magicEnd]) == magic
}

// Encode stores returned encrypted credential blobs as base64 strings for the existing credential columns.
func Encode(creds *gatewaypb.EncryptedCredentials) (username, password string, err error) {
	if creds == nil || len(creds.GetUsername()) == 0 || len(creds.GetPassword()) == 0 {
		return "", "", ErrMissingCredentials
	}
	return base64.StdEncoding.EncodeToString(creds.GetUsername()), base64.StdEncoding.EncodeToString(creds.GetPassword()), nil
}

// EncodeValid stores returned encrypted credentials after enforcing the fleet-node credential envelope shape.
func EncodeValid(creds *gatewaypb.EncryptedCredentials) (username, password string, err error) {
	username, password, err = Encode(creds)
	if err != nil {
		return "", "", err
	}
	if !IsValid(creds.GetUsername()) || !IsValid(creds.GetPassword()) {
		return "", "", ErrMalformedCredentials
	}
	return username, password, nil
}
