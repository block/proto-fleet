package fleetmanagement

import (
	"context"
	"fmt"
	"math"
	"net/netip"
	"sort"
	"strings"
	"unicode/utf8"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/workername"
)

var defaultRenameSortConfig = &interfaces.SortConfig{
	Field:     interfaces.SortFieldName,
	Direction: interfaces.SortDirectionAsc,
}

var invalidSortIPAddress = netip.MustParseAddr("0.0.0.0")

const maxCustomNameLength = 100

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

	deviceProps, err := s.loadDevicePropertiesForNameGeneration(ctx, info.OrganizationID, deviceIdentifiers, sortConfig, req.NameConfig)
	if err != nil {
		return nil, err
	}

	// Validate that all requested devices were found.
	if len(deviceProps) != len(deviceIdentifiers) {
		return nil, fleeterror.NewNotFoundErrorf("one or more device identifiers not found")
	}

	sortDevicePropsForRename(deviceProps, sortConfig)

	if err := validateRequestWideGeneratedNames(req.NameConfig, len(deviceProps)); err != nil {
		return nil, err
	}

	names := make(map[string]string, len(deviceProps))
	unchangedCount := 0
	failedCount := 0
	for idx, props := range deviceProps {
		name, err := generateName(req.NameConfig, props, idx)
		if err != nil {
			// Request-level config errors are validated before the batch loop;
			// any remaining generation error is specific to this device's data.
			failedCount++
			continue
		}
		if name == "" || isUnchangedRename(name, props) {
			// Blank results are intentional no-ops for omitted/reserved properties.
			unchangedCount++
			continue
		}
		names[props.DeviceIdentifier] = name
	}

	if err := s.deviceStore.UpdateDeviceCustomNames(ctx, info.OrganizationID, names); err != nil {
		return nil, err
	}

	count := len(deviceIdentifiers)
	s.logActivity(ctx, activitymodels.Event{
		Category:       activitymodels.CategoryFleetManagement,
		Type:           "rename_miners",
		Description:    "Rename miners",
		ScopeCount:     &count,
		UserID:         &info.ExternalUserID,
		Username:       &info.Username,
		OrganizationID: &info.OrganizationID,
	})

	return &pb.RenameMinersResponse{
		RenamedCount:   renameResponseCount(len(names)),
		UnchangedCount: renameResponseCount(unchangedCount),
		FailedCount:    renameResponseCount(failedCount),
	}, nil
}

func (s *Service) UpdateWorkerNames(ctx context.Context, req *pb.UpdateWorkerNamesRequest) (*pb.UpdateWorkerNamesResponse, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}

	if err := validateRenameNameConfig(req.NameConfig); err != nil {
		return nil, err
	}
	if s.workerNamePoolService == nil {
		return nil, fleeterror.NewInternalError("worker-name pool reapply service is not configured")
	}
	if err := s.workerNamePoolService.VerifyCredentials(ctx, req.UserUsername, req.UserPassword); err != nil {
		return nil, err
	}

	deviceIdentifiers, err := s.ResolveDeviceIdentifiers(ctx, req.DeviceSelector, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	if len(deviceIdentifiers) == 0 {
		return &pb.UpdateWorkerNamesResponse{}, nil
	}

	sortConfig := parseSortConfig(req.Sort)
	deviceProps, err := s.loadDevicePropertiesForNameGeneration(ctx, info.OrganizationID, deviceIdentifiers, sortConfig, req.NameConfig)
	if err != nil {
		return nil, err
	}

	if len(deviceProps) != len(deviceIdentifiers) {
		return nil, fleeterror.NewNotFoundErrorf("one or more device identifiers not found")
	}

	sortDevicePropsForRename(deviceProps, sortConfig)

	if err := validateRequestWideGeneratedNames(req.NameConfig, len(deviceProps)); err != nil {
		return nil, err
	}

	desiredWorkerNamesByDeviceIdentifier := make(map[string]string, len(deviceProps))
	unchangedCount := 0
	failedCount := 0
	for idx, props := range deviceProps {
		name, err := generateName(req.NameConfig, props, idx)
		if err != nil {
			failedCount++
			continue
		}

		if strings.TrimSpace(name) == "" {
			unchangedCount++
			continue
		}

		if isUnchangedWorkerName(name, props) {
			if shouldReapplyWorkerNamePools(props) {
				desiredWorkerNamesByDeviceIdentifier[props.DeviceIdentifier] = name
				continue
			}

			unchangedCount++
			continue
		}

		desiredWorkerNamesByDeviceIdentifier[props.DeviceIdentifier] = name
	}

	var batchIdentifier string
	if len(desiredWorkerNamesByDeviceIdentifier) > 0 {
		batchIdentifier, err = s.workerNamePoolService.ReapplyCurrentPoolsWithWorkerNames(
			ctx,
			desiredWorkerNamesByDeviceIdentifier,
		)
		if err != nil {
			return nil, err
		}
	}

	count := len(deviceIdentifiers)
	s.logActivity(ctx, activitymodels.Event{
		Category:       activitymodels.CategoryFleetManagement,
		Type:           "update_worker_names",
		Description:    "Update worker names",
		ScopeCount:     &count,
		UserID:         &info.ExternalUserID,
		Username:       &info.Username,
		OrganizationID: &info.OrganizationID,
		Metadata:       map[string]any{"batch_id": batchIdentifier},
	})

	return &pb.UpdateWorkerNamesResponse{
		UpdatedCount:    renameResponseCount(len(desiredWorkerNamesByDeviceIdentifier)),
		UnchangedCount:  renameResponseCount(unchangedCount),
		FailedCount:     renameResponseCount(failedCount),
		BatchIdentifier: batchIdentifier,
	}, nil
}

