import { useEffect } from "react";
import { create } from "@bufbuild/protobuf";
import type { Meta, StoryObj } from "@storybook/react";

import BuildingCard from "./BuildingCard";
import { buildingsClient } from "@/protoFleet/api/clients";
import {
  BuildingRackHealthSchema,
  BuildingSchema,
  type BuildingWithCounts,
  BuildingWithCountsSchema,
  type GetBuildingStatsResponse,
  GetBuildingStatsResponseSchema,
} from "@/protoFleet/api/generated/buildings/v1/buildings_pb";

const makeBuilding = (
  id: number,
  name: string,
  opts: { rackCount?: number; deviceCount?: number; aisles?: number; racksPerAisle?: number } = {},
): BuildingWithCounts =>
  create(BuildingWithCountsSchema, {
    building: create(BuildingSchema, {
      id: BigInt(id),
      siteId: 1n,
      name,
      aisles: opts.aisles ?? 2,
      racksPerAisle: opts.racksPerAisle ?? 10,
    }),
    rackCount: BigInt(opts.rackCount ?? 20),
    deviceCount: BigInt(opts.deviceCount ?? 200),
  });

const makeStats = (
  buildingId: number,
  opts: {
    hashing?: number;
    broken?: number;
    offline?: number;
    sleeping?: number;
    totalHashrateThs?: number;
    avgEfficiencyJth?: number;
    totalPowerKw?: number;
    rackCount?: number;
    aisles?: number;
    racksPerAisle?: number;
  } = {},
): GetBuildingStatsResponse => {
  const hashing = opts.hashing ?? 200;
  const broken = opts.broken ?? 0;
  const offline = opts.offline ?? 0;
  const sleeping = opts.sleeping ?? 0;
  const rackCount = opts.rackCount ?? 20;
  const racksPerAisle = opts.racksPerAisle ?? 10;

  return create(GetBuildingStatsResponseSchema, {
    buildingId: BigInt(buildingId),
    rackCount,
    deviceCount: hashing + broken + offline + sleeping,
    reportingCount: hashing,
    hashrateReportingCount: hashing,
    efficiencyReportingCount: hashing,
    powerReportingCount: hashing,
    hashingCount: hashing,
    brokenCount: broken,
    offlineCount: offline,
    sleepingCount: sleeping,
    totalHashrateThs: opts.totalHashrateThs ?? hashing * 120,
    avgEfficiencyJth: opts.avgEfficiencyJth ?? 22.5,
    totalPowerKw: opts.totalPowerKw ?? hashing * 3.5,
    rackHealth: Array.from({ length: rackCount }, (_, i) =>
      create(BuildingRackHealthSchema, {
        rackId: BigInt(i + 1),
        rackLabel: `R${String(i + 1).padStart(2, "0")}`,
        aisleIndex: Math.floor(i / racksPerAisle),
        positionInAisle: i % racksPerAisle,
        hashingCount: Math.round(hashing / rackCount),
        brokenCount: i % 5 === 0 ? Math.round(broken / Math.max(1, Math.floor(rackCount / 5))) : 0,
        offlineCount: i % 7 === 0 ? Math.round(offline / Math.max(1, Math.floor(rackCount / 7))) : 0,
        sleepingCount: i % 4 === 0 ? Math.round(sleeping / Math.max(1, Math.floor(rackCount / 4))) : 0,
      }),
    ),
  });
};

const mockGetBuildingStats = (stats: GetBuildingStatsResponse, delayMs = 200) => {
  const original = buildingsClient.getBuildingStats;
  (buildingsClient as any).getBuildingStats = async () => {
    await new Promise((r) => setTimeout(r, delayMs));
    return stats;
  };
  return () => {
    (buildingsClient as any).getBuildingStats = original;
  };
};

const mockGetBuildingStatsError = (message: string, delayMs = 200) => {
  const original = buildingsClient.getBuildingStats;
  (buildingsClient as any).getBuildingStats = async () => {
    await new Promise((r) => setTimeout(r, delayMs));
    throw new Error(message);
  };
  return () => {
    (buildingsClient as any).getBuildingStats = original;
  };
};

