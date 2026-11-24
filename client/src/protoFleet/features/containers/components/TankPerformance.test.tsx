import { fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import TankPerformance from "./TankPerformance";
import { fleetDurations } from "@/shared/components/DurationSelector";

const mocks = vi.hoisted(() => ({ query: vi.fn(), select: vi.fn() }));
vi.mock("../hooks/useTankHistory", () => ({ useTankHistory: mocks.query }));
vi.mock("@/protoFleet/store", () => ({
  useDuration: () => "90d",
  useSetDuration: () => mocks.select,
  useTemperatureUnit: () => "C",
}));
beforeEach(() => {
  vi.clearAllMocks();
  mocks.query.mockReturnValue({ metrics: [], error: undefined, retry: vi.fn() });
});
it("uses the shared Fleet duration and exposes every standard range", () => {
  render(<TankPerformance containerIdentifier="remote-ct1" tankId={81n} enabled />);
  expect(mocks.query).toHaveBeenCalledWith("remote-ct1", 81n, "90d", true);
  const selector = within(screen.getByRole("group", { name: "Time range" }));
  expect(selector.getAllByRole("button").map((button) => button.textContent)).toEqual([...fleetDurations]);
  expect(selector.getByRole("button", { name: "90d" })).toHaveAttribute("aria-pressed", "true");
  fireEvent.click(selector.getByRole("button", { name: "1y" }));
  expect(mocks.select).toHaveBeenCalledWith("1y");
  expect(screen.getAllByText("No data")).toHaveLength(4);
});
