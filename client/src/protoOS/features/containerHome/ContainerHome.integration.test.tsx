import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ContainerHomeLayout from "./ContainerHomeLayout";
import ContainerHomePage from "./ContainerHomePage";
import { containerHistoryKey, type ContainerHistoryRequest } from "@/protoOS/api/adapters/containerHistory";
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
      { containerMap: { tanks: [] } },
      { hashrate: 100, power: 1500, activeBoards: 3, totalBoards: 3 },
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
    await waitFor(() => expect(screen.getByText("Hashboard distribution unavailable.")).toBeInTheDocument());
    expect(mocks.adapter.getTimeSeries).not.toHaveBeenCalled();
    expect(mocks.adapter.getCurrentTelemetry).toHaveBeenCalledTimes(1);
  });

  it("only polls power history in System mode, including after direct Hashboards entry", async () => {
    vi.useFakeTimers();
    const router = createMemoryRouter(routes, { initialEntries: ["/miners/ct1/power-usage?mode=hashboards"] });
    render(<RouterProvider router={router} />);
    await act(async () => {});
    expect(screen.getByText("Hashboard distribution unavailable.")).toBeInTheDocument();
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
