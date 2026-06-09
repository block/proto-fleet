import { MemoryRouter } from "react-router-dom";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import PageHeader from "./PageHeader";
import type { UseSchedulePillDataResult } from "./useSchedulePillData";
import type { ScheduleListItem } from "@/protoFleet/api/useScheduleApi";
import { useHasPermission } from "@/protoFleet/store";

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
  // SitePicker derives `knownSiteIds` via this helper; the mock factory
  // returns an empty set so the picker renders without throwing while
  // these tests focus on PageHeader's other rows.
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
      isPhone: true,
      isTablet: false,
    });
    mockUseReactiveLocalStorage.mockReturnValue([false, vi.fn()]);
    vi.mocked(useHasPermission).mockReturnValue(true);
  });

  it("shows the phone widget row when schedules are available even if setup is not dismissed", () => {
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
          }}
        />
      </MemoryRouter>,
    );

    expect(mockCurtailmentPill).toHaveBeenCalledWith(expect.objectContaining({ detailsPath: "/energy" }));
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
          }}
        />
      </MemoryRouter>,
    );

    expect(useHasPermission).toHaveBeenCalledWith("curtailment:read");
    expect(screen.queryByText("Curtailment pill")).not.toBeInTheDocument();
    expect(mockCurtailmentPill).not.toHaveBeenCalled();
  });
});
