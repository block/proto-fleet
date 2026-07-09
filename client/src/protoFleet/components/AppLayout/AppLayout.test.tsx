import type { ReactNode } from "react";
import { MemoryRouter } from "react-router-dom";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import AppLayout from "./AppLayout";
import type { ScheduleListItem } from "@/protoFleet/api/useScheduleApi";
import type { CurtailmentPillEvent } from "@/protoFleet/components/PageHeader/CurtailmentPill";
import type { UseSchedulePillDataResult } from "@/protoFleet/components/PageHeader/useSchedulePillData";
import { useHasPermission } from "@/protoFleet/store";

const mockUseWindowDimensions = vi.fn();
const mockUseReactiveLocalStorage = vi.fn();
const mockUseCurtailmentPillData = vi.fn();
const mockUseSchedulePillData = vi.fn();

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
  default: () => <div>Navigation menu</div>,
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
    mockUseWindowDimensions.mockReturnValue({
      width: 375,
      isPhone: true,
    });
    mockUseReactiveLocalStorage.mockReturnValue([false, vi.fn()]);
    mockUseCurtailmentPillData.mockReturnValue({ activeEvent: null });
    mockUseSchedulePillData.mockReturnValue(createSchedulePillData());
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
      "overflow-x-hidden",
      "overflow-y-auto",
      "overscroll-x-none",
    );
  });

  it.each(["/sites/1", "/buildings/1", "/racks/1", "/groups/Ops"])(
    "hides the shell header and top offset on detail route %s",
    (initialEntry) => {
      render(
        <MemoryRouter initialEntries={[initialEntry]}>
          <AppLayout>
            <div>Body content</div>
          </AppLayout>
        </MemoryRouter>,
      );

      expect(screen.queryByText("Page header")).not.toBeInTheDocument();
      expect(screen.getByText("Body content").parentElement).toHaveClass("top-0");
      expect(screen.getByText("Body content").parentElement).not.toHaveClass("phone:top-[calc(theme(spacing.1)*12)]");
    },
  );

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
