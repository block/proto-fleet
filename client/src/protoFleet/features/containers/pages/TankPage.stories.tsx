import { MemoryRouter, Route, Routes } from "react-router-dom";
import { create, toJson } from "@bufbuild/protobuf";
import type { Meta, StoryObj } from "@storybook/react";
import { spyOn } from "storybook/test";
import TankPage from "./TankPage";
import { GetContainerResponseSchema } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import {
  EntityKind,
  GetTankHistoryResponseSchema,
  ListInventoryResponseSchema,
  ListModuleHistoryResponseSchema,
} from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import { SiteWithCountsSchema } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { AggregationType, MeasurementType } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { SitesContext } from "@/protoFleet/api/SitesContext";

function TankPreview(_props: { state: "collected" | "stale" | "partial" | "empty" }) {
  return (
    <MemoryRouter initialEntries={["/containers/ct1/tanks/81"]}>
      <SitesContext.Provider
        value={{
          sites: [create(SiteWithCountsSchema, { site: { id: 2n, name: "North", slug: "north" } })],
          sitesError: null,
          sitesLoaded: true,
          sitesSettled: true,
          sitesPermissionDenied: false,
          siteCatalogAccessGranted: true,
          refetchSites: () => {},
          registerSitesPoll: () => () => {},
        }}
      >
        <main className="min-h-screen bg-surface-base text-text-primary">
          <Routes>
            <Route path="/containers/:identifier/tanks/:tankId" element={<TankPage />} />
            <Route path="/containers/:identifier/tanks/:tankId/modules/:moduleId" element={<TankPage />} />
            <Route path="*" element={<p className="p-8">Navigation destination</p>} />
          </Routes>
        </main>
      </SitesContext.Provider>
    </MemoryRouter>
  );
}

const meta = {
  title: "Proto Fleet/Containers/Native tank page",
  component: TankPreview,
  parameters: { layout: "fullscreen", withRouter: false },
  args: { state: "collected" },
  beforeEach: ({ args }) => {
    const now = Math.floor(Date.now() / 90_000) * 90;
    const states = [
      "hashing",
      "hashing",
      "hashing",
      "hashing",
      "hashing",
      "hashing",
      "hashing",
      "hashing",
      "hashing",
      "hashing",
      "attention",
      "attention",
      "sleeping",
      "isolated",
      "offline",
      "unknown",
    ];
    const ancestors = [{ kind: EntityKind.CONTAINER, id: 7n }];
    const tank = {
      ref: { kind: EntityKind.TANK, id: 81n },
      name: "CT1 North Tank 1",
      deviceIdentifier: "ct1",
      position: 1,
      ancestors,
      assetCount: args.state === "empty" ? 0n : 16n,
      hashrateReportingCount: args.state === "partial" ? 15n : 16n,
      powerReportingCount: args.state === "partial" ? 14n : 16n,
      listStats: {
        temperatureReportingCount: args.state === "partial" ? 15 : 16,
        efficiencyReportingCount: args.state === "partial" ? 14 : 16,
      },
      hashrateThs: 9600,
      efficiencyJth: 30,
      powerKw: 288,
      minTemperatureC: 60,
      maxTemperatureC: 68,
    };
    const modules = (args.state === "empty" ? [] : states)
      .slice(0, args.state === "partial" ? 15 : 16)
      .map((state, index) => ({
        ref: { kind: EntityKind.MODULE, id: BigInt(101 + index) },
        name: `Tank 1 module ${index + 1}`,
        position: index + 1,
        state,
        ancestors: [...ancestors, { kind: EntityKind.TANK, id: 81n }],
      }));
    const original = window.fetch.bind(window);
    const mock = spyOn(window, "fetch").mockImplementation(async (input, init) => {
      const request = new Request(input, init);
      const json = (body: unknown) =>
        new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" } });
      if (request.url.endsWith("/GetContainer"))
        return json(
          toJson(
            GetContainerResponseSchema,
            create(GetContainerResponseSchema, {
              container: {
                id: 7n,
                deviceIdentifier: "ct1",
                name: "CT1 North",
                site: { id: 2n, label: "North" },
                stale: args.state === "stale",
              },
            }),
          ),
        );
      if (request.url.endsWith("/ListInventory")) {
        const body = await request.json();
        return json(
          toJson(
            ListInventoryResponseSchema,
            create(ListInventoryResponseSchema, {
              items: body.view === "INVENTORY_VIEW_EQUIPMENT_GROUPS" ? [tank] : modules,
            }),
          ),
        );
      }
      if (request.url.endsWith("/GetTankHistory")) {
        const body = await request.json();
        const width = body.granularitySeconds;
        const start = Math.floor(Math.max(Date.parse(body.startTime) / 1000, now - 30 * 86400) / width) * width;
        const count = Math.ceil((Date.parse(body.endTime) / 1000 - start) / width);
        return json(
          toJson(
            GetTankHistoryResponseSchema,
            create(GetTankHistoryResponseSchema, {
              metrics:
                args.state === "empty"
                  ? []
                  : Array.from({ length: count }, (_, index) => {
                      const wave = Math.sin(index / 4);
                      return [
                        [MeasurementType.HASHRATE, 9600 + wave * 320],
                        [MeasurementType.POWER, 288 + wave * 9.6],
                        [MeasurementType.EFFICIENCY, 30],
                        [MeasurementType.TEMPERATURE, 64 + wave * 4],
                      ].map(([measurementType, value]) => ({
                        measurementType,
                        openTime: { seconds: BigInt(start + index * width) },
                        deviceCount: args.state === "partial" ? 15 : 16,
                        aggregatedValues:
                          measurementType === MeasurementType.POWER && index > 15 && index < 20
                            ? []
                            : [
                                { aggregationType: AggregationType.AVERAGE, value },
                                {
                                  aggregationType: AggregationType.MIN,
                                  value: measurementType === MeasurementType.TEMPERATURE ? value - 3 : value,
                                },
                                {
                                  aggregationType: AggregationType.MAX,
                                  value: measurementType === MeasurementType.TEMPERATURE ? value + 3 : value,
                                },
                              ],
                      }));
                    }).flat(),
            }),
          ),
        );
      }
      if (request.url.endsWith("/ListModuleHistory")) {
        const body = await request.json();
        return json(
          toJson(
            ListModuleHistoryResponseSchema,
            create(ListModuleHistoryResponseSchema, {
              samples:
                args.state === "empty"
                  ? []
                  : Array.from({ length: 40 }, (_, index) => ({
                      observedAt: { seconds: BigInt(now - (40 - index) * 90) },
                      hashrateHs: 600e12 + Math.sin(index / 4) * 20e12,
                      powerW:
                        body.moduleId === "101" && index > 15 && index < 20
                          ? undefined
                          : 18000 + Math.sin(index / 4) * 600,
                      temperatureC: 64 + Math.sin(index / 4) * 4,
                    })),
            }),
          ),
        );
      }
      return original(input, init);
    });
    return () => mock.mockRestore();
  },
} satisfies Meta<typeof TankPreview>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Collected: Story = {};
export const Stale: Story = { args: { state: "stale" } };
export const Partial: Story = { args: { state: "partial" } };
export const Empty: Story = { args: { state: "empty" } };
