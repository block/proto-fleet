package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"time"
)

const CapabilityMiningGroups = "mining_groups"
const CapabilityMiningGroupPoolClear = "mining_group_pool_clear"

const (
	miningGroupActionVerifyCleared = "verify_cleared"
	miningGroupActionClearPools    = "clear_pools"
	miningGroupActionMiningStatus  = "mining_status"
	miningGroupActionStopLegacy    = "stop_legacy"
	miningGroupActionOperation     = "operation"
	miningGroupActionActivate      = "activate"
	miningGroupActionReplacePools  = "replace_pools"
	miningGroupActionTransfer      = "transfer"
)

// MiningGroupRequest is a closed, versioned controller operation. Passwords are
// present only on replace_pools; callers must encrypt this payload for storage
// or Fleet Node transport. It is never included in command logs.
type MiningGroupRequest struct {
	Action         string                 `json:"action"`
	Key            string                 `json:"key,omitempty"`
	GroupID        string                 `json:"group_id,omitempty"`
	SourceID       string                 `json:"source_id,omitempty"`
	Revision       int64                  `json:"revision,omitempty"`
	SourceRevision int64                  `json:"source_revision,omitempty"`
	ConfigID       string                 `json:"config_id,omitempty"`
	MACs           []string               `json:"macs,omitempty"`
	Pools          []MiningGroupPoolInput `json:"pools,omitempty"`
}
type MiningGroupPoolInput struct {
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
}

func (MiningGroupRequest) String() string   { return "MiningGroupRequest{[redacted]}" }
func (MiningGroupRequest) GoString() string { return "MiningGroupRequest{[redacted]}" }
func (r MiningGroupRequest) ReadOnly() bool {
	return r.Action == "observe" || r.Action == miningGroupActionVerifyCleared || r.Action == miningGroupActionMiningStatus || r.Action == miningGroupActionOperation
}
func (r MiningGroupRequest) Validate() error {
	if len(r.Key) > 128 || len(r.GroupID) > 128 || len(r.SourceID) > 128 || len(r.ConfigID) > 128 {
		return fmt.Errorf("invalid mining group identifier")
	}
	allowed := map[string]bool{}
	switch r.Action {
	case "observe", miningGroupActionMiningStatus, miningGroupActionStopLegacy:
	case miningGroupActionOperation:
		allowed["group"] = true
		if r.GroupID == "" {
			return fmt.Errorf("operation ID required")
		}
	case miningGroupActionActivate:
		allowed["revision"] = true
	case "create":
		allowed["config"] = true
		if r.ConfigID == "" {
			return fmt.Errorf("configuration identity required")
		}
	case "start", "stop", "remove", miningGroupActionClearPools:
		allowed["group"] = true
		allowed["revision"] = true
	case miningGroupActionReplacePools:
		allowed["group"] = true
		allowed["revision"] = true
		allowed["pools"] = true
		if len(r.Pools) < 1 || len(r.Pools) > 3 {
			return fmt.Errorf("one to three pools required")
		}
		for _, p := range r.Pools {
			u, e := url.Parse(p.URL)
			if e != nil || u.Scheme != "stratum+tcp" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || strings.TrimSpace(p.Username) == "" || len(p.URL) > 2048 || len(p.Username) > 512 || len(p.Password) > 1024 {
				return fmt.Errorf("valid TCP Stratum V1 pool and worker required")
			}
		}
	case miningGroupActionVerifyCleared:
		allowed["group"] = true
		allowed["macs"] = true
		if len(r.MACs) == 0 || len(r.MACs) > 288 {
			return fmt.Errorf("selected boards required")
		}
		if err := validateMiningGroupMACs(r.MACs, map[string]bool{}); err != nil {
			return err
		}
	case miningGroupActionTransfer:
		allowed["group"] = true
		allowed["revision"] = true
		allowed["source"] = true
		allowed["macs"] = true
		if r.SourceID == r.GroupID || (r.SourceID != "" && r.SourceRevision <= 0) || len(r.MACs) == 0 || len(r.MACs) > 288 {
			return fmt.Errorf("invalid transfer selection")
		}
		seen := map[string]bool{}
		for _, mac := range r.MACs {
			hw, e := net.ParseMAC(mac)
			if e != nil || len(hw) != 6 || hw.String() != mac || seen[mac] {
				return fmt.Errorf("canonical unique physical MAC addresses required")
			}
			seen[mac] = true
		}
	default:
		return fmt.Errorf("unsupported mining group operation")
	}
	if !r.ReadOnly() && r.Key == "" {
		return fmt.Errorf("idempotency key required")
	}
	if r.ReadOnly() && r.Key != "" {
		return fmt.Errorf("unexpected idempotency key")
	}
	if allowed["revision"] && r.Revision <= 0 {
		return fmt.Errorf("expected revision required")
	}
	if allowed["group"] && r.GroupID == "" {
		return fmt.Errorf("group ID required")
	}
	if (!allowed["group"] && r.GroupID != "") || (!allowed["revision"] && r.Revision != 0) || (!allowed["config"] && r.ConfigID != "") || (!allowed["source"] && (r.SourceID != "" || r.SourceRevision != 0)) || (!allowed["macs"] && len(r.MACs) != 0) || (!allowed["pools"] && len(r.Pools) != 0) {
		return fmt.Errorf("unexpected mining group fields")
	}
	return nil
}
func DecodeMiningGroupRequest(data []byte) (MiningGroupRequest, error) {
	var r MiningGroupRequest
	if len(data) > 16384 {
		return r, fmt.Errorf("mining group request too large")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		return r, fmt.Errorf("invalid mining group request")
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return r, fmt.Errorf("invalid mining group request")
	}
	return r, r.Validate()
}

