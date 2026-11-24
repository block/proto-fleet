import { create } from "@bufbuild/protobuf";
import type { Meta, StoryObj } from "@storybook/react";
import DeviceSetList from "./DeviceSetList";
import { DeviceSetSchema } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import { EntityKind, InventoryItemSchema } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";

const rack = create(DeviceSetSchema, {
  id: 1n,
  label: "West rack",
  deviceCount: 8,
  typeDetails: { case: "rackInfo", value: { rows: 2, columns: 4 } },
});
const meta = {
  title: "Proto Fleet/Racks/DeviceSetList",
  component: DeviceSetList,
  args: {
    deviceSets: [rack],
    statsMap: new Map(),
    renderName: (item) => <span>{item.deviceSet.label}</span>,
    renderMiners: (item) => <span>{item.deviceSet.deviceCount}</span>,
    currentSort: { field: "name", direction: "asc" },
    onSort: () => undefined,
    selectedIds: [],
    onSelectedIdsChange: () => undefined,
    itemName: { singular: "equipment group", plural: "equipment groups" },
    inventoryItems: [
      create(InventoryItemSchema, {
        ref: { kind: EntityKind.TANK, id: 1n },
        name: "Container 1 T1",
        facilityName: "Container 1",
        assetCount: 16n,
        hashrateThs: 4800,
        powerKw: 57.6,
        efficiencyJth: 12,
        listStats: { deviceCount: 16, hashingCount: 16 },
      }),
      create(InventoryItemSchema, { ref: { kind: EntityKind.RACK, id: 1n }, details: { case: "rack", value: rack } }),
    ],
  },
} satisfies Meta<typeof DeviceSetList>;
export default meta;
type Story = StoryObj<typeof meta>;
export const RacksAndTanks: Story = {};
