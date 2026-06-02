import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { useSchedulePillData } from "./useSchedulePillData";
import { useScheduleApiContext } from "@/protoFleet/api/ScheduleApiContext";
import { SCHEDULES_CHANGED_EVENT } from "@/protoFleet/api/scheduleEvents";
import { useHasPermission } from "@/protoFleet/store";

vi.mock("@/protoFleet/api/ScheduleApiContext", () => ({
  useScheduleApiContext: vi.fn(),
}));

vi.mock("@/protoFleet/store", () => ({
  useHasPermission: vi.fn(),
}));

vi.mock("@/shared/features/toaster", () => ({
  pushToast: vi.fn(),
  STATUSES: {
    error: "error",
  },
}));

describe("useSchedulePillData", () => {
  const refreshSchedules = vi.fn().mockResolvedValue([]);

  beforeEach(() => {
    vi.useFakeTimers();
    refreshSchedules.mockClear();
    vi.mocked(useHasPermission).mockReturnValue(true);
    vi.mocked(useScheduleApiContext).mockReturnValue({
      schedules: [],
      isLoading: false,
      listSchedules: vi.fn().mockResolvedValue([]),
      refreshSchedules,
      createSchedule: vi.fn().mockResolvedValue(undefined),
      updateSchedule: vi.fn().mockResolvedValue(undefined),
      pauseSchedule: vi.fn(),
      resumeSchedule: vi.fn(),
      deleteSchedule: vi.fn().mockResolvedValue(undefined),
      reorderSchedules: vi.fn().mockResolvedValue(undefined),
    });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("refreshes immediately and on the polling interval", async () => {
    renderHook(() => useSchedulePillData());

    expect(refreshSchedules).toHaveBeenCalledTimes(1);
    expect(refreshSchedules).toHaveBeenNthCalledWith(1, { background: true });

    await act(async () => {
      vi.advanceTimersByTime(30_000);
    });

    expect(refreshSchedules).toHaveBeenCalledTimes(2);
    expect(refreshSchedules).toHaveBeenNthCalledWith(2, { background: true });
  });

  it("does not refetch immediately for same-tab schedule mutation events", async () => {
    renderHook(() => useSchedulePillData());

    refreshSchedules.mockClear();

    await act(async () => {
      window.dispatchEvent(new CustomEvent(SCHEDULES_CHANGED_EVENT));
    });

    expect(refreshSchedules).not.toHaveBeenCalled();
  });

  it("does not poll for sessions without schedule:manage", async () => {
    // The hook gates polling on schedule:manage (see useSchedulePillData.ts):
    // the pill is a "schedules-in-flight" affordance that only acts on
    // editable schedules, so :read sessions get no header surface and
    // skipping the 30s poll loop avoids PermissionDenied every tick for
    // sessions without :manage.
    vi.mocked(useHasPermission).mockReturnValue(false);

    renderHook(() => useSchedulePillData());

    expect(refreshSchedules).not.toHaveBeenCalled();

    await act(async () => {
      vi.advanceTimersByTime(30_000);
    });

    expect(refreshSchedules).not.toHaveBeenCalled();
  });
});
