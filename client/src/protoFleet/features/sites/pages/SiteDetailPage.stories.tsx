import { useEffect } from "react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { create } from "@bufbuild/protobuf";
import type { Meta, StoryObj } from "@storybook/react";

import SiteDetailPage from "./SiteDetailPage";
import { buildingsClient, sitesClient } from "@/protoFleet/api/clients";
import {
  BuildingSchema,
  BuildingWithCountsSchema,
  type GetBuildingStatsResponse,
  GetBuildingStatsResponseSchema,
  ListBuildingsResponseSchema,
} from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import {
  GetSiteStatsResponseSchema,
  ListSitesResponseSchema,
  SiteSchema,
  SiteWithCountsSchema,
} from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { useFleetStore } from "@/protoFleet/store";

const SITE_ID = 1n;

interface BuildingFixture {
  id: number;
  name: string;
  rackCount: number;
  deviceCount: number;
  hashingCount: number;
  brokenCount: number;
  offlineCount: number;
  sleepingCount: number;
  powerKw?: number;
}

interface SiteDetailScenario {
  name: string;
  city: string;
  state: string;
  address: string;
  postalCode: string;
  powerCapacityMw: number;
  buildings: BuildingFixture[];
}

type ScenarioKey = "mixed" | "single" | "dense" | "longNames" | "empty";

interface SiteDetailStoryProps {
  scenario: ScenarioKey;
  frameClassName?: string;
}

const mixedBuildings: BuildingFixture[] = [
  {
    id: 101,
    name: "Building Alpha",
    rackCount: 24,
    deviceCount: 1728,
    hashingCount: 1694,
    brokenCount: 12,
    offlineCount: 6,
    sleepingCount: 16,
    powerKw: 12_000,
  },
  {
    id: 102,
    name: "Building Beta",
    rackCount: 20,
    deviceCount: 1440,
    hashingCount: 1280,
    brokenCount: 82,
    offlineCount: 38,
    sleepingCount: 40,
    powerKw: 10_500,
  },
  {
    id: 103,
    name: "Immersion Hall C",
    rackCount: 18,
    deviceCount: 1296,
    hashingCount: 1128,
    brokenCount: 42,
    offlineCount: 18,
    sleepingCount: 108,
    powerKw: 14_000,
  },
  {
    id: 104,
    name: "Building Delta",
    rackCount: 18,
    deviceCount: 1296,
    hashingCount: 1296,
    brokenCount: 0,
    offlineCount: 0,
    sleepingCount: 0,
    powerKw: 8_500,
  },
];

const denseBuildings: BuildingFixture[] = Array.from({ length: 18 }, (_, index) => {
  const id = 201 + index;
  const deviceCount = 960 + (index % 4) * 120;
  const brokenCount = index % 5 === 0 ? 56 : index % 3 === 0 ? 18 : 4;
  const offlineCount = index % 6 === 0 ? 34 : index % 4 === 0 ? 12 : 0;
  const sleepingCount = index % 7 === 0 ? 84 : index % 2 === 0 ? 24 : 8;
  return {
    id,
    name: `Building ${String.fromCharCode(65 + index)}`,
    rackCount: 12 + (index % 5),
    deviceCount,
    hashingCount: Math.max(deviceCount - brokenCount - offlineCount - sleepingCount, 0),
    brokenCount,
    offlineCount,
    sleepingCount,
    powerKw: 6_800 + index * 220,
  };
});

const longNameBuildings: BuildingFixture[] = [
  "Northwest Immersion Conversion Hall - Phase 2",
  "Building Alpha West Wing Auxiliary Power Room",
  "Temporary Container Yard 4 - South Fence Line",
  "High-Density Air-Cooled Expansion Building",
  "Legacy Miners Rework and Burn-In Facility",
  "Operations Spares and Quarantine Hall",
].map((name, index) => ({
  id: 301 + index,
  name,
  rackCount: 10 + index,
  deviceCount: 720 + index * 96,
  hashingCount: 680 + index * 72,
  brokenCount: index % 2 === 0 ? 16 : 3,
  offlineCount: index % 3 === 0 ? 22 : 0,
  sleepingCount: index % 2 === 1 ? 46 : 8,
  powerKw: 5_400 + index * 530,
}));

const SCENARIOS: Record<ScenarioKey, SiteDetailScenario> = {
  mixed: {
    name: "Austin North",
    city: "Austin",
    state: "TX",
    address: "4100 Metric Blvd",
    postalCode: "78758",
    powerCapacityMw: 48,
    buildings: mixedBuildings,
  },
  single: {
    name: "Austin Single Hall",
    city: "Austin",
    state: "TX",
    address: "4100 Metric Blvd",
    postalCode: "78758",
    powerCapacityMw: 16,
    buildings: [mixedBuildings[0]],
  },
  dense: {
    name: "Austin Dense Site",
    city: "Austin",
    state: "TX",
    address: "4100 Metric Blvd",
    postalCode: "78758",
    powerCapacityMw: 120,
    buildings: denseBuildings,
  },
  longNames: {
    name: "Reno Expansion Yard",
    city: "Reno",
    state: "NV",
    address: "1190 Industrial Way",
    postalCode: "89502",
    powerCapacityMw: 64,
    buildings: longNameBuildings,
  },
  empty: {
    name: "Austin Empty Site",
    city: "Austin",
    state: "TX",
    address: "4100 Metric Blvd",
    postalCode: "78758",
    powerCapacityMw: 20,
    buildings: [],
  },
};

