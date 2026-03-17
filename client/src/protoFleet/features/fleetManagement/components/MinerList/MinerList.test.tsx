import { BrowserRouter, MemoryRouter, useLocation } from "react-router-dom";
import { act, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";

import MinerList from "./MinerList";
import {
  MinerStateSnapshotSchema,
  PairingStatus,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceStatus, MinerStateCountsSchema } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import type { MinerStateSnapshot } from "@/protoFleet/store/slices/fleetSlice";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

const { mockMinerListActionBar } = vi.hoisted(() => ({
  mockMinerListActionBar: vi.fn(
    ({
      selectedMiners,
      selectionMode,
      totalCount,
      onSelectAll,
      onSelectNone,
    }: {
      selectedMiners: string[];
      selectionMode: string;
      totalCount?: number;
      onSelectAll?: () => void;
      onSelectNone?: () => void;
    }) => {
      if (selectionMode === "none" && selectedMiners.length === 0) {
        return null;
      }

      return (
        <div data-testid="mock-miner-list-action-bar">
          <span data-testid="mock-miner-list-selection-mode">{selectionMode}</span>
          <span data-testid="mock-miner-list-selected-miners">{selectedMiners.join(",")}</span>
          <span data-testid="mock-miner-list-selection-count">
            {selectionMode === "all" ? (totalCount ?? selectedMiners.length) : selectedMiners.length}
          </span>
          {onSelectAll ? (
            <button type="button" data-testid="mock-action-bar-select-all" onClick={onSelectAll}>
              Select all
            </button>
          ) : null}
          {onSelectNone ? (
            <button type="button" data-testid="mock-action-bar-select-none" onClick={onSelectNone}>
              Select none
            </button>
          ) : null}
        </div>
      );
    },
  ),
}));

vi.mock("./MinerListActionBar", () => ({
  default: mockMinerListActionBar,
}));

const renderMinerList = (props: Parameters<typeof MinerList>[0], initialEntries?: string[]) => {
  const Router = initialEntries ? MemoryRouter : BrowserRouter;
  const routerProps = initialEntries ? { initialEntries } : {};

  return render(
    <Router {...routerProps}>
      <MinerList {...props} />
    </Router>,
  );
};

const LocationDisplay = () => {
  const location = useLocation();

  return <div data-testid="location-display">{location.search}</div>;
};

describe("MinerList", () => {
  const createMinerSnapshot = (deviceIdentifier: string, pairingStatus = PairingStatus.PAIRED): MinerStateSnapshot =>
    create(MinerStateSnapshotSchema, {
      deviceIdentifier,
      name: deviceIdentifier,
      macAddress: "",
      ipAddress: "",
      deviceStatus: DeviceStatus.ONLINE,
      pairingStatus,
      hashrate: [],
      efficiency: [],
      powerUsage: [],
      temperature: [],
      url: "",
      model: "",
      firmwareVersion: "",
    });

  beforeEach(() => {
    vi.clearAllMocks();
    window.history.pushState({}, "", "/");
    useFleetStore.setState((state) => ({
      fleet: {
        ...state.fleet,
        miners: {},
        minerIds: [],
        totalMiners: 0,
        deviceStatusCounts: create(MinerStateCountsSchema, {}),
      },
    }));
  });

  describe("miner count subtitle", () => {
    it("shows total miner count", () => {
      renderMinerList({
        title: "Miners",
        minerIds: [],
        totalMiners: 14,
        onAddMiners: vi.fn(),
        loading: true,
      });

      expect(screen.getByText("14 miners")).toBeInTheDocument();
    });

    it("shows 'X of Y miners' when filters are active and filtered count differs from total", () => {
      renderMinerList(
        {
          title: "Miners",
          minerIds: [],
          totalMiners: 5,
          totalUnfilteredMiners: 14,
          onAddMiners: vi.fn(),
          loading: true,
        },
        ["/?status=hashing"],
      );

      expect(screen.getByText("5 of 14 miners")).toBeInTheDocument();
    });

    it("shows total count when filters are active but filtered count equals total", () => {
      renderMinerList(
        {
          title: "Miners",
          minerIds: [],
          totalMiners: 14,
          totalUnfilteredMiners: 14,
          onAddMiners: vi.fn(),
          loading: true,
        },
        ["/?status=hashing"],
      );

      expect(screen.getByText("14 miners")).toBeInTheDocument();
    });
  });

  describe("pagination footer", () => {
    it("shows correct range for the first page", () => {
      renderMinerList({
        title: "Miners",
        minerIds: ["m1", "m2", "m3"],
        totalMiners: 10,
        currentPage: 0,
        onAddMiners: vi.fn(),
        loading: false,
      });

      expect(screen.getByText("Showing 1–3 of 10 miners")).toBeInTheDocument();
    });

    it("shows correct range for a subsequent page", () => {
      renderMinerList({
        title: "Miners",
        minerIds: ["m1", "m2"],
        totalMiners: 102,
        currentPage: 1,
        pageSize: 100,
        onAddMiners: vi.fn(),
        loading: false,
      });

      expect(screen.getByText("Showing 101–102 of 102 miners")).toBeInTheDocument();
    });

    it("does not show pagination footer when there are no miners", () => {
      renderMinerList({
        title: "Miners",
        minerIds: [],
        totalMiners: 0,
        onAddMiners: vi.fn(),
        loading: false,
      });

      expect(screen.queryByText(/Showing/)).not.toBeInTheDocument();
    });

    it("does not show pagination footer while loading", () => {
      renderMinerList({
        title: "Miners",
        minerIds: ["m1"],
        totalMiners: 5,
        currentPage: 0,
        onAddMiners: vi.fn(),
        loading: true,
      });

      expect(screen.queryByText(/Showing/)).not.toBeInTheDocument();
    });

    it("disables the prev button on the first page", () => {
      renderMinerList({
        title: "Miners",
        minerIds: ["m1"],
        totalMiners: 5,
        currentPage: 0,
        hasPreviousPage: false,
        onPrevPage: vi.fn(),
        onAddMiners: vi.fn(),
        loading: false,
      });

      expect(screen.getByRole("button", { name: "Previous page" })).toBeDisabled();
    });

    it("disables the next button on the last page", () => {
      renderMinerList({
        title: "Miners",
        minerIds: ["m1"],
        totalMiners: 5,
        hasNextPage: false,
        onNextPage: vi.fn(),
        onAddMiners: vi.fn(),
        loading: false,
      });

      expect(screen.getByRole("button", { name: "Next page" })).toBeDisabled();
    });

    it("calls onPrevPage when prev button is clicked", async () => {
      const user = userEvent.setup();
      const onPrevPage = vi.fn();

      renderMinerList({
        title: "Miners",
        minerIds: ["m1"],
        totalMiners: 5,
        hasPreviousPage: true,
        onPrevPage,
        onAddMiners: vi.fn(),
        loading: false,
      });

      await user.click(screen.getByRole("button", { name: "Previous page" }));

      expect(onPrevPage).toHaveBeenCalledTimes(1);
    });

    it("calls onNextPage when next button is clicked", async () => {
      const user = userEvent.setup();
      const onNextPage = vi.fn();

      renderMinerList({
        title: "Miners",
        minerIds: ["m1"],
        totalMiners: 5,
        hasNextPage: true,
        onNextPage,
        onAddMiners: vi.fn(),
        loading: false,
      });

      await user.click(screen.getByRole("button", { name: "Next page" }));

      expect(onNextPage).toHaveBeenCalledTimes(1);
    });

    it("scrolls to top when next button is clicked", async () => {
      const user = userEvent.setup();
      const scrollIntoView = vi.fn();

      renderMinerList({
        title: "Miners",
        minerIds: ["m1"],
        totalMiners: 5,
        hasNextPage: true,
        onNextPage: vi.fn(),
        onAddMiners: vi.fn(),
        loading: false,
      });

      screen.getByText("Miners").closest("div")!.scrollIntoView = scrollIntoView;

      await user.click(screen.getByRole("button", { name: "Next page" }));

      expect(scrollIntoView).toHaveBeenCalledWith({ behavior: "smooth", block: "start" });
    });

    it("scrolls to top when prev button is clicked", async () => {
      const user = userEvent.setup();
      const scrollIntoView = vi.fn();

      renderMinerList({
        title: "Miners",
        minerIds: ["m1"],
        totalMiners: 5,
        hasPreviousPage: true,
        onPrevPage: vi.fn(),
        onAddMiners: vi.fn(),
        loading: false,
      });

      screen.getByText("Miners").closest("div")!.scrollIntoView = scrollIntoView;

      await user.click(screen.getByRole("button", { name: "Previous page" }));

      expect(scrollIntoView).toHaveBeenCalledWith({ behavior: "smooth", block: "start" });
    });

    it("adds bottom padding to pagination when miners are selected", async () => {
      const user = userEvent.setup();

      renderMinerList({
        title: "Miners",
        minerIds: ["m1", "m2"],
        totalMiners: 10,
        currentPage: 0,
        onAddMiners: vi.fn(),
        loading: false,
      });

      const rowCheckboxes = screen.getAllByTestId("checkbox");
      await user.click(rowCheckboxes[0].querySelector("input[type='checkbox']") as HTMLInputElement);

      expect(screen.getByTestId("miners-pagination")).toHaveClass("pb-24");
      expect(screen.getByTestId("mock-miner-list-selection-mode")).toHaveTextContent("subset");
      expect(screen.getByTestId("mock-miner-list-selection-count")).toHaveTextContent("1");
    });

    it("keeps header checkbox selection scoped to the current page", async () => {
      const user = userEvent.setup();

      renderMinerList({
        title: "Miners",
        minerIds: ["m1", "m2"],
        totalMiners: 10,
        currentPage: 0,
        onAddMiners: vi.fn(),
        loading: false,
      });

      const selectAllCheckbox = screen
        .getByTestId("list-header")
        .querySelector("input[type='checkbox']") as HTMLInputElement;

      await user.click(selectAllCheckbox);

      expect(screen.getByTestId("mock-miner-list-selection-mode")).toHaveTextContent("subset");
      expect(screen.getByTestId("mock-miner-list-selected-miners")).toHaveTextContent("m1,m2");
      expect(screen.getByTestId("mock-miner-list-selection-count")).toHaveTextContent("2");
    });

    it("hides action-bar select controls when filters are active", async () => {
      const user = userEvent.setup();

      renderMinerList(
        {
          title: "Miners",
          minerIds: ["m1", "m2"],
          totalMiners: 10,
          currentPage: 0,
          onAddMiners: vi.fn(),
          loading: false,
        },
        ["/?status=hashing"],
      );

      const rowCheckboxes = screen.getAllByTestId("checkbox");
      await user.click(rowCheckboxes[0].querySelector("input[type='checkbox']") as HTMLInputElement);

      expect(screen.getByTestId("mock-miner-list-action-bar")).toBeInTheDocument();
      expect(screen.queryByTestId("mock-action-bar-select-all")).not.toBeInTheDocument();
      expect(screen.queryByTestId("mock-action-bar-select-none")).not.toBeInTheDocument();
      expect(screen.getByTestId("mock-miner-list-selection-mode")).toHaveTextContent("subset");
      expect(screen.getByTestId("mock-miner-list-selection-count")).toHaveTextContent("1");
    });

    it("clears bulk selection when the page changes and does not restore it when returning", async () => {
      const user = userEvent.setup();

      const { rerender } = renderMinerList({
        title: "Miners",
        minerIds: ["m1", "m2"],
        totalMiners: 4,
        currentPage: 0,
        pageSize: 2,
        onAddMiners: vi.fn(),
        loading: false,
      });

      const rowCheckboxes = screen.getAllByTestId("checkbox");
      await user.click(rowCheckboxes[0].querySelector("input[type='checkbox']") as HTMLInputElement);
      await user.click(screen.getByTestId("mock-action-bar-select-all"));

      expect(screen.getByTestId("mock-miner-list-selection-mode")).toHaveTextContent("all");
      expect(screen.getByTestId("mock-miner-list-selection-count")).toHaveTextContent("4");

      rerender(
        <BrowserRouter>
          <MinerList
            title="Miners"
            minerIds={["m3", "m4"]}
            totalMiners={4}
            currentPage={1}
            pageSize={2}
            onAddMiners={vi.fn()}
            loading={false}
          />
        </BrowserRouter>,
      );

      expect(screen.queryByTestId("mock-miner-list-action-bar")).not.toBeInTheDocument();

      rerender(
        <BrowserRouter>
          <MinerList
            title="Miners"
            minerIds={["m1", "m2"]}
            totalMiners={4}
            currentPage={0}
            pageSize={2}
            onAddMiners={vi.fn()}
            loading={false}
          />
        </BrowserRouter>,
      );

      expect(screen.queryByTestId("mock-miner-list-action-bar")).not.toBeInTheDocument();
    });

    it("recomputes selectable miners when a row becomes disabled between renders", async () => {
      const user = userEvent.setup();

      useFleetStore.setState((state) => ({
        fleet: {
          ...state.fleet,
          miners: {
            m1: createMinerSnapshot("m1"),
            m2: createMinerSnapshot("m2"),
          },
        },
      }));

      renderMinerList({
        title: "Miners",
        minerIds: ["m1", "m2"],
        totalMiners: 2,
        totalDisabledMiners: 0,
        currentPage: 0,
        onAddMiners: vi.fn(),
        loading: false,
      });

      const rowCheckboxes = screen.getAllByTestId("checkbox");
      await user.click(rowCheckboxes[0].querySelector("input[type='checkbox']") as HTMLInputElement);

      await act(async () => {
        useFleetStore.setState((state) => ({
          fleet: {
            ...state.fleet,
            miners: {
              ...state.fleet.miners,
              m2: createMinerSnapshot("m2", PairingStatus.AUTHENTICATION_NEEDED),
            },
          },
        }));
      });

      await user.click(screen.getByTestId("mock-action-bar-select-all"));

      expect(screen.getByTestId("mock-miner-list-selected-miners")).toHaveTextContent("m1");
    });
  });

  describe("null state", () => {
    it("should show null state when no miners are paired", () => {
      const onAddMiners = vi.fn();

      renderMinerList({
        title: "Miners",
        minerIds: [],
        totalMiners: 0,
        onAddMiners,
      });

      expect(screen.getByText("You haven't paired any miners")).toBeInTheDocument();
      expect(screen.getByText("Add miners to your fleet to get started.")).toBeInTheDocument();
      expect(screen.getByRole("button", { name: "Get started" })).toBeInTheDocument();
      // List header and "Add miners" button should not be visible when showing null state
      expect(screen.queryByText("Miners")).not.toBeInTheDocument();
      expect(screen.queryByRole("button", { name: "Add miners" })).not.toBeInTheDocument();
    });

    it("should call onAddMiners when Get started button is clicked", async () => {
      const user = userEvent.setup();
      const onAddMiners = vi.fn();

      renderMinerList({
        title: "Miners",
        minerIds: [],
        totalMiners: 0,
        onAddMiners,
      });

      await user.click(screen.getByRole("button", { name: "Get started" }));

      expect(onAddMiners).toHaveBeenCalledTimes(1);
    });

    it("should not show null state when loading", () => {
      const onAddMiners = vi.fn();

      renderMinerList({
        title: "Miners",
        minerIds: [],
        totalMiners: 0,
        onAddMiners,
        loading: true,
      });

      expect(screen.queryByText("You haven't paired any miners")).not.toBeInTheDocument();
    });

    it("should not show null state when filters are active and no items match", () => {
      const onAddMiners = vi.fn();

      renderMinerList(
        {
          title: "Miners",
          minerIds: [],
          totalMiners: 0,
          onAddMiners,
        },
        ["/?status=hashing"],
      );

      // Null state should not appear when filters are active
      expect(screen.queryByText("You haven't paired any miners")).not.toBeInTheDocument();
      // Regular list view should be shown instead
      expect(screen.getByText("Miners")).toBeInTheDocument();
      expect(screen.getByRole("button", { name: "Add miners" })).toBeInTheDocument();
    });

    it("shows the filtered empty state and clears filters when requested", async () => {
      const user = userEvent.setup();

      render(
        <MemoryRouter initialEntries={["/?status=hashing&issues=control-board&sort=name&dir=desc"]}>
          <MinerList title="Miners" minerIds={[]} totalMiners={0} totalUnfilteredMiners={14} onAddMiners={vi.fn()} />
          <LocationDisplay />
        </MemoryRouter>,
      );

      expect(screen.getByText("No results")).toBeInTheDocument();
      expect(screen.getByText("Try adjusting or clearing your filters.")).toBeInTheDocument();
      expect(screen.getByRole("button", { name: "Clear all filters" })).toBeInTheDocument();

      await user.click(screen.getByRole("button", { name: "Clear all filters" }));

      expect(screen.getByTestId("location-display")).toHaveTextContent("?sort=name&dir=desc");
    });

    it("should not show null state when group filter is active", () => {
      renderMinerList(
        {
          title: "Miners",
          minerIds: [],
          totalMiners: 0,
          onAddMiners: vi.fn(),
        },
        ["/?group=1"],
      );

      expect(screen.queryByText("You haven't paired any miners")).not.toBeInTheDocument();
      expect(screen.getByText("Miners")).toBeInTheDocument();
    });

    it("clears group param along with other filters while preserving sort params", async () => {
      const user = userEvent.setup();

      render(
        <MemoryRouter initialEntries={["/?status=hashing&group=1,2&sort=name&dir=desc"]}>
          <MinerList title="Miners" minerIds={[]} totalMiners={0} totalUnfilteredMiners={14} onAddMiners={vi.fn()} />
          <LocationDisplay />
        </MemoryRouter>,
      );

      await user.click(screen.getByRole("button", { name: "Clear all filters" }));

      expect(screen.getByTestId("location-display")).toHaveTextContent("?sort=name&dir=desc");
    });
  });
});
