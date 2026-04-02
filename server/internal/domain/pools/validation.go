package pools

import (
	"strings"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

const invalidPoolUsernameSeparatorMessage = "Fleet-level pool usernames can’t include periods (.). Set worker names on each miner instead."

func validatePoolUsername(username string) error {
	if strings.Contains(strings.TrimSpace(username), ".") {
		return fleeterror.NewInvalidArgumentError(invalidPoolUsernameSeparatorMessage)
	}

	return nil
}
