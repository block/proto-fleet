import { MemoryRouter, Route, Routes } from "react-router-dom";
import { create, toJson } from "@bufbuild/protobuf";
import { timestampNow, TimestampSchema } from "@bufbuild/protobuf/wkt";
import type { Meta, StoryObj } from "@storybook/react";
import { spyOn } from "storybook/test";
import ModulePage from "./ModulePage";
import {
  GetContainerResponseSchema,
  GetModuleMiningOptionsResponseSchema,
} from "@/protoFleet/api/generated/containers/v1/containers_pb";
import {
  EntityKind,
  GetModuleHashboardsResponseSchema,
  ListHashboardHistoryResponseSchema,
  ListInventoryResponseSchema,
  ListModuleHistoryResponseSchema,
} from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import { SitesContext } from "@/protoFleet/api/SitesContext";

function ModulePreview(_props: { state: "collected" | "stale" | "partial" | "replaced" }) {
  return (
    <MemoryRouter initialEntries={["/containers/ct1/tanks/81/modules/91"]}>
      <SitesContext.Provider
        value={{
          sites: [],
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
            <Route path="/containers/:identifier/tanks/:tankId/modules/:moduleId" element={<ModulePage />} />
            <Route path="*" element={<p className="p-8">Navigation destination</p>} />
          </Routes>
        </main>
      </SitesContext.Provider>
    </MemoryRouter>
  );
}
const meta = {
  title: "Proto Fleet/Containers/Native module page",
  component: ModulePreview,
  parameters: { layout: "fullscreen", withRouter: false },
  args: { state: "collected" },
  beforeEach: ({ args }) => {
    const ancestors = [{ kind: EntityKind.CONTAINER, id: 7n }];
    const original = window.fetch.bind(window);
    const mock = spyOn(window, "fetch").mockImplementation(async (input, init) => {
      const request = new Request(input, init);
      const json = (body: unknown) =>
        new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" } });
      const now = timestampNow();
      if (request.url.endsWith("/GetContainer"))
        return json(
          toJson(
            GetContainerResponseSchema,
            create(GetContainerResponseSchema, {
              container: { id: 7n, deviceIdentifier: "ct1", name: "CT1 North", stale: args.state === "stale" },
            }),
          ),
        );
      if (request.url.endsWith("/ListInventory")) {
        const body = await request.json();
        const module = (id: bigint, position: number) => ({
          ref: { kind: EntityKind.MODULE, id },
          deviceIdentifier: "ct1",
          position,
          name: `CT1 North T1 M${position}`,
          observedAt: now,
          ancestors: [...ancestors, { kind: EntityKind.TANK, id: 81n }],
          state: "error",
          hashrateThs: 600,
          powerKw: 18,
          efficiencyJth: 30,
          minTemperatureC: 62,
          maxTemperatureC: 62,
          moduleIssues: {
            complete: true,
            observedAt: now,
            items: [
              {
                component: "psu",
                componentId: "PSU-07",
                cause: "PSU_OVER_TEMPERATURE_WARNING",
                severity: "warning",
                observedAt: now,
              },
            ],
          },
        });
        return json(
          toJson(
            ListInventoryResponseSchema,
            create(ListInventoryResponseSchema, {
              items:
                body.view === "INVENTORY_VIEW_EQUIPMENT_GROUPS"
                  ? [{ ref: { kind: EntityKind.TANK, id: 81n }, position: 1, ancestors }]
                  : [module(args.state === "replaced" ? 191n : 91n, 1), module(92n, 2)],
            }),
          ),
        );
      }
      if (request.url.endsWith("/GetModuleHashboards")) {
        const body = await request.json();
        const moduleId = BigInt(body.moduleIds[0]);
        return json(
          toJson(
            GetModuleHashboardsResponseSchema,
            create(GetModuleHashboardsResponseSchema, {
              modules: [
                {
                  moduleId,
                  slots: [1, 2, 3].map((position) => ({
                    position,
                    observedAt: now,
                    occupancy:
                      args.state === "partial" && position === 2
                        ? "empty"
                        : args.state === "partial" && position === 3
                          ? "unknown"
                          : "occupied",
                    board:
                      args.state === "partial" && position > 1
                        ? undefined
                        : {
                            hashboardId: BigInt(position + 10),
                            installationId: BigInt(position + 20),
                            moduleId,
                            position,
                            serial: `CT1-HB-00${position}`,
                            macAddress: `00:11:22:33:44:0${position}`,
                            state: "active",
                            miningState: "mining",
                            observedAt: now,
                            stateObservedAt: now,
                            hashrateHs: 200e12,
                            powerW: 5800,
                            efficiencyJh: 29e-12,
                            temperatureC: 60 + position,
                            metadata: {
                              psuIdentity: {
                                serial: "PSU-07",
                                vendor: "BOCO",
                                model: "CT1 PSU",
                                firmwareVersion: "1.2.3",
                              },
                            },
                            health: {
                              psuElectrical: {
                                inputVoltage: 415,
                                inputPower: 18000,
                                outputVoltage: 48,
                                outputPower: 17400,
                                sampleAgeMs: 100,
                              },
                              psuThermal: {
                                inletTemperature: 35,
                                srHeatsinkTemperature: 55,
                                pfcHeatsinkTemperature: 45,
                                temperaturesAgeMs: 100,
                              },
                              maximumAsicTemperatureC: 70 + position,
                              asicTemperatureObservedAt: toJson(TimestampSchema, now),
                              errors: [],
                              warnings:
                                position === 1 ? [{ cause: "HASHBOARD_ERROR_CAUSE_BOARD_OVER_TEMPERATURE" }] : [],
                            },
                            hashrailOk: true,
                            refclksOk: true,
                          },
                  })),
                },
              ],
            }),
          ),
        );
      }
      if (request.url.endsWith("/ListModuleHistory") || request.url.endsWith("/ListHashboardHistory")) {
        const body = await request.json();
        const board = request.url.endsWith("/ListHashboardHistory");
        const end = Date.parse(body.endTime);
        const start = Date.parse(body.startTime);
        const width = end - start <= 86400_000 ? 90_000 : end - start <= 7 * 86400_000 ? 3600_000 : 86400_000;
        const samples = Array.from({ length: Math.floor((end - start) / width) }, (_, index) => ({
          observedAt: { seconds: BigInt(Math.floor((start + index * width) / 1000)) },
          collectedAt: now,
          hashrateHs: (board ? 200 : 600) * 1e12 + Math.sin(index / 12) * 5e12,
          powerW: (board ? 5800 : 18000) + Math.sin(index / 12) * 150,
          efficiencyJh: 30e-12,
          temperatureC: 62 + Math.sin(index / 12) * 2,
        })).filter((_, index) => index < 25 || index > 31);
        return board
          ? json(
              toJson(
                ListHashboardHistoryResponseSchema,
                create(ListHashboardHistoryResponseSchema, {
                  samples: samples.map((sample) => ({
                    ...sample,
                    hashboardId: BigInt(body.hashboardId),
                    installationId: BigInt(body.hashboardId) + 10n,
                  })),
                }),
              ),
            )
          : json(toJson(ListModuleHistoryResponseSchema, create(ListModuleHistoryResponseSchema, { samples })));
      }
      if (request.url.endsWith("/GetModuleMiningOptions"))
        return json(
          toJson(GetModuleMiningOptionsResponseSchema, create(GetModuleMiningOptionsResponseSchema, { canStop: true })),
        );
      return original(input, init);
    });
    return () => mock.mockRestore();
  },
} satisfies Meta<typeof ModulePreview>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Collected: Story = {};
export const Stale: Story = { args: { state: "stale" } };
export const Partial: Story = { args: { state: "partial" } };
export const Replaced: Story = { args: { state: "replaced" } };
