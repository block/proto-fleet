import { MemoryRouter } from "react-router-dom";
import { render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import PageHeader from "./PageHeader";
import type { UseSchedulePillDataResult } from "./useSchedulePillData";
import { FirmwareRolloutSchema } from "@/protoFleet/api/generated/firmwarerollout/v1/firmwarerollout_pb";
import { SiteSchema, type SiteWithCounts, SiteWithCountsSchema } from "@/protoFleet/api/generated/sites/v1/sites_pb";
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

vi.mock("./FirmwareRolloutPill", () => ({
  default: () => <div>Firmware rollout pill</div>,
}));

vi.mock("./SchedulePill", () => ({
  __esModule: true,
  default: ({ pillSchedule }: { pillSchedule: { name: string } }) => <div>{pillSchedule.name}</div>,
}));

vi.mock("@/protoFleet/api/sites", () => ({
  useSites: () => ({
    listSites: mockListSites,
  }),
  // SitePicker imports these; stubs keep the picker from throwing.
  buildKnownSiteIds: () => new Set<string>(),
  buildSiteSlugById: () => new Map<string, string>(),
}));

// PageHeader reads the catalog from the shell-level SitesProvider; the fetch
// itself is the provider's job (covered by its own tests). Drive the context
// directly so these tests focus on header/picker rendering.
const sitesCtx = vi.hoisted(() => ({
  current: {
    sites: [] as SiteWithCounts[] | undefined,
    sitesError: null as string | null,
    sitesLoaded: false,
    sitesSettled: false,
    sitesPermissionDenied: false,
    siteCatalogAccessGranted: false,
    refetchSites: vi.fn(),
  },
}));
vi.mock("@/protoFleet/api/SitesContext", () => ({
  useSitesContext: () => sitesCtx.current,
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

vi.mock("@/shared/assets/icons", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/shared/assets/icons")>();
  return {
    ...actual,
    // Override the menu button; the SitePicker trigger + Modal pull in other
    // icons (ChevronDown, Dismiss, …) which stay real via the spread above.
    Menu: ({ ariaLabel, testId }: { ariaLabel?: string; testId?: string }) => (
      <button aria-label={ariaLabel} data-testid={testId}>
        menu
      </button>
    ),
  };
});
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
    sitesCtx.current = {
      sites: [],
      sitesError: null,
      sitesLoaded: true,
      sitesSettled: true,
      sitesPermissionDenied: false,
      siteCatalogAccessGranted: true,
      refetchSites: vi.fn(),
    };
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

  it("shows an active firmware rollout as an inline phone widget", () => {
    render(
      <MemoryRouter>
        <PageHeader
          schedulePillData={createSchedulePillData()}
          activeFirmwareRollouts={[
            create(FirmwareRolloutSchema, {
              rolloutId: "r1",
              name: "June update",
            }),
          ]}
        />
      </MemoryRouter>,
    );

    expect(within(screen.getByTestId("page-header-inline-widgets")).getByText("Firmware rollout pill")).toBeVisible();
    expect(screen.queryByTestId("phone-header-widget-row")).not.toBeInTheDocument();
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
      <MemoryRouter initialEntries={["/north/fleet/miners"]}>
        <SiteScopeProvider value={{ kind: "site", id: "7", slug: "north" }}>
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

    expect(mockCurtailmentPill).toHaveBeenCalledWith(expect.objectContaining({ detailsPath: "/north/energy" }));
  });

  it("uses stored site scope for the curtailment pill Energy link outside scoped routes", () => {
    mockUseWindowDimensions.mockReturnValue({
      isPhone: false,
      isTablet: false,
    });
    useFleetStore.setState((state) => {
      state.ui.activeSite = { kind: "site", id: "7", slug: "north" };
    });

    render(
      <MemoryRouter initialEntries={["/settings/network"]}>
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

    expect(mockCurtailmentPill).toHaveBeenCalledWith(expect.objectContaining({ detailsPath: "/north/energy" }));
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

  it("hides the SitePicker without site read permission", () => {
    vi.mocked(useHasPermission).mockImplementation((permission) => permission !== "site:read");

    render(
      <MemoryRouter>
        <PageHeader schedulePillData={createSchedulePillData()} />
      </MemoryRouter>,
    );

    expect(useHasPermission).toHaveBeenCalledWith("site:read");
    expect(screen.queryByTestId("site-picker-trigger")).not.toBeInTheDocument();
    expect(screen.queryByTestId("site-picker-error")).not.toBeInTheDocument();
  });

  it("renders the SitePicker from the shared catalog when the user has site read permission", () => {
    vi.mocked(useHasPermission).mockReturnValue(true);
    mockUseWindowDimensions.mockReturnValue({ isPhone: false, isTablet: false });
    sitesCtx.current = {
      ...sitesCtx.current,
      sites: [
        create(SiteWithCountsSchema, {
          site: create(SiteSchema, { id: 7n, name: "Austin", slug: "austin" }),
        }),
      ],
    };

    render(
      <MemoryRouter>
        <PageHeader schedulePillData={createSchedulePillData()} />
      </MemoryRouter>,
    );

    expect(screen.getByTestId("site-picker-trigger")).toBeInTheDocument();
  });
});
