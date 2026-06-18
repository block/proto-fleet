import { create } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";

import {
  type BuildingRackHealth,
  BuildingRackHealthSchema,
  BuildingSchema,
  type BuildingWithCounts,
  BuildingWithCountsSchema,
} from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { FleetListStatsSchema } from "@/protoFleet/api/generated/common/v1/fleet_list_stats_pb";
import { MeasurementSchema, MeasurementUnit } from "@/protoFleet/api/generated/common/v1/measurement_pb";
import {
  type DeviceSet,
  DeviceSetSchema,
  type DeviceSetStats,
  DeviceSetStatsSchema,
  DeviceSetType,
  RackCoolingType,
  RackInfoSchema,
  RackOrderIndex,
  RackSlotStatusSchema,
  SlotDeviceStatus,
} from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import {
  DeviceStatus,
  type MinerStateSnapshot,
  MinerStateSnapshotSchema,
  PairingStatus,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { SiteSchema, type SiteWithCounts, SiteWithCountsSchema } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { MinerStateCountsSchema } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";

// ---------------------------------------------------------------------------
// Rack health profiles — cycled across racks to simulate variety.
// Mirrors the prototype's RACK_PROFILES distribution.
// ---------------------------------------------------------------------------

const RACK_PROFILES: { hashing: number; broken: number; offline: number; sleeping: number }[] = [
  { hashing: 24, broken: 0, offline: 0, sleeping: 0 },
  { hashing: 13, broken: 5, offline: 3, sleeping: 3 },
  { hashing: 4, broken: 12, offline: 8, sleeping: 0 },
  { hashing: 18, broken: 0, offline: 0, sleeping: 6 },
  { hashing: 1, broken: 9, offline: 6, sleeping: 8 },
  { hashing: 24, broken: 0, offline: 0, sleeping: 0 },
  { hashing: 9, broken: 2, offline: 1, sleeping: 12 },
  { hashing: 12, broken: 5, offline: 3, sleeping: 4 },
];

function makeRackHealth(buildingId: bigint, rackCount: number, labelPrefix: string): BuildingRackHealth[] {
  const racks: BuildingRackHealth[] = [];
  for (let i = 0; i < rackCount; i++) {
    const profile = RACK_PROFILES[i % RACK_PROFILES.length];
    racks.push(
      create(BuildingRackHealthSchema, {
        rackId: buildingId * 1000n + BigInt(i + 1),
        rackLabel: `${labelPrefix}${String(i + 1).padStart(2, "0")}`,
        aisleIndex: Math.floor(i / 10),
        positionInAisle: i % 10,
        hashingCount: profile.hashing,
        brokenCount: profile.broken,
        offlineCount: profile.offline,
        sleepingCount: profile.sleeping,
      }),
    );
  }
  return racks;
}

function sumField(racks: BuildingRackHealth[], field: keyof BuildingRackHealth): number {
  return racks.reduce((sum, r) => sum + (r[field] as number), 0);
}

// ---------------------------------------------------------------------------
// Sites — matches the prototype's 5 sites
// ---------------------------------------------------------------------------

const SITES_RAW: {
  id: bigint;
  name: string;
  city: string;
  state: string;
  powerMw: number;
  address: string;
  tz: string;
}[] = [
  {
    id: 1n,
    name: "Reno",
    city: "Reno",
    state: "NV",
    powerMw: 2,
    address: "500 Industrial Blvd",
    tz: "America/Los_Angeles",
  },
  {
    id: 2n,
    name: "Austin",
    city: "Austin",
    state: "TX",
    powerMw: 12,
    address: "1200 Energy Dr",
    tz: "America/Chicago",
  },
  { id: 3n, name: "Denver", city: "Denver", state: "CO", powerMw: 15, address: "800 Colo Blvd", tz: "America/Denver" },
  {
    id: 4n,
    name: "Miami",
    city: "Miami",
    state: "FL",
    powerMw: 50,
    address: "400 Brickell Ave",
    tz: "America/New_York",
  },
  { id: 5n, name: "Marfa", city: "Marfa", state: "TX", powerMw: 5, address: "12 Desert Rd", tz: "America/Chicago" },
];

// ---------------------------------------------------------------------------
// Buildings per site — mirrors prototype distribution
// ---------------------------------------------------------------------------

interface BuildingDef {
  id: bigint;
  siteId: bigint;
  name: string;
  rackCount: number;
  powerKw: number;
  overheadKw: number;
  aisles: number;
  racksPerAisle: number;
}

let buildingSeq = 1n;
function bld(siteId: bigint, name: string, racks: number, powerKw: number, overheadKw: number): BuildingDef {
  const id = buildingSeq++;
  return {
    id,
    siteId,
    name,
    rackCount: racks,
    powerKw,
    overheadKw,
    aisles: Math.max(1, Math.ceil(racks / 10)),
    racksPerAisle: Math.min(racks, 10),
  };
}

const BUILDINGS_RAW: BuildingDef[] = [
  // Reno — 1 building, empty
  bld(1n, "Building 1", 0, 0, 0),
  // Austin — 4 buildings, 20 racks each
  bld(2n, "Building 1", 20, 3000, 450),
  bld(2n, "Building 2", 20, 3000, 450),
  bld(2n, "Building 3", 20, 3000, 450),
  bld(2n, "Building 4", 20, 3000, 450),
  // Denver — 8 buildings, 30 racks each
  bld(3n, "Building 1", 30, 2000, 120),
  bld(3n, "Building 2", 30, 2000, 120),
  bld(3n, "Building 3", 30, 6000, 360),
  bld(3n, "Building 4", 30, 2000, 120),
  bld(3n, "Building 5", 30, 2000, 120),
  bld(3n, "Building 6", 30, 2000, 120),
  bld(3n, "Building 7", 30, 2000, 120),
  bld(3n, "Building 8", 30, 2000, 120),
  // Miami — 10 buildings, varied racks
  bld(4n, "Building 1", 8, 2400, 144),
  bld(4n, "Building 2", 8, 2400, 144),
  bld(4n, "Building 3", 12, 3600, 216),
  bld(4n, "Building 4", 100, 30000, 1800),
  bld(4n, "Building 5", 20, 6000, 360),
  bld(4n, "Building 6", 20, 6000, 360),
  bld(4n, "Building 7", 20, 6000, 360),
  bld(4n, "Building 8", 30, 9000, 540),
  bld(4n, "Building 9", 50, 15000, 900),
  bld(4n, "Building 10", 50, 15000, 900),
  // Marfa — 5 buildings
  bld(5n, "Building 1", 10, 1500, 90),
  bld(5n, "Building 2", 0, 0, 0),
  bld(5n, "Building 3", 0, 0, 0),
  bld(5n, "Building 4", 0, 0, 0),
  bld(5n, "Building 5", 6, 900, 54),
];

// Pre-compute rack health for every building
const RACK_HEALTH_BY_BUILDING = new Map<bigint, BuildingRackHealth[]>();
for (const b of BUILDINGS_RAW) {
  RACK_HEALTH_BY_BUILDING.set(b.id, makeRackHealth(b.id, b.rackCount, "R"));
}

// ---------------------------------------------------------------------------
// Build typed protobuf objects
// ---------------------------------------------------------------------------

function makeBuildingWithCounts(def: BuildingDef): BuildingWithCounts {
  const racks = RACK_HEALTH_BY_BUILDING.get(def.id) ?? [];
  const deviceCount = racks.reduce((s, r) => s + r.hashingCount + r.brokenCount + r.offlineCount + r.sleepingCount, 0);
  const hashingCount = sumField(racks, "hashingCount");
  const brokenCount = sumField(racks, "brokenCount");
  const offlineCount = sumField(racks, "offlineCount");
  const sleepingCount = sumField(racks, "sleepingCount");

  return create(BuildingWithCountsSchema, {
    building: create(BuildingSchema, {
      id: def.id,
      siteId: def.siteId,
      name: def.name,
      powerKw: def.powerKw,
      overheadKw: def.overheadKw,
      aisles: def.aisles,
      racksPerAisle: def.racksPerAisle,
      physicalRackCount: def.rackCount,
      defaultRackRows: 4,
      defaultRackColumns: 6,
    }),
    rackCount: BigInt(def.rackCount),
    deviceCount: BigInt(deviceCount),
    listStats: create(FleetListStatsSchema, {
      rackCount: def.rackCount,
      deviceCount,
      reportingCount: hashingCount,
      totalHashrateThs: hashingCount * 200,
      avgEfficiencyJth: 21.5,
      totalPowerKw: hashingCount * 3.5,
      hashingCount,
      brokenCount,
      offlineCount,
      sleepingCount,
      hashrateReportingCount: hashingCount,
      efficiencyReportingCount: hashingCount,
      powerReportingCount: hashingCount,
      minTemperatureC: 28,
      maxTemperatureC: 65,
      temperatureReportingCount: hashingCount,
    }),
  });
}

function makeSiteWithCounts(raw: (typeof SITES_RAW)[number]): SiteWithCounts {
  const siteBuildings = BUILDINGS_RAW.filter((b) => b.siteId === raw.id);
  const allRacks = siteBuildings.flatMap((b) => RACK_HEALTH_BY_BUILDING.get(b.id) ?? []);
  const rackCount = siteBuildings.reduce((s, b) => s + b.rackCount, 0);
  const deviceCount = allRacks.reduce(
    (s, r) => s + r.hashingCount + r.brokenCount + r.offlineCount + r.sleepingCount,
    0,
  );
  const hashingCount = sumField(allRacks, "hashingCount");
  const brokenCount = sumField(allRacks, "brokenCount");
  const offlineCount = sumField(allRacks, "offlineCount");
  const sleepingCount = sumField(allRacks, "sleepingCount");

  return create(SiteWithCountsSchema, {
    site: create(SiteSchema, {
      id: raw.id,
      name: raw.name,
      locationCity: raw.city,
      locationState: raw.state,
      powerCapacityMw: raw.powerMw,
      address: raw.address,
      timezone: raw.tz,
      country: "US",
    }),
    buildingCount: BigInt(siteBuildings.length),
    rackCount: BigInt(rackCount),
    deviceCount: BigInt(deviceCount),
    listStats: create(FleetListStatsSchema, {
      buildingCount: siteBuildings.length,
      rackCount,
      deviceCount,
      reportingCount: hashingCount,
      totalHashrateThs: hashingCount * 200,
      avgEfficiencyJth: 21.5,
      totalPowerKw: hashingCount * 3.5,
      hashingCount,
      brokenCount,
      offlineCount,
      sleepingCount,
      hashrateReportingCount: hashingCount,
      efficiencyReportingCount: hashingCount,
      powerReportingCount: hashingCount,
      minTemperatureC: 28,
      maxTemperatureC: 65,
      temperatureReportingCount: hashingCount,
    }),
  });
}

// ---------------------------------------------------------------------------
// Exported fixtures
// ---------------------------------------------------------------------------

export const mockSites: SiteWithCounts[] = SITES_RAW.map(makeSiteWithCounts);

export const mockBuildingsBySite = new Map<bigint, BuildingWithCounts[]>();
for (const raw of SITES_RAW) {
  mockBuildingsBySite.set(raw.id, BUILDINGS_RAW.filter((b) => b.siteId === raw.id).map(makeBuildingWithCounts));
}

export const mockAllBuildings: BuildingWithCounts[] = BUILDINGS_RAW.map(makeBuildingWithCounts);

export const mockRackHealthByBuilding = RACK_HEALTH_BY_BUILDING;

export function mockGetBuilding(buildingId: bigint) {
  return mockAllBuildings.find((b) => b.building?.id === buildingId)?.building;
}

export function mockBuildingRacks(buildingId: bigint) {
  const racks = RACK_HEALTH_BY_BUILDING.get(buildingId) ?? [];
  return racks.map((r) => ({
    rackId: r.rackId,
    rackLabel: r.rackLabel,
    aisleIndex: r.aisleIndex,
    positionInAisle: r.positionInAisle,
  }));
}

export function mockSiteStats(siteId: bigint) {
  const site = mockSites.find((s) => s.site?.id === siteId);
  if (!site) return undefined;
  const ls = site.listStats;
  return {
    siteId,
    buildingCount: ls?.buildingCount ?? 0,
    deviceCount: ls?.deviceCount ?? 0,
    reportingCount: ls?.reportingCount ?? 0,
    totalHashrateThs: ls?.totalHashrateThs ?? 0,
    avgEfficiencyJth: ls?.avgEfficiencyJth ?? 0,
    totalPowerKw: ls?.totalPowerKw ?? 0,
    hashingCount: ls?.hashingCount ?? 0,
    brokenCount: ls?.brokenCount ?? 0,
    offlineCount: ls?.offlineCount ?? 0,
    sleepingCount: ls?.sleepingCount ?? 0,
    hashrateReportingCount: ls?.hashrateReportingCount ?? 0,
    efficiencyReportingCount: ls?.efficiencyReportingCount ?? 0,
    powerReportingCount: ls?.powerReportingCount ?? 0,
    minTemperatureC: ls?.minTemperatureC ?? 0,
    maxTemperatureC: ls?.maxTemperatureC ?? 0,
    temperatureReportingCount: ls?.temperatureReportingCount ?? 0,
    controlBoardIssueCount: 0,
    fanIssueCount: Math.floor((ls?.brokenCount ?? 0) / 3),
    hashBoardIssueCount: Math.floor((ls?.brokenCount ?? 0) / 2),
    psuIssueCount: 0,
    rackCount: ls?.rackCount ?? 0,
  };
}

// ---------------------------------------------------------------------------
// Rack DeviceSets — mirrors building rack health but as DeviceSet objects
// ---------------------------------------------------------------------------

const MINER_MODELS = ["Antminer S21", "Antminer S21 XP", "Antminer S19 Pro", "Whatsminer M60"];
const MINER_MANUFACTURERS = ["Bitmain", "Bitmain", "Bitmain", "MicroBT"];
const FIRMWARE_VERSIONS = ["v3.5.1", "v3.5.2", "v3.4.8"];

function makeRackDeviceSets(): DeviceSet[] {
  const racks: DeviceSet[] = [];
  for (const bDef of BUILDINGS_RAW) {
    const rackHealthList = RACK_HEALTH_BY_BUILDING.get(bDef.id) ?? [];
    for (const rh of rackHealthList) {
      const deviceCount = rh.hashingCount + rh.brokenCount + rh.offlineCount + rh.sleepingCount;
      racks.push(
        create(DeviceSetSchema, {
          id: rh.rackId,
          type: DeviceSetType.RACK,
          label: rh.rackLabel,
          deviceCount,
          typeDetails: {
            case: "rackInfo",
            value: create(RackInfoSchema, {
              rows: 4,
              columns: 6,
              zone: `Zone ${(rh.aisleIndex ?? 0) + 1}`,
              orderIndex: RackOrderIndex.BOTTOM_LEFT,
              coolingType: RackCoolingType.IMMERSION,
              siteId: bDef.siteId,
              buildingId: bDef.id,
            }),
          },
        }),
      );
    }
  }
  return racks;
}

const ALL_RACK_DEVICE_SETS = makeRackDeviceSets();

export function mockListRacks(pageSize: number, pageToken: string, buildingIds: bigint[]) {
  let filtered = ALL_RACK_DEVICE_SETS;
  if (buildingIds.length > 0) {
    const bSet = new Set(buildingIds.map(String));
    filtered = filtered.filter((r) => {
      if (r.typeDetails.case !== "rackInfo") return false;
      return bSet.has((r.typeDetails.value.buildingId ?? 0n).toString());
    });
  }
  const startIdx = pageToken ? parseInt(pageToken, 10) : 0;
  const effectivePageSize = pageSize > 0 ? pageSize : 50;
  const page = filtered.slice(startIdx, startIdx + effectivePageSize);
  const nextIdx = startIdx + effectivePageSize;
  const nextPageToken = nextIdx < filtered.length ? String(nextIdx) : "";
  return { deviceSets: page, nextPageToken, totalCount: filtered.length };
}

export function mockListRackZones(): string[] {
  const zones = new Set<string>();
  for (const r of ALL_RACK_DEVICE_SETS) {
    if (r.typeDetails.case === "rackInfo" && r.typeDetails.value.zone) {
      zones.add(r.typeDetails.value.zone);
    }
  }
  return Array.from(zones).sort();
}

export function mockDeviceSetStats(deviceSetIds: bigint[]): DeviceSetStats[] {
  const idSet = new Set(deviceSetIds.map(String));
  const stats: DeviceSetStats[] = [];
  for (const bDef of BUILDINGS_RAW) {
    const rackHealthList = RACK_HEALTH_BY_BUILDING.get(bDef.id) ?? [];
    for (const rh of rackHealthList) {
      if (!idSet.has(rh.rackId.toString())) continue;
      const deviceCount = rh.hashingCount + rh.brokenCount + rh.offlineCount + rh.sleepingCount;
      const slotStatuses = [];
      let slot = 0;
      for (let row = 0; row < 4; row++) {
        for (let col = 0; col < 6; col++) {
          let status: SlotDeviceStatus;
          if (slot < rh.hashingCount) status = SlotDeviceStatus.HEALTHY;
          else if (slot < rh.hashingCount + rh.brokenCount) status = SlotDeviceStatus.NEEDS_ATTENTION;
          else if (slot < rh.hashingCount + rh.brokenCount + rh.offlineCount) status = SlotDeviceStatus.OFFLINE;
          else if (slot < deviceCount) status = SlotDeviceStatus.SLEEPING;
          else status = SlotDeviceStatus.EMPTY;
          slotStatuses.push(create(RackSlotStatusSchema, { row, column: col, status }));
          slot++;
        }
      }
      stats.push(
        create(DeviceSetStatsSchema, {
          deviceSetId: rh.rackId,
          deviceCount,
          reportingCount: rh.hashingCount,
          totalHashrateThs: rh.hashingCount * 200,
          avgEfficiencyJth: 21.5,
          totalPowerKw: rh.hashingCount * 3.5,
          minTemperatureC: 28,
          maxTemperatureC: 65,
          hashingCount: rh.hashingCount,
          brokenCount: rh.brokenCount,
          offlineCount: rh.offlineCount,
          sleepingCount: rh.sleepingCount,
          hashrateReportingCount: rh.hashingCount,
          efficiencyReportingCount: rh.hashingCount,
          powerReportingCount: rh.hashingCount,
          temperatureReportingCount: rh.hashingCount,
          slotStatuses,
        }),
      );
    }
  }
  return stats;
}

// ---------------------------------------------------------------------------
// Miners — one MinerStateSnapshot per device across all racks
// ---------------------------------------------------------------------------

function makeAllMiners(): MinerStateSnapshot[] {
  const miners: MinerStateSnapshot[] = [];
  const nowSecs = BigInt(Math.floor(Date.now() / 1000));
  let minerSeq = 0;

  for (const bDef of BUILDINGS_RAW) {
    const site = SITES_RAW.find((s) => s.id === bDef.siteId);
    const rackHealthList = RACK_HEALTH_BY_BUILDING.get(bDef.id) ?? [];

    for (const rh of rackHealthList) {
      const deviceCount = rh.hashingCount + rh.brokenCount + rh.offlineCount + rh.sleepingCount;
      let slotIdx = 0;

      for (let i = 0; i < deviceCount; i++) {
        minerSeq++;
        const modelIdx = minerSeq % MINER_MODELS.length;
        let deviceStatus: DeviceStatus;
        if (slotIdx < rh.hashingCount) deviceStatus = DeviceStatus.ONLINE;
        else if (slotIdx < rh.hashingCount + rh.brokenCount) deviceStatus = DeviceStatus.ERROR;
        else if (slotIdx < rh.hashingCount + rh.brokenCount + rh.offlineCount) deviceStatus = DeviceStatus.OFFLINE;
        else deviceStatus = DeviceStatus.INACTIVE;

        const isActive = deviceStatus === DeviceStatus.ONLINE;
        const hashrateThs = isActive ? 180 + (minerSeq % 40) : 0;
        const powerKw = isActive ? 3.2 + (minerSeq % 8) * 0.1 : 0;
        const effJth = isActive ? 19 + (minerSeq % 6) : 0;
        const tempC = isActive ? 35 + (minerSeq % 30) : 0;

        miners.push(
          create(MinerStateSnapshotSchema, {
            deviceIdentifier: `miner-${minerSeq}`,
            name: `M${String(minerSeq).padStart(4, "0")}`,
            macAddress: `AA:BB:CC:${((minerSeq >> 8) & 0xff).toString(16).padStart(2, "0").toUpperCase()}:${((minerSeq >> 4) & 0x0f).toString(16).toUpperCase()}0:${(minerSeq & 0x0f).toString(16).toUpperCase()}0`,
            serialNumber: `SN${String(minerSeq).padStart(8, "0")}`,
            ipAddress: `10.0.${Math.floor(minerSeq / 254)}.${(minerSeq % 254) + 1}`,
            url: `http://10.0.${Math.floor(minerSeq / 254)}.${(minerSeq % 254) + 1}`,
            deviceStatus,
            pairingStatus: PairingStatus.PAIRED,
            model: MINER_MODELS[modelIdx],
            manufacturer: MINER_MANUFACTURERS[modelIdx],
            firmwareVersion: FIRMWARE_VERSIONS[minerSeq % FIRMWARE_VERSIONS.length],
            rackLabel: rh.rackLabel,
            rackPosition: String(slotIdx + 1).padStart(2, "0"),
            driverName: "proto",
            workerName: `worker-${minerSeq}`,
            siteId: bDef.siteId,
            siteLabel: site?.name ?? "",
            timestamp: create(TimestampSchema, { seconds: nowSecs }),
            hashrate: hashrateThs
              ? [
                  create(MeasurementSchema, {
                    value: hashrateThs,
                    unit: MeasurementUnit.TERAHASH_PER_SECOND,
                    timestamp: create(TimestampSchema, { seconds: nowSecs }),
                  }),
                ]
              : [],
            powerUsage: powerKw
              ? [
                  create(MeasurementSchema, {
                    value: powerKw,
                    unit: MeasurementUnit.KILOWATT,
                    timestamp: create(TimestampSchema, { seconds: nowSecs }),
                  }),
                ]
              : [],
            efficiency: effJth
              ? [
                  create(MeasurementSchema, {
                    value: effJth,
                    unit: MeasurementUnit.JOULES_PER_TERAHASH,
                    timestamp: create(TimestampSchema, { seconds: nowSecs }),
                  }),
                ]
              : [],
            temperature: tempC
              ? [
                  create(MeasurementSchema, {
                    value: tempC,
                    unit: MeasurementUnit.CELSIUS,
                    timestamp: create(TimestampSchema, { seconds: nowSecs }),
                  }),
                ]
              : [],
          }),
        );
        slotIdx++;
      }
    }
  }
  return miners;
}

const ALL_MINERS = makeAllMiners();

export function mockListMiners(pageSize: number, cursor: string) {
  const startIdx = cursor ? parseInt(cursor, 10) : 0;
  const effectivePageSize = pageSize > 0 ? pageSize : 50;
  const page = ALL_MINERS.slice(startIdx, startIdx + effectivePageSize);
  const nextIdx = startIdx + effectivePageSize;
  const nextCursor = nextIdx < ALL_MINERS.length ? String(nextIdx) : "";

  const hashingCount = ALL_MINERS.filter((m) => m.deviceStatus === DeviceStatus.ONLINE).length;
  const brokenCount = ALL_MINERS.filter((m) => m.deviceStatus === DeviceStatus.ERROR).length;
  const offlineCount = ALL_MINERS.filter((m) => m.deviceStatus === DeviceStatus.OFFLINE).length;
  const sleepingCount = ALL_MINERS.filter((m) => m.deviceStatus === DeviceStatus.INACTIVE).length;

  return {
    miners: page,
    cursor: nextCursor,
    totalMiners: ALL_MINERS.length,
    totalStateCounts: create(MinerStateCountsSchema, {
      hashingCount,
      brokenCount,
      offlineCount,
      sleepingCount,
    }),
    models: [...new Set(ALL_MINERS.map((m) => m.model))],
    firmwareVersions: [...new Set(ALL_MINERS.map((m) => m.firmwareVersion))],
  };
}

export function mockMinerStateCounts() {
  const hashingCount = ALL_MINERS.filter((m) => m.deviceStatus === DeviceStatus.ONLINE).length;
  const brokenCount = ALL_MINERS.filter((m) => m.deviceStatus === DeviceStatus.ERROR).length;
  const offlineCount = ALL_MINERS.filter((m) => m.deviceStatus === DeviceStatus.OFFLINE).length;
  const sleepingCount = ALL_MINERS.filter((m) => m.deviceStatus === DeviceStatus.INACTIVE).length;
  return { totalMiners: ALL_MINERS.length, hashingCount, brokenCount, offlineCount, sleepingCount };
}

export function mockMinerModelGroups() {
  const countByModel = new Map<string, { manufacturer: string; count: number }>();
  for (const m of ALL_MINERS) {
    const existing = countByModel.get(m.model);
    if (existing) existing.count++;
    else countByModel.set(m.model, { manufacturer: m.manufacturer, count: 1 });
  }
  return Array.from(countByModel.entries()).map(([model, info]) => ({
    model,
    manufacturer: info.manufacturer,
    count: info.count,
  }));
}

// ---------------------------------------------------------------------------
// Telemetry — mock time-series for performance charts
// ---------------------------------------------------------------------------

export function mockCombinedMetrics(measurementTypes: number[], aggregationTypes: number[], durationMinutes: number) {
  const nowMs = Date.now();
  const intervalMs = 5 * 60 * 1000;
  const points = Math.min(288, Math.max(12, Math.floor((durationMinutes * 60 * 1000) / intervalMs)));
  const startMs = nowMs - points * intervalMs;

  const metrics: {
    measurementType: number;
    openTime: { seconds: bigint; nanos: number };
    aggregatedValues: { aggregationType: number; value: number }[];
    deviceCount: number;
  }[] = [];

  const baseValues: Record<number, { avg: number; spread: number }> = {
    1: { avg: 48, spread: 12 },
    2: { avg: 18000, spread: 3000 },
    3: { avg: 2800, spread: 400 },
    4: { avg: 21.5, spread: 3 },
    8: { avg: 98, spread: 4 },
  };

  for (let i = 0; i < points; i++) {
    const t = startMs + i * intervalMs;
    const tSecs = BigInt(Math.floor(t / 1000));

    for (const mt of measurementTypes) {
      const base = baseValues[mt] ?? { avg: 50, spread: 10 };
      const wave = Math.sin((i / points) * Math.PI * 4);
      const avg = base.avg + wave * base.spread * 0.3;

      const aggValues = aggregationTypes.map((at) => {
        let value = avg;
        if (at === 2) value = avg - base.spread * 0.4;
        else if (at === 3) value = avg + base.spread * 0.4;
        return { aggregationType: at, value: Math.round(value * 100) / 100 };
      });

      metrics.push({
        measurementType: mt,
        openTime: { seconds: tSecs, nanos: 0 },
        aggregatedValues: aggValues,
        deviceCount: 500,
      });
    }
  }

  return { metrics };
}

// ---------------------------------------------------------------------------
// Exported site/building fixtures (unchanged below)
// ---------------------------------------------------------------------------

export function mockBuildingStats(buildingId: bigint) {
  const bDef = BUILDINGS_RAW.find((b) => b.id === buildingId);
  if (!bDef) return undefined;
  const racks = RACK_HEALTH_BY_BUILDING.get(buildingId) ?? [];
  const deviceCount = racks.reduce((s, r) => s + r.hashingCount + r.brokenCount + r.offlineCount + r.sleepingCount, 0);
  const hashingCount = sumField(racks, "hashingCount");
  const brokenCount = sumField(racks, "brokenCount");
  const offlineCount = sumField(racks, "offlineCount");
  const sleepingCount = sumField(racks, "sleepingCount");

  return {
    buildingId,
    rackCount: bDef.rackCount,
    deviceCount,
    reportingCount: hashingCount,
    totalHashrateThs: hashingCount * 200,
    avgEfficiencyJth: 21.5,
    totalPowerKw: hashingCount * 3.5,
    hashingCount,
    brokenCount,
    offlineCount,
    sleepingCount,
    rackHealth: racks,
    deviceIdentifiers: Array.from({ length: deviceCount }, (_, i) => `miner-${buildingId}-${i + 1}`),
    hashrateReportingCount: hashingCount,
    efficiencyReportingCount: hashingCount,
    powerReportingCount: hashingCount,
    minTemperatureC: 28,
    maxTemperatureC: 65,
    temperatureReportingCount: hashingCount,
    controlBoardIssueCount: 0,
    fanIssueCount: Math.floor(brokenCount / 3),
    hashBoardIssueCount: Math.floor(brokenCount / 2),
    psuIssueCount: 0,
  };
}
