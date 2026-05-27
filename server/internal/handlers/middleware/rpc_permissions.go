package middleware

import (
	"github.com/block/proto-fleet/server/generated/grpc/activity/v1/activityv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/apikey/v1/apikeyv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/auth/v1/authv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/buildings/v1/buildingsv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/collection/v1/collectionv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/curtailment/v1/curtailmentv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/device_set/v1/device_setv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/errors/v1/errorsv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1/fleetmanagementv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodeadmin/v1/fleetnodeadminv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/foremanimport/v1/foremanimportv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/minercommand/v1/minercommandv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/networkinfo/v1/networkinfov1connect"
	"github.com/block/proto-fleet/server/generated/grpc/onboarding/v1/onboardingv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/pairing/v1/pairingv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/pools/v1/poolsv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/schedule/v1/schedulev1connect"
	"github.com/block/proto-fleet/server/generated/grpc/serverlog/v1/serverlogv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/sites/v1/sitesv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/telemetry/v1/telemetryv1connect"
	"github.com/block/proto-fleet/server/internal/domain/authz"
)

// ProcedurePermissions maps gated Connect procedures to the catalog
// permission key their handler enforces via RequirePermission. The
// contract test in rpc_permissions_test.go enumerates every procedure
// registered on the production Connect server via reflection on the
// generated *ServiceHandler interfaces, and asserts each appears in
// exactly one of:
//
//   - interceptors.UnauthenticatedProcedures
//   - interceptors.FleetNodeAuthenticatedProcedures
//   - ProcedurePermissions          (gated by catalog key)
//   - ProceduresPendingMigration    (declared but not yet enforced via RequirePermission)
//
// Adding a new RPC without registering it fails the contract test
// loudly. Handlers move from ProceduresPendingMigration to
// ProcedurePermissions as they swap from RequireAdmin to
// RequirePermission.
//
// The two maps are split so the migration's progress is visible at a
// glance: shrinking ProceduresPendingMigration to zero is the exit
// criterion for retiring the legacy RequireAdmin middleware.
var ProcedurePermissions = map[string]string{
	// Populated as handler callsites swap from legacy gates to
	// RequirePermission. Empty entries are expected while the
	// migration is in flight; the contract test catches missing
	// classifications either way.
	curtailmentv1connect.CurtailmentServiceGetActiveCurtailmentProcedure: authz.PermCurtailmentRead,
}

