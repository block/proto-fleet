package poolconfigurations

import (
	"encoding/json"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
)

type StoredSlot struct {
	ID                 string `json:"id,omitempty"`
	URL                string `json:"url"`
	Username           string `json:"username"`
	WorkerTemplate     string `json:"worker_template"`
	PasswordCiphertext string `json:"password_ciphertext"`
	HasPassword        bool   `json:"has_password"`
}

func EncryptSlots(secrets *encrypt.Service, options []SlotOptions, previous []StoredSlot) ([]byte, error) {
	byID := make(map[string]StoredSlot, len(previous))
	for _, slot := range previous {
		if slot.ID != "" {
			byID[slot.ID] = slot
		}
	}
	seen := make(map[string]bool, len(options))
	stored := make([]StoredSlot, len(options))
	for i, slot := range options {
		if slot.ID != "" {
			if seen[slot.ID] {
				return nil, fleeterror.NewInvalidArgumentError("pool slot identities must be unique")
			}
			seen[slot.ID] = true
		}
		old, exists := byID[slot.ID]
		if slot.ID == "" && len(byID) == 0 && i < len(previous) {
			old, exists = previous[i], true
		}
		item := StoredSlot{ID: slot.ID, URL: slot.URL, Username: slot.Username, WorkerTemplate: slot.WorkerTemplate}
		if slot.Password == nil && exists {
			if slot.URL != old.URL || slot.Username != old.Username {
				return nil, fleeterror.NewInvalidArgumentError("changed pool destination or username requires an explicit password; use empty to clear")
			}
			item.PasswordCiphertext, item.HasPassword = old.PasswordCiphertext, old.HasPassword
		} else {
			if slot.Password == nil && len(byID) > 0 {
				return nil, fleeterror.NewInvalidArgumentError("new pool slot requires an explicit password; use empty for no password")
			}
			password := ""
			if slot.Password != nil {
				password = *slot.Password
			}
			var err error
			item.PasswordCiphertext, err = secrets.Encrypt([]byte(password))
			if err != nil {
				return nil, fleeterror.NewInternalError("encrypt pool password failed")
			}
			item.HasPassword = password != ""
		}
		stored[i] = item
	}
	encoded, err := json.Marshal(stored)
	if err != nil {
		return nil, fleeterror.NewInternalError("encode pool slots failed")
	}
	return encoded, nil
}

func DecodeSlots(encoded []byte) ([]StoredSlot, error) {
	var slots []StoredSlot
	if err := json.Unmarshal(encoded, &slots); err != nil {
		return nil, fleeterror.NewInternalError("decode pool slots failed")
	}
	return slots, nil
}
