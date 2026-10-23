package ct1

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// The two server IDs define a discovery group, not a hardware serial number.
// Fleet stores this opaque key without interpreting the CT1 identity rules.
func pairKey(id, peer string) (string, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(peer) == "" || id == peer || len(id) > 256 || len(peer) > 256 {
		return "", fmt.Errorf("invalid CT1 partner identity")
	}
	if id > peer {
		id, peer = peer, id
	}
	sum := sha256.Sum256(fmt.Appendf(nil, "%q:%q", id, peer))
	return "ct1:" + hex.EncodeToString(sum[:]), nil
}
