package fleetmanagement

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	minerInterfaces "github.com/btc-mining/proto-fleet/server/internal/domain/miner/interfaces"
	mm "github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	"github.com/btc-mining/proto-fleet/server/internal/domain/session"
	"github.com/btc-mining/proto-fleet/server/internal/domain/stores/interfaces"
)

const (
	maxCustomNameLength = 100
	defaultPoolPriority = 0

	// concurrentWorkerNameLimit bounds the number of parallel GetMiningPools RPCs
	// during worker name collection.
	concurrentWorkerNameLimit = 20

	// workerNameTimeout is the per-device timeout for GetMiningPools calls.
	workerNameTimeout = 5 * time.Second
)

// RenameMiners assigns custom names to the selected miners based on the provided name config.
// Devices are sorted by manufacturer+model for counter ordering, then names are generated
// and persisted in a single bulk UPDATE.
func (s *Service) RenameMiners(ctx context.Context, req *pb.RenameMinersRequest) (*pb.RenameMinersResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	deviceIdentifiers, err := s.ResolveDeviceIdentifiers(ctx, req.DeviceSelector, info.OrganizationID)
	if err != nil {
		return nil, err
	}

	if len(deviceIdentifiers) == 0 {
		return &pb.RenameMinersResponse{}, nil
	}

	deviceProps, err := s.deviceStore.GetDevicePropertiesForRename(ctx, info.OrganizationID, deviceIdentifiers)
	if err != nil {
		return nil, err
	}

	// Validate that all requested devices were found.
	if len(deviceProps) != len(deviceIdentifiers) {
		return nil, fleeterror.NewNotFoundErrorf("one or more device identifiers not found")
	}

	// Fetch worker names if any property requires WORKER_NAME.
	var workerNames map[string]string
	if requiresWorkerName(req.NameConfig) {
		workerNames = s.collectWorkerNames(ctx, deviceIdentifiers)
	}

	// Sort by manufacturer+model for stable counter assignment order.
	sort.Slice(deviceProps, func(i, j int) bool {
		ki := deviceProps[i].Manufacturer + " " + deviceProps[i].Model
		kj := deviceProps[j].Manufacturer + " " + deviceProps[j].Model
		if ki != kj {
			return ki < kj
		}
		return deviceProps[i].DeviceIdentifier < deviceProps[j].DeviceIdentifier
	})

	names := make(map[string]string, len(deviceProps))
	for idx, props := range deviceProps {
		workerName := ""
		if workerNames != nil {
			workerName = workerNames[props.DeviceIdentifier]
		}

		name, err := generateName(req.NameConfig, props, workerName, idx)
		if err != nil {
			return nil, err
		}
		names[props.DeviceIdentifier] = name
	}

	if err := s.deviceStore.UpdateDeviceCustomNames(ctx, info.OrganizationID, names); err != nil {
		return nil, err
	}

	return &pb.RenameMinersResponse{}, nil
}

// requiresWorkerName returns true if any property in the config is a WORKER_NAME fixed value.
func requiresWorkerName(cfg *pb.MinerNameConfig) bool {
	if cfg == nil {
		return false
	}
	for _, prop := range cfg.Properties {
		if fv, ok := prop.Kind.(*pb.NameProperty_FixedValue); ok {
			if fv.FixedValue.GetType() == pb.FixedValueType_FIXED_VALUE_TYPE_WORKER_NAME {
				return true
			}
		}
	}
	return false
}

// collectWorkerNames fetches the priority-0 pool worker name from each miner device
// using a bounded worker pool. Failures are silently ignored — devices with no
// reachable pool return an empty string, causing the WORKER_NAME segment to be
// omitted from the generated name.
func (s *Service) collectWorkerNames(ctx context.Context, deviceIdentifiers []string) map[string]string {
	results := make(map[string]string, len(deviceIdentifiers))
	if len(deviceIdentifiers) == 0 {
		return results
	}

	idCh := make(chan string, len(deviceIdentifiers))
	for _, id := range deviceIdentifiers {
		idCh <- id
	}
	close(idCh)

	var mu sync.Mutex
	var wg sync.WaitGroup

	numWorkers := min(len(deviceIdentifiers), concurrentWorkerNameLimit)
	for range numWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range idCh {
				miner, err := s.minerService.GetMinerFromDeviceIdentifier(ctx, mm.DeviceIdentifier(id))
				if err != nil {
					continue
				}
				name := fetchWorkerName(ctx, miner)
				if name == "" {
					continue
				}
				mu.Lock()
				results[id] = name
				mu.Unlock()
			}
		}()
	}

	wg.Wait()
	return results
}

