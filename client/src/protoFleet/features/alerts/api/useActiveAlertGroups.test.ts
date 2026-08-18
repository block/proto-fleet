import { renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Code, ConnectError } from "@connectrpc/connect";

import type { ActiveAlertGroupsPage } from "@/protoFleet/features/alerts/api/alertsApi";
import * as api from "@/protoFleet/features/alerts/api/alertsApi";
import { useActiveAlertGroups } from "@/protoFleet/features/alerts/api/useActiveAlertGroups";
import type { ActiveAlertGroup } from "@/protoFleet/features/alerts/types";

vi.mock("@/protoFleet/features/alerts/api/alertsApi", () => ({
  listActiveAlertGroups: vi.fn(),
}));

const listMock = vi.mocked(api.listActiveAlertGroups);

const buildGroup = (alertName: string): ActiveAlertGroup => ({
  alert_name: alertName,
  stored_alert_name: alertName,
  rule_group: "miner",
  key: JSON.stringify(["miner", alertName]),
  device_count: 1,
  alert_count: 1,
  first_started_at: "2026-07-01T00:00:00Z",
  summary: "",
});

const deferredPage = () => {
  let settle!: (page: ActiveAlertGroupsPage) => void;
  const promise = new Promise<ActiveAlertGroupsPage>((resolve) => {
    settle = resolve;
  });
  return { promise, settle };
};

describe("useActiveAlertGroups", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("resumes polling when a revoked grant is restored", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    listMock.mockRejectedValue(new ConnectError("forbidden", Code.PermissionDenied));

    // Nothing remounts this hook — the shell that owns it outlives every route change.
    const { result } = renderHook(() => useActiveAlertGroups());
    await waitFor(() => expect(result.current.denied).toBe(true));

    listMock.mockResolvedValue({ groups: [buildGroup("Hashrate dropped")], has_more: false });
    await vi.advanceTimersByTimeAsync(5 * 60 * 1000);
    await waitFor(() => expect(result.current.denied).toBe(false));
    expect(result.current.groups.map((group) => group.alert_name)).toEqual(["Hashrate dropped"]);
  });

  // A denial hides the pill entirely, so leaving it set on an error that isn't a denial would swallow the error
  // too: the header would report neither the alerts nor the failure until some later poll happened to succeed.
  it("reports a non-permission failure that follows a denial", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    listMock.mockRejectedValue(new ConnectError("forbidden", Code.PermissionDenied));

    const { result } = renderHook(() => useActiveAlertGroups());
    await waitFor(() => expect(result.current.denied).toBe(true));

    listMock.mockRejectedValue(new ConnectError("boom", Code.Internal));
    await vi.advanceTimersByTimeAsync(5 * 60 * 1000);
    await waitFor(() => expect(result.current.error).toBe("boom"));
    expect(result.current.denied).toBe(false);
  });

  it("ignores a reply that a newer poll already superseded", async () => {
    const stale = deferredPage();
    const fresh = deferredPage();
    listMock.mockReturnValueOnce(stale.promise).mockReturnValueOnce(fresh.promise);

    // Leaving a headerless route and returning starts a second poll while the first is still in flight.
    const { result, rerender } = renderHook(({ enabled }) => useActiveAlertGroups({ enabled }), {
      initialProps: { enabled: true },
    });
    rerender({ enabled: false });
    rerender({ enabled: true });

    fresh.settle({ groups: [buildGroup("Hashrate dropped")], has_more: false });
    await waitFor(() => expect(result.current.groups).toHaveLength(1));

    // The older request answers last, describing a fleet state the newer reply has already replaced.
    stale.settle({ groups: [], has_more: false });
    await waitFor(() => expect(listMock).toHaveBeenCalledTimes(2));
    expect(result.current.groups.map((group) => group.alert_name)).toEqual(["Hashrate dropped"]);
  });
});
