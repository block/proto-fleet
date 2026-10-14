import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ContainerHomeLayout from "./ContainerHomeLayout";
import ContainerHomePage from "./ContainerHomePage";
import { containerHistoryKey, type ContainerHistoryRequest } from "@/protoOS/api/adapters/containerHistory";
import { MdkV2Adapter, type MdkV2Client } from "@/protoOS/api/adapters/mdkV2/MdkV2Adapter";
import { toCanonicalContainer } from "@/protoOS/api/adapters/mdkV2/normalize";
import { containerRoutePrefetch, importContainerHome, singleMinerRoutePrefetch } from "@/protoOS/routePrefetch";
import useMinerStore from "@/protoOS/store/useMinerStore";

const mocks = vi.hoisted(() => ({
  adapter: { getCurrentTelemetry: vi.fn(), getContainerHistory: vi.fn(), getTimeSeries: vi.fn() },
}));
vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: () => ({ adapter: mocks.adapter, minerRoot: "/miners/ct1", isFleetHosted: true }),
}));

const routes = [
  {
    path: "/miners/ct1",
    element: <ContainerHomeLayout />,
    children: [
      { index: true, element: <ContainerHomePage tab="overview" /> },
      { path: "hashrate", element: <ContainerHomePage tab="hashrate" /> },
      { path: "efficiency", element: <ContainerHomePage tab="efficiency" /> },
      { path: "power-usage", element: <ContainerHomePage tab="power" /> },
      { path: "temperature", element: <ContainerHomePage tab="temperature" /> },
    ],
  },
];