const makeBuilding = (fixture: BuildingFixture) =>
  create(BuildingWithCountsSchema, {
    building: create(BuildingSchema, {
      id: BigInt(fixture.id),
      siteId: SITE_ID,
      name: fixture.name,
      aisles: 4,
      racksPerAisle: Math.ceil(fixture.rackCount / 4),
      powerKw: fixture.powerKw ?? 0,
    }),
    rackCount: BigInt(fixture.rackCount),
    deviceCount: BigInt(fixture.deviceCount),
  });

const makeBuildingStats = (fixture: BuildingFixture): GetBuildingStatsResponse =>
  create(GetBuildingStatsResponseSchema, {
    buildingId: BigInt(fixture.id),
    rackCount: fixture.rackCount,
    deviceCount: fixture.deviceCount,
    reportingCount: fixture.deviceCount,
    hashingCount: fixture.hashingCount,
    brokenCount: fixture.brokenCount,
    offlineCount: fixture.offlineCount,
    sleepingCount: fixture.sleepingCount,
  });

const sum = (buildings: BuildingFixture[], selector: (building: BuildingFixture) => number) =>
  buildings.reduce((total, building) => total + selector(building), 0);

const installMocks = (scenario: SiteDetailScenario) => {
  const siteBuildings = scenario.buildings.map(makeBuilding);
  const buildingStatsById = new Map(
    scenario.buildings.map((building) => [String(building.id), makeBuildingStats(building)]),
  );
  const deviceCount = sum(scenario.buildings, (building) => building.deviceCount);
  const rackCount = sum(scenario.buildings, (building) => building.rackCount);
  const hashingCount = sum(scenario.buildings, (building) => building.hashingCount);
  const brokenCount = sum(scenario.buildings, (building) => building.brokenCount);
  const offlineCount = sum(scenario.buildings, (building) => building.offlineCount);
  const sleepingCount = sum(scenario.buildings, (building) => building.sleepingCount);
  const totalPowerKw = sum(scenario.buildings, (building) => building.powerKw ?? 0);

  const site = create(SiteWithCountsSchema, {
    site: create(SiteSchema, {
      id: SITE_ID,
      name: scenario.name,
      locationCity: scenario.city,
      locationState: scenario.state,
      address: scenario.address,
      postalCode: scenario.postalCode,
      country: "US",
      powerCapacityMw: scenario.powerCapacityMw,
    }),
    buildingCount: BigInt(scenario.buildings.length),
    rackCount: BigInt(rackCount),
    deviceCount: BigInt(deviceCount),
  });

  useFleetStore.setState((state) => {
    state.auth.permissions = ["site:read", "site:manage"];
    state.auth.authLoading = false;
    state.auth.isAuthenticated = true;
    state.ui.activeSite = { kind: "site", id: SITE_ID.toString() };
  });

  (sitesClient as any).listSites = async () => create(ListSitesResponseSchema, { sites: [site] });
  (sitesClient as any).getSiteStats = async () =>
    create(GetSiteStatsResponseSchema, {
      siteId: SITE_ID,
      buildingCount: scenario.buildings.length,
      rackCount,
      deviceCount,
      reportingCount: deviceCount,
      totalHashrateThs: hashingCount * 95,
      totalPowerKw,
      avgEfficiencyJth: deviceCount > 0 ? 27.9 : 0,
      hashingCount,
      brokenCount,
      offlineCount,
      sleepingCount,
      hashrateReportingCount: deviceCount,
      efficiencyReportingCount: deviceCount,
      powerReportingCount: deviceCount,
    });
  (buildingsClient as any).listBuildings = async () =>
    create(ListBuildingsResponseSchema, { buildings: siteBuildings });
  (buildingsClient as any).getBuildingStats = async ({ buildingId }: { buildingId: bigint }) =>
    buildingStatsById.get(buildingId.toString()) ?? create(GetBuildingStatsResponseSchema, { buildingId });
};

const SiteDetailStory = ({ scenario = "mixed", frameClassName }: SiteDetailStoryProps) => {
  const selectedScenario = SCENARIOS[scenario];
  installMocks(selectedScenario);

  useEffect(() => {
    installMocks(selectedScenario);
  }, [selectedScenario]);

  const story = (
    <MemoryRouter initialEntries={[`/sites/${SITE_ID.toString()}`]}>
      <Routes>
        <Route path="/sites/:id" element={<SiteDetailPage />} />
        <Route path="/buildings/:id" element={<div className="p-10 text-300">Building detail route</div>} />
        <Route path="/fleet" element={<div className="p-10 text-300">Fleet route</div>} />
      </Routes>
    </MemoryRouter>
  );

  return frameClassName ? <div className={frameClassName}>{story}</div> : story;
};

const meta: Meta<typeof SiteDetailStory> = {
  title: "Proto Fleet/Sites/SiteDetailPage",
  component: SiteDetailStory,
  parameters: {
    layout: "fullscreen",
    withRouter: false,
  },
  argTypes: {
    frameClassName: { table: { disable: true } },
    scenario: {
      control: "radio",
      options: ["mixed", "single", "dense", "longNames", "empty"],
    },
  },
};

export default meta;
type Story = StoryObj<typeof SiteDetailStory>;

export const Default: Story = {
  args: { scenario: "mixed" },
};

export const SingleBuilding: Story = {
  args: { scenario: "single" },
};

export const DenseBuildingSet: Story = {
  args: { scenario: "dense" },
};

export const LongBuildingNames: Story = {
  args: { scenario: "longNames" },
};

export const EmptySite: Story = {
  args: { scenario: "empty" },
};

export const NarrowWidth: Story = {
  args: {
    scenario: "dense",
    frameClassName: "mx-auto min-h-screen max-w-[560px] border-x border-border-5 bg-surface-5",
  },
};
