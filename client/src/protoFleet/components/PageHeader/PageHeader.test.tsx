import { MemoryRouter } from "react-router-dom";
import { render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import PageHeader from "./PageHeader";
import type { UseSchedulePillDataResult } from "./useSchedulePillData";
import type { ScheduleListItem } from "@/protoFleet/api/useScheduleApi";
import { SiteScopeProvider } from "@/protoFleet/routing/siteScope";
import { useHasPermission } from "@/protoFleet/store";
import { DEFAULT_ACTIVE_SITE } from "@/protoFleet/store/types/activeSite";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

const mockUseWindowDimensions = vi.fn();
const mockUseReactiveLocalStorage = vi.fn();
const mockCurtailmentPill = vi.fn();
const mockListSites = vi.fn();

vi.mock("./CurtailmentPill", () => ({
  default: (props: { detailsPath?: string }) => {
    mockCurtailmentPill(props);
    return <div>Curtailment pill</div>;
  },
}));

vi.mock("./LocationSelector", () => ({
  default: () => <div>Location selector</div>,
}));

vi.mock("./SchedulePill", () => ({
  __esModule: true,
  default: ({ pillSchedule }: { pillSchedule: { name: string } }) => <div>{pillSchedule.name}</div>,
}));

vi.mock("@/protoFleet/api/sites", () => ({
  useSites: () => ({
    listSites: mockListSites,
  }),
  // SitePicker imports this; stub keeps the picker from throwing.
  buildKnownSiteIds: () => new Set<string>(),
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

describe("PageHeader", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockListSites.mockReturnValue(undefined);
    mockUseWindowDimensions.mockReturnValue({
      width: 375,
      isPhone: true,
      isTablet: false,
    });
    mockUseReactiveLocalStorage.mockReturnValue([false, vi.fn()]);
    vi.mocked(useHasPermission).mockReturnValue(true);
    useFleetStore.setState((state) => {
      state.ui.activeSite = DEFAULT_ACTIVE_SITE;
    });
  });

  it("shows the phone header widget when schedules are available even if setup is not dismissed", () => {
    const schedulePillData = createSchedulePillData({
      hasVisibleSchedules: true,
      pillSchedule: createPillSchedule("Night reboot"),
    });

    render(
      <MemoryRouter>
        <PageHeader schedulePillData={schedulePillData} />
      </MemoryRouter>,
    );

    expect(screen.getByText("Night reboot")).toBeVisible();
  });

  it("places the first phone widget in the top row and stacks the remaining widgets", () => {
    mockUseReactiveLocalStorage.mockReturnValue([true, vi.fn()]);
    const schedulePillData = createSchedulePillData({
      hasVisibleSchedules: true,
      pillSchedule: createPillSchedule("Night reboot"),
    });

    render(
      <MemoryRouter>
        <PageHeader
          schedulePillData={schedulePillData}
          activeCurtailmentEvent={{
            reason: "Grid peak call",
            state: "curtailing",
            scopeLabel: "Whole fleet",
            selectedMiners: 48,
            estimatedReductionKw: 126.4,
            targetMetricsAvailable: true,
          }}
        />
      </MemoryRouter>,
    );

    const inlineWidgets = screen.getByTestId("page-header-inline-widgets");
    const mobileWidgets = screen.getByTestId("page-header-mobile-widgets");

    expect(screen.getByTestId("page-header-content")).toHaveClass(
      "grid",
      "grid-cols-[minmax(0,1fr)_minmax(0,min(15rem,45vw))]",
    );
    expect(screen.getByTestId("page-header-location-area")).toHaveClass("min-w-0");
    expect(screen.getByTestId("page-header-location-area")).not.toHaveClass("flex-1");
    expect(screen.getByTestId("page-header-selector-area")).toHaveClass("min-w-0", "flex-1");
    expect(within(inlineWidgets).getByText("Curtailment pill")).toBeVisible();
    expect(inlineWidgets).toHaveClass("min-w-0", "overflow-hidden");
    expect(inlineWidgets).not.toHaveClass("ml-3");
    expect(inlineWidgets).not.toHaveClass("shrink-0");
    expect(within(mobileWidgets).queryByText("Curtailment pill")).not.toBeInTheDocument();
    expect(within(mobileWidgets).getByText("Night reboot")).toBeVisible();
    expect(within(mobileWidgets).getByText("Continue setup")).toBeVisible();
    expect(mobileWidgets).toHaveClass("flex-col", "items-end", "gap-2");
    expect(mobileWidgets).not.toHaveClass("gap-3");
    expect(screen.getByTestId("phone-header-widget-row")).toHaveClass("h-[80px]");
  });

  it("constrains the setup button when it is the inline phone widget", () => {
    mockUseReactiveLocalStorage.mockReturnValue([true, vi.fn()]);

    render(
      <MemoryRouter>
        <PageHeader schedulePillData={createSchedulePillData()} />
      </MemoryRouter>,
    );

    const inlineWidgets = screen.getByTestId("page-header-inline-widgets");
    const setupButton = within(inlineWidgets).getByRole("button", { name: "Continue setup" });
    const setupLabel = within(setupButton).getByText("Continue setup");

    expect(setupButton).toHaveClass("min-w-0", "max-w-full", "overflow-hidden");
    expect(setupLabel).toHaveClass("truncate");
    expect(screen.queryByTestId("phone-header-widget-row")).not.toBeInTheDocument();
  });

  it("keeps the phone widget row hidden when neither setup nor schedules need space", () => {
    render(
      <MemoryRouter>
        <PageHeader schedulePillData={createSchedulePillData()} />
      </MemoryRouter>,
    );

    expect(screen.queryByText("Continue setup")).not.toBeInTheDocument();
    expect(screen.queryByText("Night reboot")).not.toBeInTheDocument();
  });

  it("links the curtailment pill to the Energy page", () => {
    mockUseWindowDimensions.mockReturnValue({
      isPhone: false,
      isTablet: false,
    });

    render(
      <MemoryRouter>
        <PageHeader
          schedulePillData={createSchedulePillData()}
          activeCurtailmentEvent={{
            reason: "Grid peak call",
            state: "curtailing",
            scopeLabel: "Whole fleet",
            selectedMiners: 48,
            estimatedReductionKw: 126.4,
            targetMetricsAvailable: true,
          }}
        />
      </MemoryRouter>,
    );

    expect(mockCurtailmentPill).toHaveBeenCalledWith(expect.objectContaining({ detailsPath: "/energy" }));
  });

  it("preserves site scope for the curtailment pill Energy link", () => {
    mockUseWindowDimensions.mockReturnValue({
      isPhone: false,
      isTablet: false,
    });

    render(
      <MemoryRouter initialEntries={["/7/fleet/miners"]}>
        <SiteScopeProvider value={{ kind: "site", id: "7" }}>
          <PageHeader
            schedulePillData={createSchedulePillData()}
            activeCurtailmentEvent={{
              reason: "Grid peak call",
              state: "curtailing",
              scopeLabel: "Whole fleet",
              selectedMiners: 48,
              estimatedReductionKw: 126.4,
              targetMetricsAvailable: true,
            }}
          />
        </SiteScopeProvider>
      </MemoryRouter>,
    );

    expect(mockCurtailmentPill).toHaveBeenCalledWith(expect.objectContaining({ detailsPath: "/7/energy" }));
  });

  it("uses stored site scope for the curtailment pill Energy link outside scoped routes", () => {
    mockUseWindowDimensions.mockReturnValue({
      isPhone: false,
      isTablet: false,
    });
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "site", id: "7" };
    });

    render(
      <MemoryRouter initialEntries={["/settings/general"]}>
        <PageHeader
          schedulePillData={createSchedulePillData()}
          activeCurtailmentEvent={{
            reason: "Grid peak call",
            state: "curtailing",
            scopeLabel: "Whole fleet",
            selectedMiners: 48,
            estimatedReductionKw: 126.4,
            targetMetricsAvailable: true,
          }}
        />
      </MemoryRouter>,
    );

    expect(mockCurtailmentPill).toHaveBeenCalledWith(expect.objectContaining({ detailsPath: "/7/energy" }));
  });

  it("hides the curtailment pill without curtailment read permission", () => {
    vi.mocked(useHasPermission).mockReturnValue(false);
    mockUseWindowDimensions.mockReturnValue({
      isPhone: false,
      isTablet: false,
    });

    render(
      <MemoryRouter>
        <PageHeader
          schedulePillData={createSchedulePillData()}
          activeCurtailmentEvent={{
            reason: "Grid peak call",
            state: "curtailing",
            scopeLabel: "Whole fleet",
            selectedMiners: 48,
            estimatedReductionKw: 126.4,
            targetMetricsAvailable: true,
          }}
        />
      </MemoryRouter>,
    );

    expect(useHasPermission).toHaveBeenCalledWith("curtailment:read");
    expect(screen.queryByText("Curtailment pill")).not.toBeInTheDocument();
    expect(mockCurtailmentPill).not.toHaveBeenCalled();
  });
});