func shouldReapplyWorkerNamePools(props interfaces.DeviceRenameProperties) bool {
	return !workername.IsPoolSyncComplete(props.WorkerNamePoolSyncStatus)
}

func (s *Service) loadDevicePropertiesForNameGeneration(
	ctx context.Context,
	orgID int64,
	deviceIdentifiers []string,
	sortConfig *interfaces.SortConfig,
	cfg *pb.MinerNameConfig,
) ([]interfaces.DeviceRenameProperties, error) {
	deviceProps, err := s.deviceStore.GetDevicePropertiesForRename(
		ctx,
		orgID,
		deviceIdentifiers,
		sortConfig != nil && sortConfig.IsTelemetrySort(),
	)
	if err != nil {
		return nil, err
	}

	if !nameConfigUsesRackDetails(cfg) {
		return deviceProps, nil
	}

	if err := s.enrichDevicePropsWithRackDetails(ctx, orgID, deviceProps); err != nil {
		return nil, err
	}
	return deviceProps, nil
}

func nameConfigUsesRackDetails(cfg *pb.MinerNameConfig) bool {
	if cfg == nil {
		return false
	}

	for _, prop := range cfg.Properties {
		qualifier, ok := prop.Kind.(*pb.NameProperty_Qualifier)
		if !ok || qualifier.Qualifier == nil {
			continue
		}

		switch qualifier.Qualifier.Type {
		case pb.QualifierType_QUALIFIER_TYPE_RACK,
			pb.QualifierType_QUALIFIER_TYPE_RACK_POSITION:
			return true
		case pb.QualifierType_QUALIFIER_TYPE_BUILDING,
			pb.QualifierType_QUALIFIER_TYPE_UNSPECIFIED:
			continue
		}
	}

	return false
}

