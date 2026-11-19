import { type ComponentProps, type ReactNode, useState } from "react";
import { MemoryRouter } from "react-router-dom";
import { fireEvent, render as renderUI } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { SortConfigSchema, SortField } from "@/protoFleet/api/generated/common/v1/sort_pb";
import { EntityKind, InventoryItemSchema } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import type { InventoryItem } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import {
  MinerListFilterSchema,
  MinerStateSnapshotSchema,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import MinerListActionBar from "@/protoFleet/features/fleetManagement/components/MinerList/MinerListActionBar";

const render = (ui: ReactNode) => renderUI(ui, { wrapper: ({ children }) => <MemoryRouter>{children}</MemoryRouter> });

const selections = vi.hoisted(() => ({ miners: vi.fn(), modules: vi.fn() }));
vi.mock("@/protoFleet/features/containers/MiningActionsMenu", () => ({
  default: function ModuleActionsMenuMock(props: {
    items: InventoryItem[];
    onActionStart?: () => void;
    onActionComplete?: () => void;
  }) {
    const [confirming, setConfirming] = useState(false);
    selections.modules(props);
    return (
      <div data-testid="module-actions-menu">
        <button
          onClick={() => {
            setConfirming(true);
            props.onActionStart?.();
          }}
        >
          Module actions
        </button>
        {confirming ? (
          <div role="dialog">
            {props.items.map((module) => module.name).join(", ")}
            <button
              onClick={() => {
                setConfirming(false);
                props.onActionComplete?.();
              }}
            >
              Cancel module action
            </button>
          </div>
        ) : null}
      </div>
    );
  },
}));

// useMinerActions imports batch operation hooks from the store that were removed
// during the fleet slice refactor. Mock the hook so tests don't crash.
// MinerActionsMenu imports hooks from the removed fleet store slice.
// Mock it so the tests don't crash.
vi.mock("@/protoFleet/features/fleetManagement/components/MinerActionsMenu", () => ({
  default: (props: {
    onActionStart?: () => void;
    onActionComplete?: () => void;
    selectedMiners: string[];
    selectionMode: string;
  }) => {
    const { onActionStart } = props;
    selections.miners(props);
    return (
      <div data-testid="actions-menu-button" onClick={onActionStart}>
        <button data-testid="reboot-popover-button" onClick={onActionStart}>
          Reboot
        </button>
        <button data-testid="mining-pool-popover-button" onClick={onActionStart}>
          Mining Pools
        </button>
        <button
          onClick={(event) => {
            event.stopPropagation();
            props.onActionComplete?.();
          }}
        >
          Complete miner action
        </button>
      </div>
    );
  },
}));

vi.mock("@/protoFleet/api/usePools", () => ({
  default: () => ({
    pools: [],
    validatePool: vi.fn(({ onSuccess }) => {
      onSuccess?.();
    }),
    validatePoolPending: false,
  }),
}));

describe("Miner list action bar", () => {
  const actionBarTestId = "action-bar";

  const actionBarProps = {
    selectedKeys: ["miner:MAC1" as const],
    selectionMode: "subset" as const,
  };

  test("hides action bar when mining pool action is triggered", () => {
    const { getByTestId } = render(<MinerListActionBar {...actionBarProps} />);

    const actionBarElement = getByTestId(actionBarTestId);
    expect(actionBarElement).toBeInTheDocument();
    const actionsMenuButton = getByTestId("actions-menu-button");
    fireEvent.click(actionsMenuButton);
    const miningPoolsButton = getByTestId("mining-pool-popover-button");
    fireEvent.click(miningPoolsButton);

    expect(actionBarElement.classList.contains("invisible")).toBe(true);
  });

  test("calls onClearSelection when action bar close button is clicked", () => {
    const onClearSelectionMock = vi.fn();
    const { getByTestId } = render(<MinerListActionBar {...actionBarProps} onClearSelection={onClearSelectionMock} />);

    const closeButton = getByTestId("close-button");
    fireEvent.click(closeButton);

    expect(onClearSelectionMock).toHaveBeenCalledOnce();
  });

  test("does not throw when onClearSelection is not provided", () => {
    const { getByTestId } = render(<MinerListActionBar {...actionBarProps} />);

    const closeButton = getByTestId("close-button");

    // Should not throw error when clicking close without onClearSelection prop
    expect(() => fireEvent.click(closeButton)).not.toThrow();
  });

  test("renders select all and select none controls", () => {
    const onSelectAll = vi.fn();
    const onSelectNone = vi.fn();

    const { getAllByTestId } = render(
      <MinerListActionBar {...actionBarProps} onSelectAll={onSelectAll} onSelectNone={onSelectNone} />,
    );

    fireEvent.click(getAllByTestId("select-all-miners-button")[0]);
    fireEvent.click(getAllByTestId("select-none-miners-button")[0]);

    expect(onSelectAll).toHaveBeenCalledTimes(1);
    expect(onSelectNone).toHaveBeenCalledTimes(1);
  });

  test("only renders selection controls that have handlers", () => {
    const onClearSelection = vi.fn();
    const { queryAllByTestId, rerender } = render(<MinerListActionBar {...actionBarProps} />);

    expect(queryAllByTestId("select-all-miners-button")).toHaveLength(0);
    expect(queryAllByTestId("select-none-miners-button")).toHaveLength(0);

    rerender(<MinerListActionBar {...actionBarProps} onClearSelection={onClearSelection} />);

    expect(queryAllByTestId("select-all-miners-button")).toHaveLength(0);
    expect(queryAllByTestId("select-none-miners-button")).toHaveLength(0);

    rerender(<MinerListActionBar {...actionBarProps} onSelectNone={onClearSelection} />);

    expect(queryAllByTestId("select-all-miners-button")).toHaveLength(0);
    expect(queryAllByTestId("select-none-miners-button")).toHaveLength(1);
  });
});

test("mixed selections keep modules out of conventional miner dispatch and prevent all-mode broadening", () => {
  selections.miners.mockClear();
  selections.modules.mockClear();
  const module = create(InventoryItemSchema, { ref: { kind: EntityKind.MODULE, id: 11n }, name: "CT1 T1 M2" });
  const { rerender, queryByTestId, getByTestId } = render(
    <MinerListActionBar
      selectedKeys={["miner:miner-a", "module:11"]}
      modules={[module]}
      selectionMode="all"
      totalCount={40}
    />,
  );
  expect(selections.miners).toHaveBeenLastCalledWith(
    expect.objectContaining({ selectedMiners: ["miner-a"], selectionMode: "subset", totalCount: 1 }),
  );
  expect(selections.modules).toHaveBeenLastCalledWith(expect.objectContaining({ items: [module] }));
  expect(getByTestId("action-bar")).toHaveTextContent("2 mining assets selected");
  expect(getByTestId("action-bar")).not.toHaveTextContent("40");
  selections.miners.mockClear();
  rerender(<MinerListActionBar selectedKeys={["module:11"]} modules={[module]} selectionMode="subset" />);
  expect(getByTestId("action-bar")).toHaveTextContent("1 module selected");
  expect(queryByTestId("actions-menu-button")).not.toBeInTheDocument();
  expect(selections.miners).not.toHaveBeenCalled();
});

test("miner-only all mode retains the full result count and dispatch scope", () => {
  const { getByTestId } = render(
    <MinerListActionBar selectedKeys={["miner:miner-a"]} selectionMode="all" totalCount={200} />,
  );
  expect(getByTestId("action-bar")).toHaveTextContent("All 200 miners selected");
  expect(selections.miners).toHaveBeenLastCalledWith(
    expect.objectContaining({ selectedMiners: ["miner-a"], selectionMode: "all", totalCount: 200 }),
  );
});

test.each(["none", "subset"] as const)(
  "preserves the complete miner dispatch context while a scope change resets selection to %s",
  (selectionMode) => {
    const original = {
      selectedKeys: ["miner:miner-a"],
      selectionMode: "all",
      totalCount: 200,
      filtersActive: true,
      currentFilter: create(MinerListFilterSchema, { models: ["S21"] }),
      currentSort: create(SortConfigSchema, { field: SortField.NAME }),
      miners: { "miner-a": create(MinerStateSnapshotSchema, { model: "S21" }) },
      minerIds: ["miner-a"],
      selectionIncludesUnauthenticatedMiner: true,
    } satisfies ComponentProps<typeof MinerListActionBar>;
    const changed = {
      selectedKeys: selectionMode === "none" ? [] : ["miner:miner-b"],
      selectionMode,
      totalCount: 50,
      selectAllOnPage: true,
      filtersActive: false,
      currentFilter: create(MinerListFilterSchema, { models: ["M60"] }),
      currentSort: create(SortConfigSchema, { field: SortField.MODEL }),
      miners: { "miner-b": create(MinerStateSnapshotSchema, { model: "M60" }) },
      minerIds: ["miner-b"],
      selectionIncludesUnauthenticatedMiner: false,
    } satisfies ComponentProps<typeof MinerListActionBar>;
    const { getByTestId, getByRole, queryByTestId, rerender } = render(<MinerListActionBar {...original} />);
    fireEvent.click(getByTestId("reboot-popover-button"));

    rerender(<MinerListActionBar {...changed} />);
    expect(getByTestId("action-bar")).toHaveClass("invisible");
    expect(selections.miners).toHaveBeenLastCalledWith(
      expect.objectContaining({
        selectedMiners: ["miner-a"],
        selectionMode: "all",
        totalCount: original.totalCount,
        currentFilter: original.currentFilter,
        currentSort: original.currentSort,
        miners: original.miners,
        minerIds: original.minerIds,
        selectionIncludesUnauthenticatedMiner: true,
      }),
    );

    fireEvent.click(getByRole("button", { name: "Complete miner action" }));
    if (selectionMode === "none") {
      expect(queryByTestId("action-bar")).not.toBeInTheDocument();
    } else {
      expect(getByTestId("action-bar")).not.toHaveClass("invisible");
      expect(selections.miners).toHaveBeenLastCalledWith(
        expect.objectContaining({
          selectedMiners: ["miner-b"],
          selectionMode: "subset",
          totalCount: 1,
          currentFilter: changed.currentFilter,
          currentSort: changed.currentSort,
          miners: changed.miners,
          minerIds: changed.minerIds,
          selectionIncludesUnauthenticatedMiner: false,
        }),
      );
    }
  },
);

test.each([false, true])(
  "keeps module confirmation mounted when polling removes modules (clear all: %s)",
  (clearAll) => {
    const module = create(InventoryItemSchema, { ref: { kind: EntityKind.MODULE, id: 11n }, name: "CT1 T1 M2" });
    const { getByRole, queryByRole, getByTestId, queryByTestId, rerender } = render(
      <MinerListActionBar selectedKeys={["miner:miner-a", "module:11"]} modules={[module]} selectionMode="subset" />,
    );
    fireEvent.click(getByRole("button", { name: "Module actions" }));
    expect(getByTestId("action-bar")).toHaveClass("invisible");

    rerender(
      <MinerListActionBar selectedKeys={clearAll ? [] : ["miner:miner-a"]} modules={[]} selectionMode="subset" />,
    );
    expect(getByRole("dialog")).toHaveTextContent("CT1 T1 M2");
    fireEvent.click(getByRole("button", { name: "Cancel module action" }));
    expect(queryByRole("dialog")).not.toBeInTheDocument();
    expect(queryByTestId("module-actions-menu")).not.toBeInTheDocument();
    if (clearAll) {
      expect(queryByTestId("action-bar")).not.toBeInTheDocument();
    } else {
      expect(getByTestId("action-bar")).not.toHaveClass("invisible");
      expect(getByTestId("action-bar")).toHaveTextContent("1 miner selected");
    }

    rerender(<MinerListActionBar selectedKeys={["miner:miner-b"]} selectionMode="subset" />);
    expect(getByTestId("action-bar")).not.toHaveClass("invisible");
  },
);

test.each(["module:11", "miner:module:11", "module:"])(
  "dispatches the original miner identifier %s without treating it as a module key",
  (identifier) => {
    const module = create(InventoryItemSchema, { ref: { kind: EntityKind.MODULE, id: 11n }, name: "CT1 T1 M2" });
    const { getByTestId, queryByTestId, rerender } = render(
      <MinerListActionBar
        selectedKeys={[`miner:${identifier}`, "module:11"]}
        modules={[module]}
        selectionMode="subset"
      />,
    );
    expect(selections.miners).toHaveBeenLastCalledWith(
      expect.objectContaining({ selectedMiners: [identifier], totalCount: 1 }),
    );
    expect(selections.modules).toHaveBeenLastCalledWith(expect.objectContaining({ items: [module] }));
    expect(getByTestId("action-bar")).toHaveTextContent("2 mining assets selected");

    rerender(<MinerListActionBar selectedKeys={[`miner:${identifier}`]} selectionMode="subset" />);
    expect(queryByTestId("module-actions-menu")).not.toBeInTheDocument();
    expect(selections.miners).toHaveBeenLastCalledWith(expect.objectContaining({ selectedMiners: [identifier] }));
  },
);

test("module pages identify the select-all control as page scoped", () => {
  const { getByTestId } = render(
    <MinerListActionBar
      selectedKeys={["miner:miner-a"]}
      selectionMode="subset"
      selectAllOnPage
      onSelectAll={vi.fn()}
    />,
  );
  expect(getByTestId("select-all-miners-button")).toHaveTextContent("Select all visible");
});
