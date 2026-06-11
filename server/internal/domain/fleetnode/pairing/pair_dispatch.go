package pairing

import (
	"context"
	"slices"
	"time"

	"google.golang.org/protobuf/proto"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/infrastructure/id"
)

// PairCommandTimeout bounds how long PairOnNode waits for results and the final
// ack, mirroring DiscoverCommandTimeout: it must exceed the agent's pairing budget
// plus slack so a slow batch's ack isn't rejected as stale. Var for tests.
var PairCommandTimeout = 12 * time.Minute

// PairOnNode dispatches a batch pair command over the node's ControlStream and
// invokes onResults per result batch for live operator display. Persistence is
// authoritative in the gateway ReportPairedDevices handler, not here, so onResults
// is best-effort display only.
//
// The command runs on a context detached from the operator's request: pairing
// mutates miners, so once dispatched it must finish server-side (the gateway keeps
// persisting even if the operator disconnects), bounded by PairCommandTimeout. We
// don't abort the node on cancel -- half-paired miners with no cloud record is worse.
func (s *Service) PairOnNode(ctx context.Context, fleetNodeID int64, targets []*pairingpb.FleetNodePairTarget, credentials *pairingpb.Credentials, orgID int64, assignedBy *int64, onResults func([]*gatewaypb.FleetNodePairResult) error) error {
	if s.dispatcher == nil {
		return fleeterror.NewInternalError("fleet node pairing dispatch is not configured")
	}
	payload, err := proto.Marshal(&pairingpb.AgentCommand{
		Command: &pairingpb.AgentCommand_Pair{Pair: &pairingpb.FleetNodePairRequest{
			Targets:     targets,
			Credentials: credentials,
		}},
	})
	if err != nil {
		return fleeterror.NewInternalErrorf("marshal pair payload: %v", err)
	}

	// scopeTargets is consumed by the registry/gateway for persistence scoping;
	// displayPending is a separate copy we consume as results arrive, to synthesize
	// a terminal display status for any target the node never reported.
	scopeTargets := make(map[string]struct{}, len(targets))
	displayPending := make(map[string]struct{}, len(targets))
	for _, t := range targets {
		scopeTargets[t.GetDeviceIdentifier()] = struct{}{}
		displayPending[t.GetDeviceIdentifier()] = struct{}{}
	}

	pair := &control.PairMeta{OrgID: orgID, AssignedBy: assignedBy, Targets: scopeTargets}
	cmd := &gatewaypb.ControlCommand{CommandId: id.GenerateID(), Payload: payload}
	err = control.RunCommand(context.WithoutCancel(ctx), s.dispatcher, fleetNodeID, cmd, nil, control.ReportKindPair, pair, PairCommandTimeout, "pair",
		func(ev control.CommandEvent) (terminal bool, err error) {
			if len(ev.PairResults) == 0 {
				return false, nil
			}
			for _, r := range ev.PairResults {
				delete(displayPending, r.GetDeviceIdentifier())
			}
			// Best-effort display: a callback error must never abort the command,
			// which runs to completion server-side where persistence is authoritative.
			_ = onResults(ev.PairResults)
			return false, nil
		})

	// Give every un-reported target a terminal display status, even on a command
	// error (timeout/disconnect); onResults is best-effort when the operator is gone.
	_ = reportUnpaired(displayPending, onResults)
	return err
}

// reportUnpaired emits a synthesized ERROR result for each requested device the
// node never reported, so every selected device ends with a terminal status.
func reportUnpaired(requested map[string]struct{}, onResults func([]*gatewaypb.FleetNodePairResult) error) error {
	if len(requested) == 0 {
		return nil
	}
	missing := make([]string, 0, len(requested))
	for id := range requested {
		missing = append(missing, id)
	}
	slices.Sort(missing) // deterministic order for callers/tests
	results := make([]*gatewaypb.FleetNodePairResult, 0, len(missing))
	for _, id := range missing {
		results = append(results, &gatewaypb.FleetNodePairResult{
			DeviceIdentifier: id,
			Outcome:          gatewaypb.PairOutcome_PAIR_OUTCOME_ERROR,
			ErrorMessage:     "device was not paired before the batch completed (timed out or truncated); retry",
		})
	}
	return onResults(results)
}
