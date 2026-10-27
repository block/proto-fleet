package sdk

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
)

// DecodeMiningGroupResult enforces the closed response contract at transport
// boundaries. Errors deliberately omit payload contents, which may be sensitive.
func DecodeMiningGroupResult(data []byte, request MiningGroupRequest) (MiningGroupResult, error) {
	var result *MiningGroupResult
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&result); err != nil || result == nil {
		return MiningGroupResult{}, fmt.Errorf("invalid mining group result JSON")
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return MiningGroupResult{}, fmt.Errorf("invalid mining group result JSON")
	}
	if err := result.Validate(request); err != nil {
		return MiningGroupResult{}, err
	}
	return *result, nil
}

// Validate checks the receipt for this action, rather than treating mutation
// receipts as full observations. Validation failures do not prove a write was
// rejected: callers must preserve the original request's retry identity.
func (r MiningGroupResult) Validate(request MiningGroupRequest) error {
	observation := r.Supported || r.Suspended || r.Revision != 0 || r.MaxGroups != 0 || !r.ObservedAt.IsZero() || len(r.Groups) != 0 || r.Pending
	if r.RevisionConflict {
		if request.ReadOnly() || observation || r.Enabled || r.Removed || r.Group != nil || r.Operation != nil || r.MiningStatus != nil || r.StopRequested {
			return fmt.Errorf("invalid mining group revision conflict receipt")
		}
		return nil
	}
	groupAction := request.Action == "create" || request.Action == miningGroupActionReplacePools || request.Action == "start"
	operationAction := request.Action == miningGroupActionTransfer || request.Action == miningGroupActionOperation
	if (request.Action != "observe" && observation) ||
		(request.Action != "observe" && request.Action != miningGroupActionActivate && r.Enabled) ||
		(request.Action != "remove" && r.Removed) ||
		(!groupAction && r.Group != nil) || (!operationAction && r.Operation != nil) ||
		(request.Action != miningGroupActionMiningStatus && r.MiningStatus != nil) || (request.Action != miningGroupActionStopLegacy && r.StopRequested) {
		return fmt.Errorf("unexpected mining group result fields")
	}
	switch request.Action {
	case miningGroupActionMiningStatus:
		if r.MiningStatus == nil || r.MiningStatus.ActiveBoards < 0 {
			return fmt.Errorf("missing or invalid mining status")
		}
	case miningGroupActionStopLegacy:
		if !r.StopRequested {
			return fmt.Errorf("missing stop acknowledgement")
		}
	case "observe":
		if !r.Supported {
			if observation || r.Enabled {
				return fmt.Errorf("invalid unsupported mining group observation")
			}
			return nil
		}
		if r.Revision <= 0 || r.MaxGroups <= 0 || r.ObservedAt.IsZero() || len(r.Groups) > r.MaxGroups || (!r.Enabled && (len(r.Groups) != 0 || r.Pending)) {
			return fmt.Errorf("invalid mining group observation")
		}
		ids, macs := map[string]bool{}, map[string]bool{}
		for _, group := range r.Groups {
			if err := validateMiningGroup(group); err != nil {
				return err
			}
			if ids[group.ID] {
				return fmt.Errorf("duplicate mining group identifier")
			}
			ids[group.ID] = true
			if err := validateMiningGroupMACs(group.MACs, macs); err != nil {
				return err
			}
		}
	case miningGroupActionActivate:
		if !r.Enabled {
			return fmt.Errorf("missing mining group activation receipt")
		}
	case "remove":
		if !r.Removed {
			return fmt.Errorf("missing mining group removal receipt")
		}
	case "create", miningGroupActionReplacePools, "start":
		if r.Group == nil {
			return fmt.Errorf("missing mining group receipt")
		}
		if err := validateMiningGroup(*r.Group); err != nil {
			return err
		}
		if (request.Action != "create" && r.Group.ID != request.GroupID) || (request.Action == "start" && !r.Group.Mining) {
			return fmt.Errorf("mining group receipt does not match request")
		}
	case miningGroupActionTransfer, miningGroupActionOperation:
		if r.Operation == nil || !validMiningGroupIdentifier(r.Operation.ID) || strings.TrimSpace(r.Operation.Phase) == "" {
			return fmt.Errorf("invalid mining group operation receipt")
		}
		if request.Action == miningGroupActionOperation && r.Operation.ID != request.GroupID {
			return fmt.Errorf("mining group operation receipt does not match request")
		}
		return validateMiningGroupMACs(r.Operation.BlockedMACs, map[string]bool{})
	default:
		return fmt.Errorf("unsupported mining group result action")
	}
	return nil
}

func validMiningGroupIdentifier(id string) bool {
	return strings.TrimSpace(id) != "" && len(id) <= 128
}

func validateMiningGroup(group MiningGroup) error {
	if !validMiningGroupIdentifier(group.ID) || group.Revision <= 0 {
		return fmt.Errorf("invalid mining group identity or revision")
	}
	return validateMiningGroupMACs(group.MACs, map[string]bool{})
}

func validateMiningGroupMACs(macs []string, seen map[string]bool) error {
	for _, mac := range macs {
		hw, err := net.ParseMAC(mac)
		if err != nil || len(hw) != 6 || hw.String() != mac || seen[mac] {
			return fmt.Errorf("canonical unique physical MAC addresses required in mining group result")
		}
		seen[mac] = true
	}
	return nil
}