func (s *Service) enrichDevicePropsWithRackDetails(
	ctx context.Context,
	orgID int64,
	deviceProps []interfaces.DeviceRenameProperties,
) error {
	if len(deviceProps) == 0 || s.collectionStore == nil {
		return nil
	}

	deviceIdentifiers := make([]string, 0, len(deviceProps))
	for _, props := range deviceProps {
		deviceIdentifiers = append(deviceIdentifiers, props.DeviceIdentifier)
	}

	rackDetails, err := s.collectionStore.GetRackDetailsForDevices(ctx, orgID, deviceIdentifiers)
	if err != nil {
		return fleeterror.NewInternalErrorf("failed to get rack details for name generation: %v", err)
	}

	for idx := range deviceProps {
		if details, ok := rackDetails[deviceProps[idx].DeviceIdentifier]; ok {
			deviceProps[idx].RackLabel = details.Label
			deviceProps[idx].RackPosition = details.Position
		}
	}

	return nil
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
	switch sortConfig.Field { //nolint:exhaustive // remaining fields use the shared stable-name fallback below
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
	case interfaces.SortFieldWorkerName:
		return lessNullableString(
			nullableTrimmedString(left.WorkerName),
			nullableTrimmedString(right.WorkerName),
			sortConfig.Direction,
			left.DiscoveredDeviceID,
			right.DiscoveredDeviceID,
		)
	default:
		// Collection-only sort fields fall back to the stable name-based ordering below.
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
	switch field { //nolint:exhaustive // remaining fields use the stable-name fallback below
	case interfaces.SortFieldIPAddress:
		return compareIPAddresses(left.IPAddress, right.IPAddress)
	case interfaces.SortFieldMACAddress:
		return strings.Compare(left.MacAddress, right.MacAddress)
	case interfaces.SortFieldModel:
		return compareNullableString(left.ModelSortValue, right.ModelSortValue)
	case interfaces.SortFieldWorkerName:
		return compareNullableString(nullableTrimmedString(left.WorkerName), nullableTrimmedString(right.WorkerName))
	default:
		// Telemetry and collection-only sorts are handled before this helper or
		// share the same stable name-based fallback.
		return strings.Compare(getRenameSortName(left), getRenameSortName(right))
	}
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

func nullableTrimmedString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
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

func isUnchangedWorkerName(nextName string, props interfaces.DeviceRenameProperties) bool {
	return strings.TrimSpace(nextName) == strings.TrimSpace(props.WorkerName)
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

	if err := validateSupportedFixedValueTypes(cfg); err != nil {
		return err
	}
	if err := validateSupportedQualifierTypes(cfg); err != nil {
		return err
	}

	switch cfg.Separator {
	case "-", "_", ".", "":
		return nil
	default:
		return fleeterror.NewInvalidArgumentError("name_config.separator must be one of '-', '_', '.', or empty")
	}
}

func validateSupportedFixedValueTypes(cfg *pb.MinerNameConfig) error {
	if cfg == nil {
		return nil
	}

	for _, prop := range cfg.Properties {
		fixedValue, ok := prop.Kind.(*pb.NameProperty_FixedValue)
		if !ok {
			continue
		}
		if fixedValue.FixedValue == nil {
			return fleeterror.NewInvalidArgumentError("name_config.properties.fixed_value is required")
		}

		switch fixedValue.FixedValue.Type {
		case pb.FixedValueType_FIXED_VALUE_TYPE_UNSPECIFIED,
			pb.FixedValueType_FIXED_VALUE_TYPE_MAC_ADDRESS,
			pb.FixedValueType_FIXED_VALUE_TYPE_SERIAL_NUMBER,
			pb.FixedValueType_FIXED_VALUE_TYPE_WORKER_NAME,
			pb.FixedValueType_FIXED_VALUE_TYPE_MODEL,
			pb.FixedValueType_FIXED_VALUE_TYPE_MANUFACTURER,
			pb.FixedValueType_FIXED_VALUE_TYPE_LOCATION,
			pb.FixedValueType_FIXED_VALUE_TYPE_MINER_NAME:
			continue
		default:
			return fleeterror.NewInvalidArgumentErrorf("unsupported fixed value type: %d", fixedValue.FixedValue.Type)
		}
	}

	return nil
}

func validateSupportedQualifierTypes(cfg *pb.MinerNameConfig) error {
	if cfg == nil {
		return nil
	}

	for _, prop := range cfg.Properties {
		qualifier, ok := prop.Kind.(*pb.NameProperty_Qualifier)
		if !ok {
			continue
		}
		if qualifier.Qualifier == nil {
			return fleeterror.NewInvalidArgumentError("name_config.properties.qualifier is required")
		}

		switch qualifier.Qualifier.Type {
		case pb.QualifierType_QUALIFIER_TYPE_RACK,
			pb.QualifierType_QUALIFIER_TYPE_RACK_POSITION:
			continue
		case pb.QualifierType_QUALIFIER_TYPE_UNSPECIFIED,
			pb.QualifierType_QUALIFIER_TYPE_BUILDING:
			return fleeterror.NewInvalidArgumentErrorf("unsupported qualifier type: %d", qualifier.Qualifier.Type)
		default:
			return fleeterror.NewInvalidArgumentErrorf("unsupported qualifier type: %d", qualifier.Qualifier.Type)
		}
	}

	return nil
}

func validateRequestWideGeneratedNames(cfg *pb.MinerNameConfig, deviceCount int) error {
	if cfg == nil || deviceCount == 0 || renameConfigDependsOnDeviceData(cfg) {
		return nil
	}

	_, err := generateName(cfg, interfaces.DeviceRenameProperties{}, deviceCount-1)
	return err
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
			pb.FixedValueType_FIXED_VALUE_TYPE_MINER_NAME,
			pb.FixedValueType_FIXED_VALUE_TYPE_MODEL,
			pb.FixedValueType_FIXED_VALUE_TYPE_MANUFACTURER:
			return true
		case pb.FixedValueType_FIXED_VALUE_TYPE_LOCATION,
			pb.FixedValueType_FIXED_VALUE_TYPE_UNSPECIFIED:
			continue
		}
	}

	for _, prop := range cfg.Properties {
		qualifier, ok := prop.Kind.(*pb.NameProperty_Qualifier)
		if !ok {
			continue
		}

		switch qualifier.Qualifier.GetType() {
		case pb.QualifierType_QUALIFIER_TYPE_RACK,
			pb.QualifierType_QUALIFIER_TYPE_RACK_POSITION:
			return true
		case pb.QualifierType_QUALIFIER_TYPE_BUILDING,
			pb.QualifierType_QUALIFIER_TYPE_UNSPECIFIED:
			continue
		}
	}

	return false
}

