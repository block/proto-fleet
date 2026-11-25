package sdk

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestContainerPowerFeedRequestIsClosed(t *testing.T) {
	valid := ContainerPowerFeedRequest{Action: ContainerPowerFeedInspect, Module: ContainerPowerFeedModule{Tank: 1, Module: 16, ReportedID: "module-a"}}
	require.NoError(t, valid.Validate())
	require.True(t, valid.ReadOnly())
	for name, req := range map[string]ContainerPowerFeedRequest{
		"unknown action":    {Action: "reboot", Module: valid.Module},
		"tank out of range": {Action: ContainerPowerFeedInspect, Module: ContainerPowerFeedModule{Tank: CT1TankCount + 1, Module: 1}},
		"module zero":       {Action: ContainerPowerFeedInspect, Module: ContainerPowerFeedModule{Tank: 1}},
		"untrimmed id":      {Action: ContainerPowerFeedInspect, Module: ContainerPowerFeedModule{Tank: 1, Module: 1, ReportedID: " a"}},
	} {
		require.Error(t, req.Validate(), name)
	}
	_, err := DecodeContainerPowerFeedRequest([]byte(`{"action":"inspect","module":{"tank":1,"module":1},"outlet":"pdu-1-outlet-01"}`))
	require.Error(t, err, "unknown fields such as a raw outlet are refused")
	_, err = DecodeContainerPowerFeedRequest([]byte(`{"action":"inspect","module":{"tank":1,"module":1}}{}`))
	require.Error(t, err)
}

func TestContainerPowerFeedWriteRequestNamesConfirmedTarget(t *testing.T) {
	module := ContainerPowerFeedModule{Tank: 1, Module: 1, ReportedID: "module-a"}
	for _, action := range []string{ContainerPowerFeedTurnOn, ContainerPowerFeedTurnOff, ContainerPowerFeedCycle, ContainerPowerFeedClearOverride} {
		req := ContainerPowerFeedRequest{Action: action, Module: module, FeedID: "pdu-1-outlet-01"}
		require.NoError(t, req.Validate(), action)
		require.False(t, req.ReadOnly(), action)
		require.Error(t, ContainerPowerFeedRequest{Action: action, Module: module}.Validate(), "%s needs the confirmed feed", action)
		require.Error(t, ContainerPowerFeedRequest{Action: action, Module: ContainerPowerFeedModule{Tank: 1, Module: 1}, FeedID: "pdu-1-outlet-01"}.Validate(), "%s needs a confirmed module identity", action)
		require.Error(t, ContainerPowerFeedRequest{Action: action, Module: module, FeedID: " pdu-1-outlet-01"}.Validate(), action)
		require.Error(t, ContainerPowerFeedRequest{Action: action, Module: module, FeedID: "pdu-1\noutlet-01"}.Validate(), "%s feed must be printable", action)
	}
	require.Error(t, ContainerPowerFeedRequest{Action: ContainerPowerFeedInspect, Module: module, FeedID: "pdu-1-outlet-01"}.Validate(), "inspection never names a feed")
}

func TestContainerPowerFeedWriteResultNeedsAcknowledgement(t *testing.T) {
	req := func(action string) ContainerPowerFeedRequest {
		return ContainerPowerFeedRequest{Action: action, Module: ContainerPowerFeedModule{Tank: 1, Module: 1, ReportedID: "module-a"}, FeedID: "pdu-1-outlet-01"}
	}
	applied := func(state string, overridden bool) ContainerPowerFeedResult {
		return ContainerPowerFeedResult{Applied: &PowerFeedReading{ID: "pdu-1-outlet-01", State: state, Overridden: overridden}}
	}
	require.NoError(t, applied(PowerFeedStateEnabled, true).Validate(req(ContainerPowerFeedTurnOn)))
	require.NoError(t, applied(PowerFeedStateDisabled, true).Validate(req(ContainerPowerFeedTurnOff)))
	require.NoError(t, applied(PowerFeedStateEnabled, true).Validate(req(ContainerPowerFeedCycle)), "a cycle acknowledges its restore target")
	require.NoError(t, applied(PowerFeedStateDisabled, false).Validate(req(ContainerPowerFeedClearOverride)))

	require.Error(t, applied(PowerFeedStateDisabled, true).Validate(req(ContainerPowerFeedTurnOn)))
	require.Error(t, applied(PowerFeedStateUnknown, true).Validate(req(ContainerPowerFeedTurnOff)))
	require.Error(t, applied(PowerFeedStateEnabled, true).Validate(req(ContainerPowerFeedClearOverride)))
	require.Error(t, ContainerPowerFeedResult{}.Validate(req(ContainerPowerFeedTurnOff)), "a write needs an acknowledgement")
	other := applied(PowerFeedStateDisabled, true)
	other.Applied.ID = "pdu-1-outlet-02"
	require.Error(t, other.Validate(req(ContainerPowerFeedTurnOff)), "the acknowledgement must name the confirmed feed")
	require.Error(t, applied(PowerFeedStateEnabled, false).Validate(ContainerPowerFeedRequest{Action: ContainerPowerFeedInspect, Module: ContainerPowerFeedModule{Tank: 1, Module: 1}}), "inspection results carry no write acknowledgement")
}