// ProceduresPendingMigration lists authenticated Connect procedures that
// have not yet migrated to RequirePermission. The map value is a
// brief note about the procedure's current gate — the legacy
// RequireAdmin middleware, an inline role-string check, or (for
// command, fleetmanagement, deviceset) no gate at all.
//
// Adding entries to this map is a regression: every new RPC SHOULD
// declare its catalog key in ProcedurePermissions from the moment it
// ships. The contract test prevents new procedures from being added
// without classification, but it cannot tell the difference between
// "intentional pending entry" and "shipped without thinking about
// authz." Reviewers should treat any growth here as a red flag.
var ProceduresPendingMigration = map[string]string{
	// Activity log reads — currently authenticated but ungated.
	activityv1connect.ActivityServiceListActivitiesProcedure:            "ungated; read-only activity log",
	activityv1connect.ActivityServiceExportActivitiesProcedure:          "ungated; activity log CSV export",
	activityv1connect.ActivityServiceListActivityFilterOptionsProcedure: "ungated; filter option lookup",

	// API key management — local requireAdmin helper in apikey/handler.go.
	apikeyv1connect.ApiKeyServiceCreateApiKeyProcedure: "inline requireAdmin in apikey/handler.go",
	apikeyv1connect.ApiKeyServiceListApiKeysProcedure:  "inline requireAdmin in apikey/handler.go",
	apikeyv1connect.ApiKeyServiceRevokeApiKeyProcedure: "inline requireAdmin in apikey/handler.go",

	// Auth user management — checkCanManageUser in auth/service.go.
	authv1connect.AuthServiceCreateUserProcedure:        "domain-layer checkCanManageUser",
	authv1connect.AuthServiceDeactivateUserProcedure:    "domain-layer checkCanManageUser",
	authv1connect.AuthServiceResetUserPasswordProcedure: "domain-layer checkCanManageUser",
	authv1connect.AuthServiceListUsersProcedure:         "UNGATED: auth/service.go ListUsers has no role check; any authenticated org member can enumerate users — migration must add a real gate",
	authv1connect.AuthServiceGetUserAuditInfoProcedure:  "authenticated self-read, no role check",
	authv1connect.AuthServiceUpdatePasswordProcedure:    "authenticated self-write, no role check",
	authv1connect.AuthServiceUpdateUsernameProcedure:    "authenticated self-write, no role check",
	authv1connect.AuthServiceVerifyCredentialsProcedure: "authenticated self-read, no role check",
	authv1connect.AuthServiceLogoutProcedure:            "session-only; FailedPrecondition guard in handler",

	// Buildings — middleware.RequireAdmin in buildings/handler.go.
	buildingsv1connect.BuildingServiceListBuildingsProcedure:  "middleware.RequireAdmin",
	buildingsv1connect.BuildingServiceGetBuildingProcedure:    "middleware.RequireAdmin",
	buildingsv1connect.BuildingServiceCreateBuildingProcedure: "middleware.RequireAdmin",
	buildingsv1connect.BuildingServiceUpdateBuildingProcedure: "middleware.RequireAdmin",
	buildingsv1connect.BuildingServiceDeleteBuildingProcedure: "middleware.RequireAdmin",

	// DeviceCollectionService — ungated reads + writes on shared collections.
	collectionv1connect.DeviceCollectionServiceCreateCollectionProcedure:            "ungated",
	collectionv1connect.DeviceCollectionServiceGetCollectionProcedure:               "ungated",
	collectionv1connect.DeviceCollectionServiceGetCollectionStatsProcedure:          "ungated",
	collectionv1connect.DeviceCollectionServiceListCollectionsProcedure:             "ungated",
	collectionv1connect.DeviceCollectionServiceListCollectionMembersProcedure:       "ungated",
	collectionv1connect.DeviceCollectionServiceUpdateCollectionProcedure:            "ungated",
	collectionv1connect.DeviceCollectionServiceDeleteCollectionProcedure:            "ungated",
	collectionv1connect.DeviceCollectionServiceAddDevicesToCollectionProcedure:      "ungated",
	collectionv1connect.DeviceCollectionServiceRemoveDevicesFromCollectionProcedure: "ungated",
	collectionv1connect.DeviceCollectionServiceGetDeviceCollectionsProcedure:        "ungated",
	collectionv1connect.DeviceCollectionServiceListRackTypesProcedure:               "ungated",
	collectionv1connect.DeviceCollectionServiceListRackZonesProcedure:               "ungated",
	collectionv1connect.DeviceCollectionServiceSaveRackProcedure:                    "ungated",
	collectionv1connect.DeviceCollectionServiceGetRackSlotsProcedure:                "ungated",
	collectionv1connect.DeviceCollectionServiceSetRackSlotPositionProcedure:         "ungated",
	collectionv1connect.DeviceCollectionServiceClearRackSlotPositionProcedure:       "ungated",

	// CurtailmentService — gates are conditional or absent; migration must close the gaps.
	curtailmentv1connect.CurtailmentServiceStartCurtailmentProcedure:       "CONDITIONAL: requireAdminFromContext only when CandidateMinPowerWOverride set or AllowUnbounded; otherwise any authenticated user can start",
	curtailmentv1connect.CurtailmentServiceStopCurtailmentProcedure:        "CONDITIONAL: requireAdminFromContext only when force=true; non-force stop is ungated",
	curtailmentv1connect.CurtailmentServiceUpdateCurtailmentEventProcedure: "UNIMPLEMENTED STUB: returns Unimplemented with no gate; needs a real gate when implemented",
	curtailmentv1connect.CurtailmentServiceAdminTerminateEventProcedure:    "session-only + inline requireAdminFromContext",
	curtailmentv1connect.CurtailmentServiceListCurtailmentEventsProcedure:  "ungated read",
	curtailmentv1connect.CurtailmentServicePreviewCurtailmentPlanProcedure: "CONDITIONAL: requireAdminFromContext only when CandidateMinPowerWOverride set; otherwise ungated",

	// DeviceSetService (racks) — ungated.
	device_setv1connect.DeviceSetServiceCreateDeviceSetProcedure:            "ungated",
	device_setv1connect.DeviceSetServiceGetDeviceSetProcedure:               "ungated",
	device_setv1connect.DeviceSetServiceGetDeviceSetStatsProcedure:          "ungated",
	device_setv1connect.DeviceSetServiceListDeviceSetsProcedure:             "ungated",
	device_setv1connect.DeviceSetServiceListDeviceSetMembersProcedure:       "ungated",
	device_setv1connect.DeviceSetServiceUpdateDeviceSetProcedure:            "ungated",
	device_setv1connect.DeviceSetServiceDeleteDeviceSetProcedure:            "ungated",
	device_setv1connect.DeviceSetServiceAddDevicesToDeviceSetProcedure:      "ungated",
	device_setv1connect.DeviceSetServiceRemoveDevicesFromDeviceSetProcedure: "ungated",
	device_setv1connect.DeviceSetServiceGetDeviceDeviceSetsProcedure:        "ungated",
	device_setv1connect.DeviceSetServiceListRackTypesProcedure:              "ungated",
	device_setv1connect.DeviceSetServiceListRackZonesProcedure:              "ungated",
	device_setv1connect.DeviceSetServiceListRackZoneRefsProcedure:           "ungated",
	device_setv1connect.DeviceSetServiceSaveRackProcedure:                   "ungated",
	device_setv1connect.DeviceSetServiceGetRackSlotsProcedure:               "ungated",
	device_setv1connect.DeviceSetServiceSetRackSlotPositionProcedure:        "ungated",
	device_setv1connect.DeviceSetServiceClearRackSlotPositionProcedure:      "ungated",

	// ErrorQueryService — ungated diagnostics reads.
	errorsv1connect.ErrorQueryServiceGetErrorProcedure:        "ungated diagnostics read",
	errorsv1connect.ErrorQueryServiceQueryProcedure:           "ungated diagnostics read",
	errorsv1connect.ErrorQueryServiceListMinerErrorsProcedure: "ungated diagnostics read",
	errorsv1connect.ErrorQueryServiceWatchProcedure:           "ungated diagnostics stream",

	// FleetManagementService — ungated. This is the first service that gets a NEW gate.
	fleetmanagementv1connect.FleetManagementServiceListMinerStateSnapshotsProcedure: "ungated",
	fleetmanagementv1connect.FleetManagementServiceGetMinerPoolAssignmentsProcedure: "ungated",
	fleetmanagementv1connect.FleetManagementServiceGetMinerCoolingModeProcedure:     "ungated",
	fleetmanagementv1connect.FleetManagementServiceGetMinerStateCountsProcedure:     "ungated",
	fleetmanagementv1connect.FleetManagementServiceGetMinerModelGroupsProcedure:     "ungated",
	fleetmanagementv1connect.FleetManagementServiceUpdateWorkerNamesProcedure:       "ungated",
	fleetmanagementv1connect.FleetManagementServiceRenameMinersProcedure:            "ungated",
	fleetmanagementv1connect.FleetManagementServiceDeleteMinersProcedure:            "ungated",
	fleetmanagementv1connect.FleetManagementServiceExportMinerListCsvProcedure:      "ungated",

	// FleetNodeAdminService — only the first four overrides exist; the rest fall through
	// the embedded UnimplementedFleetNodeAdminServiceHandler and never reach a role check.
	fleetnodeadminv1connect.FleetNodeAdminServiceCreateEnrollmentCodeProcedure:  "inline requireAdminSession (info.Role check)",
	fleetnodeadminv1connect.FleetNodeAdminServiceListFleetNodesProcedure:        "inline requireAdminSession (info.Role check)",
	fleetnodeadminv1connect.FleetNodeAdminServiceConfirmFleetNodeProcedure:      "inline requireAdminSession (info.Role check)",
	fleetnodeadminv1connect.FleetNodeAdminServiceRevokeFleetNodeProcedure:       "inline requireAdminSession (info.Role check)",
	fleetnodeadminv1connect.FleetNodeAdminServicePairDeviceToFleetNodeProcedure: "UNIMPLEMENTED STUB: handler does not override, returns Unimplemented with no gate",
	fleetnodeadminv1connect.FleetNodeAdminServiceUnpairDeviceProcedure:          "UNIMPLEMENTED STUB: handler does not override, returns Unimplemented with no gate",
	fleetnodeadminv1connect.FleetNodeAdminServiceListFleetNodeDevicesProcedure:  "UNIMPLEMENTED STUB: handler does not override, returns Unimplemented with no gate",
	fleetnodeadminv1connect.FleetNodeAdminServiceDiscoverOnFleetNodeProcedure:   "UNIMPLEMENTED STUB: handler does not override, returns Unimplemented with no gate",

	// ForemanImportService — ungated.
	foremanimportv1connect.ForemanImportServiceImportFromForemanProcedure: "ungated",
	foremanimportv1connect.ForemanImportServiceCompleteImportProcedure:    "ungated",

	// MinerCommandService — ungated. The largest single block of new gates.
	minercommandv1connect.MinerCommandServiceBlinkLEDProcedure:                     "ungated",
	minercommandv1connect.MinerCommandServiceRebootProcedure:                       "ungated",
	minercommandv1connect.MinerCommandServiceStartMiningProcedure:                  "ungated",
	minercommandv1connect.MinerCommandServiceStopMiningProcedure:                   "ungated",
	minercommandv1connect.MinerCommandServiceUpdateMiningPoolsProcedure:            "ungated",
	minercommandv1connect.MinerCommandServiceSetCoolingModeProcedure:               "ungated",
	minercommandv1connect.MinerCommandServiceSetPowerTargetProcedure:               "ungated",
	minercommandv1connect.MinerCommandServiceFirmwareUpdateProcedure:               "ungated",
	minercommandv1connect.MinerCommandServiceDownloadLogsProcedure:                 "ungated",
	minercommandv1connect.MinerCommandServiceUpdateMinerPasswordProcedure:          "ungated",
	minercommandv1connect.MinerCommandServiceUnpairProcedure:                       "ungated",
	minercommandv1connect.MinerCommandServiceCheckCommandCapabilitiesProcedure:     "ungated",
	minercommandv1connect.MinerCommandServiceGetCommandBatchDeviceResultsProcedure: "ungated",
	minercommandv1connect.MinerCommandServiceGetCommandBatchLogBundleProcedure:     "ungated",
	minercommandv1connect.MinerCommandServiceStreamCommandBatchUpdatesProcedure:    "ungated",

	// NetworkInfoService — ungated.
	networkinfov1connect.NetworkInfoServiceGetNetworkInfoProcedure:        "ungated",
	networkinfov1connect.NetworkInfoServiceUpdateNetworkNicknameProcedure: "ungated",

	// OnboardingService — fleet-init status. Other onboarding procedures are unauthenticated.
	onboardingv1connect.OnboardingServiceGetFleetOnboardingStatusProcedure: "ungated authenticated read",

	// PairingService — ungated.
	pairingv1connect.PairingServiceDiscoverProcedure: "ungated",
	pairingv1connect.PairingServicePairProcedure:     "ungated",

	// PoolsService — ungated.
	poolsv1connect.PoolsServiceCreatePoolProcedure:   "ungated",
	poolsv1connect.PoolsServiceListPoolsProcedure:    "ungated",
	poolsv1connect.PoolsServiceUpdatePoolProcedure:   "ungated",
	poolsv1connect.PoolsServiceDeletePoolProcedure:   "ungated",
	poolsv1connect.PoolsServiceValidatePoolProcedure: "ungated",

	// ScheduleService — ungated.
	schedulev1connect.ScheduleServiceListSchedulesProcedure:    "ungated",
	schedulev1connect.ScheduleServiceCreateScheduleProcedure:   "ungated",
	schedulev1connect.ScheduleServiceUpdateScheduleProcedure:   "ungated",
	schedulev1connect.ScheduleServiceDeleteScheduleProcedure:   "ungated",
	schedulev1connect.ScheduleServicePauseScheduleProcedure:    "ungated",
	schedulev1connect.ScheduleServiceResumeScheduleProcedure:   "ungated",
	schedulev1connect.ScheduleServiceReorderSchedulesProcedure: "ungated",

	// ServerLogService — inline role check.
	serverlogv1connect.ServerLogServiceListServerLogsProcedure: "inline info.Role check",

	// SiteService — middleware.RequireAdmin.
	sitesv1connect.SiteServiceListSitesProcedure:             "middleware.RequireAdmin",
	sitesv1connect.SiteServiceCreateSiteProcedure:            "middleware.RequireAdmin",
	sitesv1connect.SiteServiceUpdateSiteProcedure:            "middleware.RequireAdmin",
	sitesv1connect.SiteServiceDeleteSiteProcedure:            "middleware.RequireAdmin",
	sitesv1connect.SiteServiceReassignDevicesToSiteProcedure: "middleware.RequireAdmin",
	sitesv1connect.SiteServiceAssignBuildingToSiteProcedure:  "middleware.RequireAdmin",

	// TelemetryService — ungated.
	telemetryv1connect.TelemetryServiceGetCombinedMetricsProcedure:          "ungated",
	telemetryv1connect.TelemetryServiceStreamCombinedMetricUpdatesProcedure: "ungated",
}
