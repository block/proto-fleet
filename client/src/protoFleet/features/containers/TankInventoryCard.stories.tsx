import { create } from "@bufbuild/protobuf";
import type { Meta, StoryObj } from "@storybook/react";
import TankInventoryCard from "./TankInventoryCard";
import { EquipmentCoverageSchema } from "@/protoFleet/api/generated/common/v1/equipment_coverage_pb";
import { FleetListStatsSchema } from "@/protoFleet/api/generated/common/v1/fleet_list_stats_pb";
import { EntityKind, InventoryItemSchema } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";

const item = create(InventoryItemSchema, {
  ref: { kind: EntityKind.TANK, id: 1n },
  name: "CT1 North T2",
  assetCount: 16n,
  hashrateThs: 7850,
  powerKw: 121.7,
  hashrateReportingCount: 15n,
  powerReportingCount: 15n,
  listStats: { hashingCount: 13, brokenCount: 1, offlineCount: 1, sleepingCount: 1 },
});

const meta = {
  title: "Proto Fleet/Containers/Tank inventory card",
  component: TankInventoryCard,
  args: { item, onClick: () => {} },
  decorators: [
    (Story) => (
      <div className="min-h-screen bg-surface-base p-6 text-text-primary">
        <div className="max-w-[340px]">
          <Story />
        </div>
      </div>
    ),
  ],
} satisfies Meta<typeof TankInventoryCard>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Partial: Story = {};
export const Complete: Story = {
  args: { item: create(InventoryItemSchema, { ...item, hashrateReportingCount: 16n, powerReportingCount: 16n }) },
};
export const IndependentCoverage: Story = {
  args: {
    item: create(InventoryItemSchema, {
      ...item,
      coverage: create(EquipmentCoverageSchema, {
        hashrate: { reporting: 47, expected: 48, topologyComplete: true },
        power: { reporting: 14, expected: 16, topologyComplete: false },
      }),
    }),
  },
};
export const Unavailable: Story = {
  args: { item: create(InventoryItemSchema, { ...item, hashrateThs: undefined, powerKw: undefined }) },
};

export const SingleModule: Story = {
  args: {
    item: create(InventoryItemSchema, {
      ...item,
      assetCount: 1n,
      hashrateThs: 600,
      powerKw: 18,
      hashrateReportingCount: 1n,
      powerReportingCount: 1n,
      listStats: create(FleetListStatsSchema, { hashingCount: 1 }),
    }),
  },
};

export const Empty: Story = {
  args: {
    item: create(InventoryItemSchema, {
      ref: item.ref,
      name: item.name,
      assetCount: 0n,
    }),
  },
};
