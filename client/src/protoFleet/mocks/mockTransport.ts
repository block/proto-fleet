import { create } from "@bufbuild/protobuf";
import { createRouterTransport } from "@connectrpc/connect";

import { ActivityService } from "@/protoFleet/api/generated/activity/v1/activity_pb";
import { AuthService, UserAuditInfoSchema, UserInfoSchema } from "@/protoFleet/api/generated/auth/v1/auth_pb";
import { BuildingService, GetBuildingStatsResponseSchema } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { CurtailmentService } from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import { DeviceSetService } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import { ErrorQueryService } from "@/protoFleet/api/generated/errors/v1/errors_pb";
import { FleetManagementService } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { NetworkInfoService } from "@/protoFleet/api/generated/networkinfo/v1/networkinfo_pb";
import {
  FleetInitStatusSchema,
  FleetOnboardingStatusSchema,
  OnboardingService,
} from "@/protoFleet/api/generated/onboarding/v1/onboarding_pb";
import { PoolsService } from "@/protoFleet/api/generated/pools/v1/pools_pb";
import { ScheduleService } from "@/protoFleet/api/generated/schedule/v1/schedule_pb";
import { ServerLogService } from "@/protoFleet/api/generated/serverlog/v1/serverlog_pb";
import { GetSiteStatsResponseSchema, SiteService } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { TelemetryService } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import {
  mockAllBuildings,
  mockBuildingRacks,
  mockBuildingsBySite,
  mockBuildingStats,
  mockCombinedMetrics,
  mockDeviceSetStats,
  mockGetBuilding,
  mockListMiners,
  mockListRacks,
  mockListRackZones,
  mockMinerModelGroups,
  mockMinerStateCounts,
  mockSites,
  mockSiteStats,
} from "@/protoFleet/mocks/fixtures";

