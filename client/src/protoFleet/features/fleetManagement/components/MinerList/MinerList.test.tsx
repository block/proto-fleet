import { useLayoutEffect, useRef } from "react";
import { BrowserRouter, MemoryRouter, useLocation } from "react-router-dom";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";

import MinerList from "./MinerList";
import { getMinerTableColumnPreferencesStorageKey } from "./minerTableColumnPreferences";
import useMinerTableColumnPreferences from "./useMinerTableColumnPreferences";
import {
  type MinerStateSnapshot,
  MinerStateSnapshotSchema,
  PairingStatus,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { DeviceStatus } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { useFleetStore } from "@/protoFleet/store";

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

// useMinerActions (used by SingleMinerActionsMenu/MinerActionsMenu in column
// config) imports batch operation hooks from the store that were removed during
// the fleet slice refactor. Mock the hook so tests don't crash.
// MinerActionsMenu components import hooks from the removed fleet store slice.
// Mock the entire menu components so they don't render real action menus.
vi.mock("@/protoFleet/features/fleetManagement/components/MinerActionsMenu", () => ({
  default: () => null,
}));
vi.mock("@/protoFleet/features/fleetManagement/components/MinerActionsMenu/SingleMinerActionsMenu", () => ({
  default: () => null,
}));

const mockGetActiveBatches = vi.fn(() => []);

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

/** Auto-generates miners map from minerIds when miners prop is not provided. */
const autoMiners = (minerIds: string[]): Record<string, MinerStateSnapshot> =>
  Object.fromEntries(minerIds.map((id) => [id, createMinerSnapshot(id)]));

const renderMinerList = (
  props: Omit<Parameters<typeof MinerList>[0], "miners" | "errorsByDevice" | "errorsLoaded" | "getActiveBatches"> &
    Partial<Pick<Parameters<typeof MinerList>[0], "miners" | "errorsByDevice" | "errorsLoaded" | "getActiveBatches">>,
  initialEntries?: string[],
) => {
  const Router = initialEntries ? MemoryRouter : BrowserRouter;
  const routerProps = initialEntries ? { initialEntries } : {};
  const fullProps = {
    errorsByDevice: {} as Record<string, never[]>,
    errorsLoaded: true,
    getActiveBatches: mockGetActiveBatches,
    ...props,
    miners: props.miners ?? autoMiners(props.minerIds ?? []),
  };

  return render(
    <Router {...routerProps}>
      <MinerList {...fullProps} />
    </Router>,
  );
};

const LocationDisplay = () => {
  const location = useLocation();

  return <div data-testid="location-display">{location.search}</div>;
};

const isModelColumnVisible = (preferences: { columns: { id: string; visible: boolean }[] }) =>
  preferences.columns.find((column) => column.id === "model")?.visible ?? false;

const PreferenceStorageKeyProbe = ({ username }: { username: string }) => {
  const { preferences, setPreferences } = useMinerTableColumnPreferences(username);
  const previousUsername = useRef(username);

  useLayoutEffect(() => {
    if (previousUsername.current === username) {
      return;
    }

    previousUsername.current = username;
    setPreferences(preferences);
  }, [preferences, setPreferences, username]);

  return <div data-testid="preference-probe-model-visible">{String(isModelColumnVisible(preferences))}</div>;
};

describe("MinerList", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    window.history.pushState({}, "", "/");
    localStorage.clear();
    useFleetStore.setState((state) => ({
      auth: {
        ...state.auth,
        username: "",
      },
    }));
  });

  const getColumnHeaders = () =>
    within(screen.getByTestId("list-header"))
      .getAllByRole("columnheader")
      .map((header) => header.textContent?.trim() ?? "")
      .filter(Boolean);

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

  describe("export csv", () => {
    it("renders an export button and calls the export handler", async () => {
      const user = userEvent.setup();
      const onExportCsv = vi.fn();

      renderMinerList({
        title: "Miners",
        minerIds: ["m1"],
        totalMiners: 1,
        onAddMiners: vi.fn(),
        onExportCsv,
        loading: false,
      });

      await user.click(screen.getByRole("button", { name: "Export CSV" }));

      expect(onExportCsv).toHaveBeenCalledTimes(1);
    });

    it("disables the export button while export is in progress", () => {
      renderMinerList({
        title: "Miners",
        minerIds: ["m1"],
        totalMiners: 1,
        onAddMiners: vi.fn(),
        exportCsvLoading: true,
        loading: false,
      });

      expect(screen.getByRole("button", { name: "Export CSV" })).toBeDisabled();
    });

    it("disables the export button when there are no miners", () => {
      renderMinerList(
        {
          title: "Miners",
          minerIds: [],
          totalMiners: 0,
          onAddMiners: vi.fn(),
          loading: false,
        },
        ["/?status=hashing"],
      );

      expect(screen.getByRole("button", { name: "Export CSV" })).toBeDisabled();
    });
  });

  describe("manage columns", () => {
    it("opens the manage columns modal", async () => {
      const user = userEvent.setup();

      renderMinerList({
        title: "Miners",
        minerIds: ["m1"],
        totalMiners: 1,
        onAddMiners: vi.fn(),
        loading: false,
      });

      await user.click(screen.getByRole("button", { name: "Manage columns" }));

      expect(screen.getByTestId("manage-columns-modal")).toBeInTheDocument();
      expect(
        screen.getByText("Choose which data to display and rearrange columns to match your workflow."),
      ).toBeInTheDocument();
      expect(screen.getByTestId("manage-columns-reorder-model").firstChild).toHaveClass("w-4", "h-4", "shrink-0");
    });

    it("saves hidden columns for the current user and reapplies them on rerender", async () => {
      const user = userEvent.setup();

      useFleetStore.setState((state) => ({
        auth: {
          ...state.auth,
          username: "alice",
        },
      }));

      const { rerender } = render(
        <BrowserRouter>
          <MinerList
            title="Miners"
            minerIds={["m1"]}
            miners={autoMiners(["m1"])}
            errorsByDevice={{}}
            errorsLoaded={true}
            getActiveBatches={mockGetActiveBatches}
            totalMiners={1}
            onAddMiners={vi.fn()}
            loading={false}
          />
        </BrowserRouter>,
      );

      expect(getColumnHeaders()).toContain("Model");

      await user.click(screen.getByRole("button", { name: "Manage columns" }));
      await user.click(screen.getByRole("checkbox", { name: "Toggle Model column" }));
      await user.click(screen.getByRole("button", { name: "Save" }));

      expect(getColumnHeaders()).not.toContain("Model");

      rerender(
        <BrowserRouter>
          <MinerList
            title="Miners"
            minerIds={["m1"]}
            miners={autoMiners(["m1"])}
            errorsByDevice={{}}
            errorsLoaded={true}
            getActiveBatches={mockGetActiveBatches}
            totalMiners={1}
            onAddMiners={vi.fn()}
            loading={false}
          />
        </BrowserRouter>,
      );

      expect(getColumnHeaders()).not.toContain("Model");
    });

    it("keeps the modal draft in sync when the active user changes while it is open", async () => {
      const user = userEvent.setup();

      localStorage.setItem(
        getMinerTableColumnPreferencesStorageKey("alice"),
        JSON.stringify({
          columns: [
            { id: "groups", visible: true },
            { id: "model", visible: false },
          ],
        }),
      );

      useFleetStore.setState((state) => ({
        auth: {
          ...state.auth,
          username: "alice",
        },
      }));

      renderMinerList({
        title: "Miners",
        minerIds: ["m1"],
        totalMiners: 1,
        onAddMiners: vi.fn(),
        loading: false,
      });

      await user.click(screen.getByRole("button", { name: "Manage columns" }));
      expect(screen.getByRole("checkbox", { name: "Toggle Model column" })).not.toBeChecked();

      act(() => {
        useFleetStore.setState((state) => ({
          auth: {
            ...state.auth,
            username: "bob",
          },
        }));
      });

      expect(screen.getByRole("checkbox", { name: "Toggle Model column" })).toBeChecked();

      await user.click(screen.getByRole("button", { name: "Save" }));

      expect(localStorage.getItem(getMinerTableColumnPreferencesStorageKey("bob"))).toBeNull();
    });

    it("switches to the new user's preferences before layout effects can resave stale state", () => {
      localStorage.setItem(
        getMinerTableColumnPreferencesStorageKey("alice"),
        JSON.stringify({
          columns: [
            { id: "groups", visible: true },
            { id: "model", visible: false },
          ],
        }),
      );

      const { rerender } = render(<PreferenceStorageKeyProbe username="alice" />);

      expect(screen.getByTestId("preference-probe-model-visible")).toHaveTextContent("false");

      rerender(<PreferenceStorageKeyProbe username="bob" />);

      expect(screen.getByTestId("preference-probe-model-visible")).toHaveTextContent("true");
      expect(localStorage.getItem(getMinerTableColumnPreferencesStorageKey("bob"))).toBeNull();
    });

    it("keeps the table usable when persistence writes fail while saving preferences", async () => {
      const user = userEvent.setup();

      useFleetStore.setState((state) => ({
        auth: {
          ...state.auth,
          username: "alice",
        },
      }));

      renderMinerList({
        title: "Miners",
        minerIds: ["m1"],
        totalMiners: 1,
        onAddMiners: vi.fn(),
        loading: false,
      });

      await user.click(screen.getByRole("button", { name: "Manage columns" }));
      await user.click(screen.getByRole("checkbox", { name: "Toggle Model column" }));

      const setItemSpy = vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
        throw new Error("quota exceeded");
      });

      try {
        await user.click(screen.getByRole("button", { name: "Save" }));
      } finally {
        setItemSpy.mockRestore();
      }

      expect(getColumnHeaders()).not.toContain("Model");
    });

    it("clears the active sort when the saved preferences hide the sorted column", async () => {
      const user = userEvent.setup();

      render(
        <MemoryRouter initialEntries={["/?sort=model&dir=asc"]}>
          <MinerList
            title="Miners"
            minerIds={["m1"]}
            miners={autoMiners(["m1"])}
            errorsByDevice={{}}
            errorsLoaded={true}
            getActiveBatches={mockGetActiveBatches}
            totalMiners={1}
            onAddMiners={vi.fn()}
            loading={false}
          />
          <LocationDisplay />
        </MemoryRouter>,
      );

      expect(screen.getByTestId("location-display")).toHaveTextContent("?sort=model&dir=asc");

      await user.click(screen.getByRole("button", { name: "Manage columns" }));
      await user.click(screen.getByRole("checkbox", { name: "Toggle Model column" }));
      await user.click(screen.getByRole("button", { name: "Save" }));

      expect(getColumnHeaders()).not.toContain("Model");
      expect(screen.getByTestId("location-display").textContent).toBe("");
    });

    it("clears a hidden URL sort when stored preferences load on first render", async () => {
      localStorage.setItem(
        getMinerTableColumnPreferencesStorageKey("alice"),
        JSON.stringify({
          columns: [
            { id: "groups", visible: true },
            { id: "model", visible: false },
          ],
        }),
      );

      useFleetStore.setState((state) => ({
        auth: {
          ...state.auth,
          username: "alice",
        },
      }));

      render(
        <MemoryRouter initialEntries={["/?sort=model&dir=asc"]}>
          <MinerList
            title="Miners"
            minerIds={["m1"]}
            miners={autoMiners(["m1"])}
            errorsByDevice={{}}
            errorsLoaded={true}
            getActiveBatches={mockGetActiveBatches}
            totalMiners={1}
            onAddMiners={vi.fn()}
            loading={false}
          />
          <LocationDisplay />
        </MemoryRouter>,
      );

      await waitFor(() => {
        expect(screen.getByTestId("location-display").textContent).toBe("");
      });

      expect(getColumnHeaders()).not.toContain("Model");
    });

    it("clears a hidden URL sort when the active user changes", async () => {
      localStorage.setItem(
        getMinerTableColumnPreferencesStorageKey("bob"),
        JSON.stringify({
          columns: [
            { id: "groups", visible: true },
            { id: "model", visible: false },
          ],
        }),
      );

      useFleetStore.setState((state) => ({
        auth: {
          ...state.auth,
          username: "alice",
        },
      }));

      render(
        <MemoryRouter initialEntries={["/?sort=model&dir=asc"]}>
          <MinerList
            title="Miners"
            minerIds={["m1"]}
            miners={autoMiners(["m1"])}
            errorsByDevice={{}}
            errorsLoaded={true}
            getActiveBatches={mockGetActiveBatches}
            totalMiners={1}
            onAddMiners={vi.fn()}
            loading={false}
          />
          <LocationDisplay />
        </MemoryRouter>,
      );

      expect(screen.getByTestId("location-display")).toHaveTextContent("?sort=model&dir=asc");
      expect(getColumnHeaders()).toContain("Model");

      act(() => {
        useFleetStore.setState((state) => ({
          auth: {
            ...state.auth,
            username: "bob",
          },
        }));
      });

      await waitFor(() => {
        expect(screen.getByTestId("location-display").textContent).toBe("");
      });

      expect(getColumnHeaders()).not.toContain("Model");
    });

    it("resets column preferences back to the default layout", async () => {
      const user = userEvent.setup();

      useFleetStore.setState((state) => ({
        auth: {
          ...state.auth,
          username: "alice",
        },
      }));

      localStorage.setItem(
        getMinerTableColumnPreferencesStorageKey("alice"),
        JSON.stringify({
          columns: [
            { id: "groups", visible: true },
            { id: "model", visible: false },
          ],
        }),
      );

      renderMinerList({
        title: "Miners",
        minerIds: ["m1"],
        totalMiners: 1,
        onAddMiners: vi.fn(),
        loading: false,
      });

      expect(getColumnHeaders()).not.toContain("Model");

      await user.click(screen.getByRole("button", { name: "Manage columns" }));
      await user.click(screen.getByRole("button", { name: "Reset to defaults" }));
      await user.click(screen.getByRole("button", { name: "Save" }));

      expect(getColumnHeaders()).toContain("Model");
    });

    it("keeps the table usable when clearing persisted defaults fails", async () => {
      const user = userEvent.setup();

      useFleetStore.setState((state) => ({
        auth: {
          ...state.auth,
          username: "alice",
        },
      }));

      localStorage.setItem(
        getMinerTableColumnPreferencesStorageKey("alice"),
        JSON.stringify({
          columns: [
            { id: "groups", visible: true },
            { id: "model", visible: false },
          ],
        }),
      );

      renderMinerList({
        title: "Miners",
        minerIds: ["m1"],
        totalMiners: 1,
        onAddMiners: vi.fn(),
        loading: false,
      });

      await user.click(screen.getByRole("button", { name: "Manage columns" }));
      await user.click(screen.getByRole("button", { name: "Reset to defaults" }));

      const removeItemSpy = vi.spyOn(Storage.prototype, "removeItem").mockImplementation(() => {
        throw new Error("storage denied");
      });

      try {
        await user.click(screen.getByRole("button", { name: "Save" }));
      } finally {
        removeItemSpy.mockRestore();
      }

      expect(getColumnHeaders()).toContain("Model");
    });

    it("loads column preferences per user without leaking between accounts", async () => {
      localStorage.setItem(
        getMinerTableColumnPreferencesStorageKey("alice"),
        JSON.stringify({
          columns: [
            { id: "groups", visible: true },
            { id: "model", visible: false },
          ],
        }),
      );

      useFleetStore.setState((state) => ({
        auth: {
          ...state.auth,
          username: "alice",
        },
      }));

      renderMinerList({
        title: "Miners",
        minerIds: ["m1"],
        totalMiners: 1,
        onAddMiners: vi.fn(),
        loading: false,
      });

      expect(getColumnHeaders()).not.toContain("Model");

      act(() => {
        useFleetStore.setState((state) => ({
          auth: {
            ...state.auth,
            username: "bob",
          },
        }));
      });

      expect(getColumnHeaders()).toContain("Model");

      act(() => {
        useFleetStore.setState((state) => ({
          auth: {
            ...state.auth,
            username: "alice",
          },
        }));
      });

      expect(getColumnHeaders()).not.toContain("Model");
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
            miners={autoMiners(["m3", "m4"])}
            errorsByDevice={{}}
            errorsLoaded={true}
            getActiveBatches={mockGetActiveBatches}
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
            miners={autoMiners(["m1", "m2"])}
            errorsByDevice={{}}
            errorsLoaded={true}
            getActiveBatches={mockGetActiveBatches}
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

      const initialMiners = {
        m1: createMinerSnapshot("m1"),
        m2: createMinerSnapshot("m2"),
      };

      const { rerender } = renderMinerList({
        title: "Miners",
        minerIds: ["m1", "m2"],
        miners: initialMiners,
        totalMiners: 2,
        totalDisabledMiners: 0,
        currentPage: 0,
        onAddMiners: vi.fn(),
        loading: false,
      });

      const rowCheckboxes = screen.getAllByTestId("checkbox");
      await user.click(rowCheckboxes[0].querySelector("input[type='checkbox']") as HTMLInputElement);

      const updatedMiners = {
        ...initialMiners,
        m2: createMinerSnapshot("m2", PairingStatus.AUTHENTICATION_NEEDED),
      };

      await act(async () => {
        rerender(
          <BrowserRouter>
            <MinerList
              title="Miners"
              minerIds={["m1", "m2"]}
              miners={updatedMiners}
              errorsByDevice={{}}
              errorsLoaded={true}
              getActiveBatches={mockGetActiveBatches}
              totalMiners={2}
              totalDisabledMiners={0}
              currentPage={0}
              onAddMiners={vi.fn()}
              loading={false}
            />
          </BrowserRouter>,
        );
      });

      await user.click(screen.getByTestId("mock-action-bar-select-all"));

      expect(screen.getByTestId("mock-miner-list-selected-miners")).toHaveTextContent("m1");
    });
  });

  describe("row click navigation", () => {
    it("opens miner URL in a new tab when miner has a URL", async () => {
      const user = userEvent.setup();
      const openSpy = vi.spyOn(window, "open").mockImplementation(() => null);

      const snapshot = createMinerSnapshot("m1");
      snapshot.url = "https://192.168.1.100";

      renderMinerList({
        title: "Miners",
        minerIds: ["m1"],
        miners: { m1: snapshot },
        totalMiners: 1,
        onAddMiners: vi.fn(),
        loading: false,
      });

      const row = screen.getByTestId("list-row");
      await user.click(row);

      expect(openSpy).toHaveBeenCalledWith("https://192.168.1.100", "_blank", "noopener,noreferrer");
      openSpy.mockRestore();
    });

    it("does not open a new tab when miner has no URL", async () => {
      const user = userEvent.setup();
      const openSpy = vi.spyOn(window, "open").mockImplementation(() => null);

      renderMinerList({
        title: "Miners",
        minerIds: ["m1"],
        miners: { m1: createMinerSnapshot("m1") },
        totalMiners: 1,
        onAddMiners: vi.fn(),
        loading: false,
      });

      const row = screen.getByTestId("list-row");
      await user.click(row);

      expect(openSpy).not.toHaveBeenCalled();
      openSpy.mockRestore();
    });
  });

  describe("null state", () => {
    it("should show null state when no miners are paired", () => {
      const onAddMiners = vi.fn();

      renderMinerList({
        title: "Miners",
        minerIds: [],
        totalMiners: 0,
        totalUnfilteredMiners: 0,
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
        totalUnfilteredMiners: 0,
        onAddMiners,
      });

      await user.click(screen.getByRole("button", { name: "Get started" }));

      expect(onAddMiners).toHaveBeenCalledTimes(1);
    });

    it("falls back to totalMiners=0 when totalUnfilteredMiners is omitted", () => {
      // Callers that don't plumb totalUnfilteredMiners still get the null state
      // when totalMiners is 0 — preserves the prior contract on a shared component.
      renderMinerList({
        title: "Miners",
        minerIds: [],
        totalMiners: 0,
        onAddMiners: vi.fn(),
      });

      expect(screen.getByText("You haven't paired any miners")).toBeInTheDocument();
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
          <MinerList
            title="Miners"
            minerIds={[]}
            miners={{}}
            errorsByDevice={{}}
            errorsLoaded={true}
            getActiveBatches={mockGetActiveBatches}
            totalMiners={0}
            totalUnfilteredMiners={14}
            onAddMiners={vi.fn()}
          />
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

    it("shows filtered empty state when items are empty but totalMiners is non-zero", () => {
      render(
        <MemoryRouter initialEntries={["/?group=1"]}>
          <MinerList
            title="Miners"
            minerIds={[]}
            miners={{}}
            errorsByDevice={{}}
            errorsLoaded={true}
            getActiveBatches={mockGetActiveBatches}
            totalMiners={8}
            totalUnfilteredMiners={8}
            onAddMiners={vi.fn()}
          />
        </MemoryRouter>,
      );

      expect(screen.getByText("No results")).toBeInTheDocument();
      expect(screen.getByText("Try adjusting or clearing your filters.")).toBeInTheDocument();
      expect(screen.getByRole("button", { name: "Clear all filters" })).toBeInTheDocument();
      expect(screen.queryByText(/Showing/)).not.toBeInTheDocument();
    });

    it("clears group param along with other filters while preserving sort params", async () => {
      const user = userEvent.setup();

      render(
        <MemoryRouter initialEntries={["/?status=hashing&group=1,2&sort=name&dir=desc"]}>
          <MinerList
            title="Miners"
            minerIds={[]}
            miners={{}}
            errorsByDevice={{}}
            errorsLoaded={true}
            getActiveBatches={mockGetActiveBatches}
            totalMiners={0}
            totalUnfilteredMiners={14}
            onAddMiners={vi.fn()}
          />
          <LocationDisplay />
        </MemoryRouter>,
      );

      await user.click(screen.getByRole("button", { name: "Clear all filters" }));

      expect(screen.getByTestId("location-display")).toHaveTextContent("?sort=name&dir=desc");
    });
  });
});
