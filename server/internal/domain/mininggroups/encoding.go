package mininggroups

import (
	"encoding/json"
	"fmt"
)

func encode(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode mining group journal: %w", err)
	}
	return data, nil
}
func decode(data []byte, value any) error {
	if err := json.Unmarshal(data, value); err != nil {
		return fmt.Errorf("decode mining group journal: %w", err)
	}
	return nil
}
