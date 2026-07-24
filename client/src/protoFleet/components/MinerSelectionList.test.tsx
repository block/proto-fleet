import type { ReactNode } from "react";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import MinerSelectionList from "./MinerSelectionList";
import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import {
  FLEET_SELECTABLE_PAIRING_STATUSES,
  FLEET_VISIBLE_PAIRING_STATUSES,
} from "@/protoFleet/features/fleetManagement/utils/fleetVisiblePairingFilter";

const { fleetArgsSpy, listPropsSpy, listRacksMock, listGroupsMock, hasPermMock } = vi.hoisted(() => ({
  fleetArgsSpy: vi.fn(),
  listPropsSpy: vi.fn(),
  listRacksMock: vi.fn(),
  listGroupsMock: vi.fn(),
  hasPermMock: vi.fn((_perm: string) => true),
}));

vi.mock("@/protoFleet/api/useFleet", () => ({
  __esModule: true,
  default: (args: unknown) => {
    fleetArgsSpy(args);
    return {
      minerIds: ["miner-1"],
      miners: {
        "miner-1": {
          deviceIdentifier: "miner-1",
          name: "Miner 1",
          model: "S21",
          ipAddress: "192.0.2.10",
        },
      },
      totalMiners: 2,
      isLoading: false,
      hasMore: false,
      currentPage: 0,
      hasPreviousPage: false,
      goToNextPage: vi.fn(),
      goToPrevPage: vi.fn(),
      availableModels: [],
    };
  },
}));

vi.mock("@/protoFleet/api/useDeviceSets", () => ({
  useDeviceSets: () => ({
    listRacks: listRacksMock,
    listGroups: listGroupsMock,
  }),
}));

vi.mock("@/protoFleet/api/sites", () => ({
  useSites: () => ({ listSites: vi.fn() }),
}));

vi.mock("@/protoFleet/api/buildings", () => ({
  useBuildings: () => ({ listBuildings: vi.fn() }),
}));

vi.mock("@/protoFleet/store", () => ({
  useHasPermission: (perm: string) => hasPermMock(perm),
}));

vi.mock("@/shared/components/List", () => ({
  __esModule: true,
  default: (props: { headerControls?: ReactNode }) => {
    listPropsSpy(props);
    // Render headerControls so the assignable-only toggle is interactive in tests.
    return <div data-testid="list-stub">{props.headerControls}</div>;
  },
}));

