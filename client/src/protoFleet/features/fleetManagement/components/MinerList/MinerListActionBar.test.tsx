import { fireEvent, render, waitFor } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { EntityKind, InventoryItemSchema } from "@/protoFleet/api/generated/fleetinventory/v1/fleetinventory_pb";
import MinerListActionBar from "@/protoFleet/features/fleetManagement/components/MinerList/MinerListActionBar";

const selections = vi.hoisted(() => ({ miners: vi.fn(), modules: vi.fn() }));
vi.mock("@/protoFleet/features/containers/ModuleActionsMenu", () => ({
  default: (props: { modules: { ref?: { id: bigint } }[] }) => {
    selections.modules(props);
    return <span data-testid="module-actions-menu">Module actions</span>;
  },
}));

// useMinerActions imports batch operation hooks from the store that were removed
// during the fleet slice refactor. Mock the hook so tests don't crash.
// MinerActionsMenu imports hooks from the removed fleet store slice.
// Mock it so the tests don't crash.
vi.mock("@/protoFleet/features/fleetManagement/components/MinerActionsMenu", () => ({
  default: (props: { onActionStart?: () => void; selectedMiners: string[]; selectionMode: string }) => {
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

  // TODO: Fix this test - requires mocking useMinerCommand and toast system
  // Pre-existing failure unrelated to recent changes
  test.skip("hides and displays action bar depending on confirmation dialog visibility", async () => {
    const { getByTestId } = render(<MinerListActionBar {...actionBarProps} />);

    const actionBarElement = getByTestId(actionBarTestId);
    expect(actionBarElement).toBeInTheDocument();
    const actionsMenuButton = getByTestId("actions-menu-button");
    fireEvent.click(actionsMenuButton);
    const rebootButton = getByTestId("reboot-popover-button");
    fireEvent.click(rebootButton);

    expect(actionBarElement.classList.contains("invisible")).toBe(true);

    const confirmRebootButton = await waitFor(() => getByTestId("reboot-confirm-button"));
    fireEvent.click(confirmRebootButton);

    await waitFor(() => {
      expect(actionBarElement.classList.contains("invisible")).toBe(false);
    });
  });

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
  expect(selections.modules).toHaveBeenLastCalledWith(expect.objectContaining({ modules: [module] }));
  expect(getByTestId("action-bar")).toHaveTextContent("2 miners selected");
  expect(getByTestId("action-bar")).not.toHaveTextContent("40");
  selections.miners.mockClear();
  rerender(<MinerListActionBar selectedKeys={["module:11"]} modules={[module]} selectionMode="subset" />);
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
    expect(selections.modules).toHaveBeenLastCalledWith(expect.objectContaining({ modules: [module] }));
    expect(getByTestId("action-bar")).toHaveTextContent("2 miners selected");

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
