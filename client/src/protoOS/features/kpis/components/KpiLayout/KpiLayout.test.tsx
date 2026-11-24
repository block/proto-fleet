import { MemoryRouter, Route, Routes, useNavigate } from "react-router-dom";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";

import KpiLayout from "./KpiLayout";
import { useTelemetry, useTimeSeries } from "@/protoOS/api";
import { toCanonicalContainer } from "@/protoOS/api/adapters/mdkV2/normalize";
import useMinerStore from "@/protoOS/store/useMinerStore";

const hosting = vi.hoisted(() => ({ minerRoot: "" }));
vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({ useMinerHosting: () => hosting }));
vi.mock("@/protoOS/api", () => ({ useTelemetry: vi.fn(), useTimeSeries: vi.fn() }));
vi.mock("@/protoOS/features/kpis/components/TabMenu", () => ({ default: () => null }));

const activeHistoryQueries = () =>
  vi
    .mocked(useTimeSeries)
    .mock.calls.slice(-2)
    .map(([query]) => query)
    .filter((query) => query.enabled);
const requestedLevels = () => activeHistoryQueries().flatMap((query) => query.levels);

const SwitchChart = () => {
  const navigate = useNavigate();
  return <button onClick={() => navigate("/power-usage")}>Power chart</button>;
};
const showPage = (path: string) =>
  render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path={`${hosting.minerRoot}/`} element={<KpiLayout />}>
          <Route index element={<SwitchChart />} />
          <Route path=":metric" element={<SwitchChart />} />
        </Route>
      </Routes>
    </MemoryRouter>,
  );

describe("KpiLayout history selection", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    hosting.minerRoot = "";
    useMinerStore.getState().resetDeviceData();
    const { hardware } = toCanonicalContainer(
      {
        containerMap: {
          tanks: [1, 2].map((tank) => ({
            id: tank,
            modules: [1, 2].map((module) => ({
              id: module,
              slots: [1, 2, 3].map((position) => ({
                hashboard: {
                  serial: `T${tank}-M${module}-HB${position}`,
                  moduleId: `T${tank}-M${module}`,
                  location: { tank, module, hashboardPosition: position },
                },
              })),
            })),
          })),
        },
      },
      {},
      {},
      "ct1",
    );
    useMinerStore.getState().hardware.replaceHardware(hardware);
  });

  test.each([
    ["/", "hashrate"],
    ["/hashrate", "hashrate"],
    ["/power-usage", "power"],
    ["/efficiency", "efficiency"],
    ["/temperature/", "temperature"],
  ])("loads only the visible chart on %s while keeping live tab summaries", (path, field) => {
    showPage(path);
    expect(requestedLevels()).toEqual([
      { type: "miner", fields: [field] },
      { type: "hashboard", fields: [field], indexes: Array.from({ length: 12 }, (_, i) => i) },
    ]);
    expect(useTelemetry).toHaveBeenLastCalledWith({ level: ["miner"] });
  });

  test("changes requested history when the visible tab changes", () => {
    showPage("/");
    fireEvent.click(screen.getByRole("button", { name: "Power chart" }));
    expect(requestedLevels().map((level) => level.fields)).toEqual([["power"], ["power"]]);
  });

  test("uses hashrate at an embedded Home root even if the miner ID matches a tab", () => {
    hosting.minerRoot = "/miners/temperature";
    showPage(hosting.minerRoot);
    expect(requestedLevels()[0].fields).toEqual(["hashrate"]);
  });

  test("preserves the rig's batched history request", () => {
    useMinerStore.getState().resetDeviceData();
    useMinerStore.getState().hardware.setHashboards([{ serial: "rig-board", slot: 1 }]);
    showPage("/power-usage");
    expect(activeHistoryQueries()).toHaveLength(1);
    expect(requestedLevels()).toEqual([
      { type: "miner", fields: ["hashrate", "power", "efficiency", "temperature"] },
      { type: "hashboard", fields: ["hashrate", "power", "efficiency", "temperature"] },
    ]);
  });
});
