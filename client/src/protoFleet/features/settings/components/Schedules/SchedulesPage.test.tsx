import type { ReactElement } from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import SchedulesPage from "./SchedulesPage";
import type { Schedule } from "@/protoFleet/api/generated/schedule/v1/schedule_pb";
import { useScheduleApiContext } from "@/protoFleet/api/ScheduleApiContext";
import type { ScheduleListItem } from "@/protoFleet/api/useScheduleApi";

const mockPushToast = vi.fn();
const mockScheduleModal = vi.fn<(props: { open: boolean }) => ReactElement | null>(() => null);

vi.mock("@/shared/features/toaster", () => ({
  pushToast: (...args: unknown[]) => mockPushToast(...args),
  STATUSES: {
    error: "error",
  },
}));

vi.mock("@/protoFleet/api/ScheduleApiContext", () => ({
  useScheduleApiContext: vi.fn(),
}));

vi.mock("@/protoFleet/features/settings/components/Schedules/ScheduleModal", () => ({
  __esModule: true,
  default: (props: { open: boolean }) => mockScheduleModal(props),
}));

const createSchedule = (overrides: Partial<ScheduleListItem> = {}): ScheduleListItem => ({
  id: "1",
  priority: 1,
  name: "Night sleep",
  targetSummary: "Applies to all miners",
  scheduleSummary: "Weekdays · 10:00 PM",
  nextRunSummary: "Runs tomorrow at 10:00 PM",
  action: "sleep",
  status: "active",
  createdBy: "Negar Naghshbandi",
  rawSchedule: {} as Schedule,
  ...overrides,
});

const createScheduleApiContextValue = (
  overrides: Partial<ReturnType<typeof useScheduleApiContext>> = {},
): ReturnType<typeof useScheduleApiContext> => ({
  schedules: [],
  isLoading: false,
  listSchedules: vi.fn().mockResolvedValue([]),
  refreshSchedules: vi.fn().mockResolvedValue(undefined),
  createSchedule: vi.fn().mockResolvedValue(undefined),
  updateSchedule: vi.fn().mockResolvedValue(undefined),
  pauseSchedule: vi.fn().mockResolvedValue(undefined),
  resumeSchedule: vi.fn().mockResolvedValue(undefined),
  deleteSchedule: vi.fn().mockResolvedValue(undefined),
  reorderSchedules: vi.fn().mockResolvedValue(undefined),
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
    mockScheduleModal.mockReset();
    mockScheduleModal.mockImplementation(({ open }: { open: boolean }) => (open ? <div>Schedule modal</div> : null));

    vi.mocked(useScheduleApiContext).mockReturnValue(createScheduleApiContextValue());
  });

  it("keeps the loading state visible until the initial schedules load finishes", async () => {
    const deferred = createDeferred<void>();

    vi.mocked(useScheduleApiContext).mockReturnValue(
      createScheduleApiContextValue({
        refreshSchedules: vi.fn().mockReturnValue(deferred.promise),
      }),
    );

    render(<SchedulesPage />);

    expect(screen.queryByText(/No schedules yet/)).not.toBeInTheDocument();

    deferred.resolve(undefined);

    await waitFor(() => expect(screen.getByText(/No schedules yet/)).toBeVisible());
  });

  it("renders the empty schedules state when no schedules exist", async () => {
    render(<SchedulesPage />);

    await waitFor(() => expect(screen.getAllByText("Schedules")).toHaveLength(1));
    expect(screen.getByText(/No schedules yet/)).toBeVisible();
    expect(screen.getByRole("button", { name: "Add a schedule" })).toBeEnabled();
    expect(mockScheduleModal).not.toHaveBeenCalled();
  });

  it("opens the schedule modal from the add schedule button", async () => {
    render(<SchedulesPage />);

    await waitFor(() => expect(screen.getByRole("button", { name: "Add a schedule" })).toBeEnabled());
    fireEvent.click(screen.getByRole("button", { name: "Add a schedule" }));

    expect(screen.getByText("Schedule modal")).toBeVisible();
  });

  it("renders the populated schedules table", async () => {
    vi.mocked(useScheduleApiContext).mockReturnValue(
      createScheduleApiContextValue({
        schedules: [createSchedule()],
      }),
    );

    render(<SchedulesPage />);

    await waitFor(() => expect(screen.getByRole("columnheader", { name: "Reorder" })).toBeInTheDocument());
    expect(screen.getByRole("columnheader", { name: "Name" })).toBeInTheDocument();
    expect(screen.getByText("Night sleep")).toBeVisible();
    expect(screen.getByText("Weekdays · 10:00 PM")).toBeVisible();
  });

  it("shows an error toast when schedules fail to load", async () => {
    vi.mocked(useScheduleApiContext).mockReturnValue(
      createScheduleApiContextValue({
        schedules: [createSchedule()],
        refreshSchedules: vi.fn().mockRejectedValue(new Error("Load failed")),
      }),
    );

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
