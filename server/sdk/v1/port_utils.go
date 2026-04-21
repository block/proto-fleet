package sdk

import (
	"fmt"
	"math"
	"strconv"
)

const (
	minValidPortNumber = 0
	maxValidPortNumber = math.MaxUint16 // 65535
	decimalBase        = 10
	int32Bits          = 32
)

// ParsePort converts a port string to int32 with validation.
// Returns an error if the port is not a valid number or is out of valid port range (0-65535).
func ParsePort(port string) (int32, error) {
	portInt64, err := strconv.ParseInt(port, decimalBase, int32Bits)
	if err != nil {
		return 0, fmt.Errorf("invalid port number: %s", port)
	}

	if portInt64 < minValidPortNumber || portInt64 > maxValidPortNumber {
		return 0, fmt.Errorf("port number out of range: %d (valid range: %d-%d)",
			portInt64, minValidPortNumber, maxValidPortNumber)
	}

	return int32(portInt64), nil
}
