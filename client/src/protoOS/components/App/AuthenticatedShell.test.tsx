import { act, render } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";

import AuthenticatedShell from "./AuthenticatedShell";
import { asComponentId, asModuleId, type MinerErrorObservations } from "@/protoOS/store/contract";
import useMinerStore from "@/protoOS/store/useMinerStore";

const mocks = vi.hoisted(() => ({
  hashboardStatus: vi.fn(),
  checkFirmwareUpdate: vi.fn(() => Promise.resolve()),
  hosting: vi.fn(() => ({})),
}));

vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({ useMinerHosting: mocks.hosting }));
vi.mock("@/protoOS/api/hooks/useHashboardStatus", () => ({ useHashboardStatus: mocks.hashboardStatus }));
vi.mock("@/protoOS/api/hooks/useNetworkInfo", () => ({ useNetworkInfo: vi.fn() }));
vi.mock("@/protoOS/api/hooks/useErrors", () => ({ useErrors: vi.fn() }));
vi.mock("@/protoOS/api/hooks/usePoolsInfo", () => ({ usePoolsInfo: vi.fn() }));
vi.mock("@/protoOS/api/hooks/useFirmwareUpdate", () => ({
  useFirmwareUpdate: () => ({ checkFirmwareUpdate: mocks.checkFirmwareUpdate }),
}));

beforeEach(() => vi.clearAllMocks());

test("skips eager ASIC loading", () => {
  render(<AuthenticatedShell reloadSystemInfo={vi.fn()} />);
  expect(mocks.hashboardStatus).not.toHaveBeenCalled();
});

test("rechecks CT1 time on resume and expires thermal faults while error polling is stopped", () => {
  vi.useFakeTimers();
  let now = 119_999;
  const adapter = {
    telemetryNow: () => now,
    checkTelemetryClock: vi.fn(async () => {}),
    resumeTelemetryClock: vi.fn(async () => {
      now = NaN;
    }),
  };
  mocks.hosting.mockReturnValue({ adapter });
  const store = useMinerStore.getState();
  const component = {
    kind: "psu",
    id: asComponentId("psu-1"),
    owner: { kind: "module", id: asModuleId("module-1") },
  } as const;
  const observations: MinerErrorObservations = {
    reports: [],
    thermal: [{ id: "thermal", component, samples: [{ frameAt: 0, observedAt: 0, health: "fault" }] }],
  };
  const { unmount } = render(<AuthenticatedShell reloadSystemInfo={vi.fn()} />);
  try {
    store.minerStatus.setErrors(observations, now);
    act(() => vi.advanceTimersByTime(1_000));
    expect(useMinerStore.getState().minerStatus.errorsByOwner.size).toBe(1);
    now = 120_000;
    act(() => vi.advanceTimersByTime(1_000));
    expect(useMinerStore.getState().minerStatus.errorsByOwner.size).toBe(0);

    now = 100_000;
    store.minerStatus.setErrors(observations, now);
    window.dispatchEvent(new Event("focus"));
    expect(adapter.checkTelemetryClock).toHaveBeenCalledTimes(2);
    expect(adapter.resumeTelemetryClock).toHaveBeenCalledOnce();
    expect(useMinerStore.getState().minerStatus.errorsByOwner.size).toBe(0);
    now = 105_000;
    act(() => vi.advanceTimersByTime(1_000));
    expect(useMinerStore.getState().minerStatus.errorsByOwner.size).toBe(1);
  } finally {
    unmount();
    store.minerStatus.setErrors([]);
    mocks.hosting.mockReturnValue({});
    vi.useRealTimers();
  }
  window.dispatchEvent(new Event("focus"));
  expect(adapter.resumeTelemetryClock).toHaveBeenCalledOnce();
});