describe("MinerSelectionList site scope", () => {
  beforeEach(() => {
    fleetArgsSpy.mockReset();
    listPropsSpy.mockReset();
    listRacksMock.mockReset();
    listGroupsMock.mockReset();
  });

  const lastFleetFilter = () => {
    const calls = fleetArgsSpy.mock.calls;
    return calls[calls.length - 1]?.[0]?.filter;
  };

  it("passes the all-sites filter through unchanged (no regression)", async () => {
    render(<MinerSelectionList scope={{ siteIds: [], includeUnassigned: false }} />);

    const filter = lastFleetFilter();
    expect(filter.siteIds).toEqual([]);
    expect(filter.includeUnassigned).toBe(false);

    await waitFor(() => expect(listRacksMock).toHaveBeenCalled());
    expect(listRacksMock).toHaveBeenCalledWith(expect.objectContaining({ siteIds: [], includeUnassigned: false }));
  });

  it("scopes the miner list and rack facet options to the selected site", async () => {
    render(<MinerSelectionList scope={{ siteIds: [7n], includeUnassigned: false }} />);

    const filter = lastFleetFilter();
    expect(filter.siteIds).toEqual([7n]);
    expect(filter.includeUnassigned).toBe(false);

    await waitFor(() => expect(listRacksMock).toHaveBeenCalled());
    expect(listRacksMock).toHaveBeenCalledWith(expect.objectContaining({ siteIds: [7n], includeUnassigned: false }));
  });

  it("includes site-unassigned miners in the list but keeps rack facet options within the site", async () => {
    // A specific scoped site with includeUnassigned: the miner list should also
    // surface site-unassigned miners, but the rack/building facet options must
    // stay strictly within the site (no unassigned racks in the dropdown).
    render(<MinerSelectionList scope={{ siteIds: [7n], includeUnassigned: true }} />);

    const filter = lastFleetFilter();
    expect(filter.siteIds).toEqual([7n]);
    expect(filter.includeUnassigned).toBe(true);

    await waitFor(() => expect(listRacksMock).toHaveBeenCalled());
    expect(listRacksMock).toHaveBeenCalledWith(expect.objectContaining({ siteIds: [7n], includeUnassigned: false }));
  });

  it("re-applies the filter when the active site changes mid-modal", () => {
    const { rerender } = render(<MinerSelectionList scope={{ siteIds: [7n], includeUnassigned: false }} />);
    expect(lastFleetFilter().siteIds).toEqual([7n]);

    rerender(<MinerSelectionList scope={{ siteIds: [], includeUnassigned: true }} />);
    expect(lastFleetFilter().siteIds).toEqual([]);
    expect(lastFleetFilter().includeUnassigned).toBe(true);
  });

  it("does not offer select-all for filtered results the curtailment backend cannot represent", async () => {
    render(<MinerSelectionList disableFilteredSelectAll />);
    expect(screen.getByText("Select all")).toBeInTheDocument();

    const listProps = listPropsSpy.mock.calls[listPropsSpy.mock.calls.length - 1]?.[0] as {
      onServerFilter: (filters: {
        buttonFilters: string[];
        dropdownFilters: Record<string, string[]>;
        numericFilters: Record<string, unknown>;
        textareaListFilters: Record<string, string[]>;
      }) => Promise<void>;
    };

    await act(async () => {
      await listProps.onServerFilter({
        buttonFilters: [],
        dropdownFilters: { model: ["S21"] },
        numericFilters: {},
        textareaListFilters: {},
      });
    });

    await waitFor(() => expect(screen.queryByText("Select all")).not.toBeInTheDocument());
  });

  it("does not offer select-all for a range-only subnet filter (ip_ranges, no ip_cidrs)", async () => {
    render(<MinerSelectionList disableFilteredSelectAll filterConfig={{ showSubnetFilter: true }} />);
    expect(screen.getByText("Select all")).toBeInTheDocument();

    const listProps = listPropsSpy.mock.calls[listPropsSpy.mock.calls.length - 1]?.[0] as {
      onServerFilter: (filters: {
        buttonFilters: string[];
        dropdownFilters: Record<string, string[]>;
        numericFilters: Record<string, unknown>;
        textareaListFilters: Record<string, string[]>;
      }) => Promise<void>;
    };

    await act(async () => {
      await listProps.onServerFilter({
        buttonFilters: [],
        dropdownFilters: {},
        numericFilters: {},
        textareaListFilters: { subnet: ["10.0.0.10-10.0.0.20"] },
      });
    });

    await waitFor(() => expect(screen.queryByText("Select all")).not.toBeInTheDocument());
  });

  it("keeps filtered select-all available by default for callers that expand filters", async () => {
    render(<MinerSelectionList />);

    const listProps = listPropsSpy.mock.calls[listPropsSpy.mock.calls.length - 1]?.[0] as {
      onServerFilter: (filters: {
        buttonFilters: string[];
        dropdownFilters: Record<string, string[]>;
        numericFilters: Record<string, unknown>;
        textareaListFilters: Record<string, string[]>;
      }) => Promise<void>;
    };

    await act(async () => {
      await listProps.onServerFilter({
        buttonFilters: [],
        dropdownFilters: { model: ["S21"] },
        numericFilters: {},
        textareaListFilters: {},
      });
    });

    expect(screen.getByText("Select all")).toBeInTheDocument();
  });
});

