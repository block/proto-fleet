import { MemoryRouter } from "react-router-dom";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import PageHeader from "./PageHeader";
import type { UseCurtailmentPillDataResult } from "./useCurtailmentPillData";
import type { UseSchedulePillDataResult } from "./useSchedulePillData";
import type { ScheduleListItem } from "@/protoFleet/api/useScheduleApi";

const mockUseWindowDimensions = vi.fn();
const mockUseReactiveLocalStorage = vi.fn();

vi.mock("./LocationSelector", () => ({
  default: () => <div>Location selector</div>,
}));

vi.mock("./SchedulePill", () => ({
  __esModule: true,
  default: ({ pillSchedule }: { pillSchedule: { name: string } }) => <div>{pillSchedule.name}</div>,
}));

vi.mock("./CurtailmentPill", () => ({
  __esModule: true,
  default: ({ event }: { event: { reason: string } }) => <div>{event.reason}</div>,
}));

vi.mock("@/shared/hooks/useWindowDimensions", () => ({
  useWindowDimensions: () => mockUseWindowDimensions(),
}));

vi.mock("@/shared/hooks/useReactiveLocalStorage", () => ({
  useReactiveLocalStorage: () => mockUseReactiveLocalStorage(),
}));

vi.mock("@/shared/assets/icons", () => ({
  Pause: ({ ariaLabel }: { ariaLabel?: string }) => <button aria-label={ariaLabel}>menu</button>,
}));
const createPillSchedule = (name: string): ScheduleListItem =>
  ({
    id: "1",
    priority: 1,
    name,
    targetSummary: "Applies to all miners",
    scheduleSummary: "Weekdays · 10:00 PM",
    nextRunSummary: "Runs tomorrow at 10:00 PM",
    action: "sleep",
    status: "active",
    createdBy: "Review",
    rawSchedule: {},
  }) as ScheduleListItem;

const createSchedulePillData = (overrides: Partial<UseSchedulePillDataResult> = {}): UseSchedulePillDataResult => ({
  hasVisibleSchedules: false,
  pillSchedule: null,
  sections: [],
  pendingScheduleId: null,
  onToggleScheduleStatus: vi.fn(),
  ...overrides,
});

const createCurtailmentPillData = (
  overrides: Partial<UseCurtailmentPillDataResult> = {},
): UseCurtailmentPillDataResult => ({
  activeEvent: null,
  refreshActiveCurtailment: vi.fn(),
  ...overrides,
});

interface RenderPageHeaderOptions {
  curtailmentPillData?: UseCurtailmentPillDataResult;
  schedulePillData?: UseSchedulePillDataResult;
}

function renderPageHeader({
  curtailmentPillData = createCurtailmentPillData(),
  schedulePillData = createSchedulePillData(),
}: RenderPageHeaderOptions = {}): void {
  render(
    <MemoryRouter>
      <PageHeader curtailmentPillData={curtailmentPillData} schedulePillData={schedulePillData} />
    </MemoryRouter>,
  );
}

describe("PageHeader", () => {
  beforeEach(() => {
    mockUseWindowDimensions.mockReturnValue({
      isPhone: true,
      isTablet: false,
    });
    mockUseReactiveLocalStorage.mockReturnValue([false, vi.fn()]);
  });

  it("shows the phone widget row when schedules are available even if setup is not dismissed", () => {
    const schedulePillData = createSchedulePillData({
      hasVisibleSchedules: true,
      pillSchedule: createPillSchedule("Night reboot"),
    });

    renderPageHeader({ schedulePillData });

    expect(screen.getByText("Night reboot")).toBeVisible();
  });

  it("shows the phone widget row when an energy event is active even if setup is not dismissed", () => {
    const curtailmentPillData = createCurtailmentPillData({
      activeEvent: {
        reason: "Grid peak call",
        state: "active",
        scopeLabel: "Whole org",
        selectedMiners: 12,
        estimatedReductionKw: 40,
      },
    });

    renderPageHeader({ curtailmentPillData });

    expect(screen.getByText("Grid peak call")).toBeVisible();
  });

  it("keeps the phone widget row hidden when neither setup nor schedules need space", () => {
    renderPageHeader();

    expect(screen.queryByText("Continue setup")).not.toBeInTheDocument();
    expect(screen.queryByText("Night reboot")).not.toBeInTheDocument();
    expect(screen.queryByText("Grid peak call")).not.toBeInTheDocument();
  });
});
