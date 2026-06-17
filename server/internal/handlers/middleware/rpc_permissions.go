package middleware

import (
	"github.com/block/proto-fleet/server/generated/grpc/activity/v1/activityv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/apikey/v1/apikeyv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/auth/v1/authv1connect"
	"github.com/block/proto-fleet/server/generated/grpc/authz/v1/authzv1connect"
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
	"github.com/block/proto-fleet/server/generated/grpc/notifications/v1/notificationsv1connect"
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
	// Activity log — read-only audit trail. Export is the CSV variant of
	// the same query; filter options is the lookup endpoint that drives
	// the UI's filter panel. All three sit on activity:read.
	activityv1connect.ActivityServiceListActivitiesProcedure:            authz.PermActivityRead,
	activityv1connect.ActivityServiceExportActivitiesProcedure:          authz.PermActivityRead,
	activityv1connect.ActivityServiceListActivityFilterOptionsProcedure: authz.PermActivityRead,

	// API key management — gated by RequirePermission(PermAPIKeyManage).
	apikeyv1connect.ApiKeyServiceCreateApiKeyProcedure: authz.PermAPIKeyManage,
	apikeyv1connect.ApiKeyServiceListApiKeysProcedure:  authz.PermAPIKeyManage,
	apikeyv1connect.ApiKeyServiceRevokeApiKeyProcedure: authz.PermAPIKeyManage,

	// AuthzService — role management. Mutations gate on role:manage.
	// ListPermissions returns the catalog (no PII / no org data) but is
	// gated all the same because the catalog picker is only useful inside
	// the role editor, which itself requires role:manage; widening this
	// surface adds no UX and gives away less than zero.
	//
	// ListRoles is the exception: its handler calls
	// middleware.RequireAnyPermission with [role:manage, user:manage] so
	// the AddTeamMember modal works for the built-in ADMIN role (which
	// holds user:manage but intentionally lacks role:manage). The map
	// value below records the primary gate for classification; the
	// secondary alternate is enforced in the handler.
	authzv1connect.AuthzServiceListPermissionsProcedure:  authz.PermRoleManage,
	authzv1connect.AuthzServiceListRolesProcedure:        authz.PermRoleManage,
	authzv1connect.AuthzServiceCreateCustomRoleProcedure: authz.PermRoleManage,
	authzv1connect.AuthzServiceUpdateCustomRoleProcedure: authz.PermRoleManage,
	authzv1connect.AuthzServiceDeleteCustomRoleProcedure: authz.PermRoleManage,

	// Auth user management — gated at the handler layer via
	// RequirePermission. ListUsers previously had no role check at all;
	// it is now gated by PermUserRead (ADMIN + SUPER_ADMIN). Mutations
	// require PermUserManage; the auth domain layer additionally enforces
	// a role-hierarchy check so an ADMIN cannot create, reset, or
	// deactivate an elevated (ADMIN/SUPER_ADMIN) target.
	authv1connect.AuthServiceCreateUserProcedure:        authz.PermUserManage,
	authv1connect.AuthServiceDeactivateUserProcedure:    authz.PermUserManage,
	authv1connect.AuthServiceResetUserPasswordProcedure: authz.PermUserManage,
	authv1connect.AuthServiceUpdateUserRoleProcedure:    authz.PermUserManage,
	authv1connect.AuthServiceListUsersProcedure:         authz.PermUserRead,

	// Buildings CRUD — site:read for reads, site:manage for writes.
	// ListBuildingRacks is a building-scoped read; AssignRacksToBuilding
	// mutates the rack's building/site/zone/grid placement.
	buildingsv1connect.BuildingServiceListBuildingsProcedure:         authz.PermSiteRead,
	buildingsv1connect.BuildingServiceGetBuildingProcedure:           authz.PermSiteRead,
	buildingsv1connect.BuildingServiceListBuildingRacksProcedure:     authz.PermSiteRead,
	buildingsv1connect.BuildingServiceCreateBuildingProcedure:        authz.PermSiteManage,
	buildingsv1connect.BuildingServiceUpdateBuildingProcedure:        authz.PermSiteManage,
	buildingsv1connect.BuildingServiceDeleteBuildingProcedure:        authz.PermSiteManage,
	buildingsv1connect.BuildingServiceAssignRacksToBuildingProcedure: authz.PermSiteManage,
	// GetBuildingStats also calls RequirePermission(PermFleetRead) and
	// RequirePermission(PermMinerRead) inline — those gate the telemetry
	// rollup and the device_identifiers surface respectively. The map
	// entry is the primary gate (site:read = "can see this building").
	buildingsv1connect.BuildingServiceGetBuildingStatsProcedure: authz.PermSiteRead,

	// CurtailmentService — reads use curtailment:read; user-facing preview
	// and mutation flows use curtailment:manage; MQTT source settings reads are
	// also manage-gated because broker/user config and runtime health are
	// sensitive operational controls. External signal ingest uses
	// curtailment:ingest.
	curtailmentv1connect.CurtailmentServiceListCurtailmentEventsProcedure:               authz.PermCurtailmentRead,
	curtailmentv1connect.CurtailmentServiceGetCurtailmentEventProcedure:                 authz.PermCurtailmentRead,
	curtailmentv1connect.CurtailmentServiceListActiveCurtailmentsProcedure:              authz.PermCurtailmentRead,
	curtailmentv1connect.CurtailmentServiceGetCurtailmentSettingsProcedure:              authz.PermCurtailmentManage,
	curtailmentv1connect.CurtailmentServiceUpdateCurtailmentSettingsProcedure:           authz.PermCurtailmentManage,
	curtailmentv1connect.CurtailmentServicePreviewCurtailmentPlanProcedure:              authz.PermCurtailmentManage,
	curtailmentv1connect.CurtailmentServiceStartCurtailmentProcedure:                    authz.PermCurtailmentManage,
	curtailmentv1connect.CurtailmentServiceStopCurtailmentProcedure:                     authz.PermCurtailmentManage,
	curtailmentv1connect.CurtailmentServiceUpdateCurtailmentEventProcedure:              authz.PermCurtailmentManage,
	curtailmentv1connect.CurtailmentServiceAdminTerminateEventProcedure:                 authz.PermCurtailmentManage,
	curtailmentv1connect.CurtailmentServiceListMqttCurtailmentSourcesProcedure:          authz.PermCurtailmentManage,
	curtailmentv1connect.CurtailmentServiceGetMqttCurtailmentSourceProcedure:            authz.PermCurtailmentManage,
	curtailmentv1connect.CurtailmentServiceCreateMqttCurtailmentSourceProcedure:         authz.PermCurtailmentManage,
	curtailmentv1connect.CurtailmentServiceUpdateMqttCurtailmentSourceProcedure:         authz.PermCurtailmentManage,
	curtailmentv1connect.CurtailmentServiceTestMqttCurtailmentSourceConnectionProcedure: authz.PermCurtailmentManage,
	curtailmentv1connect.CurtailmentServiceSetMqttCurtailmentSourceEnabledProcedure:     authz.PermCurtailmentManage,
	curtailmentv1connect.CurtailmentServiceDeleteMqttCurtailmentSourceProcedure:         authz.PermCurtailmentManage,
	curtailmentv1connect.CurtailmentServiceListCurtailmentResponseProfilesProcedure:     authz.PermCurtailmentManage,
	curtailmentv1connect.CurtailmentServiceGetCurtailmentResponseProfileProcedure:       authz.PermCurtailmentManage,
	curtailmentv1connect.CurtailmentServiceCreateCurtailmentResponseProfileProcedure:    authz.PermCurtailmentManage,
	curtailmentv1connect.CurtailmentServiceUpdateCurtailmentResponseProfileProcedure:    authz.PermCurtailmentManage,
	curtailmentv1connect.CurtailmentServiceDeleteCurtailmentResponseProfileProcedure:    authz.PermCurtailmentManage,
	curtailmentv1connect.CurtailmentServiceListCurtailmentAutomationRulesProcedure:      authz.PermCurtailmentManage,
	curtailmentv1connect.CurtailmentServiceGetCurtailmentAutomationRuleProcedure:        authz.PermCurtailmentManage,
	curtailmentv1connect.CurtailmentServiceCreateCurtailmentAutomationRuleProcedure:     authz.PermCurtailmentManage,
	curtailmentv1connect.CurtailmentServiceUpdateCurtailmentAutomationRuleProcedure:     authz.PermCurtailmentManage,
	curtailmentv1connect.CurtailmentServiceSetCurtailmentAutomationRuleEnabledProcedure: authz.PermCurtailmentManage,
	curtailmentv1connect.CurtailmentServiceDeleteCurtailmentAutomationRuleProcedure:     authz.PermCurtailmentManage,
	curtailmentv1connect.CurtailmentServiceIngestCurtailmentSignalProcedure:             authz.PermCurtailmentIngest,

	// DeviceCollectionService — rack:read for reads, rack:manage for writes.
	// Collections are the legacy name for racks; the wire surface still
	// carries Collection-prefixed names while the domain has been
	// renamed.
	collectionv1connect.DeviceCollectionServiceGetCollectionProcedure:         authz.PermRackRead,
	collectionv1connect.DeviceCollectionServiceGetCollectionStatsProcedure:    authz.PermRackRead,
	collectionv1connect.DeviceCollectionServiceListCollectionsProcedure:       authz.PermRackRead,
	collectionv1connect.DeviceCollectionServiceListCollectionMembersProcedure: authz.PermRackRead,
	collectionv1connect.DeviceCollectionServiceGetDeviceCollectionsProcedure:  authz.PermRackRead,
	collectionv1connect.DeviceCollectionServiceListRackTypesProcedure:         authz.PermRackRead,
	collectionv1connect.DeviceCollectionServiceListRackZonesProcedure:         authz.PermRackRead,
	collectionv1connect.DeviceCollectionServiceGetRackSlotsProcedure:          authz.PermRackRead,
	collectionv1connect.DeviceCollectionServiceCreateCollectionProcedure:      authz.PermRackManage,
	collectionv1connect.DeviceCollectionServiceUpdateCollectionProcedure:      authz.PermRackManage,
	collectionv1connect.DeviceCollectionServiceDeleteCollectionProcedure:      authz.PermRackManage,
	collectionv1connect.DeviceCollectionServiceSaveRackProcedure:              authz.PermRackManage,
	collectionv1connect.DeviceCollectionServiceSetRackSlotPositionProcedure:   authz.PermRackManage,
	collectionv1connect.DeviceCollectionServiceClearRackSlotPositionProcedure: authz.PermRackManage,

	// DeviceSetService (racks via the new wire surface) — same mapping
	// as DeviceCollectionService; the handler is a proto-adapter shim.
	device_setv1connect.DeviceSetServiceGetDeviceSetProcedure:           authz.PermRackRead,
	device_setv1connect.DeviceSetServiceGetDeviceSetStatsProcedure:      authz.PermRackRead,
	device_setv1connect.DeviceSetServiceListDeviceSetsProcedure:         authz.PermRackRead,
	device_setv1connect.DeviceSetServiceListDeviceSetMembersProcedure:   authz.PermRackRead,
	device_setv1connect.DeviceSetServiceGetDeviceDeviceSetsProcedure:    authz.PermRackRead,
	device_setv1connect.DeviceSetServiceListRackTypesProcedure:          authz.PermRackRead,
	device_setv1connect.DeviceSetServiceListRackZonesProcedure:          authz.PermRackRead,
	device_setv1connect.DeviceSetServiceListRackZoneRefsProcedure:       authz.PermRackRead,
	device_setv1connect.DeviceSetServiceGetRackSlotsProcedure:           authz.PermRackRead,
	device_setv1connect.DeviceSetServiceCreateDeviceSetProcedure:        authz.PermRackManage,
	device_setv1connect.DeviceSetServiceUpdateDeviceSetProcedure:        authz.PermRackManage,
	device_setv1connect.DeviceSetServiceDeleteDeviceSetProcedure:        authz.PermRackManage,
	device_setv1connect.DeviceSetServiceAddDevicesToGroupProcedure:      authz.PermRackManage,
	device_setv1connect.DeviceSetServiceRemoveDevicesFromGroupProcedure: authz.PermRackManage,
	device_setv1connect.DeviceSetServiceSaveRackProcedure:               authz.PermRackManage,
	device_setv1connect.DeviceSetServiceAssignDevicesToRackProcedure:    authz.PermRackManage,
	device_setv1connect.DeviceSetServiceSetRackSlotPositionProcedure:    authz.PermRackManage,
	device_setv1connect.DeviceSetServiceClearRackSlotPositionProcedure:  authz.PermRackManage,

	// ErrorQueryService — fleet:read; diagnostics are scoped to the org
	// and live alongside the fleet dashboard.
	errorsv1connect.ErrorQueryServiceGetErrorProcedure:        authz.PermFleetRead,
	errorsv1connect.ErrorQueryServiceQueryProcedure:           authz.PermFleetRead,
	errorsv1connect.ErrorQueryServiceListMinerErrorsProcedure: authz.PermFleetRead,
	errorsv1connect.ErrorQueryServiceWatchProcedure:           authz.PermFleetRead,

	// FleetManagementService — list/read against fleet/miner reads,
	// mutations against matching miner action keys.
	fleetmanagementv1connect.FleetManagementServiceListMinerStateSnapshotsProcedure: authz.PermMinerRead,
	fleetmanagementv1connect.FleetManagementServiceRefreshMinersProcedure:           authz.PermMinerRead,
	fleetmanagementv1connect.FleetManagementServiceGetMinerPoolAssignmentsProcedure: authz.PermMinerRead,
	fleetmanagementv1connect.FleetManagementServiceGetMinerCoolingModeProcedure:     authz.PermMinerRead,
	fleetmanagementv1connect.FleetManagementServiceGetMinerStateCountsProcedure:     authz.PermFleetRead,
	fleetmanagementv1connect.FleetManagementServiceGetMinerModelGroupsProcedure:     authz.PermFleetRead,
	fleetmanagementv1connect.FleetManagementServiceUpdateWorkerNamesProcedure:       authz.PermMinerUpdateWorkerName,
	fleetmanagementv1connect.FleetManagementServiceRenameMinersProcedure:            authz.PermMinerRename,
	fleetmanagementv1connect.FleetManagementServiceDeleteMinersProcedure:            authz.PermMinerDelete,
	fleetmanagementv1connect.FleetManagementServiceExportMinerListCsvProcedure:      authz.PermMinerExportCSV,

	// FleetNodeAdminService — fully migrated. Read for the list endpoints,
	// manage for everything that mutates fleet-node state or triggers a scan.
	fleetnodeadminv1connect.FleetNodeAdminServiceCreateEnrollmentCodeProcedure:           authz.PermFleetnodeManage,
	fleetnodeadminv1connect.FleetNodeAdminServiceListFleetNodesProcedure:                 authz.PermFleetnodeRead,
	fleetnodeadminv1connect.FleetNodeAdminServiceConfirmFleetNodeProcedure:               authz.PermFleetnodeManage,
	fleetnodeadminv1connect.FleetNodeAdminServiceRevokeFleetNodeProcedure:                authz.PermFleetnodeManage,
	fleetnodeadminv1connect.FleetNodeAdminServicePairDeviceToFleetNodeProcedure:          authz.PermFleetnodeManage,
	fleetnodeadminv1connect.FleetNodeAdminServiceUnpairDeviceProcedure:                   authz.PermFleetnodeManage,
	fleetnodeadminv1connect.FleetNodeAdminServiceListFleetNodeDevicesProcedure:           authz.PermFleetnodeRead,
	fleetnodeadminv1connect.FleetNodeAdminServiceDiscoverOnFleetNodeProcedure:            authz.PermFleetnodeManage,
	fleetnodeadminv1connect.FleetNodeAdminServiceListFleetNodeDiscoveredDevicesProcedure: authz.PermFleetnodeRead,
	// Pairing miners requires both miner:pair (primary gate, matches all other
	// miner onboarding paths) and fleetnode:manage (inline handler check below).
	fleetnodeadminv1connect.FleetNodeAdminServicePairDiscoveredDevicesOnFleetNodeProcedure: authz.PermMinerPair,

	// ForemanImportService — bulk miner import flow. Gated on
	// miner:pair, the same key as the per-miner pairing endpoints —
	// Foreman import is "pair N miners we found out-of-band."
	foremanimportv1connect.ForemanImportServiceImportFromForemanProcedure: authz.PermMinerPair,
	foremanimportv1connect.ForemanImportServiceCompleteImportProcedure:    authz.PermMinerPair,

	// MinerCommandService — each action gates on its matching catalog
	// key. Stream/batch endpoints gate on fleet:read since they're
	// status surfaces.
	minercommandv1connect.MinerCommandServiceBlinkLEDProcedure:          authz.PermMinerBlinkLED,
	minercommandv1connect.MinerCommandServiceRebootProcedure:            authz.PermMinerReboot,
	minercommandv1connect.MinerCommandServiceStartMiningProcedure:       authz.PermMinerStartMining,
	minercommandv1connect.MinerCommandServiceStopMiningProcedure:        authz.PermMinerStopMining,
	minercommandv1connect.MinerCommandServiceUpdateMiningPoolsProcedure: authz.PermMinerUpdatePools,
	minercommandv1connect.MinerCommandServiceSetCoolingModeProcedure:    authz.PermMinerSetCoolingMode,
	minercommandv1connect.MinerCommandServiceSetPowerTargetProcedure:    authz.PermMinerSetPowerTarget,
	// Primary procedure gate remains miner:firmware_update; the handler
	// also requires miner:reboot because successful installs now reboot.
	minercommandv1connect.MinerCommandServiceFirmwareUpdateProcedure:               authz.PermMinerFirmwareUpdate,
	minercommandv1connect.MinerCommandServiceDownloadLogsProcedure:                 authz.PermMinerDownloadLogs,
	minercommandv1connect.MinerCommandServiceUpdateMinerPasswordProcedure:          authz.PermMinerUpdatePassword,
	minercommandv1connect.MinerCommandServiceUnpairProcedure:                       authz.PermMinerUnpair,
	minercommandv1connect.MinerCommandServiceCheckCommandCapabilitiesProcedure:     authz.PermMinerRead,
	minercommandv1connect.MinerCommandServiceGetCommandBatchDeviceResultsProcedure: authz.PermFleetRead,
	minercommandv1connect.MinerCommandServiceGetCommandBatchLogBundleProcedure:     authz.PermMinerDownloadLogs,
	minercommandv1connect.MinerCommandServiceStreamCommandBatchUpdatesProcedure:    authz.PermFleetRead,

	// NetworkInfoService — GetNetworkInfo returns the device's own
	// IP/gateway/subnet (shown on Settings → General to every user with
	// fleet:read, including FIELD_TECH). UpdateNetworkNickname mutates the
	// site's network identity and stays on site:manage.
	networkinfov1connect.NetworkInfoServiceGetNetworkInfoProcedure:        authz.PermFleetRead,
	networkinfov1connect.NetworkInfoServiceUpdateNetworkNicknameProcedure: authz.PermSiteManage,

	notificationsv1connect.ChannelServiceListChannelsProcedure:                      authz.PermNotificationRead,
	notificationsv1connect.ChannelServiceCreateChannelProcedure:                     authz.PermNotificationManage,
	notificationsv1connect.ChannelServiceUpdateChannelProcedure:                     authz.PermNotificationManage,
	notificationsv1connect.ChannelServiceDeleteChannelProcedure:                     authz.PermNotificationManage,
	notificationsv1connect.ChannelServiceTestChannelProcedure:                       authz.PermNotificationManage,
	notificationsv1connect.RuleServiceListRulesProcedure:                            authz.PermNotificationRead,
	notificationsv1connect.RuleServicePauseRuleProcedure:                            authz.PermNotificationManage,
	notificationsv1connect.RuleServiceResumeRuleProcedure:                           authz.PermNotificationManage,
	notificationsv1connect.MaintenanceWindowServiceListMaintenanceWindowsProcedure:  authz.PermNotificationRead,
	notificationsv1connect.MaintenanceWindowServiceCreateMaintenanceWindowProcedure: authz.PermNotificationManage,
	notificationsv1connect.MaintenanceWindowServiceUpdateMaintenanceWindowProcedure: authz.PermNotificationManage,
	notificationsv1connect.MaintenanceWindowServiceDeleteMaintenanceWindowProcedure: authz.PermNotificationManage,
	notificationsv1connect.HistoryServiceListNotificationsProcedure:                 authz.PermNotificationRead,

	// OnboardingService — fleet-init status. Other onboarding procedures
	// are unauthenticated (covered by UnauthenticatedProcedures).
	onboardingv1connect.OnboardingServiceGetFleetOnboardingStatusProcedure: authz.PermFleetRead,

	// PairingService — discovery + pair.
	pairingv1connect.PairingServiceDiscoverProcedure: authz.PermMinerPair,
	pairingv1connect.PairingServicePairProcedure:     authz.PermMinerPair,

	// PoolsService — saved mining pool definitions. ValidatePool drives
	// an outbound Stratum/SV2 handshake against the caller-supplied
	// URL, so it sits on the manage key alongside the mutations to
	// prevent a read-only role from triggering server-side network
	// probes.
	poolsv1connect.PoolsServiceListPoolsProcedure:    authz.PermPoolRead,
	poolsv1connect.PoolsServiceValidatePoolProcedure: authz.PermPoolManage,
	poolsv1connect.PoolsServiceCreatePoolProcedure:   authz.PermPoolManage,
	poolsv1connect.PoolsServiceUpdatePoolProcedure:   authz.PermPoolManage,
	poolsv1connect.PoolsServiceDeletePoolProcedure:   authz.PermPoolManage,

	// ScheduleService — recurring miner actions. CreateSchedule and
	// UpdateSchedule additionally re-check the underlying miner action
	// permission (reboot / stop_mining / set_power_target) inside the
	// handler so a schedule:manage holder cannot smuggle a privileged
	// action through the scheduler.
	schedulev1connect.ScheduleServiceListSchedulesProcedure:    authz.PermScheduleRead,
	schedulev1connect.ScheduleServiceCreateScheduleProcedure:   authz.PermScheduleManage,
	schedulev1connect.ScheduleServiceUpdateScheduleProcedure:   authz.PermScheduleManage,
	schedulev1connect.ScheduleServiceDeleteScheduleProcedure:   authz.PermScheduleManage,
	schedulev1connect.ScheduleServicePauseScheduleProcedure:    authz.PermScheduleManage,
	schedulev1connect.ScheduleServiceResumeScheduleProcedure:   authz.PermScheduleManage,
	schedulev1connect.ScheduleServiceReorderSchedulesProcedure: authz.PermScheduleManage,

	// ServerLogService — gated by PermServerlogRead.
	serverlogv1connect.ServerLogServiceListServerLogsProcedure: authz.PermServerlogRead,

	// Sites CRUD — site:read for List, site:manage for everything else.
	sitesv1connect.SiteServiceListSitesProcedure:             authz.PermSiteRead,
	sitesv1connect.SiteServiceCreateSiteProcedure:            authz.PermSiteManage,
	sitesv1connect.SiteServiceUpdateSiteProcedure:            authz.PermSiteManage,
	sitesv1connect.SiteServiceDeleteSiteProcedure:            authz.PermSiteManage,
	sitesv1connect.SiteServiceAssignDevicesToSiteProcedure:   authz.PermSiteManage,
	sitesv1connect.SiteServiceAssignBuildingsToSiteProcedure: authz.PermSiteManage,
	sitesv1connect.SiteServiceAssignRacksToSiteProcedure:     authz.PermSiteManage,
	// GetSiteStats also calls RequirePermission(PermFleetRead) inline to
	// cover the aggregate telemetry surface (matching the gate on
	// telemetry.GetCombinedMetrics). The map entry is the primary gate.
	sitesv1connect.SiteServiceGetSiteStatsProcedure: authz.PermSiteRead,

	// TelemetryService — fleet:read for combined-metrics surfaces.
	telemetryv1connect.TelemetryServiceGetCombinedMetricsProcedure:          authz.PermFleetRead,
	telemetryv1connect.TelemetryServiceStreamCombinedMetricUpdatesProcedure: authz.PermFleetRead,
}