describe("MinerSelectionList eligibility", () => {
  beforeEach(() => {
    fleetArgsSpy.mockReset();
    listPropsSpy.mockReset();
    listRacksMock.mockReset();
    listGroupsMock.mockReset();
    hasPermMock.mockReturnValue(true);
  });

  const lastFleetFilter = () => {
    const calls = fleetArgsSpy.mock.calls;
    return calls[calls.length - 1]?.[0]?.filter;
  };

  const lastListProps = () => listPropsSpy.mock.calls[listPropsSpy.mock.calls.length - 1]?.[0];

  it("folds the target rack's rack/building/site into the filter when assignable-only is on (default)", () => {
    render(<MinerSelectionList eligibility={{ rackId: 1n, siteId: 2n, buildingId: 3n }} />);

    const filter = lastFleetFilter();
    expect(filter.includeNoRack).toBe(true);
    expect(filter.rackIds).toEqual([1n]);
    expect(filter.includeNoBuilding).toBe(true);
    expect(filter.buildingIds).toEqual([3n]);
    expect(filter.includeUnassigned).toBe(true);
    expect(filter.siteIds).toEqual([2n]);
  });

  it("pins site + no-building when the target rack has a site but no building", () => {
    render(<MinerSelectionList eligibility={{ rackId: 1n, siteId: 2n }} />);

    // Direct-under-site rack: site pins to that site, and building pins to "no
    // building" (includeNoBuilding). Server-side that admits the rack's own
    // members and excludes rackless miners directly placed in a building.
    // rackId is set (existing rack), so the server keeps the rack-derived
    // no-building branch that surfaces the current members.
    const filter = lastFleetFilter();
    expect(filter.siteIds).toEqual([2n]);
    expect(filter.includeUnassigned).toBe(true);
    expect(filter.buildingIds).toEqual([]);
    expect(filter.includeNoBuilding).toBe(true);
  });

  it("pins every dimension to unplaced-only for a new/unplaced rack (no eligibility)", () => {
    render(<MinerSelectionList eligibility={{}} />);

    // A new/unplaced rack: assignable-without-reparent = fully-unplaced miners.
    // Every dimension pins to "no id + include unassigned/no-building". With no
    // rackId, the server drops includeNoBuilding's rack-derived branch and
    // reads this as "no rack AND no direct building AND no site".
    const filter = lastFleetFilter();
    expect(filter.includeNoRack).toBe(true);
    expect(filter.rackIds).toEqual([]);
    expect(filter.includeUnassigned).toBe(true);
    expect(filter.siteIds).toEqual([]);
    expect(filter.includeNoBuilding).toBe(true);
    expect(filter.buildingIds).toEqual([]);
  });

  it("respects a Rack facet that includes the target rack, dropping unracked, in assignable-only mode", async () => {
    render(<MinerSelectionList eligibility={{ rackId: 1n }} filterConfig={{ showRackFilter: true }} />);

    const listProps = lastListProps() as {
      onServerFilter: (filters: {
        buttonFilters: string[];
        dropdownFilters: Record<string, string[]>;
        numericFilters: Record<string, unknown>;
        textareaListFilters: Record<string, string[]>;
      }) => Promise<void>;
    };
    // Facet the target rack itself: the facet now defines the dimension, so the
    // request pins to it and stops OR-ing in unracked miners.
    await act(async () => {
      await listProps.onServerFilter({
        buttonFilters: [],
        dropdownFilters: { rack: ["1"] },
        numericFilters: {},
        textareaListFilters: {},
      });
    });

    expect(lastFleetFilter().rackIds).toEqual([1n]);
    expect(lastFleetFilter().includeNoRack).toBe(false);
  });

  it("shows an empty result when a placement facet conflicts with the target rack (assignable-only)", async () => {
    render(
      <MinerSelectionList eligibility={{ rackId: 1n, buildingId: 3n }} filterConfig={{ showBuildingFilter: true }} />,
    );

    const listProps = lastListProps() as {
      onServerFilter: (filters: {
        buttonFilters: string[];
        dropdownFilters: Record<string, string[]>;
        numericFilters: Record<string, unknown>;
        textareaListFilters: Record<string, string[]>;
      }) => Promise<void>;
    };
    // Filter to a building the target rack isn't in — nothing assignable matches.
    await act(async () => {
      await listProps.onServerFilter({
        buttonFilters: [],
        dropdownFilters: { building: ["9"] },
        numericFilters: {},
        textareaListFilters: {},
      });
    });

    expect(lastListProps()?.items).toEqual([]);
  });

  it("hides Select all while 'Show assigned miners' is on (avoids silently dropping reparent picks)", () => {
    render(<MinerSelectionList eligibility={{ rackId: 1n }} />);
    expect(screen.getByText("Select all")).toBeInTheDocument();

    fireEvent.click(screen.getByLabelText("Show assigned miners"));
    expect(screen.queryByText("Select all")).not.toBeInTheDocument();
  });

  it("hides the Site/Building facets when the user lacks site:read", () => {
    hasPermMock.mockReturnValue(false);
    render(
      <MinerSelectionList filterConfig={{ showTypeFilter: true, showSiteFilter: true, showBuildingFilter: true }} />,
    );
    const filters = lastListProps()?.filters as { children?: { title: string }[] }[] | undefined;
    const titles = filters?.[0]?.children?.map((c) => c.title) ?? [];
    expect(titles).toContain("Model");
    expect(titles).not.toContain("Site");
    expect(titles).not.toContain("Building");
  });

  it("renders the assignable-only toggle only when eligibility is provided", () => {
    const { rerender } = render(<MinerSelectionList />);
    expect(lastListProps()?.headerControls).toBeFalsy();

    rerender(<MinerSelectionList eligibility={{ rackId: 1n }} />);
    expect(lastListProps()?.headerControls).toBeTruthy();
  });

  it("applies eligibility server-side by default and drops it when 'Show assigned miners' is on", () => {
    render(<MinerSelectionList eligibility={{ rackId: 1n, siteId: 2n, buildingId: 3n }} />);

    // Default (toggle off): eligibility folded into the fetch so assigned-elsewhere
    // miners are excluded server-side.
    expect(lastFleetFilter().includeNoRack).toBe(true);
    expect(lastFleetFilter().rackIds).toEqual([1n]);

    // Turning "Show assigned miners" on → a fresh fetch with the constraints
    // removed (server request, not a client-side row filter).
    fireEvent.click(screen.getByLabelText("Show assigned miners"));

    expect(lastFleetFilter().includeNoRack).toBe(false);
    expect(lastFleetFilter().rackIds).toEqual([]);
    expect(lastFleetFilter().buildingIds).toEqual([]);
    expect(lastFleetFilter().siteIds).toEqual([]);
  });

  it("keeps ineligible rows selectable (does not disable them)", () => {
    render(<MinerSelectionList eligibility={{ rackId: 1n, siteId: 2n, buildingId: 3n }} />);
    // Reassignment is allowed (with a confirm at continue), so eligibility no
    // longer disables rows — no isRowDisabled predicate is passed.
    expect(lastListProps()?.isRowDisabled).toBeUndefined();
  });

  it("adds a conflict icon-button to the name cell only for reassignment rows", () => {
    render(<MinerSelectionList eligibility={{ rackId: 1n, siteId: 2n, buildingId: 3n }} />);
    const colConfig = lastListProps()?.colConfig as Record<
      string,
      { component: (item: Record<string, unknown>, selected: string[]) => ReactNode }
    >;
    const base = {
      name: "n",
      model: "m",
      ipAddress: "",
      rackLabel: "",
      siteLabel: "",
      buildingLabel: "",
      groupLabels: [],
    };
    const conflictButton = "button[aria-label*='Assignment conflict']";

    // In the target rack — not a reassignment, no conflict icon.
    const eligible = render(
      <>{colConfig.name.component({ deviceIdentifier: "a", rackId: 1n, siteId: 2n, buildingId: 3n, ...base }, [])}</>,
    );
    expect(eligible.container.querySelector(conflictButton)).toBeNull();

    // In another rack — reassignment, conflict icon present.
    const ineligible = render(
      <>{colConfig.name.component({ deviceIdentifier: "b", rackId: 9n, siteId: 2n, buildingId: 3n, ...base }, [])}</>,
    );
    expect(ineligible.container.querySelector(conflictButton)).not.toBeNull();
  });
});

