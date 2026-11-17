import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import ModulePowerFeed, { HOSTED_POWER_FEED_REASON } from "./ModulePowerFeed";
import { modulePowerFeedTarget } from "./useModulePowerFeed";
import type { ModulePowerFeedInspection } from "@/protoOS/api/adapters/types";
import type { ModuleNode, TankNode } from "@/protoOS/store/contract";

const mocks = vi.hoisted(() => ({
  adapter: {} as { getModulePowerFeed?: ReturnType<typeof vi.fn>; refreshAccessToken?: ReturnType<typeof vi.fn> },
  isFleetHosted: false,
}));
vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: () => ({ adapter: mocks.adapter, isFleetHosted: mocks.isFleetHosted }),
}));

const tank = { id: "ct1:tank:1", slot: 1, moduleIds: [] } as unknown as TankNode;
const module = {
  id: "ct1:module:M-1-5",
  location: { tankLabel: "Tank 1", moduleSlot: 5 },
} as unknown as ModuleNode;

const inspection: ModulePowerFeedInspection = {
  feedId: "pdu-1-outlet-05",
  confidence: "configured",
  reasons: [],
  sharedWith: [],
  recoveryCycle: null,
  reading: { state: "enabled", active: true, faulted: false, overridden: false, powerW: 18_250 },
  observedAt: new Date(2026, 10, 16, 12, 0, 5),
};

const value = (label: string) => screen.getByText(label).previousElementSibling?.textContent;

beforeEach(() => {
  mocks.isFleetHosted = false;
  mocks.adapter = { getModulePowerFeed: vi.fn().mockResolvedValue(inspection) };
});

it("targets the module's controller position and store ID", () => {
  expect(modulePowerFeedTarget(module, tank)).toEqual({ tank: 1, module: 5, moduleId: "ct1:module:M-1-5" });
});

it("reads the module's feed once and shows the outlet, mapping and read time", async () => {
  vi.useFakeTimers({ shouldAdvanceTime: true });
  render(<ModulePowerFeed module={module} tank={tank} />);
  expect(screen.getByRole("status").textContent).toBe("Loading power feed…");
  await screen.findByText("Outlet");
  expect(value("Outlet")).toBe("pdu-1-outlet-05");
  expect(value("Mapping")).toBe("Configured");
  expect(screen.getByText(/^Read 11\/16\/26 at 12:00:05PM/)).toBeTruthy();
  expect(mocks.adapter.getModulePowerFeed).toHaveBeenCalledWith(
    { tank: 1, module: 5, moduleId: "ct1:module:M-1-5" },
    expect.objectContaining({ signal: expect.any(AbortSignal) }),
  );
  await act(() => vi.advanceTimersByTimeAsync(60_000));
  expect(mocks.adapter.getModulePowerFeed).toHaveBeenCalledTimes(1);
  vi.useRealTimers();
});

it("reads again only on refresh", async () => {
  render(<ModulePowerFeed module={module} tank={tank} />);
  fireEvent.click(await screen.findByRole("button", { name: "Refresh power feed" }));
  await waitFor(() => expect(mocks.adapter.getModulePowerFeed).toHaveBeenCalledTimes(2));
});

it("disables Refresh while reading, and a failed refresh replaces the last reading", async () => {
  render(<ModulePowerFeed module={module} tank={tank} />);
  const refresh = await screen.findByRole<HTMLButtonElement>("button", { name: "Refresh power feed" });
  let fail = (_: Error) => {};
  mocks.adapter.getModulePowerFeed!.mockReturnValueOnce(new Promise((_, reject) => (fail = reject)));
  fireEvent.click(refresh);
  await waitFor(() => expect(refresh.disabled).toBe(true));
  expect(refresh.textContent).toBe("Refreshing…");
  expect(value("Outlet")).toBe("pdu-1-outlet-05");
  await act(async () => fail(new Error("Controller map unavailable")));
  expect((await screen.findByRole("alert")).textContent).toBe("Could not load power feed: Controller map unavailable");
  expect(screen.queryByText("Outlet")).toBeNull();
});

it("disables Retry while the retry is in flight", async () => {
  mocks.adapter.getModulePowerFeed!.mockRejectedValueOnce(new Error("Controller map unavailable"));
  render(<ModulePowerFeed module={module} tank={tank} />);
  const retry = await screen.findByRole<HTMLButtonElement>("button", { name: "Retry power feed" });
  mocks.adapter.getModulePowerFeed!.mockReturnValueOnce(new Promise(() => {}));
  fireEvent.click(retry);
  await waitFor(() => expect(retry.disabled).toBe(true));
  expect(retry.textContent).toBe("Retrying…");
  expect(mocks.adapter.getModulePowerFeed).toHaveBeenCalledTimes(2);
});

it("reports a failure that carries no message", async () => {
  mocks.adapter.getModulePowerFeed!.mockRejectedValueOnce(Object.assign(new Error(""), { status: 500 }));
  render(<ModulePowerFeed module={module} tank={tank} />);
  expect((await screen.findByRole("alert")).textContent).toBe(
    "Could not load power feed: The controller did not answer.",
  );
});

it("refreshes an expired token and reads again", async () => {
  mocks.adapter.getModulePowerFeed!.mockRejectedValueOnce(Object.assign(new Error("expired"), { status: 401 }));
  mocks.adapter.refreshAccessToken = vi.fn().mockResolvedValue({ accessToken: "fresh" });
  render(<ModulePowerFeed module={module} tank={tank} />);
  await screen.findByText("Outlet");
  expect(mocks.adapter.getModulePowerFeed).toHaveBeenCalledTimes(2);
  expect(screen.queryByRole("alert")).toBeNull();
});

it("reports a failed read and retries on request", async () => {
  mocks.adapter.getModulePowerFeed!.mockRejectedValueOnce(new Error("Controller map unavailable"));
  render(<ModulePowerFeed module={module} tank={tank} />);
  expect((await screen.findByRole("alert")).textContent).toBe("Could not load power feed: Controller map unavailable");
  fireEvent.click(screen.getByRole("button", { name: "Retry power feed" }));
  await screen.findByText("Outlet");
});

it("does not read through Fleet's miner proxy", () => {
  mocks.isFleetHosted = true;
  render(<ModulePowerFeed module={module} tank={tank} />);
  expect(screen.getByText(HOSTED_POWER_FEED_REASON)).toBeTruthy();
  expect(mocks.adapter.getModulePowerFeed).not.toHaveBeenCalled();
});

it("is absent for adapters without feed reads", () => {
  mocks.adapter = {};
  const { container } = render(<ModulePowerFeed module={module} tank={tank} />);
  expect(container.textContent).toBe("");
});