describe("container Home integration", () => {
  afterEach(() => vi.useRealTimers());

  beforeEach(() => {
    vi.clearAllMocks();
    useMinerStore.getState().resetDeviceData();
    useMinerStore.getState().ui.setDuration("12h");
    const fixture = toCanonicalContainer(
      {
        containerMap: {
          tanks: [
            { id: 1, modules: [{ id: 1, slots: [{ hashboard: { serial: "HB-1", health: { hashrate: 100 } } }] }] },
          ],
        },
      },
      { hashrate: 900, power: 1500, activeBoards: 3, totalBoards: 3 },
      { plant: { facilityPowerW: 2000 }, coolingFans: [{ componentId: "fan-one", powerW: 10 }] },
      "ct1",
    );
    useMinerStore.getState().hardware.replaceHardware(fixture.hardware);
    useMinerStore.getState().telemetry.mergeTelemetry(fixture.telemetry);
    mocks.adapter.getCurrentTelemetry.mockResolvedValue(fixture.telemetry);
    mocks.adapter.getContainerHistory.mockImplementation(async (request: ContainerHistoryRequest) => ({
      series: Object.fromEntries(
        request.selections.map((selection) => [
          containerHistoryKey(selection),
          {
            units: "W",
            bucketSizeMs: request.intervalMs,
            points: [{ timestampMs: Date.parse(request.startTime), value: 10 }],
          },
        ]),
      ),
      unavailable: [],
    }));
  });

  it("keeps one live polling owner across duration and tab changes, with no per-board histories", async () => {
    const router = createMemoryRouter(routes, { initialEntries: ["/miners/ct1/"] });
    render(<RouterProvider router={router} />);
    await waitFor(() => expect(mocks.adapter.getContainerHistory).toHaveBeenCalledTimes(1));
    expect(mocks.adapter.getCurrentTelemetry).toHaveBeenCalledTimes(1);
    expect(screen.getAllByRole("navigation", { name: "Container Home" })).toHaveLength(1);
    expect(screen.getByRole("link", { name: "View Fluid levels and temperature" })).toHaveAttribute(
      "href",
      "/miners/ct1/temperature?mode=fluid",
    );

    fireEvent.click(screen.getByRole("button", { name: "48h" }));
    await waitFor(() => expect(mocks.adapter.getContainerHistory).toHaveBeenCalledTimes(2));
    expect(mocks.adapter.getCurrentTelemetry).toHaveBeenCalledTimes(1);
    expect(screen.getByText("100.0 TH/s")).toBeInTheDocument();
    const request = mocks.adapter.getContainerHistory.mock.calls[1][0];
    expect(Date.parse(request.endTime) - Date.parse(request.startTime)).toBe(48 * 3_600_000);

    fireEvent.click(screen.getByRole("link", { name: "Power" }));
    await waitFor(() => expect(mocks.adapter.getContainerHistory).toHaveBeenCalledTimes(3));
    expect(mocks.adapter.getCurrentTelemetry).toHaveBeenCalledTimes(1);
    expect(mocks.adapter.getContainerHistory.mock.calls[2][0].selections).toEqual([
      { metric: "power" },
      { metric: "fanPower", identifier: "fan-one" },
    ]);
    fireEvent.click(screen.getByRole("button", { name: "Hashboards" }));
    await waitFor(() => expect(screen.getByText("History unavailable for this selection.")).toBeInTheDocument());
    expect(mocks.adapter.getTimeSeries).not.toHaveBeenCalled();
    expect(mocks.adapter.getCurrentTelemetry).toHaveBeenCalledTimes(1);
  });

  it("refreshes live module summaries with one bulk map call per poll and no board histories", async () => {
    vi.useFakeTimers();
    const map = {
      containerMap: {
        tanks: [
          {
            id: 1,
            modules: [
              {
                id: 1,
                slots: [
                  {
                    hashboard: {
                      serial: "HB-1",
                      health: { temperatures: [20], psuElectrical: { inputPower: 4000 } },
                    },
                  },
                ],
              },
            ],
          },
        ],
      },
    };
    const fixture = toCanonicalContainer(map, {}, {}, "ct1");
    useMinerStore.getState().hardware.replaceHardware(fixture.hardware);
    const client = {
      hashboardServiceGetContainerMap: vi.fn().mockResolvedValue({ data: map }),
      miningServiceGetMiningStatus: vi.fn().mockResolvedValue({ data: {} }),
      telemetryServiceGetContainerHealth: vi.fn().mockResolvedValue({ data: {} }),
      telemetryServiceQueryTimeSeries: vi.fn(),
    };
    const adapter = new MdkV2Adapter(client as unknown as MdkV2Client, { deviceId: "ct1" });
    mocks.adapter.getCurrentTelemetry.mockImplementation(adapter.getCurrentTelemetry.bind(adapter));
    const router = createMemoryRouter(routes, { initialEntries: ["/miners/ct1/power-usage"] });
    render(<RouterProvider router={router} />);
    await act(async () => {});
    expect(screen.getAllByText("4.0 kW")).toHaveLength(3);
    expect(client.hashboardServiceGetContainerMap).toHaveBeenCalledTimes(1);

    map.containerMap.tanks[0].modules[0].slots[0].hashboard.health.psuElectrical.inputPower = 6000;
    await act(() => vi.advanceTimersByTimeAsync(15_000));
    expect(screen.getAllByText("6.0 kW")).toHaveLength(3);
    expect(client.hashboardServiceGetContainerMap).toHaveBeenCalledTimes(2);
    expect(client.miningServiceGetMiningStatus).toHaveBeenCalledTimes(2);
    expect(client.telemetryServiceGetContainerHealth).toHaveBeenCalledTimes(2);

    await act(async () => fireEvent.click(screen.getByRole("link", { name: "Temperature" })));
    expect(screen.getAllByText("20.0 °C")).toHaveLength(3);
    expect(client.hashboardServiceGetContainerMap).toHaveBeenCalledTimes(2);
    expect(client.telemetryServiceQueryTimeSeries).not.toHaveBeenCalled();
    expect(mocks.adapter.getTimeSeries).not.toHaveBeenCalled();
  });

  it("refreshes live hashrate from board health in Overview and Hashrate without extra requests", async () => {
    vi.useFakeTimers();
    const boards = [
      { serial: "HB-1", health: { hashrate: 60 } },
      { serial: "HB-2", health: { hashrate: 40 } },
    ];
    const map = {
      containerMap: {
        tanks: [{ id: 1, modules: [{ id: 1, slots: boards.map((hashboard) => ({ hashboard })) }] }],
      },
    };
    const fixture = toCanonicalContainer(map, {}, {}, "ct1");
    useMinerStore.getState().hardware.replaceHardware(fixture.hardware);
    const client = {
      hashboardServiceGetContainerMap: vi.fn().mockResolvedValue({ data: map }),
      miningServiceGetMiningStatus: vi.fn().mockResolvedValue({ data: { hashrate: 900 } }),
      telemetryServiceGetContainerHealth: vi.fn().mockResolvedValue({ data: {} }),
    };
    const adapter = new MdkV2Adapter(client as unknown as MdkV2Client, { deviceId: "ct1" });
    mocks.adapter.getCurrentTelemetry.mockImplementation(adapter.getCurrentTelemetry.bind(adapter));
    const router = createMemoryRouter(routes, { initialEntries: ["/miners/ct1/"] });
    render(<RouterProvider router={router} />);
    await act(async () => {});
    expect(screen.getByText("100.0 TH/s")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "View Hashrate" })).toBeVisible();
    expect(screen.queryByText("900.0 TH/s")).not.toBeInTheDocument();

    boards[0].health.hashrate = 80;
    await act(() => vi.advanceTimersByTimeAsync(15_000));
    expect(screen.getByText("120.0 TH/s")).toBeInTheDocument();
    await act(async () => fireEvent.click(screen.getByRole("link", { name: "View Hashrate" })));
    expect(screen.getByRole("region", { name: "Live readings" })).toHaveTextContent("Hashrate120.0 TH/s");
    expect(screen.queryByText(/Accepted-share/)).not.toBeInTheDocument();
    expect(client.hashboardServiceGetContainerMap).toHaveBeenCalledTimes(2);
    expect(client.miningServiceGetMiningStatus).toHaveBeenCalledTimes(2);
  });

  it("only polls power history in System mode, including after direct Hashboards entry", async () => {
    vi.useFakeTimers();
    const router = createMemoryRouter(routes, { initialEntries: ["/miners/ct1/power-usage?mode=hashboards"] });
    render(<RouterProvider router={router} />);
    await act(async () => {});
    expect(screen.getByText("History unavailable for this selection.")).toBeInTheDocument();
    await act(() => vi.advanceTimersByTimeAsync(60_000));
    expect(mocks.adapter.getContainerHistory).not.toHaveBeenCalled();

    await act(async () => fireEvent.click(screen.getByRole("button", { name: "System" })));
    expect(mocks.adapter.getContainerHistory).toHaveBeenCalledTimes(1);
    expect(mocks.adapter.getContainerHistory.mock.calls[0][0].selections).toEqual([
      { metric: "power" },
      { metric: "fanPower", identifier: "fan-one" },
    ]);
    const systemSignal = mocks.adapter.getContainerHistory.mock.calls[0][1].signal;
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Hashboards" })));
    expect(systemSignal.aborted).toBe(true);
    await act(() => vi.advanceTimersByTimeAsync(60_000));
    expect(mocks.adapter.getContainerHistory).toHaveBeenCalledTimes(1);

    await act(async () => fireEvent.click(screen.getByRole("button", { name: "System" })));
    expect(mocks.adapter.getContainerHistory).toHaveBeenCalledTimes(2);
    await act(() => vi.advanceTimersByTimeAsync(30_000));
    expect(mocks.adapter.getContainerHistory).toHaveBeenCalledTimes(3);
  });

  it("supports direct Fluid entry and back/forward navigation without unrelated history queries", async () => {
    const router = createMemoryRouter(routes, { initialEntries: ["/miners/ct1/temperature?mode=fluid"] });
    render(<RouterProvider router={router} />);
    await act(async () => {});
    expect(screen.getByRole("button", { name: "Fluid" })).toHaveAttribute("aria-pressed", "true");
    expect(mocks.adapter.getContainerHistory).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "System" }));
    await waitFor(() => expect(mocks.adapter.getContainerHistory).toHaveBeenCalledTimes(1));
    await act(() => router.navigate(-1));
    expect(screen.getByRole("button", { name: "Fluid" })).toHaveAttribute("aria-pressed", "true");
  });

  it("prefetches the container page standalone and when Fleet-hosted", () => {
    expect(containerRoutePrefetch).toContain(importContainerHome);
    expect(singleMinerRoutePrefetch).toContain(importContainerHome);
  });

  it("keeps efficiency history on facility power when the live facility reading is missing", async () => {
    const fixture = toCanonicalContainer(
      { containerMap: { tanks: [] } },
      { hashrate: 100, power: 1500 },
      { plant: { totalPowerW: 1500 } },
      "ct1",
    );
    useMinerStore.getState().resetDeviceData();
    useMinerStore.getState().hardware.replaceHardware(fixture.hardware);
    useMinerStore.getState().telemetry.mergeTelemetry(fixture.telemetry);
    mocks.adapter.getCurrentTelemetry.mockResolvedValue(fixture.telemetry);
    mocks.adapter.getContainerHistory.mockResolvedValue({ series: {}, unavailable: ["facilityPower:"] });
    const router = createMemoryRouter(routes, { initialEntries: ["/miners/ct1/efficiency"] });
    render(<RouterProvider router={router} />);
    await waitFor(() => expect(mocks.adapter.getContainerHistory).toHaveBeenCalledTimes(1));
    expect(mocks.adapter.getContainerHistory.mock.calls[0][0].selections).toEqual([
      { metric: "hashrate" },
      { metric: "facilityPower" },
    ]);
    expect(screen.queryByText("15.0 J/TH")).not.toBeInTheDocument();
    expect(screen.getByText("History unavailable for this selection.")).toBeVisible();
    expect(mocks.adapter.getTimeSeries).not.toHaveBeenCalled();
  });
});
