package command

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/block/proto-fleet/server/generated/sqlc"
)

func TestDeviceCommandStatusToProto(t *testing.T) {
	cases := map[sqlc.DeviceCommandStatusEnum]string{
		sqlc.DeviceCommandStatusEnumSUCCESS: "success",
		sqlc.DeviceCommandStatusEnumFAILED:  "failed",
	}
	for in, want := range cases {
		assert.Equal(t, want, deviceCommandStatusToProto(in))
	}

	// Any unexpected enum value should still produce a lowercase string rather
	// than panic. Future enum additions get a sensible default until they are
	// mapped explicitly.
	assert.Equal(t, "some_new_value", deviceCommandStatusToProto(sqlc.DeviceCommandStatusEnum("SOME_NEW_VALUE")))
}
