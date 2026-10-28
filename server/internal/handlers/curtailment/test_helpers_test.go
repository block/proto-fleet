package curtailment

import (
	"github.com/block/proto-fleet/server/internal/domain/curtailment"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/mqttingest"
)

func newTestHandler(service *curtailment.Service, mqttSettings ...*mqttingest.SettingsService) *Handler {
	var settings *mqttingest.SettingsService
	if len(mqttSettings) > 0 {
		settings = mqttSettings[0]
	}
	return NewHandler(service, nil, nil, settings)
}

func newTestHandlerWithProfiles(service *curtailment.Service, profiles *curtailment.ResponseProfileService) *Handler {
	return NewHandler(service, profiles, nil, nil)
}
