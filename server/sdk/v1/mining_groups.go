package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"slices"
	"strings"
	"time"
)

const CapabilityMiningGroups = "mining_groups"
const CapabilityMiningGroupPoolClear = "mining_group_pool_clear"

const (
	MiningGroupReadDefault = "read_default"
	miningGroupActionActivationInventory = "activation_inventory"
	miningGroupActionVerifyCleared       = "verify_cleared"
	miningGroupActionClearPools          = "clear_pools"
	miningGroupActionMiningStatus        = "mining_status"
	miningGroupActionStopLegacy          = "stop_legacy"
	miningGroupActionOperation           = "operation"
	miningGroupActionActivate            = "activate"
	miningGroupActionDeactivate          = "deactivate"
	miningGroupActionReplacePools        = "replace_pools"
	miningGroupActionTransfer            = "transfer"
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
	// PoolIDs follows the order of Pools. Zero requests a new controller ID.
	// An omitted list preserves the legacy slot IDs for existing requests.
	PoolIDs []int32 `json:"pool_ids,omitempty"`
	// PoolNames preserves the name for each reused ID in the journaled replacement.
	PoolNames []string `json:"pool_names,omitempty"`
	// DefaultURLSigningKey is scoped to this controller and used only for an opaque review fingerprint.
	DefaultURLSigningKey []byte `json:"default_url_signing_key,omitempty"`
}
type MiningGroupPoolInput struct {
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
}

func (MiningGroupRequest) String() string   { return "MiningGroupRequest{[redacted]}" }
func (MiningGroupRequest) GoString() string { return "MiningGroupRequest{[redacted]}" }
func (r MiningGroupRequest) ReadOnly() bool {
	return r.Action == miningGroupActionActivationInventory || r.Action == "observe" || r.Action == MiningGroupReadDefault || r.Action == miningGroupActionVerifyCleared || r.Action == miningGroupActionMiningStatus || r.Action == miningGroupActionOperation
}
func (r MiningGroupRequest) Validate() error {
	if len(r.DefaultURLSigningKey) != 0 && (r.Action != MiningGroupReadDefault || len(r.DefaultURLSigningKey) != 32) {
		return fmt.Errorf("invalid Default URL signing key")
	}
	if len(r.Key) > 128 || len(r.GroupID) > 128 || len(r.SourceID) > 128 || len(r.ConfigID) > 128 {
		return fmt.Errorf("invalid mining group identifier")
	}
	allowed := map[string]bool{}
	switch r.Action {
	case miningGroupActionActivationInventory, "observe", MiningGroupReadDefault, miningGroupActionMiningStatus, miningGroupActionStopLegacy:
	case miningGroupActionOperation:
		allowed["group"] = true
		if r.GroupID == "" {
			return fmt.Errorf("operation ID required")
		}
	case miningGroupActionActivate, miningGroupActionDeactivate:
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
		if len(r.PoolIDs) != 0 && len(r.PoolIDs) != len(r.Pools) {
			return fmt.Errorf("pool IDs must match the replacement pools")
		}
		seen := map[int32]bool{}
		for _, id := range r.PoolIDs {
			if id < 0 || (id != 0 && seen[id]) {
				return fmt.Errorf("invalid or duplicate pool ID")
			}
			seen[id] = true
		}
		if len(r.PoolNames) != 0 && len(r.PoolNames) != len(r.PoolIDs) {
			return fmt.Errorf("pool names must match the replacement pool IDs")
		}
		seenNames := map[string]bool{}
		for _, name := range r.PoolNames {
			if strings.TrimSpace(name) == "" || seenNames[name] {
				return fmt.Errorf("invalid or duplicate pool name")
			}
			seenNames[name] = true
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
	if (!allowed["group"] && r.GroupID != "") || (!allowed["revision"] && r.Revision != 0) || (!allowed["config"] && r.ConfigID != "") || (!allowed["source"] && (r.SourceID != "" || r.SourceRevision != 0)) || (!allowed["macs"] && len(r.MACs) != 0) || (!allowed["pools"] && (len(r.Pools) != 0 || len(r.PoolIDs) != 0 || len(r.PoolNames) != 0)) {
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
	ID                    string            `json:"id"`
	Name                  string            `json:"name"`
	Revision              int64             `json:"revision"`
	Mining                bool              `json:"mining"`
	CustomPolicy          bool              `json:"custom_policy"`
	MACs                  []string          `json:"macs"`
	Pools                 []MiningGroupPool `json:"pools"`
	DefaultURLFingerprint string            `json:"default_url_fingerprint,omitempty"`
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
	Inventory     *MiningGroupInventory    `json:"inventory,omitempty"`
	Deactivated   bool                     `json:"deactivated,omitempty"`
	PoolsCleared  *bool                    `json:"pools_cleared,omitempty"`
	MiningStatus  *MiningGroupMiningStatus `json:"mining_status,omitempty"`
	StopRequested bool                     `json:"stop_requested,omitempty"`
	Removed       bool                     `json:"removed,omitempty"`
	// RevisionConflict proves the original request was rejected before applying a write.
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

func ReadDefaultMiningGroup(ctx context.Context, device DeviceMiningGroups) (MiningGroup, error) {
	request := MiningGroupRequest{Action: MiningGroupReadDefault}
	result, err := device.MiningGroups(ctx, request)
	if err != nil {
		return MiningGroup{}, err
	}
	if err := result.Validate(request); err != nil {
		return MiningGroup{}, err
	}
	return *result.Group, nil
}

// WithDefaultMiningPools loads Default settings only for consumers that need
// them. Keep nil pools as unavailable and an empty slice as a confirmed empty set.
func WithDefaultMiningPools(observed MiningGroupResult, read func() (MiningGroup, error), macs ...string) MiningGroupResult {
	if !observed.Enabled || observed.Pending || observed.Suspended {
		return observed
	}
	for i, group := range observed.Groups {
		if group.ID != "default" || group.Pools != nil || (len(macs) > 0 && !slices.ContainsFunc(macs, func(mac string) bool {
			return slices.Contains(group.MACs, mac)
		})) {
			continue
		}
		current, err := read()
		if err == nil && current.ID == group.ID && current.Revision == group.Revision && current.Pools != nil {
			observed.Groups = slices.Clone(observed.Groups)
			observed.Groups[i].Pools = current.Pools
		}
		break
	}
	return observed
}
