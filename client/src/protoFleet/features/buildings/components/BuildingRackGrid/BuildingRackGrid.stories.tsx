import { create } from "@bufbuild/protobuf";
import type { Meta, StoryObj } from "@storybook/react";

import { BuildingRackHealthSchema } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import BuildingRackGrid from "./BuildingRackGrid";
import type { BuildingRackGridProps } from "./BuildingRackGrid";

const rack = (
  id: number,
  label: string,
  aisle: number,
  position: number,
  hashing: number,
  broken: number,
  offline: number,
  sleeping: number,
) =>
  create(BuildingRackHealthSchema, {
    rackId: BigInt(id),
    rackLabel: label,
    aisleIndex: aisle,
    positionInAisle: position,
    hashingCount: hashing,
    brokenCount: broken,
    offlineCount: offline,
    sleepingCount: sleeping,
  });

const buildRacks = (aisleCount: number, perAisle: number) => {
  const racks = [];
  let id = 1;
  for (let a = 0; a < aisleCount; a++) {
    for (let p = 0; p < perAisle; p++) {
      const label = `R${String(a * perAisle + p + 1).padStart(2, "0")}`;
      const roll = (id * 7) % 10;
      const hashing = 8 + roll;
      const broken = roll > 6 ? 3 : roll > 4 ? 1 : 0;
      const offline = roll > 7 ? 2 : 0;
      const sleeping = roll > 5 ? 1 : 0;
      racks.push(rack(id++, label, a, p, hashing, broken, offline, sleeping));
    }
  }
  return racks;
};

const meta: Meta<BuildingRackGridProps> = {
  title: "ProtoFleet/BuildingRackGrid",
  component: BuildingRackGrid,
  parameters: {
    layout: "padded",
    docs: {
      description: {
        component:
          "Paginated rack health grid for a building detail page. Name sort uses the building's aisle × position floor-plan layout with a minimap and viewport indicator. Issue sort shows a flat grid sorted by issue count.",
      },
    },
  },
  tags: ["autodocs"],
  decorators: [
    (Story) => (
      <div className="max-w-[960px]">
        <Story />
      </div>
    ),
  ],
};

export default meta;
type Story = StoryObj<BuildingRackGridProps>;

export const Default: Story = {
  args: {
    rackHealth: buildRacks(2, 10),
    aisles: 2,
    racksPerAisle: 10,
  },
};

export const LargeBuilding: Story = {
  args: {
    rackHealth: buildRacks(4, 15),
    aisles: 4,
    racksPerAisle: 15,
  },
};

export const SmallBuilding: Story = {
  args: {
    rackHealth: buildRacks(1, 4),
    aisles: 1,
    racksPerAisle: 4,
  },
};

export const SparseBuilding: Story = {
  name: "Sparse (empty positions)",
  args: {
    rackHealth: [
      rack(1, "R01", 0, 0, 10, 0, 0, 0),
      rack(2, "R02", 0, 3, 8, 2, 0, 0),
      rack(3, "R03", 0, 7, 6, 0, 1, 0),
      rack(4, "R04", 1, 1, 12, 0, 0, 0),
      rack(5, "R05", 1, 5, 4, 3, 2, 1),
      rack(6, "R06", 2, 0, 10, 0, 0, 2),
      rack(7, "R07", 2, 9, 8, 1, 0, 0),
    ],
    aisles: 3,
    racksPerAisle: 10,
  },
};

export const AllHealthy: Story = {
  args: {
    rackHealth: buildRacks(2, 8).map((r) =>
      create(BuildingRackHealthSchema, {
        ...r,
        brokenCount: 0,
        offlineCount: 0,
        sleepingCount: 0,
        hashingCount: 12,
      }),
    ),
    aisles: 2,
    racksPerAisle: 8,
  },
};

export const Empty: Story = {
  args: {
    rackHealth: [],
    aisles: 2,
    racksPerAisle: 10,
  },
};
