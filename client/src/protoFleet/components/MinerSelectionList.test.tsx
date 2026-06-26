import { act, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import MinerSelectionList from "./MinerSelectionList";

const { fleetArgsSpy, listPropsSpy, listRacksMock, listGroupsMock } = vi.hoisted(() => ({
  fleetArgsSpy: vi.fn(),
  listPropsSpy: vi.fn(),
  listRacksMock: vi.fn(),
  listGroupsMock: vi.fn(),
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

vi.mock("@/shared/components/List", () => ({
  __esModule: true,
  default: (props: unknown) => {
    listPropsSpy(props);
    return <div data-testid="list-stub" />;
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
        dropdownFilters: { type: ["S21"] },
        numericFilters: {},
        textareaListFilters: {},
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
        dropdownFilters: { type: ["S21"] },
        numericFilters: {},
        textareaListFilters: {},
      });
    });

    expect(screen.getByText("Select all")).toBeInTheDocument();
  });
});
