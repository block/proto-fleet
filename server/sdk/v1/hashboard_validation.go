package sdk

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// HashboardIdentityClaim is untrusted placement evidence. Claims only veto
// conflicting observations; they never create, enrich, or move hardware.
type HashboardIdentityClaim struct {
	MACAddress string `json:"mac_address,omitempty"`
	Serial     string `json:"serial,omitempty"`
}

// SafeHashboardIdentity excludes values that cannot be stored losslessly in
// PostgreSQL text and its identity index. Hardware identifiers are bounded to
// 256 bytes. Never repair an identity by deleting invalid characters.
func SafeHashboardIdentity(value string) bool {
	return len(value) <= 256 && utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}

// ValidateHashboardJSON checks the JSONB storage contract before a slot enters
// the shared telemetry transaction. encoding/json alone accepts NUL, unpaired
// UTF-16 surrogates and numbers outside PostgreSQL numeric's range.
func ValidateHashboardJSON(data []byte) error {
	if !utf8.Valid(data) || !json.Valid(data) {
		return fmt.Errorf("invalid hashboard JSON")
	}
	for i := 0; i < len(data); i++ {
		if data[i] != '\\' {
			continue
		}
		i++
		if data[i] != 'u' {
			continue
		}
		code, _ := strconv.ParseUint(string(data[i+1:i+5]), 16, 16)
		i += 4
		if code == 0 {
			return fmt.Errorf("NUL in hashboard JSON")
		}
		if code >= 0xd800 && code <= 0xdbff {
			if i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
				return fmt.Errorf("unpaired hashboard surrogate")
			}
			low, err := strconv.ParseUint(string(data[i+3:i+7]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return fmt.Errorf("unpaired hashboard surrogate")
			}
			i += 6
		} else if code >= 0xdc00 && code <= 0xdfff {
			return fmt.Errorf("unpaired hashboard surrogate")
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	depth := 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("decode hashboard JSON token: %w", err)
		}
		if delimiter, ok := token.(json.Delim); ok {
			if delimiter == '{' || delimiter == '[' {
				depth++
			} else {
				depth--
			}
			if depth > 64 {
				return fmt.Errorf("hashboard JSON nesting exceeds 64 levels")
			}
		}
		number, ok := token.(json.Number)
		if !ok {
			continue
		}
		coefficient, exponent, hasExponent := strings.Cut(strings.ToLower(string(number)), "e")
		shift := 0
		if hasExponent {
			shift, err = strconv.Atoi(exponent)
			if err != nil || shift > 131072 || shift < -16383 {
				return fmt.Errorf("hashboard number outside JSONB range")
			}
		}
		integer, fraction, _ := strings.Cut(strings.TrimPrefix(coefficient, "-"), ".")
		if len(integer)+shift > 131072 || len(fraction)-shift > 16383 {
			return fmt.Errorf("hashboard number outside JSONB range")
		}
	}
}
