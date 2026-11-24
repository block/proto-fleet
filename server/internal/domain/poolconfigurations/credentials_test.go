package poolconfigurations

import (
	"encoding/base64"
	"testing"

	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/stretchr/testify/require"
)

func testCipher(t *testing.T) *encrypt.Service {
	t.Helper()
	cipher, err := encrypt.NewService(&encrypt.Config{ServiceMasterKey: base64.StdEncoding.EncodeToString(make([]byte, 32))})
	require.NoError(t, err)
	return cipher
}

func TestReorderingSlotsPreservesTheirOwnCredentials(t *testing.T) {
	cipher := testCipher(t)
	primary, backup := "primary-secret", "backup-secret"
	options := []SlotOptions{
		{ID: "primary", URL: "stratum+tcp://primary.example", Username: "payout", Password: &primary},
		{ID: "backup", URL: "stratum+tcp://backup.example", Username: "payout", Password: &backup},
	}
	encoded, err := EncryptSlots(cipher, options, nil)
	require.NoError(t, err)
	previous, err := DecodeSlots(encoded)
	require.NoError(t, err)
	options[0], options[1] = options[1], options[0]
	options[0].Password, options[1].Password = nil, nil
	encoded, err = EncryptSlots(cipher, options, previous)
	require.NoError(t, err)
	reordered, err := DecodeSlots(encoded)
	require.NoError(t, err)
	for i, want := range []string{backup, primary} {
		plain, err := cipher.Decrypt(reordered[i].PasswordCiphertext)
		require.NoError(t, err)
		require.Equal(t, want, string(plain))
	}
	require.Equal(t, "backup", reordered[0].ID)
	require.Equal(t, "primary", previous[0].ID, "the original revision is immutable")
}

func TestSlotIdentityCannotStealCredentialsByPosition(t *testing.T) {
	cipher := testCipher(t)
	password := "secret"
	encoded, err := EncryptSlots(cipher, []SlotOptions{{ID: "original", URL: "stratum+tcp://pool.example", Username: "payout", Password: &password}}, nil)
	require.NoError(t, err)
	previous, err := DecodeSlots(encoded)
	require.NoError(t, err)
	for _, id := range []string{"", "different"} {
		t.Run("identity="+id, func(t *testing.T) {
			_, err := EncryptSlots(cipher, []SlotOptions{{ID: id, URL: previous[0].URL, Username: previous[0].Username}}, previous)
			require.Error(t, err, "a missing or unknown identity cannot preserve a saved password")
			require.NotContains(t, err.Error(), password)
		})
	}
	_, err = EncryptSlots(cipher, []SlotOptions{
		{ID: "original", URL: previous[0].URL, Username: previous[0].Username},
		{ID: "original", URL: previous[0].URL, Username: previous[0].Username},
	}, previous)
	require.Error(t, err, "duplicate slot identities must be rejected")
}

func TestIdentifiedSlotPasswordReplaceClearAndDestinationChange(t *testing.T) {
	cipher := testCipher(t)
	password := "secret"
	options := []SlotOptions{{ID: "slot", URL: "stratum+tcp://pool.example", Username: "payout", Password: &password}}
	encoded, err := EncryptSlots(cipher, options, nil)
	require.NoError(t, err)
	previous, err := DecodeSlots(encoded)
	require.NoError(t, err)
	options[0].Username = "different-payout"
	options[0].Password = nil
	_, err = EncryptSlots(cipher, options, previous)
	require.Error(t, err)
	for _, replacement := range []string{"replacement", ""} {
		options[0].Password = &replacement
		encoded, err = EncryptSlots(cipher, options, previous)
		require.NoError(t, err)
		slots, err := DecodeSlots(encoded)
		require.NoError(t, err)
		plain, err := cipher.Decrypt(slots[0].PasswordCiphertext)
		require.NoError(t, err)
		require.Equal(t, replacement, string(plain))
		require.Equal(t, replacement != "", slots[0].HasPassword)
	}
}
