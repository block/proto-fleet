package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/fleetnode/bootstrap"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

const (
	credentialKeySize        = 32
	credentialBlobVersion    = byte(1)
	credentialBlobMagic      = "PFNC"
	credentialBlobAAD        = "proto-fleet/fleetnode/miner-credential/v1"
	maxCredentialBlobBytes   = 4096
	credentialPayloadVersion = "v1"
)

type credentialCodec struct {
	key []byte
}

func ensureCredentialCodec(path string, st *bootstrap.State) (*credentialCodec, error) {
	key, changed, err := loadOrGenerateCredentialKey(st)
	if err != nil {
		return nil, err
	}
	if changed {
		if err := bootstrap.SaveState(path, st); err != nil {
			return nil, fmt.Errorf("persist credential key: %w", err)
		}
	}
	return &credentialCodec{key: append([]byte(nil), key...)}, nil
}

func loadOrGenerateCredentialKey(st *bootstrap.State) ([]byte, bool, error) {
	if st.CredentialKeyHex != "" {
		key, err := hex.DecodeString(st.CredentialKeyHex)
		if err != nil {
			return nil, false, fmt.Errorf("decode credential key: %w", err)
		}
		if len(key) != credentialKeySize {
			return nil, false, fmt.Errorf("credential key must be %d bytes, got %d", credentialKeySize, len(key))
		}
		return key, false, nil
	}

	key := make([]byte, credentialKeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, false, fmt.Errorf("generate credential key: %w", err)
	}
	st.CredentialKeyHex = hex.EncodeToString(key)
	return key, true, nil
}

func (c *credentialCodec) Seal(bundle sdk.SecretBundle) (*gatewaypb.EncryptedCredentials, error) {
	if c == nil {
		return nil, nil
	}

	switch kind := bundle.Kind.(type) {
	case nil:
		return nil, nil
	case sdk.UsernamePassword:
		username, err := c.sealValue("username", kind.Username)
		if err != nil {
			return nil, fmt.Errorf("encrypt username: %w", err)
		}
		password, err := c.sealValue("password", kind.Password)
		if err != nil {
			return nil, fmt.Errorf("encrypt password: %w", err)
		}
		return &gatewaypb.EncryptedCredentials{Username: username, Password: password}, nil
	default:
		return nil, fmt.Errorf("unsupported credential kind %T", bundle.Kind)
	}
}

func (c *credentialCodec) SecretBundle(target *gatewaypb.MinerConnectionDescriptor) (sdk.SecretBundle, error) {
	if target == nil || (len(target.GetCredentialUsername()) == 0 && len(target.GetCredentialPassword()) == 0) {
		return sdk.SecretBundle{}, nil
	}
	return c.Open(&gatewaypb.EncryptedCredentials{
		Username: target.GetCredentialUsername(),
		Password: target.GetCredentialPassword(),
	})
}

func (c *credentialCodec) Open(encrypted *gatewaypb.EncryptedCredentials) (sdk.SecretBundle, error) {
	if encrypted == nil || (len(encrypted.GetUsername()) == 0 && len(encrypted.GetPassword()) == 0) {
		return sdk.SecretBundle{}, nil
	}
	username, err := c.openValue("username", encrypted.GetUsername())
	if err != nil {
		return sdk.SecretBundle{}, err
	}
	password, err := c.openValue("password", encrypted.GetPassword())
	if err != nil {
		return sdk.SecretBundle{}, err
	}
	return sdk.SecretBundle{
		Version: credentialPayloadVersion,
		Kind: sdk.UsernamePassword{
			Username: username,
			Password: password,
		},
	}, nil
}

func (c *credentialCodec) sealValue(label, plaintext string) ([]byte, error) {
	aead, err := c.aead()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate credential nonce: %w", err)
	}
	ciphertext := aead.Seal(nil, nonce, []byte(plaintext), credentialAAD(label))

	blob := make([]byte, 0, 1+len(credentialBlobMagic)+len(nonce)+len(ciphertext))
	blob = append(blob, credentialBlobVersion)
	blob = append(blob, credentialBlobMagic...)
	blob = append(blob, nonce...)
	blob = append(blob, ciphertext...)
	if len(blob) > maxCredentialBlobBytes {
		return nil, fmt.Errorf("encrypted credential exceeds %d bytes", maxCredentialBlobBytes)
	}
	return blob, nil
}

func (c *credentialCodec) openValue(label string, blob []byte) (string, error) {
	aead, err := c.aead()
	if err != nil {
		return "", credentialAuthError(err)
	}
	magicStart := 1
	magicEnd := magicStart + len(credentialBlobMagic)
	nonceStart := magicEnd
	nonceEnd := nonceStart + aead.NonceSize()
	if len(blob) < nonceEnd+aead.Overhead() || len(blob) > maxCredentialBlobBytes || blob[0] != credentialBlobVersion || string(blob[magicStart:magicEnd]) != credentialBlobMagic {
		return "", credentialAuthError(fmt.Errorf("invalid encrypted credential"))
	}

	plaintext, err := aead.Open(nil, blob[nonceStart:nonceEnd], blob[nonceEnd:], credentialAAD(label))
	if err != nil {
		return "", credentialAuthError(err)
	}
	return string(plaintext), nil
}

func (c *credentialCodec) aead() (cipher.AEAD, error) {
	if c == nil || len(c.key) != credentialKeySize {
		return nil, fmt.Errorf("credential key is not configured")
	}
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return nil, fmt.Errorf("create credential cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create credential AEAD: %w", err)
	}
	return aead, nil
}

func credentialAuthError(err error) error {
	return sdk.SDKError{Code: sdk.ErrCodeAuthenticationFailed, Message: fmt.Sprintf("invalid encrypted miner credentials: %v", err)}
}

func credentialAAD(label string) []byte {
	return []byte(credentialBlobAAD + "/" + credentialPayloadVersion + "/" + label)
}