describe("MinerSelectionList pairing statuses", () => {
  beforeEach(() => {
    fleetArgsSpy.mockReset();
    listRacksMock.mockReset();
    listGroupsMock.mockReset();
  });

  const lastFleetPairingStatuses = () => {
    const calls = fleetArgsSpy.mock.calls;
    return calls[calls.length - 1]?.[0]?.pairingStatuses;
  };

  it("fetches PAIRED-only by default (other selection flows unchanged)", () => {
    render(<MinerSelectionList />);
    expect(lastFleetPairingStatuses()).toEqual(FLEET_SELECTABLE_PAIRING_STATUSES);
    expect(lastFleetPairingStatuses()).toEqual([PairingStatus.PAIRED]);
  });

  it("forwards a widened pairing set so non-paired rack members render (#777)", () => {
    render(<MinerSelectionList pairingStatuses={FLEET_VISIBLE_PAIRING_STATUSES} />);
    expect(lastFleetPairingStatuses()).toEqual(FLEET_VISIBLE_PAIRING_STATUSES);
    expect(lastFleetPairingStatuses()).toContain(PairingStatus.AUTHENTICATION_NEEDED);
    expect(lastFleetPairingStatuses()).toContain(PairingStatus.DEFAULT_PASSWORD);
  });
});
