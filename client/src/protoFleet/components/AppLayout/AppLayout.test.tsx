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
  default: ({ isVisible }: { isVisible?: boolean }) => (isVisible ? <div>Navigation menu</div> : null),
}));

vi.mock("@/protoFleet/components/PageHeader", () => ({
  __esModule: true,
  default: () => <div>Page header</div>,
}));

vi.mock("@/protoFleet/features/aiChat", () => ({
  ChatFab: () => <div data-testid="ai-chat-fab" />,
  ChatPanel: () => <div data-testid="ai-chat-panel" />,
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

  it("shows AI chat when the operator can read the fleet", () => {
    render(
      <MemoryRouter>
        <AppLayout>
          <div>Body content</div>
        </AppLayout>
      </MemoryRouter>,
    );

    expect(screen.getByTestId("ai-chat-fab")).toBeInTheDocument();
    expect(screen.getByTestId("ai-chat-panel")).toBeInTheDocument();
  });

  it("hides AI chat when the operator cannot read the fleet", () => {
    vi.mocked(useHasPermission).mockImplementation((permission) => permission !== "fleet:read");

    render(
      <MemoryRouter>
        <AppLayout>
          <div>Body content</div>
        </AppLayout>
      </MemoryRouter>,
    );

    expect(screen.queryByTestId("ai-chat-fab")).not.toBeInTheDocument();
    expect(screen.queryByTestId("ai-chat-panel")).not.toBeInTheDocument();
  });
});
