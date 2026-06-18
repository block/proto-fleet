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

const makeBuilding = (id: number, name: string, rackCount: number, deviceCount: number): BuildingWithCounts =>
  create(BuildingWithCountsSchema, {
    building: create(BuildingSchema, {
      id: BigInt(id),
      siteId: 1n,
      name,
      aisles: 2,
      racksPerAisle: 10,
    }),
    rackCount: BigInt(rackCount),
    deviceCount: BigInt(deviceCount),
  });

const makeStats = (
  buildingId: number,
  hashing: number,
  broken: number,
  offline: number,
  sleeping: number,
): GetBuildingStatsResponse =>
  create(GetBuildingStatsResponseSchema, {
    buildingId: BigInt(buildingId),
    rackCount: 20,
    deviceCount: hashing + broken + offline + sleeping,
    reportingCount: hashing,
    hashingCount: hashing,
    brokenCount: broken,
    offlineCount: offline,
    sleepingCount: sleeping,
    rackHealth: Array.from({ length: 20 }, (_, i) =>
      create(BuildingRackHealthSchema, {
        rackId: BigInt(i + 1),
        rackLabel: `R${String(i + 1).padStart(2, "0")}`,
        aisleIndex: Math.floor(i / 10),
        positionInAisle: i % 10,
        hashingCount: Math.round(hashing / 20),
        brokenCount: i % 5 === 0 ? Math.round(broken / 4) : 0,
        offlineCount: i % 7 === 0 ? Math.round(offline / 3) : 0,
        sleepingCount: i % 4 === 0 ? Math.round(sleeping / 5) : 0,
      }),
    ),
  });

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
    <div className="w-48">
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
          "Card for a single building on the site detail page. Shows a HealthBar summary and a cursor-following hover popover with per-status breakdowns.",
      },
    },
  },
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj;

export const Healthy: Story = {
  render: () => (
    <StoryWrapper building={makeBuilding(1, "Building Alpha", 20, 240)} stats={makeStats(1, 240, 0, 0, 0)} />
  ),
};

export const Mixed: Story = {
  render: () => (
    <StoryWrapper building={makeBuilding(2, "Building Beta", 20, 200)} stats={makeStats(2, 150, 25, 15, 10)} />
  ),
};

export const MostlyIssues: Story = {
  render: () => (
    <StoryWrapper building={makeBuilding(3, "Building Gamma", 20, 200)} stats={makeStats(3, 40, 80, 50, 30)} />
  ),
};

export const Loading: Story = {
  render: () => (
    <StoryWrapper
      building={makeBuilding(4, "Building Delta", 20, 200)}
      stats={makeStats(4, 200, 0, 0, 0)}
      delayMs={60000}
    />
  ),
};

export const StatsError: Story = {
  name: "Error",
  render: () => (
    <StoryWrapper
      building={makeBuilding(5, "Building Epsilon", 20, 200)}
      errorMessage="Failed to fetch building stats"
    />
  ),
};

export const LongName: Story = {
  render: () => (
    <StoryWrapper
      building={makeBuilding(6, "Denver Immersion Facility West Wing B2", 20, 200)}
      stats={makeStats(6, 180, 10, 5, 5)}
    />
  ),
};