const StoryWrapper = ({
  building,
  stats,
  errorMessage,
  delayMs = 200,
}: {
  building: BuildingWithCounts;
  stats?: GetBuildingStatsResponse;
  errorMessage?: string;
  delayMs?: number;
}) => {
  useEffect(() => {
    if (errorMessage) return mockGetBuildingStatsError(errorMessage, delayMs);
    if (stats) return mockGetBuildingStats(stats, delayMs);
  }, [stats, errorMessage, delayMs]);

  return (
    <div className="w-80">
      <BuildingCard building={building} />
    </div>
  );
};

const meta: Meta = {
  title: "Proto Fleet/BuildingCard",
  component: BuildingCard,
  parameters: {
    layout: "centered",
    docs: {
      description: {
        component:
          "Card for a single building on the buildings tab. Shows a rack grid floor plan, status summary, actions menu, and footer stats (hashrate, efficiency, power, rack count).",
      },
    },
  },
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj;

export const AllHealthy: Story = {
  render: () => (
    <StoryWrapper
      building={makeBuilding(1, "Building Alpha")}
      stats={makeStats(1, { hashing: 240, totalHashrateThs: 28800, avgEfficiencyJth: 22.5, totalPowerKw: 840 })}
    />
  ),
};

export const Mixed: Story = {
  render: () => (
    <StoryWrapper
      building={makeBuilding(2, "Building Beta")}
      stats={makeStats(2, {
        hashing: 150,
        broken: 25,
        offline: 15,
        sleeping: 10,
        totalHashrateThs: 18000,
        avgEfficiencyJth: 24.1,
        totalPowerKw: 525,
      })}
    />
  ),
};

export const MostlyIssues: Story = {
  render: () => (
    <StoryWrapper
      building={makeBuilding(3, "Building Gamma")}
      stats={makeStats(3, {
        hashing: 40,
        broken: 80,
        offline: 50,
        sleeping: 30,
        totalHashrateThs: 4800,
        avgEfficiencyJth: 28.3,
        totalPowerKw: 140,
      })}
    />
  ),
};

export const NoRacks: Story = {
  render: () => (
    <StoryWrapper
      building={makeBuilding(4, "Building Delta", { rackCount: 0, aisles: 0, racksPerAisle: 0 })}
      stats={makeStats(4, { hashing: 0, rackCount: 0, aisles: 0, racksPerAisle: 0 })}
    />
  ),
};

export const Loading: Story = {
  render: () => (
    <StoryWrapper
      building={makeBuilding(5, "Building Epsilon")}
      stats={makeStats(5, { hashing: 200 })}
      delayMs={60000}
    />
  ),
};

export const StatsError: Story = {
  name: "Error",
  render: () => (
    <StoryWrapper building={makeBuilding(6, "Building Zeta")} errorMessage="Failed to fetch building stats" />
  ),
};

export const LongName: Story = {
  render: () => (
    <StoryWrapper
      building={makeBuilding(7, "Denver Immersion Facility West Wing B2")}
      stats={makeStats(7, {
        hashing: 180,
        broken: 10,
        offline: 5,
        sleeping: 5,
        totalHashrateThs: 21600,
        avgEfficiencyJth: 23.0,
        totalPowerKw: 630,
      })}
    />
  ),
};

export const WideGrid: Story = {
  name: "Wide Grid (4×20)",
  render: () => (
    <StoryWrapper
      building={makeBuilding(8, "Building Theta", { aisles: 4, racksPerAisle: 20, rackCount: 80 })}
      stats={makeStats(8, {
        hashing: 800,
        broken: 40,
        offline: 20,
        sleeping: 10,
        rackCount: 80,
        aisles: 4,
        racksPerAisle: 20,
        totalHashrateThs: 96000,
        avgEfficiencyJth: 21.8,
        totalPowerKw: 2800,
      })}
    />
  ),
};
