import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import SchedulesPage from "./SchedulesPage";

const mockUseScheduleApi = vi.fn();
const mockPushToast = vi.fn();

vi.mock("@/shared/features/toaster", () => ({
  pushToast: (...args: unknown[]) => mockPushToast(...args),
  STATUSES: {
    error: "error",
  },
}));

vi.mock("@/protoFleet/api/useScheduleApi", () => ({
  __esModule: true,
  default: () => mockUseScheduleApi(),
}));

const createSchedule = (overrides: Partial<Record<string, unknown>> = {}) => ({
  id: "1",
  priority: 1,
  name: "Night sleep",
  targetSummary: "Applies to all miners",
  scheduleSummary: "Weekdays · 10:00 PM",
  nextRunSummary: "Runs tomorrow at 10:00 PM",
  action: "sleep",
  status: "active",
  createdBy: "Negar Naghshbandi",
  ...overrides,
});

const createDeferred = <T,>() => {
  let resolve!: (value: T | PromiseLike<T>) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });

  return { promise, resolve, reject };
};

describe("SchedulesPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockPushToast.mockReset();

    mockUseScheduleApi.mockReturnValue({
      schedules: [],
      isLoading: false,
      refreshSchedules: vi.fn().mockResolvedValue(undefined),
      createSchedule: vi.fn().mockResolvedValue(undefined),
      updateSchedule: vi.fn().mockResolvedValue(undefined),
      pauseSchedule: vi.fn().mockResolvedValue(undefined),
      resumeSchedule: vi.fn().mockResolvedValue(undefined),
      deleteSchedule: vi.fn().mockResolvedValue(undefined),
      reorderSchedules: vi.fn().mockResolvedValue(undefined),
    });
  });

  it("keeps the loading state visible until the initial schedules load finishes", async () => {
    const deferred = createDeferred<void>();

    mockUseScheduleApi.mockReturnValue({
      schedules: [],
      isLoading: false,
      refreshSchedules: vi.fn().mockReturnValue(deferred.promise),
      createSchedule: vi.fn().mockResolvedValue(undefined),
      updateSchedule: vi.fn().mockResolvedValue(undefined),
      pauseSchedule: vi.fn().mockResolvedValue(undefined),
      resumeSchedule: vi.fn().mockResolvedValue(undefined),
      deleteSchedule: vi.fn().mockResolvedValue(undefined),
      reorderSchedules: vi.fn().mockResolvedValue(undefined),
    });

    render(<SchedulesPage />);

    expect(screen.queryByText("Configure schedules to automate actions for your miners.")).not.toBeInTheDocument();

    deferred.resolve(undefined);

    await waitFor(() =>
      expect(screen.getByText("Configure schedules to automate actions for your miners.")).toBeVisible(),
    );
  });

  it("renders the empty schedules state when no schedules exist", async () => {
    render(<SchedulesPage />);

    await waitFor(() => expect(screen.getAllByText("Schedules")).toHaveLength(1));
    expect(screen.getByText("Configure schedules to automate actions for your miners.")).toBeVisible();
    expect(screen.getByRole("button", { name: "Add a schedule" })).toBeDisabled();
    expect(screen.queryByRole("columnheader", { name: "Name" })).not.toBeInTheDocument();
  });

  it("renders the populated schedules table", async () => {
    mockUseScheduleApi.mockReturnValue({
      schedules: [createSchedule()],
      isLoading: false,
      refreshSchedules: vi.fn().mockResolvedValue(undefined),
      createSchedule: vi.fn().mockResolvedValue(undefined),
      updateSchedule: vi.fn().mockResolvedValue(undefined),
      pauseSchedule: vi.fn().mockResolvedValue(undefined),
      resumeSchedule: vi.fn().mockResolvedValue(undefined),
      deleteSchedule: vi.fn().mockResolvedValue(undefined),
      reorderSchedules: vi.fn().mockResolvedValue(undefined),
    });

    render(<SchedulesPage />);

    await waitFor(() => expect(screen.getByRole("columnheader", { name: "Reorder" })).toBeInTheDocument());
    expect(screen.getByRole("columnheader", { name: "Name" })).toBeInTheDocument();
    expect(screen.getByText("Night sleep")).toBeVisible();
    expect(screen.getByText("Weekdays · 10:00 PM")).toBeVisible();
  });

  it("shows an error toast when schedules fail to load", async () => {
    mockUseScheduleApi.mockReturnValue({
      schedules: [createSchedule()],
      isLoading: false,
      refreshSchedules: vi.fn().mockRejectedValue(new Error("Load failed")),
      createSchedule: vi.fn().mockResolvedValue(undefined),
      updateSchedule: vi.fn().mockResolvedValue(undefined),
      pauseSchedule: vi.fn().mockResolvedValue(undefined),
      resumeSchedule: vi.fn().mockResolvedValue(undefined),
      deleteSchedule: vi.fn().mockResolvedValue(undefined),
      reorderSchedules: vi.fn().mockResolvedValue(undefined),
    });

    render(<SchedulesPage />);

    await waitFor(() =>
      expect(mockPushToast).toHaveBeenCalledWith(
        expect.objectContaining({
          message: "Load failed",
          status: "error",
        }),
      ),
    );
  });
});
