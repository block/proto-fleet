import type { ReactNode } from "react";
import { MemoryRouter } from "react-router-dom";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import AppLayout from "./AppLayout";
import type { ScheduleListItem } from "@/protoFleet/api/useScheduleApi";
import type { CurtailmentPillEvent } from "@/protoFleet/components/PageHeader/CurtailmentPill";
import type { UseSchedulePillDataResult } from "@/protoFleet/components/PageHeader/useSchedulePillData";
import { useHasPermission } from "@/protoFleet/store";

const mockUseWindowDimensions = vi.fn();
const mockUseReactiveLocalStorage = vi.fn();
const mockUseCurtailmentPillData = vi.fn();
const mockUseActiveAlertsPillData = vi.fn();
const mockUseSchedulePillData = vi.fn();
const mockUseUpdateIndicator = vi.fn();

vi.mock("@/protoFleet/api/ScheduleApiProvider", () => ({
  ScheduleApiProvider: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

// AppLayout now wraps its content in SitesProvider; the catalog fetch isn't
// under test here (PageHeader is mocked), so stub it as a passthrough.
vi.mock("@/protoFleet/api/SitesProvider", () => ({
  SitesProvider: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock("@/protoFleet/components/NavigationMenu", () => ({
  __esModule: true,
  default: ({ isVisible }: { isVisible?: boolean }) => (isVisible ? <div>Navigation menu</div> : null),
}));

vi.mock("@/protoFleet/components/PageHeader", () => ({
  __esModule: true,
  default: () => <div>Page header</div>,
}));

vi.mock("@/protoFleet/components/PageHeader/useSchedulePillData", () => ({
  useSchedulePillData: () => mockUseSchedulePillData(),
}));

vi.mock("@/protoFleet/components/PageHeader/useCurtailmentPillData", () => ({
  useCurtailmentPillData: () => mockUseCurtailmentPillData(),
}));

vi.mock("@/protoFleet/components/PageHeader/useActiveAlertsPillData", () => ({
  useActiveAlertsPillData: (options: { enabled?: boolean }) => mockUseActiveAlertsPillData(options),
}));

vi.mock("@/protoFleet/features/updates/useUpdateIndicator", () => ({
  useUpdateIndicator: (options: { enabled?: boolean }) => mockUseUpdateIndicator(options),
}));

vi.mock("@/shared/hooks/useWindowDimensions", () => ({
  useWindowDimensions: () => mockUseWindowDimensions(),
}));

vi.mock("@/shared/hooks/useReactiveLocalStorage", () => ({
  useReactiveLocalStorage: () => mockUseReactiveLocalStorage(),
}));

vi.mock("@/protoFleet/store", () => ({
  useHasPermission: vi.fn(),
}));

const createPillSchedule = (): ScheduleListItem =>
  ({
    id: "1",
    priority: 1,
    name: "Night reboot",
    targetSummary: "Applies to all miners",
    scheduleSummary: "Weekdays · 10:00 PM",
    nextRunSummary: "Runs tomorrow at 10:00 PM",
    action: "sleep",
    status: "active",
    createdBy: "Review",
    rawSchedule: {},
  }) as ScheduleListItem;

const createSchedulePillData = (overrides: Partial<UseSchedulePillDataResult> = {}): UseSchedulePillDataResult => {
  const pillSchedule = overrides.pillSchedule ?? null;

  return {
    sections: [],
    pendingScheduleId: null,
    onToggleScheduleStatus: vi.fn(),
    ...overrides,
    pillSchedule,
    hasVisibleSchedules: pillSchedule !== null,
  };
};

const activeCurtailmentEvent: CurtailmentPillEvent = {
  reason: "Grid peak call",
  state: "curtailing",
  scopeLabel: "Whole fleet",
  selectedMiners: 48,
  estimatedReductionKw: 126.4,
  targetMetricsAvailable: true,
};

describe("AppLayout", () => {
  beforeEach(() => {
    // Without this, a `toHaveBeenCalledWith` assertion matches any earlier test's render instead of its own.
    vi.clearAllMocks();
    mockUseWindowDimensions.mockReturnValue({
      width: 375,
      isPhone: true,
    });
    mockUseReactiveLocalStorage.mockReturnValue([false, vi.fn()]);
    mockUseCurtailmentPillData.mockReturnValue({ activeEvent: null });
    mockUseActiveAlertsPillData.mockReturnValue({ groups: [], error: null, hasMore: false, hasVisiblePill: false });
    mockUseSchedulePillData.mockReturnValue(createSchedulePillData());
    mockUseUpdateIndicator.mockReturnValue(null);
    vi.mocked(useHasPermission).mockReturnValue(true);
  });

  it("keeps the base phone content offset when the only schedule widget fits inline", () => {
    mockUseSchedulePillData.mockReturnValue(
      createSchedulePillData({
        pillSchedule: createPillSchedule(),
      }),
    );

    render(
      <MemoryRouter>
        <AppLayout>
          <div>Body content</div>
        </AppLayout>
      </MemoryRouter>,
    );

    expect(screen.getByText("Body content").parentElement).toHaveClass("phone:top-[calc(theme(spacing.1)*12)]");
  });

  it("keeps mobile content views from becoming page-level horizontal scrollers", () => {
    render(
      <MemoryRouter>
        <AppLayout>
          <div>Body content</div>
        </AppLayout>
      </MemoryRouter>,
    );

    expect(screen.getByText("Body content").parentElement).toHaveClass(
      "overflow-y-auto",
      "phone:overflow-x-hidden",
      "phone:overscroll-x-none",
      "tablet-only:overflow-x-hidden",
      "tablet-only:overscroll-x-none",
    );
    expect(screen.getByText("Body content").parentElement).not.toHaveClass("overflow-x-hidden");
  });

  it("hides the shell header and top offset when the matched route opts in", () => {
    render(
      <MemoryRouter>
        <AppLayout hideShellHeader>
          <div>Body content</div>
        </AppLayout>
      </MemoryRouter>,
    );

    expect(screen.queryByText("Page header")).not.toBeInTheDocument();
    expect(screen.getByText("Body content").parentElement).toHaveClass("top-0");
    expect(screen.getByText("Body content").parentElement).toHaveClass("phone:pt-12", "tablet-only:pt-12");
    expect(screen.getByText("Body content").parentElement).not.toHaveClass("phone:top-[calc(theme(spacing.1)*12)]");
    expect(screen.getByTestId("navigation-menu-button")).toBeInTheDocument();
  });

  it("opens navigation from the detail route mobile menu trigger", () => {
    render(
      <MemoryRouter>
        <AppLayout hideShellHeader>
          <div>Body content</div>
        </AppLayout>
      </MemoryRouter>,
    );

    expect(screen.queryByText("Navigation menu")).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId("navigation-menu-button"));

    expect(screen.getByText("Navigation menu")).toBeInTheDocument();
    expect(screen.queryByTestId("navigation-menu-button")).not.toBeInTheDocument();
  });

  it("keeps the shell header and top offset on non-detail routes", () => {
    render(
      <MemoryRouter initialEntries={["/fleet/miners"]}>
        <AppLayout>
          <div>Body content</div>
        </AppLayout>
      </MemoryRouter>,
    );

    expect(screen.getByText("Page header")).toBeInTheDocument();
    expect(screen.getByText("Body content").parentElement).toHaveClass("top-[calc(theme(spacing.1)*12)]");
  });

  it("uses the two-widget phone content offset when all three header widgets are visible", () => {
    mockUseReactiveLocalStorage.mockReturnValue([true, vi.fn()]);
    mockUseCurtailmentPillData.mockReturnValue({ activeEvent: activeCurtailmentEvent });
    mockUseSchedulePillData.mockReturnValue(
      createSchedulePillData({
        pillSchedule: createPillSchedule(),
      }),
    );

    render(
      <MemoryRouter>
        <AppLayout>
          <div>Body content</div>
        </AppLayout>
      </MemoryRouter>,
    );

    expect(screen.getByText("Body content").parentElement).toHaveClass("phone:top-[calc(theme(spacing.1)*12+80px)]");
  });

  it("uses the single-widget phone content offset when one widget remains below the header", () => {
    mockUseReactiveLocalStorage.mockReturnValue([true, vi.fn()]);
    mockUseSchedulePillData.mockReturnValue(
      createSchedulePillData({
        pillSchedule: createPillSchedule(),
      }),
    );

    render(
      <MemoryRouter>
        <AppLayout>
          <div>Body content</div>
        </AppLayout>
      </MemoryRouter>,
    );

    expect(screen.getByText("Body content").parentElement).toHaveClass("phone:top-[calc(theme(spacing.1)*12+40px)]");
  });

  it("uses the three-widget phone content offset when the update indicator makes four widgets visible", () => {
    mockUseReactiveLocalStorage.mockReturnValue([true, vi.fn()]);
    mockUseCurtailmentPillData.mockReturnValue({ activeEvent: activeCurtailmentEvent });
    mockUseSchedulePillData.mockReturnValue(
      createSchedulePillData({
        pillSchedule: createPillSchedule(),
      }),
    );
    mockUseUpdateIndicator.mockReturnValue({ version: "v1.3.0", onClick: vi.fn() });

    render(
      <MemoryRouter>
        <AppLayout>
          <div>Body content</div>
        </AppLayout>
      </MemoryRouter>,
    );

    expect(screen.getByText("Body content").parentElement).toHaveClass("phone:top-[calc(theme(spacing.1)*12+120px)]");
  });

  it("uses the four-widget phone content offset when a firing alert makes five widgets visible", () => {
    mockUseReactiveLocalStorage.mockReturnValue([true, vi.fn()]);
    mockUseActiveAlertsPillData.mockReturnValue({
      groups: [{ key: "miner|offline" }],
      error: null,
      hasMore: false,
      hasVisiblePill: true,
    });
    mockUseCurtailmentPillData.mockReturnValue({ activeEvent: activeCurtailmentEvent });
    mockUseSchedulePillData.mockReturnValue(createSchedulePillData({ pillSchedule: createPillSchedule() }));
    mockUseUpdateIndicator.mockReturnValue({ version: "v1.3.0", onClick: vi.fn() });

    render(
      <MemoryRouter>
        <AppLayout>
          <div>Body content</div>
        </AppLayout>
      </MemoryRouter>,
    );

    // The alerts pill takes the inline slot, leaving four stacked pills that the three-row ladder would clip.
    expect(screen.getByText("Body content").parentElement).toHaveClass("phone:top-[calc(theme(spacing.1)*12+160px)]");
  });

  it("disables update polling when the route hides the shell header", () => {
    render(
      <MemoryRouter>
        <AppLayout hideShellHeader>
          <div>Body content</div>
        </AppLayout>
      </MemoryRouter>,
    );

    expect(mockUseUpdateIndicator).toHaveBeenCalledWith({ enabled: false });
  });

  it("disables active-alert polling when the route hides the shell header", () => {
    render(
      <MemoryRouter>
        <AppLayout hideShellHeader>
          <div>Body content</div>
        </AppLayout>
      </MemoryRouter>,
    );

    expect(mockUseActiveAlertsPillData).toHaveBeenCalledWith({ enabled: false });
  });

  it("offsets the phone content for a firing alert so the pill has a row", () => {
    mockUseActiveAlertsPillData.mockReturnValue({
      groups: [{ key: "miner|offline" }],
      error: null,
      hasMore: false,
      hasVisiblePill: true,
    });
    mockUseSchedulePillData.mockReturnValue(createSchedulePillData({ pillSchedule: createPillSchedule() }));

    render(
      <MemoryRouter>
        <AppLayout>
          <div>Body content</div>
        </AppLayout>
      </MemoryRouter>,
    );

    // The alerts pill takes the inline slot, pushing the schedule pill into a row of its own.
    expect(screen.getByText("Body content").parentElement).toHaveClass("phone:top-[calc(theme(spacing.1)*12+40px)]");
  });

  it("keeps the base phone content offset when the only curtailment widget fits inline", () => {
    mockUseCurtailmentPillData.mockReturnValue({ activeEvent: activeCurtailmentEvent });

    render(
      <MemoryRouter>
        <AppLayout>
          <div>Body content</div>
        </AppLayout>
      </MemoryRouter>,
    );

    expect(screen.getByText("Body content").parentElement).toHaveClass("phone:top-[calc(theme(spacing.1)*12)]");
  });

  it("does not offset the phone content for active curtailment without read permission", () => {
    vi.mocked(useHasPermission).mockReturnValue(false);
    mockUseCurtailmentPillData.mockReturnValue({ activeEvent: activeCurtailmentEvent });

    render(
      <MemoryRouter>
        <AppLayout>
          <div>Body content</div>
        </AppLayout>
      </MemoryRouter>,
    );

    expect(screen.getByText("Body content").parentElement).toHaveClass("phone:top-[calc(theme(spacing.1)*12)]");
  });
});