// fetchWorkerName retrieves the priority-0 pool username from a miner.
// Returns an empty string if the pool cannot be reached or no priority-0 pool is configured.
func fetchWorkerName(ctx context.Context, miner minerInterfaces.Miner) string {
	callCtx, cancel := context.WithTimeout(ctx, workerNameTimeout)
	defer cancel()

	pools, err := miner.GetMiningPools(callCtx)
	if err != nil || len(pools) == 0 {
		return ""
	}

	sort.Slice(pools, func(i, j int) bool {
		return pools[i].Priority < pools[j].Priority
	})

	if pools[0].Priority != defaultPoolPriority {
		return ""
	}
	return pools[0].Username
}

// generateName produces a single name string for a device according to the name config.
// counterIndex is the 0-based position of the device in the sorted device set.
func generateName(cfg *pb.MinerNameConfig, props interfaces.DeviceRenameProperties, workerName string, counterIndex int) (string, error) {
	if cfg == nil {
		return "", fleeterror.NewInvalidArgumentError("name_config is required")
	}
	sep := cfg.Separator

	var segments []string
	for _, prop := range cfg.Properties {
		segment, err := generateSegment(prop, props, workerName, counterIndex)
		if err != nil {
			return "", err
		}
		if segment != "" {
			segments = append(segments, segment)
		}
	}

	name := strings.TrimSpace(strings.Join(segments, sep))
	if name == "" {
		return "", fleeterror.NewInvalidArgumentError("generated name is blank after applying name config")
	}
	if utf8.RuneCountInString(name) > maxCustomNameLength {
		return "", fleeterror.NewInvalidArgumentErrorf("generated name exceeds %d characters", maxCustomNameLength)
	}
	return name, nil
}

// generateSegment generates a single name segment from a NameProperty.
func generateSegment(prop *pb.NameProperty, props interfaces.DeviceRenameProperties, workerName string, counterIndex int) (string, error) {
	switch kind := prop.Kind.(type) {
	case *pb.NameProperty_StringAndCounter:
		sc := kind.StringAndCounter
		counter := formatCounter(int(sc.CounterStart)+counterIndex, int(sc.CounterScale))
		return sc.Prefix + counter + sc.Suffix, nil

	case *pb.NameProperty_Counter:
		c := kind.Counter
		return formatCounter(int(c.CounterStart)+counterIndex, int(c.CounterScale)), nil

	case *pb.NameProperty_StringValue:
		return kind.StringValue.Value, nil

	case *pb.NameProperty_FixedValue:
		return generateFixedValueSegment(kind.FixedValue, props, workerName)

	case *pb.NameProperty_Qualifier:
		// BUILDING, RACK, RACK_POSITION are reserved and not yet implemented.
		return "", nil

	default:
		return "", nil
	}
}

// generateFixedValueSegment generates a segment from a device fixed attribute.
func generateFixedValueSegment(fv *pb.FixedValueProperty, props interfaces.DeviceRenameProperties, workerName string) (string, error) {
	var raw string
	switch fv.Type {
	case pb.FixedValueType_FIXED_VALUE_TYPE_MAC_ADDRESS:
		raw = props.MacAddress
	case pb.FixedValueType_FIXED_VALUE_TYPE_SERIAL_NUMBER:
		raw = props.SerialNumber
	case pb.FixedValueType_FIXED_VALUE_TYPE_WORKER_NAME:
		// Empty workerName means no priority-0 pool is configured; omit the segment.
		raw = workerName
	case pb.FixedValueType_FIXED_VALUE_TYPE_MODEL:
		raw = props.Model
	case pb.FixedValueType_FIXED_VALUE_TYPE_MANUFACTURER:
		raw = props.Manufacturer
	case pb.FixedValueType_FIXED_VALUE_TYPE_LOCATION:
		// Reserved — not yet implemented; omit segment.
		return "", nil
	case pb.FixedValueType_FIXED_VALUE_TYPE_UNSPECIFIED:
		return "", nil
	default:
		return "", nil
	}

	if raw == "" {
		return "", nil
	}

	if fv.CharacterCount == nil {
		return raw, nil
	}

	count := int(*fv.CharacterCount)
	runes := []rune(raw)
	if count >= len(runes) {
		return raw, nil
	}

	if fv.Section == nil || *fv.Section != pb.CharacterSection_CHARACTER_SECTION_LAST {
		return string(runes[:count]), nil
	}
	return string(runes[len(runes)-count:]), nil
}

// formatCounter formats an integer as a zero-padded string with the given number of digits.
func formatCounter(value, scale int) string {
	return fmt.Sprintf("%0*d", scale, value)
}
