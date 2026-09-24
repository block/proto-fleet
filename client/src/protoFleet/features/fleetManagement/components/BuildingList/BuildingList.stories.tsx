import { create } from "@bufbuild/protobuf";
import type { Meta, StoryObj } from "@storybook/react";
import BuildingList from "./BuildingList";
import { BuildingSchema, BuildingWithCountsSchema } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { FleetListStatsSchema } from "@/protoFleet/api/generated/common/v1/fleet_list_stats_pb";
import { ContainerSchema, TankHealthSchema } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import { SiteWithCountsSchema } from "@/protoFleet/api/generated/sites/v1/sites_pb";

const meta = {
  title: "Proto Fleet/Buildings/BuildingList",
  component: BuildingList,
  args: {
    sites: [create(SiteWithCountsSchema, { site: { id: 1n, name: "West site" } })],
    buildings: ["West hall", "Building A"].map((name, index) =>
      create(BuildingWithCountsSchema, {
        building: create(BuildingSchema, { id: BigInt(index + 1), name, siteId: 1n }),
        rackCount: 6n,
        deviceCount: 96n,
        listStats: create(FleetListStatsSchema, {
          rackCount: 6,
          deviceCount: 96,
          hashingCount: 94,
          brokenCount: 2,
          totalHashrateThs: 38000,
          hashrateReportingCount: 96,
          totalPowerKw: 920,
          powerReportingCount: 96,
        }),
      }),
    ),
    containers: [
      create(ContainerSchema, {
        deviceIdentifier: "ct1-demo",
        embeddedWebViewAvailable: true,
        name: "Container 1",
        site: { id: 1n, label: "West site" },
        hashrateHs: 38e15,
        powerW: 920000,
        tanks: [0, 1, 2, 3, 6, 8].map((affected, index) =>
          create(TankHealthSchema, {
            position: index + 1,
            hashingCount: 16 - affected,
            attentionCount: affected,
          }),
        ),
      }),
    ],
    selectedIds: [],
    onSelectedIdsChange: () => undefined,
  },
} satisfies Meta<typeof BuildingList>;
export default meta;
type Story = StoryObj<typeof meta>;
export const BuildingsAndContainers: Story = {};