// generateName produces a single name string for a device according to the name config.
// counterIndex is the 0-based position of the device in the sorted device set.
func generateName(cfg *pb.MinerNameConfig, props interfaces.DeviceRenameProperties, counterIndex int) (string, error) {
	if cfg == nil {
		return "", fleeterror.NewInvalidArgumentError("name_config is required")
	}
	sep := cfg.Separator

	var segments []string
	for _, prop := range cfg.Properties {
		segment, err := generateSegment(prop, props, counterIndex)
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
func generateSegment(prop *pb.NameProperty, props interfaces.DeviceRenameProperties, counterIndex int) (string, error) {
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
		return generateFixedValueSegment(kind.FixedValue, props)

	case *pb.NameProperty_Qualifier:
		return generateQualifierSegment(kind.Qualifier, props), nil

	default:
		return "", nil
	}
}

// generateFixedValueSegment generates a segment from a device fixed attribute.
func generateFixedValueSegment(fv *pb.FixedValueProperty, props interfaces.DeviceRenameProperties) (string, error) {
	var raw string
	switch fv.Type {
	case pb.FixedValueType_FIXED_VALUE_TYPE_MAC_ADDRESS:
		raw = props.MacAddress
	case pb.FixedValueType_FIXED_VALUE_TYPE_SERIAL_NUMBER:
		raw = props.SerialNumber
	case pb.FixedValueType_FIXED_VALUE_TYPE_WORKER_NAME:
		raw = props.WorkerName
	case pb.FixedValueType_FIXED_VALUE_TYPE_MINER_NAME:
		raw = getRenameSortName(props)
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
		return "", fleeterror.NewInvalidArgumentErrorf("unsupported fixed value type: %d", fv.Type)
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

func generateQualifierSegment(qp *pb.QualifierProperty, props interfaces.DeviceRenameProperties) string {
	if qp == nil {
		return ""
	}

	var raw string
	switch qp.Type {
	case pb.QualifierType_QUALIFIER_TYPE_RACK:
		raw = props.RackLabel
	case pb.QualifierType_QUALIFIER_TYPE_RACK_POSITION:
		raw = props.RackPosition
	case pb.QualifierType_QUALIFIER_TYPE_BUILDING, pb.QualifierType_QUALIFIER_TYPE_UNSPECIFIED:
		return ""
	}

	if strings.TrimSpace(raw) == "" {
		return ""
	}

	return qp.Prefix + raw + qp.Suffix
}

// formatCounter formats an integer as a zero-padded string with the given number of digits.
func formatCounter(value, scale int) string {
	return fmt.Sprintf("%0*d", scale, value)
}
