package authz

import (
	"regexp"
	"strconv"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// roleIDPattern locks ParseRoleID to the canonical base-10 form. Without
// it strconv.ParseInt would accept "+123" / leading whitespace / unicode
// digits, all of which round-trip to a different string than the one
// the wire layer emits in roleViewToProto.
var roleIDPattern = regexp.MustCompile(`^[1-9][0-9]*$`)

// ParseRoleID converts a wire role_id string into the int64 primary key.
// Anything that doesn't round-trip to the canonical form surfaces as
// InvalidArgument so probes with "+1" / leading whitespace / unicode
// digits don't leak existence the way a NotFound would.
func ParseRoleID(s string) (int64, error) {
	if !roleIDPattern.MatchString(s) {
		return 0, fleeterror.NewInvalidArgumentError("invalid role_id")
	}
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fleeterror.NewInvalidArgumentError("invalid role_id")
	}
	return id, nil
}