// ProceduresPendingMigration lists authenticated Connect procedures that
// have not yet migrated to RequirePermission. The map value is a
// brief note about the procedure's current gate — the legacy
// RequireAdmin middleware, an inline role-string check, or no gate
// at all.
//
// Adding entries to this map is a regression: every new RPC SHOULD
// declare its catalog key in ProcedurePermissions from the moment it
// ships. The contract test prevents new procedures from being added
// without classification, but it cannot tell the difference between
// "intentional pending entry" and "shipped without thinking about
// authz." Reviewers should treat any growth here as a red flag.
var ProceduresPendingMigration = map[string]string{
	// Auth self-service and session procedures — caller acts on own
	// session/identity, no separate role check needed.
	authv1connect.AuthServiceGetUserAuditInfoProcedure:  "authenticated self-read, no role check",
	authv1connect.AuthServiceUpdatePasswordProcedure:    "authenticated self-write, no role check",
	authv1connect.AuthServiceUpdateUsernameProcedure:    "authenticated self-write, no role check",
	authv1connect.AuthServiceVerifyCredentialsProcedure: "authenticated self-read, no role check",
	authv1connect.AuthServiceLogoutProcedure:            "session-only; FailedPrecondition guard in handler",

	// AuthzService — assignment trio is not implemented yet. The Roles
	// management surface (Settings → Roles) does not need these; they
	// land alongside the Team-page assignment flow in a follow-up. The
	// other five RPCs are gated in ProcedurePermissions above.
	authzv1connect.AuthzServiceAssignRoleProcedure:          "UNIMPLEMENTED: lands with Team-page assignment flow",
	authzv1connect.AuthzServiceUnassignRoleProcedure:        "UNIMPLEMENTED: lands with Team-page assignment flow",
	authzv1connect.AuthzServiceListUserAssignmentsProcedure: "UNIMPLEMENTED: lands with Team-page assignment flow",
}
