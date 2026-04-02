package encrypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"io"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

type Service struct {
	serviceMasterKey []byte
}

func NewService(config *Config) (*Service, error) {
	masterKey, err := base64.StdEncoding.DecodeString(config.ServiceMasterKey)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error decoding service master key %v", err)
	}
	if len(masterKey) != 32 {
		return nil, fleeterror.NewInternalErrorf("decoded master key not of len 32, but %d", len(masterKey))
	}
	return &Service{
		serviceMasterKey: masterKey,
	}, nil
}

func (s *Service) Encrypt(toEncrypt []byte) (string, error) {
	block, err := aes.NewCipher(s.serviceMasterKey)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("error creating aes cipher from the master key: %v", err)
	}

	iv := make([]byte, aes.BlockSize)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return "", fleeterror.NewInternalErrorf("error generating IV: %v", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fleeterror.NewInternalErrorf("error creating GCM: %v", err)
	}

	nonce := iv[:gcm.NonceSize()]
	ciphertext := gcm.Seal(nil, nonce, toEncrypt, nil)

	encryptedData := make([]byte, 0, len(nonce)+len(ciphertext))
	encryptedData = append(encryptedData, nonce...)
	encryptedData = append(encryptedData, ciphertext...)

	encodedKey := base64.StdEncoding.EncodeToString(encryptedData)

	return encodedKey, nil
}

func (s *Service) Decrypt(toDecrypt string) ([]byte, error) {
	encryptedData, err := base64.StdEncoding.DecodeString(toDecrypt)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error decoding encrypted data: %v", err)
	}

	block, err := aes.NewCipher(s.serviceMasterKey)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error creating aes cipher from the master key: %v", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error creating GCM: %v", err)
	}

	nonceSize := gcm.NonceSize()
	if len(encryptedData) < nonceSize {
		return nil, fleeterror.NewInternalErrorf("encrypted data too short")
	}

	nonce, ciphertext := encryptedData[:nonceSize], encryptedData[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf("error decrypting data: %v", err)
	}

	return plaintext, nil
}
