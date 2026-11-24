import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import ContainerPerformance from "./ContainerPerformance";
import type { useTelemetryMetrics } from "@/protoFleet/api/useTelemetryMetrics";

const mocks = vi.hoisted(() => ({ query: vi.fn<typeof useTelemetryMetrics>() }));
vi.mock("@/protoFleet/api/useTelemetryMetrics", () => ({ useTelemetryMetrics: mocks.query }));
vi.mock("@/protoFleet/store", () => ({
  useDuration: () => "12h",
  useSetDuration: () => vi.fn(),
  useTemperatureUnit: () => "C",
}));
beforeEach(() => {
  vi.clearAllMocks();
  mocks.query.mockReturnValue({ data: null, hasLoaded: true, isLoading: false, error: null, refetch: vi.fn() });
});
describe("container performance", () => {
  it("scopes stored history to the container and current site, using Fleet chart panels", () => {
    render(<ContainerPerformance identifier="remote" siteId={3n} />);
    expect(mocks.query).toHaveBeenCalledWith(
      expect.objectContaining({ deviceIds: ["remote"], siteIds: [3n], includeUnassigned: false, enabled: true }),
    );
    expect(screen.getByText("Hashrate")).toBeInTheDocument();
    expect(screen.getByText("Temperature")).toBeInTheDocument();
    expect(screen.getByText("Total power")).toBeInTheDocument();
    expect(screen.getAllByText("No data")).toHaveLength(4);
  });
  it("requests the unassigned container and exposes initial history errors without an endless skeleton", () => {
    mocks.query.mockReturnValue({
      data: null,
      hasLoaded: false,
      isLoading: false,
      error: new Error("Offline"),
      refetch: vi.fn(),
    });
    render(<ContainerPerformance identifier="unassigned-ct1" />);
    expect(mocks.query).toHaveBeenCalledWith(
      expect.objectContaining({ deviceIds: ["unassigned-ct1"], siteIds: [], includeUnassigned: true }),
    );
    expect(screen.getByRole("alert")).toHaveTextContent("Offline");
    expect(screen.getAllByText("No data")).toHaveLength(4);
  });
});