// Observations never contain passwords, including encrypted passwords.
type MiningGroupPool struct {
	ID          int32    `json:"id"`
	Name        string   `json:"name"`
	URL         string   `json:"url"`
	Username    string   `json:"username"`
	Priority    int32    `json:"priority"`
	Enabled     bool     `json:"enabled"`
	ShareWeight *float64 `json:"share_weight,omitempty"`
}
type MiningGroup struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Revision     int64             `json:"revision"`
	Mining       bool              `json:"mining"`
	CustomPolicy bool              `json:"custom_policy"`
	MACs         []string          `json:"macs"`
	Pools        []MiningGroupPool `json:"pools"`
}
type MiningGroupOperation struct {
	ID          string   `json:"id"`
	Phase       string   `json:"phase"`
	BlockedMACs []string `json:"blocked_macs,omitempty"`
}

// MiningGroupMiningStatus is a fresh controller read, not a write receipt.
// Zero active boards is only a prerequisite check; activation itself enforces
// the controller's shutdown, reservation and owner-safety requirements.
type MiningGroupMiningStatus struct {
	Mining       bool  `json:"mining"`
	ActiveBoards int32 `json:"active_boards"`
}
type MiningGroupResult struct {
	PoolsCleared  *bool                    `json:"pools_cleared,omitempty"`
	MiningStatus  *MiningGroupMiningStatus `json:"mining_status,omitempty"`
	StopRequested bool                     `json:"stop_requested,omitempty"`
	Removed       bool                     `json:"removed,omitempty"`
	// RevisionConflict proves the MDK transaction rejected before applying a write.
	RevisionConflict bool                  `json:"revision_conflict,omitempty"`
	Supported        bool                  `json:"supported"`
	Enabled          bool                  `json:"enabled"`
	Suspended        bool                  `json:"suspended"`
	Revision         int64                 `json:"revision"`
	MaxGroups        int                   `json:"max_groups"`
	ObservedAt       time.Time             `json:"observed_at"`
	Groups           []MiningGroup         `json:"groups"`
	Pending          bool                  `json:"pending"`
	Group            *MiningGroup          `json:"group,omitempty"`
	Operation        *MiningGroupOperation `json:"operation,omitempty"`
}
type DeviceMiningGroups interface {
	MiningGroups(ctx context.Context, request MiningGroupRequest) (MiningGroupResult, error)
}