func TestContainerPowerFeedResultValidation(t *testing.T) {
	req := ContainerPowerFeedRequest{Action: ContainerPowerFeedInspect, Module: ContainerPowerFeedModule{Tank: 1, Module: 1}}
	valid := func() *ModulePowerFeedInspection {
		return &ModulePowerFeedInspection{ObservedAt: time.Now(), Mapping: PowerFeedMapping{Confidence: PowerFeedMappingConfigured, FeedID: "pdu-1-outlet-01"}, Feed: &PowerFeedReading{ID: "pdu-1-outlet-01", State: PowerFeedStateEnabled}, RecoveryCycleKnown: true}
	}
	require.NoError(t, ContainerPowerFeedResult{Inspection: valid()}.Validate(req))
	for name, mutate := range map[string]func(*ModulePowerFeedInspection){
		"missing time":           func(i *ModulePowerFeedInspection) { i.ObservedAt = time.Time{} },
		"unique without feed":    func(i *ModulePowerFeedInspection) { i.Mapping.FeedID = ""; i.Feed = nil },
		"unique but shared":      func(i *ModulePowerFeedInspection) { i.Mapping.SharedWith = []PowerFeedModuleRef{{Tank: 1, Module: 2}} },
		"unknown confidence":     func(i *ModulePowerFeedInspection) { i.Mapping.Confidence = "guessed" },
		"reading for other feed": func(i *ModulePowerFeedInspection) { i.Feed.ID = "pdu-1-outlet-02" },
		"reading and reason":     func(i *ModulePowerFeedInspection) { i.FeedUnavailableReason = PowerFeedReadingReadFailed },
		"unknown reason":         func(i *ModulePowerFeedInspection) { i.Feed = nil; i.FeedUnavailableReason = "maybe" },
		"unknown state":          func(i *ModulePowerFeedInspection) { i.Feed.State = "on" },
		"non-finite":             func(i *ModulePowerFeedInspection) { i.Feed.PowerW = new(math.Inf(1)) },
		"cycle while unknown": func(i *ModulePowerFeedInspection) {
			i.RecoveryCycleKnown = false
			i.RecoveryCycle = &PowerRecoveryCycle{ID: "c"}
		},
	} {
		i := valid()
		mutate(i)
		require.Error(t, ContainerPowerFeedResult{Inspection: i}.Validate(req), name)
	}
	data, err := json.Marshal(ContainerPowerFeedResult{Inspection: valid()})
	require.NoError(t, err)
	got, err := DecodeContainerPowerFeedResult(data, req)
	require.NoError(t, err)
	require.Equal(t, "pdu-1-outlet-01", got.Inspection.Feed.ID)
}

func TestContainerPowerFeedResultToleratesUnknownFields(t *testing.T) {
	req := ContainerPowerFeedRequest{Action: ContainerPowerFeedInspect, Module: ContainerPowerFeedModule{Tank: 1, Module: 1}}
	data, err := json.Marshal(ContainerPowerFeedResult{Inspection: &ModulePowerFeedInspection{
		ObservedAt:         time.Now(),
		Mapping:            PowerFeedMapping{Confidence: PowerFeedMappingConfigured, FeedID: "pdu-1-outlet-01"},
		Feed:               &PowerFeedReading{ID: "pdu-1-outlet-01", State: PowerFeedStateEnabled},
		RecoveryCycleKnown: true,
	}})
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(data, &fields))
	fields["added_by_newer_node"] = true
	for _, v := range fields {
		if nested, ok := v.(map[string]any); ok {
			nested["added_by_newer_plugin"] = "x"
		}
	}
	extended, err := json.Marshal(fields)
	require.NoError(t, err)

	got, err := DecodeContainerPowerFeedResult(extended, req)
	require.NoError(t, err, "a newer node or plugin may add result fields")
	require.Equal(t, "pdu-1-outlet-01", got.Inspection.Feed.ID)

	_, err = DecodeContainerPowerFeedResult(append(extended, []byte(`{}`)...), req)
	require.Error(t, err, "trailing data is still refused")
	_, err = DecodeContainerPowerFeedResult(make([]byte, powerFeedPayloadLimit+1), req)
	require.Error(t, err, "the size limit still applies")
}

func TestPowerFeedWritesForFollowsFeedState(t *testing.T) {
	no, yes := false, true
	for name, tc := range map[string]struct {
		feed *PowerFeedReading
		want []string
	}{
		"no reading":         {nil, nil},
		"unknown state":      {&PowerFeedReading{State: PowerFeedStateUnknown}, nil},
		"off":                {&PowerFeedReading{State: PowerFeedStateDisabled}, []string{ContainerPowerFeedTurnOn}},
		"off and overridden": {&PowerFeedReading{State: PowerFeedStateDisabled, Overridden: true}, []string{ContainerPowerFeedTurnOn, ContainerPowerFeedClearOverride}},
		"on":                 {&PowerFeedReading{State: PowerFeedStateEnabled, Faulted: &no}, []string{ContainerPowerFeedTurnOff, ContainerPowerFeedCycle}},
		"on, fault unknown":  {&PowerFeedReading{State: PowerFeedStateEnabled}, []string{ContainerPowerFeedTurnOff}},
		"on and faulted":     {&PowerFeedReading{State: PowerFeedStateEnabled, Faulted: &yes}, []string{ContainerPowerFeedTurnOff}},
	} {
		require.Equal(t, tc.want, PowerFeedWritesFor(tc.feed), name)
	}
}
