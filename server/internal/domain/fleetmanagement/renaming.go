package fleetmanagement

import (
	"context"
	"fmt"
	"math"
	"net/netip"
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

var defaultRenameSortConfig = &interfaces.SortConfig{
	Field:     interfaces.SortFieldName,
	Direction: interfaces.SortDirectionAsc,
}

var invalidSortIPAddress = netip.MustParseAddr("0.0.0.0")

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
// Devices are sorted using the current fleet table sort (defaulting to name ascending), then
// names are generated and persisted in a single bulk UPDATE.
func (s *Service) RenameMiners(ctx context.Context, req *pb.RenameMinersRequest) (*pb.RenameMinersResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	if err := validateRenameNameConfig(req.NameConfig); err != nil {
		return nil, err
	}

	deviceIdentifiers, err := s.ResolveDeviceIdentifiers(ctx, req.DeviceSelector, info.OrganizationID)
	if err != nil {
		return nil, err
	}

	if len(deviceIdentifiers) == 0 {
		return &pb.RenameMinersResponse{}, nil
	}

	sortConfig := parseSortConfig(req.Sort)

	deviceProps, err := s.deviceStore.GetDevicePropertiesForRename(
		ctx,
		info.OrganizationID,
		deviceIdentifiers,
		sortConfig != nil && sortConfig.IsTelemetrySort(),
	)
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

	sortDevicePropsForRename(deviceProps, sortConfig)

	if err := validateRequestWideGeneratedNames(req.NameConfig, len(deviceProps)); err != nil {
		return nil, err
	}

	names := make(map[string]string, len(deviceProps))
	unchangedCount := 0
	failedCount := 0
	for idx, props := range deviceProps {
		workerName := ""
		if workerNames != nil {
			workerName = workerNames[props.DeviceIdentifier]
		}

		name, err := generateName(req.NameConfig, props, workerName, idx)
		if err != nil {
			// Request-level config errors are validated before the batch loop;
			// any remaining generation error is specific to this device's data.
			failedCount++
			continue
		}
		if name == "" || isUnchangedRename(name, props) {
			// Blank results are intentional no-ops for omitted/reserved properties
			// and devices without a usable worker-name segment.
			unchangedCount++
			continue
		}
		names[props.DeviceIdentifier] = name
	}

	if err := s.deviceStore.UpdateDeviceCustomNames(ctx, info.OrganizationID, names); err != nil {
		return nil, err
	}

	return &pb.RenameMinersResponse{
		RenamedCount:   renameResponseCount(len(names)),
		UnchangedCount: renameResponseCount(unchangedCount),
		FailedCount:    renameResponseCount(failedCount),
	}, nil
}

func sortDevicePropsForRename(deviceProps []interfaces.DeviceRenameProperties, sortConfig *interfaces.SortConfig) {
	if len(deviceProps) <= 1 {
		return
	}

	normalizedSortConfig := defaultRenameSortConfig
	if sortConfig != nil && !sortConfig.IsUnspecified() {
		normalizedSortConfig = sortConfig
	}

	sort.Slice(deviceProps, func(i, j int) bool {
		return lessDevicePropsForRename(deviceProps[i], deviceProps[j], normalizedSortConfig)
	})
}

func lessDevicePropsForRename(
	left interfaces.DeviceRenameProperties,
	right interfaces.DeviceRenameProperties,
	sortConfig *interfaces.SortConfig,
) bool {
	switch sortConfig.Field {
	case interfaces.SortFieldUnspecified,
		interfaces.SortFieldName,
		interfaces.SortFieldIPAddress,
		interfaces.SortFieldMACAddress:
	case interfaces.SortFieldHashrate:
		return lessNullableFloat64(left.Hashrate, right.Hashrate, sortConfig.Direction, left.DiscoveredDeviceID, right.DiscoveredDeviceID)
	case interfaces.SortFieldTemperature:
		return lessNullableFloat64(left.Temperature, right.Temperature, sortConfig.Direction, left.DiscoveredDeviceID, right.DiscoveredDeviceID)
	case interfaces.SortFieldPower:
		return lessNullableFloat64(left.Power, right.Power, sortConfig.Direction, left.DiscoveredDeviceID, right.DiscoveredDeviceID)
	case interfaces.SortFieldEfficiency:
		return lessNullableFloat64(left.Efficiency, right.Efficiency, sortConfig.Direction, left.DiscoveredDeviceID, right.DiscoveredDeviceID)
	case interfaces.SortFieldFirmware:
		return lessNullableString(
			left.FirmwareSortValue,
			right.FirmwareSortValue,
			sortConfig.Direction,
			left.DiscoveredDeviceID,
			right.DiscoveredDeviceID,
		)
	case interfaces.SortFieldModel:
		return lessNullableString(
			left.ModelSortValue,
			right.ModelSortValue,
			sortConfig.Direction,
			left.DiscoveredDeviceID,
			right.DiscoveredDeviceID,
		)
	}

	comparison := compareDevicePropsForRename(left, right, sortConfig.Field)
	if comparison == 0 {
		return lessDiscoveredDeviceID(left.DiscoveredDeviceID, right.DiscoveredDeviceID, sortConfig.Direction)
	}

	return comparisonForDirection(comparison, sortConfig.Direction)
}

func compareDevicePropsForRename(
	left interfaces.DeviceRenameProperties,
	right interfaces.DeviceRenameProperties,
	field interfaces.SortField,
) int {
	switch field {
	case interfaces.SortFieldUnspecified,
		interfaces.SortFieldName,
		interfaces.SortFieldHashrate,
		interfaces.SortFieldTemperature,
		interfaces.SortFieldPower,
		interfaces.SortFieldEfficiency,
		interfaces.SortFieldFirmware:
		// Telemetry sorts are handled earlier by lessDevicePropsForRename; this
		// fallback preserves deterministic behavior if compareDevicePropsForRename
		// is ever called directly with those fields.
		return strings.Compare(getRenameSortName(left), getRenameSortName(right))
	case interfaces.SortFieldIPAddress:
		return compareIPAddresses(left.IPAddress, right.IPAddress)
	case interfaces.SortFieldMACAddress:
		return strings.Compare(left.MacAddress, right.MacAddress)
	case interfaces.SortFieldModel:
		return compareNullableString(left.ModelSortValue, right.ModelSortValue)
	}

	return strings.Compare(getRenameSortName(left), getRenameSortName(right))
}

func getRenameSortName(props interfaces.DeviceRenameProperties) string {
	if strings.TrimSpace(props.CustomName) != "" {
		return strings.TrimSpace(props.CustomName)
	}

	return strings.TrimSpace(props.Manufacturer + " " + props.Model)
}

func compareNullableFloat64(left *float64, right *float64) int {
	if left == nil && right == nil {
		return 0
	}
	if left == nil {
		return 1
	}
	if right == nil {
		return -1
	}
	if math.IsNaN(*left) && math.IsNaN(*right) {
		return 0
	}
	if math.IsNaN(*left) {
		return 1
	}
	if math.IsNaN(*right) {
		return -1
	}
	switch {
	case *left < *right:
		return -1
	case *left > *right:
		return 1
	default:
		return 0
	}
}

func lessNullableFloat64(
	left *float64,
	right *float64,
	direction interfaces.SortDirection,
	leftTie int64,
	rightTie int64,
) bool {
	if left == nil && right == nil {
		return lessDiscoveredDeviceID(leftTie, rightTie, direction)
	}
	if left == nil {
		return false
	}
	if right == nil {
		return true
	}

	comparison := compareNullableFloat64(left, right)
	if comparison == 0 {
		return lessDiscoveredDeviceID(leftTie, rightTie, direction)
	}
	if direction == interfaces.SortDirectionDesc {
		return comparison > 0
	}
	return comparison < 0
}

func compareNullableString(left *string, right *string) int {
	if left == nil && right == nil {
		return 0
	}
	if left == nil {
		return 1
	}
	if right == nil {
		return -1
	}
	return strings.Compare(*left, *right)
}

func lessNullableString(
	left *string,
	right *string,
	direction interfaces.SortDirection,
	leftTie int64,
	rightTie int64,
) bool {
	if left == nil && right == nil {
		return lessDiscoveredDeviceID(leftTie, rightTie, direction)
	}
	if left == nil {
		return false
	}
	if right == nil {
		return true
	}

	comparison := compareNullableString(left, right)
	if comparison == 0 {
		return lessDiscoveredDeviceID(leftTie, rightTie, direction)
	}
	if direction == interfaces.SortDirectionDesc {
		return comparison > 0
	}
	return comparison < 0
}

func compareIPAddresses(left string, right string) int {
	leftIP := parseSortIPAddress(left)
	rightIP := parseSortIPAddress(right)
	return leftIP.Compare(rightIP)
}

func parseSortIPAddress(value string) netip.Addr {
	if parsed, err := netip.ParseAddr(value); err == nil {
		return parsed
	}

	return invalidSortIPAddress
}

func lessDiscoveredDeviceID(left int64, right int64, direction interfaces.SortDirection) bool {
	if direction == interfaces.SortDirectionDesc {
		return left > right
	}

	return left < right
}

func comparisonForDirection(comparison int, direction interfaces.SortDirection) bool {
	if direction == interfaces.SortDirectionDesc {
		return comparison > 0
	}

	return comparison < 0
}

func isUnchangedRename(nextName string, props interfaces.DeviceRenameProperties) bool {
	return strings.TrimSpace(nextName) == getRenameSortName(props)
}

func renameResponseCount(n int) int32 {
	if n > math.MaxInt32 {
		return math.MaxInt32
	}

	return int32(n) // #nosec G115 -- bounded above by math.MaxInt32
}

func validateRenameNameConfig(cfg *pb.MinerNameConfig) error {
	if cfg == nil {
		return fleeterror.NewInvalidArgumentError("name_config is required")
	}

	if len(cfg.Properties) == 0 {
		return fleeterror.NewInvalidArgumentError("name_config.properties must contain at least one item")
	}

	switch cfg.Separator {
	case "-", "_", ".", "":
		return nil
	default:
		return fleeterror.NewInvalidArgumentError("name_config.separator must be one of '-', '_', '.', or empty")
	}
}

func validateRequestWideGeneratedNames(cfg *pb.MinerNameConfig, deviceCount int) error {
	if cfg == nil || deviceCount == 0 || renameConfigDependsOnDeviceData(cfg) {
		return nil
	}

	_, err := generateName(cfg, interfaces.DeviceRenameProperties{}, "", deviceCount-1)
	return err
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

func renameConfigDependsOnDeviceData(cfg *pb.MinerNameConfig) bool {
	if cfg == nil {
		return false
	}

	for _, prop := range cfg.Properties {
		fv, ok := prop.Kind.(*pb.NameProperty_FixedValue)
		if !ok {
			continue
		}

		switch fv.FixedValue.GetType() {
		case pb.FixedValueType_FIXED_VALUE_TYPE_MAC_ADDRESS,
			pb.FixedValueType_FIXED_VALUE_TYPE_SERIAL_NUMBER,
			pb.FixedValueType_FIXED_VALUE_TYPE_WORKER_NAME,
			pb.FixedValueType_FIXED_VALUE_TYPE_MODEL,
			pb.FixedValueType_FIXED_VALUE_TYPE_MANUFACTURER:
			return true
		case pb.FixedValueType_FIXED_VALUE_TYPE_LOCATION,
			pb.FixedValueType_FIXED_VALUE_TYPE_UNSPECIFIED:
			continue
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
