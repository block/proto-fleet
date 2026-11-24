import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import ControllerLogs, { CONTROLLER_RECORDS_POLL_MS } from "./ControllerLogs";
import { controllerRecordsSource } from "./controllerRecordsSource";
import type { MinerAdapter } from "@/protoOS/api/adapters/types";
import type { useAuthRetry } from "@/protoOS/store/hooks/useAuthRetry";
import type { ControllerLogSnapshot } from "@/shared/features/controllerLogs/types";

const mocks = vi.hoisted(() => ({ handleAuthErrors: vi.fn() }));
vi.mock("@/protoOS/store/hooks/useAuth", () => ({
  useAuthErrors: () => ({ handleAuthErrors: mocks.handleAuthErrors }),
}));
vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({ useMinerHosting: () => ({ isFleetHosted: false }) }));

const snapshot = (message: string): ControllerLogSnapshot => ({
  entries: [{ timestamp: new Date().toISOString(), severity: "info", component: "plant", message }],
  limit: 1000,
  coverage: { state: "retained" },
  fetchedAt: new Date().toISOString(),
});

describe("ProtoOS controller logs", () => {
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    Element.prototype.scrollIntoView = vi.fn();
    // A refreshed token: the request runs again.
    mocks.handleAuthErrors.mockImplementation(({ onSuccess }) => onSuccess());
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.clearAllMocks();
  });

  it("reads again after the poll interval", async () => {
    const getControllerLogs = vi
      .fn()
      .mockResolvedValueOnce(snapshot("first read"))
      .mockResolvedValue(snapshot("second read"));
    render(<ControllerLogs adapter={{ getControllerLogs } as unknown as MinerAdapter} />);
    expect(await screen.findByText(/first read/)).toBeInTheDocument();

    await act(() => vi.advanceTimersByTimeAsync(CONTROLLER_RECORDS_POLL_MS));

    expect(await screen.findByText(/second read/)).toBeInTheDocument();
    expect(getControllerLogs).toHaveBeenCalledTimes(2);
  });

  it("refreshes the token once on a 401 and shows the retried read", async () => {
    const getControllerLogs = vi
      .fn()
      .mockRejectedValueOnce({ status: 401 })
      .mockResolvedValue(snapshot("after refresh"));
    render(<ControllerLogs adapter={{ getControllerLogs } as unknown as MinerAdapter} />);

    expect(await screen.findByText(/after refresh/)).toBeInTheDocument();
    expect(mocks.handleAuthErrors).toHaveBeenCalledTimes(1);
    expect(getControllerLogs).toHaveBeenCalledTimes(2);
  });
});

describe("controllerRecordsSource", () => {
  const settleWithoutCallbacks = (() => Promise.resolve()) as unknown as ReturnType<typeof useAuthRetry>;
  const adapter = { getControllerLogs: vi.fn() } as unknown as MinerAdapter;

  it("rejects an aborted read with the abort reason, so the view shows no error", async () => {
    const controller = new AbortController();
    controller.abort();
    const read = controllerRecordsSource(adapter, settleWithoutCallbacks).readLogs!({ limit: 10 }, controller.signal);
    await expect(read).rejects.toBe(controller.signal.reason);
  });

  it("never leaves a read pending when the retry settles without a result", async () => {
    const read = controllerRecordsSource(adapter, settleWithoutCallbacks).readLogs!(
      { limit: 10 },
      new AbortController().signal,
    );
    await expect(read).rejects.toThrow("ended without a result");
  });
});