export const mockTransport = createRouterTransport((router) => {
  // -----------------------------------------------------------------------
  // Onboarding — health check + onboarding status
  // -----------------------------------------------------------------------
  router.service(OnboardingService, {
    getFleetInitStatus() {
      return { status: create(FleetInitStatusSchema, { adminCreated: true }) };
    },
    getFleetOnboardingStatus() {
      return {
        status: create(FleetOnboardingStatusSchema, {
          poolConfigured: true,
          devicePaired: true,
        }),
      };
    },
  });

  // -----------------------------------------------------------------------
  // Auth
  // -----------------------------------------------------------------------
  router.service(AuthService, {
    authenticate() {
      return {
        userInfo: create(UserInfoSchema, {
          userId: "mock-user-1",
          username: "demo",
          role: "SUPER_ADMIN",
          permissions: [
            "activity:read",
            "apikey:manage",
            "building:manage",
            "building:read",
            "curtailment:manage",
            "curtailment:read",
            "device:manage",
            "device:read",
            "miner:manage",
            "miner:read",
            "pool:manage",
            "pool:read",
            "rack:manage",
            "rack:read",
            "role:manage",
            "schedule:manage",
            "serverlog:read",
            "site:manage",
            "site:read",
            "user:read",
          ],
        }),
        sessionExpiry: BigInt(Math.floor(Date.now() / 1000) + 86400),
      };
    },
    getUserAuditInfo() {
      return { info: create(UserAuditInfoSchema, {}) };
    },
    verifyCredentials() {
      return {};
    },
    logout() {
      return {};
    },
  });

  // -----------------------------------------------------------------------
  // Sites
  // -----------------------------------------------------------------------
  router.service(SiteService, {
    listSites() {
      return { sites: mockSites };
    },
    getSiteStats(req) {
      const stats = mockSiteStats(req.siteId);
      if (!stats) return create(GetSiteStatsResponseSchema, {});
      return create(GetSiteStatsResponseSchema, stats);
    },
  });

  // -----------------------------------------------------------------------
  // Buildings
  // -----------------------------------------------------------------------
  router.service(BuildingService, {
    listBuildings(req) {
      if (req.siteFilter.case === "siteId") {
        return { buildings: mockBuildingsBySite.get(req.siteFilter.value) ?? [] };
      }
      return { buildings: mockAllBuildings };
    },
    getBuilding(req) {
      return { building: mockGetBuilding(req.id) };
    },
    listBuildingRacks(req) {
      return { racks: mockBuildingRacks(req.buildingId), nextPageToken: "" };
    },
    getBuildingStats(req) {
      const stats = mockBuildingStats(req.buildingId);
      if (!stats) return create(GetBuildingStatsResponseSchema, {});
      return create(GetBuildingStatsResponseSchema, stats);
    },
  });

  // -----------------------------------------------------------------------
  // Fleet management — miners, racks
  // -----------------------------------------------------------------------
  router.service(FleetManagementService, {
    listMinerStateSnapshots(req) {
      return mockListMiners(req.pageSize, req.cursor);
    },
    getMinerStateCounts() {
      return mockMinerStateCounts();
    },
    getMinerCoolingMode() {
      return {};
    },
    getMinerModelGroups() {
      return { groups: mockMinerModelGroups() };
    },
    getMinerPoolAssignments() {
      return { pools: [] };
    },
  });

  // -----------------------------------------------------------------------
  // Activity
  // -----------------------------------------------------------------------
  router.service(ActivityService, {
    listActivities() {
      return { activities: [], nextPageToken: "", totalCount: 0 };
    },
    listActivityFilterOptions() {
      return {};
    },
  });

  // -----------------------------------------------------------------------
  // Curtailment / Energy
  // -----------------------------------------------------------------------
  router.service(CurtailmentService, {
    listActiveCurtailments() {
      return { events: [] };
    },
    listCurtailmentEvents() {
      return { events: [], nextPageToken: "" };
    },
    listMqttCurtailmentSources() {
      return { sources: [] };
    },
    listCurtailmentAutomationRules() {
      return { rules: [] };
    },
    listCurtailmentResponseProfiles() {
      return { profiles: [] };
    },
  });

  // -----------------------------------------------------------------------
  // Telemetry
  // -----------------------------------------------------------------------
  router.service(TelemetryService, {
    getCombinedMetrics(req) {
      const startSecs = Number(req.startTime?.seconds ?? 0n);
      const endSecs = Number(req.endTime?.seconds ?? BigInt(Math.floor(Date.now() / 1000)));
      const durationMinutes = Math.max(60, Math.floor((endSecs - startSecs) / 60));
      return mockCombinedMetrics(req.measurementTypes, req.aggregations, durationMinutes);
    },
  });

  // -----------------------------------------------------------------------
  // Device sets
  // -----------------------------------------------------------------------
  router.service(DeviceSetService, {
    listDeviceSets(req) {
      return mockListRacks(req.pageSize, req.pageToken, req.buildingIds);
    },
    getDeviceSetStats(req) {
      return { stats: mockDeviceSetStats(req.deviceSetIds) };
    },
    listRackZones() {
      return { zones: mockListRackZones() };
    },
    listRackTypes() {
      return { rackTypes: [] };
    },
  });

  // -----------------------------------------------------------------------
  // Pools
  // -----------------------------------------------------------------------
  router.service(PoolsService, {
    listPools() {
      return { pools: [] };
    },
  });

  // -----------------------------------------------------------------------
  // Schedules
  // -----------------------------------------------------------------------
  router.service(ScheduleService, {
    listSchedules() {
      return { schedules: [] };
    },
  });

  // -----------------------------------------------------------------------
  // Error diagnostics
  // -----------------------------------------------------------------------
  router.service(ErrorQueryService, {
    query() {
      return {};
    },
  });

  // -----------------------------------------------------------------------
  // Network info
  // -----------------------------------------------------------------------
  router.service(NetworkInfoService, {
    getNetworkInfo() {
      return {};
    },
  });

  // -----------------------------------------------------------------------
  // Server logs
  // -----------------------------------------------------------------------
  router.service(ServerLogService, {
    listServerLogs() {
      return { entries: [], latestId: 0n };
    },
  });
});
