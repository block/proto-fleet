import { MemoryRouter, Route, Routes } from "react-router-dom";
import { create } from "@bufbuild/protobuf";
import type { Meta, StoryObj } from "@storybook/react";
import { expect, spyOn, within } from "storybook/test";
import RacksPage from "./RacksPage";
import { buildingsClient, deviceSetClient, fleetInventoryClient, sitesClient } from "@/protoFleet/api/clients";
import { ListBuildingsResponseSchema } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { ListRackZonesResponseSchema } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import {
  EntityKind,
  ListInventoryResponseSchema,
} from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import { ListSitesResponseSchema } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { useFleetStore } from "@/protoFleet/store";

function GridPreview() {
  return (
    <MemoryRouter initialEntries={["/fleet/racks?display=grid"]}>
      <div
        className="fixed inset-0 overflow-auto bg-surface-base text-text-primary tablet:left-16 desktop:left-50"
        data-testid="grid-viewport"
      >
        {/* Match FleetLayout's desktop max-content shell, which also supports wide tables. */}
        <div className="flex h-full w-full min-w-0 flex-col laptop:w-max laptop:min-w-full">
          <div className="min-h-0 min-w-0 flex-1">
            <Routes>
              <Route path="/fleet/racks" element={<RacksPage />} />
            </Routes>
          </div>
        </div>
      </div>
    </MemoryRouter>
  );
}

const meta = {
  title: "Proto Fleet/Rack Management/Racks page",
  component: GridPreview,
  parameters: { layout: "fullscreen", withRouter: false },
  beforeEach: () => {
    const permissions = useFleetStore.getState().auth.permissions;
    useFleetStore.setState((state) => {
      state.auth.permissions = ["site:read", "fleet:read", "miner:read", "rack:read"];
    });
    const mocks = [
      spyOn(deviceSetClient, "listRackZones").mockResolvedValue(create(ListRackZonesResponseSchema)),
      spyOn(buildingsClient, "listBuildings").mockResolvedValue(create(ListBuildingsResponseSchema)),
      spyOn(sitesClient, "listSites").mockResolvedValue(create(ListSitesResponseSchema)),
      spyOn(fleetInventoryClient, "listInventory").mockResolvedValue(
        create(ListInventoryResponseSchema, {
          totalCount: 12n,
          items: Array.from({ length: 12 }, (_, index) => ({
            ref: { kind: EntityKind.RACK, id: BigInt(index + 1) },
            name: `Rack_${index + 1}_Long_Unbroken_Identifier_1234567890_1234567890`,
            assetCount: 12n,
            details: {
              case: "rack" as const,
              value: {
                id: BigInt(index + 1),
                label: `Rack_${index + 1}_Long_Unbroken_Identifier_1234567890_1234567890`,
                deviceCount: 12,
                typeDetails: { case: "rackInfo" as const, value: { rows: 8, columns: 12 } },
              },
            },
            listStats: {
              deviceCount: 12,
              totalHashrateThs: 1200,
              hashrateReportingCount: 12,
              totalPowerKw: 36,
              powerReportingCount: 12,
            },
          })),
        }),
      ),
    ];
    return () => {
      mocks.forEach((mock) => mock.mockRestore());
      useFleetStore.setState((state) => {
        state.auth.permissions = permissions;
      });
    };
  },
} satisfies Meta<typeof GridPreview>;
export default meta;
type Story = StoryObj<typeof meta>;

export const LongRackNames: Story = {
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    const cards = await canvas.findAllByTestId("rack-card");
    const viewport = canvas.getByTestId("grid-viewport");
    // Observe several painted frames: a ResizeObserver feedback loop can look
    // correct in one frame, then widen the grid again in the next.
    const widths: number[] = [];
    for (let frame = 0; frame < 30; frame++) {
      await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
      const first = cards[0].getBoundingClientRect();
      widths.push(first.width);
      await expect(viewport.scrollWidth).toBeLessThanOrEqual(viewport.clientWidth);
    }
    await expect(Math.max(...widths) - Math.min(...widths)).toBeLessThan(1);
  },
};
